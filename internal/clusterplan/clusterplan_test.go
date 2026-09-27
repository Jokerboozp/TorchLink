package clusterplan

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func example(t *testing.T) *Inventory {
	t.Helper()
	inv, err := Load(filepath.Join("..", "..", "deploy", "cluster", "inventory.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func testSecrets() Secrets {
	v := reflect.ValueOf(&Secrets{}).Elem()
	for i := 0; i < v.NumField(); i++ {
		v.Field(i).SetString("s3cret-" + strings.ToLower(v.Type().Field(i).Name) + "-0123456789abcdefghij")
	}
	return v.Interface().(Secrets)
}

func TestExampleInventoryRendersIsolatedSecretsAndConfigs(t *testing.T) {
	inv := example(t)
	s := testSecrets()
	files, err := Render(inv, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range inv.Nodes {
		var compose struct {
			Name     string                    `yaml:"name"`
			Services map[string]map[string]any `yaml:"services"`
		}
		if err := yaml.Unmarshal(files[n.Name+"/compose.yaml"], &compose); err != nil || compose.Name != inv.Name || len(compose.Services) == 0 {
			t.Fatal(n.Name, err)
		}
		for name, svc := range compose.Services {
			if svc["network_mode"] != "host" {
				t.Fatal(n.Name, name, "not host networking")
			}
		}
	}
	// Secret values only live in .env files, and only where needed.
	secretValues := []string{s.PostgresPassword, s.PostgresSuperuserPassword, s.RedisPassword, s.ClickHousePassword, s.JWTSecret, s.AdminPassword, s.HarnessToken, s.EMQXCookie, s.MinIORootPassword}
	for name, body := range files {
		if strings.HasSuffix(name, ".env") {
			continue
		}
		for _, v := range secretValues {
			if strings.Contains(string(body), v) {
				t.Fatalf("secret leaked into %s", name)
			}
		}
	}
	if strings.Contains(string(files["n4/.env"]), s.PostgresSuperuserPassword) {
		t.Fatal("node without a PostgreSQL member received the superuser password")
	}
	if !strings.Contains(string(files["n1/.env"]), "POSTGRES_SUPERUSER_PASSWORD="+s.PostgresSuperuserPassword) {
		t.Fatal("postgres member lacks its secret")
	}
	n1 := string(files["n1/compose.yaml"])
	for _, want := range []string{
		"postgres://iot:${POSTGRES_PASSWORD}@10.0.0.11:5432,10.0.0.12:5432,10.0.0.13:5432/iot?sslmode=disable&target_session_attrs=read-write",
		"IOT_KAFKA_AUTO_CREATE_TOPICS: \"false\"",
		"IOT_CLICKHOUSE_CLUSTER: iot_cluster",
		"IOT_NODE_URL: http://10.0.0.11:8081",
		"IOT_API_EMBEDDED_WORKERS: \"false\"",
		"redpanda.default_topic_replications=3",
	} {
		if !strings.Contains(n1, want) {
			t.Fatalf("n1 compose lacks %q", want)
		}
	}
	ch := string(files["n3/clickhouse/config.d/cluster.xml"])
	if !strings.Contains(ch, "<shard>02</shard><replica>n3</replica>") || !strings.Contains(ch, `from_env="CLICKHOUSE_PASSWORD"`) || strings.Count(ch, "<node><host>") != 3 {
		t.Fatal("clickhouse macros/keeper config", ch)
	}
	if k := string(files["n2/keeper/keeper_config.xml"]); !strings.Contains(k, "<server_id>1</server_id>") || strings.Count(k, "<server>") != 3 {
		t.Fatal("keeper config", k)
	}
	prom := string(files["n4/prometheus/prometheus.yml"])
	if !strings.Contains(prom, `"10.0.0.13:8102"`) || !strings.Contains(prom, `instance: "processor-n3"`) {
		t.Fatal("prometheus must scrape every role instance", prom)
	}
	if !strings.Contains(string(files["cluster.json"]), `"coordination"`) {
		t.Fatal("start stages missing")
	}
	// docker compose must accept every rendered node project.
	if _, err := exec.LookPath("docker"); err == nil && exec.Command("docker", "compose", "version").Run() == nil {
		dir := t.TempDir()
		for name, body := range files {
			p := filepath.Join(dir, name)
			_ = os.MkdirAll(filepath.Dir(p), 0o700)
			_ = os.WriteFile(p, body, 0o600)
		}
		for _, n := range inv.Nodes {
			cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, n.Name, "compose.yaml"), "--env-file", filepath.Join(dir, n.Name, ".env"), "config", "--quiet")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("docker compose rejected %s: %s", n.Name, out)
			}
		}
	}
}

