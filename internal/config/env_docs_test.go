package config

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	envCommentsFile   = "../../scripts/lib/env-comments.tsv"
	deploymentDocFile = "../../docs/DEPLOYMENT.md"
	referenceStart    = "<!-- config-reference:start -->"
	referenceEnd      = "<!-- config-reference:end -->"
)

// envComments reads the variable descriptions the deployment scripts write
// above each line of a generated environment file, in file order.
func envComments(t *testing.T) ([]string, map[string]string) {
	t.Helper()
	f, err := os.Open(envCommentsFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var names []string
	descriptions := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		name, description, ok := strings.Cut(scanner.Text(), "\t")
		if !ok || name == "" || description == "" {
			t.Fatalf("malformed line in %s: %q", envCommentsFile, scanner.Text())
		}
		if _, dup := descriptions[name]; dup {
			t.Fatalf("%s is described twice in %s", name, envCommentsFile)
		}
		names = append(names, name)
		descriptions[name] = description
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return names, descriptions
}

// Every variable the configuration reads is described for operators, so a
// new setting cannot ship without its explanation in generated env files and
// in the deployment guide.
func TestEveryConfigVariableIsDescribed(t *testing.T) {
	_, descriptions := envComments(t)
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	variable := regexp.MustCompile(`^IOT_[A-Z0-9_]+$`)
	read := map[string]bool{}
	fset := token.NewFileSet()
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if value, err := strconv.Unquote(lit.Value); err == nil && variable.MatchString(value) {
					read[value] = true
				}
			}
			return true
		})
	}
	if len(read) < 100 {
		t.Fatalf("found only %d configuration variables; the scan is broken", len(read))
	}
	var missing []string
	for name := range read {
		if descriptions[name] == "" {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		t.Fatalf("describe these variables in scripts/lib/env-comments.tsv: %s", strings.Join(missing, ", "))
	}
}

// configGroups orders the reference in the deployment guide by the second
// word of each variable (IOT_<WORD>_…, or <WORD>_… for service settings).
var configGroups = []struct {
	title string
	words []string
}{
	{"平台进程与访问", []string{"PROCESS", "INSTANCE", "CLUSTER", "API", "NODE", "ACCESS", "DATA", "HTTP", "DEV", "PLATFORM", "PUBLIC", "WEB", "CORS", "TLS", "JWT", "ADMIN", "METRICS", "CAPACITY"}},
	{"接收与处理", []string{"INGEST", "CONSUMER", "OFFLINE", "PUBLISH", "DEVICE", "RAW", "PROTOCOL", "MODBUS", "EXTERNAL"}},
	{"存储", []string{"POSTGRES", "CLICKHOUSE", "REDIS", "MINIO"}},
	{"消息", []string{"KAFKA", "REDPANDA", "MQTT", "MQTTS", "EMQX"}},
	{"AI 与知识库", []string{"AI", "HARNESS", "DEEPSEEK", "EMBEDDING", "RERANK", "LOCAL", "LLAMA", "HF"}},
	{"摄像头直播", []string{"VIDEO", "GB28181", "ZLMEDIAKIT"}},
	{"运维中心与日志", []string{"OPS", "LOG", "GRAFANA", "PROMETHEUS"}},
	{"备份与数据保留", []string{"BACKUP", "RETENTION"}},
	{"告警通知", []string{"NOTIFY"}},
}

func configGroup(name string) string {
	word, _, _ := strings.Cut(strings.TrimPrefix(name, "IOT_"), "_")
	for _, g := range configGroups {
		if slices.Contains(g.words, word) {
			return g.title
		}
	}
	return "其他"
}

func renderConfigReference(names []string, descriptions map[string]string) string {
	byGroup := map[string][]string{}
	for _, name := range names {
		group := configGroup(name)
		byGroup[group] = append(byGroup[group], name)
	}
	var b strings.Builder
	b.WriteString(referenceStart + "\n")
	titles := make([]string, 0, len(configGroups)+1)
	for _, g := range configGroups {
		titles = append(titles, g.title)
	}
	for _, title := range append(titles, "其他") {
		rows := byGroup[title]
		if len(rows) == 0 {
			continue
		}
		slices.Sort(rows)
		b.WriteString("\n#### " + title + "\n\n| 变量 | 说明 |\n| --- | --- |\n")
		for _, name := range rows {
			b.WriteString("| `" + name + "` | " + descriptions[name] + " |\n")
		}
	}
	b.WriteString("\n" + referenceEnd)
	return b.String()
}

// The configuration reference in docs/DEPLOYMENT.md is generated from
// scripts/lib/env-comments.tsv; regenerate it with IOT_UPDATE_CONFIG_DOCS=1.
func TestDeploymentConfigReferenceIsCurrent(t *testing.T) {
	names, descriptions := envComments(t)
	doc, err := os.ReadFile(deploymentDocFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	start, end := strings.Index(text, referenceStart), strings.Index(text, referenceEnd)
	if start < 0 || end < start {
		t.Fatalf("%s lacks the %s … %s markers", deploymentDocFile, referenceStart, referenceEnd)
	}
	current := text[start : end+len(referenceEnd)]
	want := renderConfigReference(names, descriptions)
	if current == want {
		return
	}
	if os.Getenv("IOT_UPDATE_CONFIG_DOCS") == "1" {
		if err := os.WriteFile(deploymentDocFile, []byte(text[:start]+want+text[end+len(referenceEnd):]), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("docs/DEPLOYMENT.md configuration reference is out of date; run with IOT_UPDATE_CONFIG_DOCS=1")
}
