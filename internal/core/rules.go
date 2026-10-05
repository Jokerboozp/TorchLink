package core

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bilibili/gengine/builder"
	gcontext "github.com/bilibili/gengine/context"
	"github.com/bilibili/gengine/engine"
	"iot-platform/internal/model"
)

// ruleCovers reports whether a rule's tenant and product scope includes the
// message. Disabled rules stay covered so their open alarms can still recover.
func ruleCovers(rule model.AlarmRule, msg model.StandardMessage) bool {
	return (rule.TenantID == "" || rule.TenantID == msg.TenantID) && (rule.ProductID == "" || rule.ProductID == msg.ProductID)
}

func MatchRule(rule model.AlarmRule, msg model.StandardMessage) bool {
	if !rule.Enabled || !ruleCovers(rule, msg) {
		return false
	}
	if strings.TrimSpace(rule.Expression) != "" {
		matched, err := EvaluateGengineExpression(rule.Expression, msg)
		return err == nil && matched
	}
	if len(rule.Conditions) == 0 {
		return false
	}
	all := !strings.EqualFold(rule.Match, "any")
	for _, c := range rule.Conditions {
		v, ok := fieldValue(msg, c.Field)
		matched := ok && compare(v, c.Operator, c.Value)
		if all && !matched {
			return false
		}
		if !all && matched {
			return true
		}
	}
	return all
}

func ValidateGengineExpression(expression string) error {
	_, err := evaluateGengine(expression, model.StandardMessage{Properties: map[string]any{"temperature": 0.0}, Tags: map[string]string{}, Event: map[string]any{}}, false)
	return err
}

func EvaluateGengineExpression(expression string, msg model.StandardMessage) (bool, error) {
	return evaluateGengine(expression, msg, true)
}

func evaluateGengine(expression string, msg model.StandardMessage, execute bool) (bool, error) {
	compiled, err := compileExpression(expression)
	if err != nil {
		return false, err
	}
	if !execute {
		return false, nil
	}
	matched := false
	dc := gcontext.NewDataContext()
	for _, binding := range compiled.bindings {
		var value any = float64(0)
		var source map[string]any
		switch binding.kind {
		case "Properties":
			source = msg.Properties
		case "Event":
			source = msg.Event
		case "Tags":
			if v, ok := msg.Tags[binding.field]; ok {
				value = v
			}
		}
		if v, ok := source[binding.field]; ok {
			value = v
		}
		dc.Add(binding.name, value)
	}
	dc.Add("Message", msg)
	dc.Add("MarkMatched", func() { matched = true })
	dc.Add("Contains", func(value, target any) bool {
		return strings.Contains(fmt.Sprint(value), fmt.Sprint(target))
	})
	dc.Add("Exists", func(field string) bool {
		_, ok := fieldValue(msg, field)
		return ok
	})
	// The compiled rule tree is read-only during execution and shared; the
	// data context and engine carry the per-message state.
	if err := engine.NewGengine().Execute(&builder.RuleBuilder{Kc: compiled.compiled.Kc, Dc: dc}, true); err != nil {
		return false, fmt.Errorf("execute gengine expression: %w", err)
	}
	return matched, nil
}

// compiledExpression is an expression parsed once: the rewritten rule text
// depends only on the expression, so every message reuses the rule tree and
// binds its own field values.
type compiledExpression struct {
	bindings []expressionBinding
	compiled *builder.RuleBuilder
	err      error
}

type expressionBinding struct{ name, kind, field string }

// maxCompiledExpressions bounds the cache; rule edits create new keys and the
// cache is reset when full rather than tracking per-entry use.
const maxCompiledExpressions = 4096

var compiledExpressions = struct {
	sync.RWMutex
	items map[string]*expressionCacheEntry
}{items: map[string]*expressionCacheEntry{}}

// expressionCompiles counts parses so tests can verify cache reuse.
var expressionCompiles atomic.Int64

// compileExpression returns the cached compilation of expression. Invalid
// expressions are cached too so a broken rule is not parsed per message, and
// concurrent first uses wait for one parse.
func compileExpression(expression string) (*compiledExpression, error) {
	expression = strings.TrimSpace(expression)
	compiledExpressions.RLock()
	entry, ok := compiledExpressions.items[expression]
	compiledExpressions.RUnlock()
	if !ok {
		compiledExpressions.Lock()
		if entry, ok = compiledExpressions.items[expression]; !ok {
			if len(compiledExpressions.items) >= maxCompiledExpressions {
				compiledExpressions.items = map[string]*expressionCacheEntry{}
			}
			entry = &expressionCacheEntry{}
			compiledExpressions.items[expression] = entry
		}
		compiledExpressions.Unlock()
	}
	entry.once.Do(func() { entry.value = buildExpression(expression) })
	return entry.value, entry.value.err
}

type expressionCacheEntry struct {
	once  sync.Once
	value *compiledExpression
}