func TestValidationRejectsUnsafeLayouts(t *testing.T) {
	cases := map[string]func(*Inventory){
		"shard 1 members are all in one fault domain": func(i *Inventory) { i.ClickHouse.Shards[0] = []string{"n1", "n4"} },
		"holds 2 of 3 members":                        func(i *Inventory) { i.Etcd.Nodes = []string{"n1", "n4", "n2"} },
		"odd number":                                  func(i *Inventory) { i.Etcd.Nodes = []string{"n1", "n2"} },
		"hosts two clickhouse replicas":               func(i *Inventory) { i.ClickHouse.Shards[1] = []string{"n1", "n3"} },
		"connection budget":                           func(i *Inventory) { i.Postgres.MaxConnections = 150 },
		"unknown node":                                func(i *Inventory) { i.MinIO.Node = "n9" },
		"secrets come from the secrets file":          func(i *Inventory) { i.Env["IOT_JWT_SECRET"] = "x" },
		"needs at least one node":                     func(i *Inventory) { delete(i.Platform.Roles, "processor") },
	}
	for want, mutate := range cases {
		inv := example(t)
		mutate(inv)
		if _, err := inv.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
	old := Ports["web"]
	Ports["web"] = []int{8081}
	defer func() { Ports["web"] = old }()
	if _, err := example(t).Validate(); err == nil || !strings.Contains(err.Error(), "both need port 8081") {
		t.Fatal("port clash not detected", err)
	}
}

func TestSecretsAreValidated(t *testing.T) {
	inv := example(t)
	s := testSecrets()
	s.PostgresPassword = "has@sign"
	if _, err := Render(inv, s); err == nil || !strings.Contains(err.Error(), "postgresPassword") {
		t.Fatal("URL-unsafe password accepted", err)
	}
	s = testSecrets()
	s.JWTSecret = "change-me"
	if _, err := Render(inv, s); err == nil {
		t.Fatal("placeholder accepted")
	}
}

func TestDeployScriptsFollowStageOrder(t *testing.T) {
	inv := example(t)
	files, err := Render(inv, testSecrets())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, body, 0o600)
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	check := func(t *testing.T, out string) {
		t.Helper()
		idx := func(s string) int {
			i := strings.Index(out, s)
			if i < 0 {
				t.Fatalf("missing %q in:\n%s", s, out)
			}
			return i
		}
		if !(idx("up -d --no-build --pull never etcd") < idx("up -d --no-build --pull never clickhouse") && idx("clickhouse") < idx("-bootstrap-postgres -execute") && idx("-bootstrap-postgres -execute") < idx("iot-processor") && idx("iot-processor") < idx("iot-api")) {
			t.Fatalf("stages out of order:\n%s", out)
		}
		if !strings.Contains(out, "http://10.0.0.13:8102/health/ready") {
			t.Fatal("processor readiness not checked")
		}
		if strings.Contains(out, testSecrets().JWTSecret) {
			t.Fatal("secret printed")
		}
	}
	if _, err := exec.LookPath("bash"); err == nil {
		out, err := exec.Command("bash", filepath.Join(root, "scripts", "cluster-deploy.sh"), "--rendered", dir, "--dry-run").CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		check(t, string(out))
		out, err = exec.Command("bash", filepath.Join(root, "scripts", "cluster-deploy.sh"), "--rendered", dir, "--dry-run", "--stage", "workers", "--nodes", "n4").CombinedOutput()
		if err != nil || strings.Contains(string(out), "10.0.0.11") || !strings.Contains(string(out), "iot-jobs") {
			t.Fatal("node/stage filter", err, string(out))
		}
	}
	if pwsh, err := exec.LookPath("pwsh"); err == nil {
		out, err := exec.Command(pwsh, "-NoProfile", "-File", filepath.Join(root, "scripts", "cluster-deploy.ps1"), "-Rendered", dir, "-DryRun").CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		check(t, string(out))
	}
}
