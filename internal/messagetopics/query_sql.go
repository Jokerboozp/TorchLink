package messagetopics

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"iot-platform/internal/model"
)

type queryToken struct{ kind, text string }
type querySQLParser struct {
	tokens     []queryToken
	pos, depth int
}

func tokenizeQuerySQL(sql string) ([]queryToken, error) {
	if len(sql) > 16<<10 || !utf8.ValidString(sql) {
		return nil, errors.New("查询SQL最多16KB且须为有效文本")
	}
	var result []queryToken
	for i := 0; i < len(sql); {
		r, size := utf8.DecodeRuneInString(sql[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		start := i
		switch {
		case r == '"':
			i++
			var value strings.Builder
			closed := false
			for i < len(sql) {
				if sql[i] == '"' {
					if i+1 < len(sql) && sql[i+1] == '"' {
						value.WriteByte('"')
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				value.WriteByte(sql[i])
				i++
			}
			if !closed {
				return nil, errors.New("SQL字段双引号未闭合")
			}
			result = append(result, queryToken{"quotedIdentifier", value.String()})
		case r == '\'':
			i++
			var value strings.Builder
			closed := false
			for i < len(sql) {
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						value.WriteByte('\'')
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				value.WriteByte(sql[i])
				i++
			}
			if !closed {
				return nil, errors.New("SQL字符串引号未闭合")
			}
			result = append(result, queryToken{"string", value.String()})
		case unicode.IsLetter(r) || r == '_':
			i += size
			for i < len(sql) {
				next, n := utf8.DecodeRuneInString(sql[i:])
				if !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' && next != '.' {
					break
				}
				i += n
			}
			result = append(result, queryToken{"identifier", sql[start:i]})
		case r >= '0' && r <= '9' || r == '-' && i+1 < len(sql) && sql[i+1] >= '0' && sql[i+1] <= '9':
			i += size
			for i < len(sql) && strings.ContainsRune("0123456789.eE+-", rune(sql[i])) {
				i++
			}
			value := sql[start:i]
			if !json.Valid([]byte(value)) {
				return nil, errors.New("SQL数字格式无效")
			}
			if _, ok := queryNumber(json.Number(value)); !ok {
				return nil, errors.New("SQL数字超出范围")
			}
			result = append(result, queryToken{"number", value})
		case strings.ContainsRune("(),*=<>!;", r):
			i += size
			if i < len(sql) && ((r == '<' && (sql[i] == '=' || sql[i] == '>')) || (r == '>' || r == '!') && sql[i] == '=') {
				i++
			}
			result = append(result, queryToken{"symbol", sql[start:i]})
		default:
			return nil, fmt.Errorf("SQL不支持字符 %q", r)
		}
		if len(result) > 2048 {
			return nil, errors.New("SQL查询过于复杂")
		}
	}
	return result, nil
}
func (p *querySQLParser) peek() queryToken {
	if p.pos >= len(p.tokens) {
		return queryToken{}
	}
	return p.tokens[p.pos]
}
func (p *querySQLParser) accept(text string) bool {
	t := p.peek()
	if (t.kind == "identifier" || t.kind == "symbol") && strings.EqualFold(t.text, text) {
		p.pos++
		return true
	}
	return false
}
func (p *querySQLParser) expect(text string) error {
	if !p.accept(text) {
		return fmt.Errorf("SQL此处需要 %s", text)
	}
	return nil
}
func (p *querySQLParser) identifier() (string, error) {
	t := p.peek()
	if t.kind != "identifier" && t.kind != "quotedIdentifier" {
		return "", errors.New("SQL此处需要业务字段或数据名称")
	}
	p.pos++
	return t.text, nil
}
func (p *querySQLParser) value() (any, error) {
	t := p.peek()
	p.pos++
	switch t.kind {
	case "string":
		return t.text, nil
	case "number":
		return json.Number(t.text), nil
	case "identifier":
		switch strings.ToUpper(t.text) {
		case "TRUE":
			return true, nil
		case "FALSE":
			return false, nil
		}
	}
	return nil, errors.New("SQL条件只支持文本、数字或布尔常量；空值请使用IS NULL")
}
func (p *querySQLParser) expression() (*model.MessageTopicFilter, error) {
	left, err := p.andExpression()
	if err != nil {
		return nil, err
	}
	for p.accept("OR") {
		right, err := p.andExpression()
		if err != nil {
			return nil, err
		}
		left = combineQueryFilters("or", left, right)
	}
	return left, nil
}
func (p *querySQLParser) andExpression() (*model.MessageTopicFilter, error) {
	left, err := p.condition()
	if err != nil {
		return nil, err
	}
	for p.accept("AND") {
		right, err := p.condition()
		if err != nil {
			return nil, err
		}
		left = combineQueryFilters("and", left, right)
	}
	return left, nil
}
func combineQueryFilters(logic string, a, b *model.MessageTopicFilter) *model.MessageTopicFilter {
	if a.Logic == logic {
		a.Children = append(a.Children, *b)
		return a
	}
	return &model.MessageTopicFilter{Logic: logic, Children: []model.MessageTopicFilter{*a, *b}}
}
func (p *querySQLParser) condition() (*model.MessageTopicFilter, error) {
	if p.accept("(") {
		p.depth++
		if p.depth > 8 {
			return nil, errors.New("SQL条件最多8层括号")
		}
		f, err := p.expression()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		p.depth--
		return f, nil
	}
	field, err := p.identifier()
	if err != nil {
		return nil, err
	}
	f := &model.MessageTopicFilter{Field: field}
	if p.accept("IS") {
		f.Operator = "is_null"
		if p.accept("NOT") {
			f.Operator = "not_null"
		}
		if err := p.expect("NULL"); err != nil {
			return nil, err
		}
		return f, nil
	}
	negate := p.accept("NOT")
	if p.accept("IN") {
		f.Operator = "in"
		if negate {
			f.Operator = "not_in"
		}
		if err := p.expect("("); err != nil {
			return nil, err
		}
		var values []any
		for {
			value, err := p.value()
			if err != nil {
				return nil, err
			}
			values = append(values, value)
			if len(values) > 100 {
				return nil, errors.New("IN最多100个值")
			}
			if !p.accept(",") {
				break
			}
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		f.Value = values
		return f, nil
	}
	if negate {
		return nil, errors.New("NOT仅支持NOT IN和IS NOT NULL")
	}
	op := p.peek()
	p.pos++
	if op.kind != "symbol" && !(op.kind == "identifier" && strings.EqualFold(op.text, "CONTAINS")) {
		return nil, errors.New("SQL比较运算符无效")
	}
	switch strings.ToUpper(op.text) {
	case "=":
		f.Operator = "eq"
	case "!=", "<>":
		f.Operator = "ne"
	case ">":
		f.Operator = "gt"
	case ">=":
		f.Operator = "gte"
	case "<":
		f.Operator = "lt"
	case "<=":
		f.Operator = "lte"
	case "CONTAINS":
		f.Operator = "contains"
	default:
		return nil, errors.New("SQL比较运算符无效")
	}
	f.Value, err = p.value()
	return f, err
}

// CompileQuerySQL parses a bounded SELECT language over named business datasets.
// The result is evaluated in Go; this text is never passed to a database driver.
func CompileQuerySQL(sql string) (model.MessageTopicQuery, error) {
	q := model.MessageTopicQuery{DeviceScope: "all", Mode: "realtime"}
	tokens, err := tokenizeQuerySQL(sql)
	if err != nil {
		return q, err
	}
	p := querySQLParser{tokens: tokens}
	if err := p.expect("SELECT"); err != nil {
		return q, err
	}
	if !p.accept("*") {
		q.Fields = map[string]string{}
		for {
			path, err := p.identifier()
			if err != nil {
				return q, err
			}
			parts := strings.Split(path, ".")
			alias := parts[len(parts)-1]
			if p.accept("AS") {
				alias, err = p.identifier()
				if err != nil {
					return q, err
				}
			}
			if _, exists := q.Fields[alias]; exists {
				return q, fmt.Errorf("重复输出字段 %s，请设置不同别名", alias)
			}
			q.Fields[alias] = path
			if len(q.Fields) > 64 {
				return q, errors.New("最多返回64个字段")
			}
			if !p.accept(",") {
				break
			}
		}
	}
	if err := p.expect("FROM"); err != nil {
		return q, err
	}
	q.Dataset, err = p.identifier()
	if err != nil {
		return q, err
	}
	if p.accept("WHERE") {
		q.Filter, err = p.expression()
		if err != nil {
			return q, err
		}
	}
	p.accept(";")
	if p.pos != len(p.tokens) {
		return q, errors.New("仅支持SELECT字段、FROM业务数据和WHERE条件，不支持函数、连接、排序或其他语句")
	}
	if d, ok := queryDataset(q.Dataset); ok {
		q.Mode = d.Mode
		if d.Mode == "interval" {
			q.IntervalSeconds = 60
		}
	}
	return q, ValidateQuery(q)
}
func querySQLValue(value any) string {
	switch v := value.(type) {
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "NULL"
	}
	return string(data)
}
func queryFilterSQL(f *model.MessageTopicFilter) string {
	if f.Logic != "" {
		items := make([]string, len(f.Children))
		for i := range f.Children {
			items[i] = queryFilterSQL(&f.Children[i])
		}
		return "(" + strings.Join(items, " "+strings.ToUpper(f.Logic)+" ") + ")"
	}
	switch f.Operator {
	case "is_null":
		return querySQLIdentifier(f.Field) + " IS NULL"
	case "not_null":
		return querySQLIdentifier(f.Field) + " IS NOT NULL"
	case "in", "not_in":
		values, _ := queryList(f.Value)
		items := make([]string, len(values))
		for i, v := range values {
			items[i] = querySQLValue(v)
		}
		op := " IN "
		if f.Operator == "not_in" {
			op = " NOT IN "
		}
		return querySQLIdentifier(f.Field) + op + "(" + strings.Join(items, ", ") + ")"
	}
	op := map[string]string{"eq": "=", "ne": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "contains": "CONTAINS"}[f.Operator]
	return querySQLIdentifier(f.Field) + " " + op + " " + querySQLValue(f.Value)
}

var querySQLPlainPath = regexp.MustCompile(`^[\pL_][\pL\pN_]*(?:\.[\pL\pN_]+)*$`)

func querySQLIdentifier(path string) string {
	if querySQLPlainPath.MatchString(path) {
		return path
	}
	return `"` + strings.ReplaceAll(path, `"`, `""`) + `"`
}
func QuerySQL(q model.MessageTopicQuery) string {
	if ValidateQuery(q) != nil {
		return ""
	}
	return querySQLUnchecked(q)
}
func querySQLUnchecked(q model.MessageTopicQuery) string {
	fields := "*"
	if len(q.Fields) > 0 {
		keys := make([]string, 0, len(q.Fields))
		for key := range q.Fields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		items := make([]string, 0, len(keys))
		for _, alias := range keys {
			original := q.Fields[alias]
			path := querySQLIdentifier(original)
			parts := strings.Split(original, ".")
			if parts[len(parts)-1] != alias {
				path += " AS " + querySQLIdentifier(alias)
			}
			items = append(items, path)
		}
		fields = strings.Join(items, ", ")
	}
	sql := "SELECT " + fields + " FROM " + q.Dataset
	if q.Filter != nil {
		sql += " WHERE " + queryFilterSQL(q.Filter)
	}
	return sql
}