func buildExpression(expression string) *compiledExpression {
	if expression == "" {
		return &compiledExpression{err: fmt.Errorf("expression is empty")}
	}
	if len(expression) > 4096 {
		return &compiledExpression{err: fmt.Errorf("expression exceeds 4096 bytes")}
	}
	rewritten, bindings, err := rewriteExpressionFields(expression)
	if err != nil {
		return &compiledExpression{err: err}
	}
	expressionCompiles.Add(1)
	rb := builder.NewRuleBuilder(gcontext.NewDataContext())
	ruleText := "rule \"iot_expression\" \"controlled expression\"\nbegin\nif " + rewritten + " { MarkMatched() }\nend"
	if err := rb.BuildRuleFromString(ruleText); err != nil {
		return &compiledExpression{err: fmt.Errorf("compile gengine expression: %w", err)}
	}
	return &compiledExpression{bindings: bindings, compiled: rb}
}

var expressionField = regexp.MustCompile(`^(Properties|Tags|Event)\s*\[\s*(?:"([A-Za-z0-9_.-]+)"|'([A-Za-z0-9_.-]+)')\s*\]`)

// Scan strings before identifiers so quoted business data is neither rejected
// as a keyword nor rewritten as a field reference. Bind full field identities
// to unique names; punctuation normalization would alias a-b, a_b and a.b.
func rewriteExpressionFields(expression string) (string, []expressionBinding, error) {
	bound := map[string]string{}
	bindings := []expressionBinding{}
	var out strings.Builder
	for i := 0; i < len(expression); {
		ch := expression[i]
		if ch == '"' || ch == '\'' {
			end := i + 1
			closed := false
			for end < len(expression) {
				if expression[end] == '\\' {
					end += 2
					continue
				}
				if expression[end] == ch {
					// Gengine also accepts doubled double quotes in a string.
					if ch == '"' && end+1 < len(expression) && expression[end+1] == '"' {
						end += 2
						continue
					}
					end++
					closed = true
					break
				}
				end++
			}
			if !closed {
				return "", nil, fmt.Errorf("unterminated expression string")
			}
			out.WriteString(expression[i:end])
			i = end
			continue
		}
		if strings.ContainsRune("{};", rune(ch)) {
			return "", nil, fmt.Errorf("expression contains forbidden token %q", string(ch))
		}
		if !expressionIdentifier(ch) || ch >= '0' && ch <= '9' {
			out.WriteByte(ch)
			i++
			continue
		}
		parts := expressionField.FindStringSubmatch(expression[i:])
		if parts == nil {
			end := i + 1
			for end < len(expression) && expressionIdentifier(expression[end]) {
				end++
			}
			token := expression[i:end]
			switch strings.ToLower(token) {
			case "rule", "begin", "end", "import", "exec", "system":
				return "", nil, fmt.Errorf("expression contains forbidden token %q", token)
			}
			out.WriteString(token)
			i = end
			continue
		}
		field := parts[2]
		if field == "" {
			field = parts[3]
		}
		key := parts[1] + "\x00" + field
		name, exists := bound[key]
		if !exists {
			name = "iotField" + strconv.Itoa(len(bound))
			bound[key] = name
			bindings = append(bindings, expressionBinding{name: name, kind: parts[1], field: field})
		}
		out.WriteString(name)
		i += len(parts[0])
	}
	return out.String(), bindings, nil
}

func expressionIdentifier(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_'
}
func MatchConditions(conditions []model.RuleCondition, msg model.StandardMessage) bool {
	if len(conditions) == 0 {
		return false
	}
	for _, c := range conditions {
		v, ok := fieldValue(msg, c.Field)
		if !ok || !compare(v, c.Operator, c.Value) {
			return false
		}
	}
	return true
}
func fieldValue(msg model.StandardMessage, path string) (any, bool) {
	if strings.HasPrefix(path, "properties.") {
		v, ok := msg.Properties[strings.TrimPrefix(path, "properties.")]
		return v, ok
	}
	if strings.HasPrefix(path, "tags.") {
		v, ok := msg.Tags[strings.TrimPrefix(path, "tags.")]
		return v, ok
	}
	if strings.HasPrefix(path, "event.") {
		v, ok := msg.Event[strings.TrimPrefix(path, "event.")]
		return v, ok
	}
	if v, ok := msg.Properties[path]; ok {
		return v, true
	}
	if v, ok := msg.Tags[path]; ok {
		return v, true
	}
	if v, ok := msg.Event[path]; ok {
		return v, true
	}
	return nil, false
}
func compare(a any, op string, b any) bool {
	op = strings.ToLower(strings.TrimSpace(op))
	if op == "exists" {
		return a != nil
	}
	if op == "contains" {
		return strings.Contains(fmt.Sprint(a), fmt.Sprint(b))
	}
	if op == "in" {
		rv := reflect.ValueOf(b)
		if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				if compare(a, "eq", rv.Index(i).Interface()) {
					return true
				}
			}
		}
		return false
	}
	af, aok := asFloat(a)
	bf, bok := asFloat(b)
	if aok && bok {
		switch op {
		case ">", "gt":
			return af > bf
		case ">=", "gte":
			return af >= bf
		case "<", "lt":
			return af < bf
		case "<=", "lte":
			return af <= bf
		case "!=", "ne":
			return af != bf
		default:
			return af == bf
		}
	}
	as := strings.ToLower(fmt.Sprint(a))
	bs := strings.ToLower(fmt.Sprint(b))
	switch op {
	case "!=", "ne":
		return as != bs
	default:
		return as == bs
	}
}
func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		n, e := strconv.ParseFloat(x, 64)
		return n, e == nil
	}
	return 0, false
}
