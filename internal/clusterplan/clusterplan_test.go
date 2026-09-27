package clusterplan

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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

// fakeTools puts docker, ssh, scp and curl shims first on PATH. Commands are
// logged to $FAKE/calls.log; docker run of the platform image executes the
// real cluster-render so the whole one-click flow runs without nodes.
func fakeTools(t *testing.T, root string) (string, []string) {
	t.Helper()
	fake := t.TempDir()
	bin := filepath.Join(fake, "bin")
	_ = os.MkdirAll(filepath.Join(fake, "images"), 0o755)
	_ = os.MkdirAll(bin, 0o755)
	build := exec.Command("go", "build", "-o", filepath.Join(fake, "cluster-render"), "./cmd/cluster-render")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	shims := map[string]string{
		"docker": `#!/usr/bin/env bash
echo "docker $*" >> "$FAKE/calls.log"
key() { printf '%s' "$1" | tr '/:@' '___'; }
case "$1" in
  info|load) cat >/dev/null 2>&1 || true; exit 0;;
  compose)
    [ "$2" = version ] && echo 2.29.0
    if [[ " $* " == *" build "* ]]; then
      for img in "$IOT_PLATFORM_WEB_IMAGE" "$IOT_DEEPSEEK_HARNESS_IMAGE" "$IOT_BACKUP_IMAGE" "$IOT_ZLMEDIAKIT_IMAGE"; do [ -n "$img" ] && touch "$FAKE/images/$(key "$img")"; done
    fi
    exit 0;;
  build) while [ $# -gt 0 ]; do [ "$1" = -t ] && touch "$FAKE/images/$(key "$2")"; shift; done; exit 0;;
  pull) touch "$FAKE/images/$(key "$2")"; exit 0;;
  image) img="${@: -1}"; [ -f "$FAKE/images/$(key "$img")" ] || exit 1; [ "$3" = "{{.Id}}" ] && echo "sha256:$(key "$img")"; exit 0;;
  save) shift; echo "saved $*" >> "$FAKE/calls.log"; echo archive; exit 0;;
  run)
    args=() mounts=() entry=""
    shift
    while [ $# -gt 0 ]; do
      case "$1" in
        -v) mounts+=("$2"); shift 2;;
        --entrypoint) entry="$2"; shift 2;;
        --rm) shift;;
        --user|--network|--env-file) shift 2;;
        *) break;;
      esac
    done
    shift  # image
    for a in "$@"; do
      for m in "${mounts[@]}"; do src="${m%%:*}"; rest="${m#*:}"; dst="${rest%%:*}"; a="${a/#$dst/$src}"; done
      args+=("$a")
    done
    [ "$entry" = /app/cluster-render ] && exec "$FAKE/cluster-render" "${args[@]}"
    exit 0;;
esac
exit 0
`,
		"ssh": `#!/usr/bin/env bash
echo "ssh $*" >> "$FAKE/calls.log"
if [[ "$*" == *"sh -s --"* ]]; then
  cat >/dev/null
  printf 'docker=27.3.1\ncompose=2.29.0\ndisk=%s\nclock=%s\nrunning=%s\nbusy=%s\n' "${FAKE_DISK:-100}" "$(date +%s)" "${FAKE_RUNNING:-0}" "${FAKE_BUSY:-}"
elif [[ "$*" == *"docker image inspect"* ]]; then
  all="$*"; list="${all#*for i in }"; list="${list%%; do*}"
  for i in $list; do echo missing; done
elif [[ "$*" == *"docker load"* ]]; then
  cat >/dev/null
fi
exit 0
`,
		"scp":  "#!/usr/bin/env bash\necho \"scp $*\" >> \"$FAKE/calls.log\"\n",
		"curl": "#!/usr/bin/env bash\necho \"curl $*\" >> \"$FAKE/calls.log\"\n",
	}
	for name, body := range shims {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "FAKE="+fake, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return fake, env
}

func TestClusterUpRunsTheWholeDeploymentAgainstFakeNodes(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil || runtime.GOOS == "windows" {
		t.Skip("bash shims")
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	fake, env := fakeTools(t, root)
	state := filepath.Join(fake, "state")
	up := func(extra ...string) (string, error) {
		cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts", "cluster-up.sh"), "--inventory", filepath.Join(root, "deploy", "cluster", "inventory.example.yaml"), "--ssh-user", "deploy", "--state-dir", state, "--yes"}, extra...)...)
		cmd.Env = env
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	calls := func() string { b, _ := os.ReadFile(filepath.Join(fake, "calls.log")); return string(b) }

	// Ports taken by other programs on a first deployment stop everything
	// before any node is changed.
	out, err := up()
	if err != nil {
		t.Fatal(err, out)
	}
	secretsPath := filepath.Join(state, "secrets.yaml")
	info, err := os.Stat(secretsPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("secrets file", err)
	}
	first, _ := LoadSecrets(secretsPath)
	log := calls()
	for _, want := range []string{
		"docker build -t iot-platform-api:offline", "compose -f " + filepath.Join(root, "compose.yaml"), "platform-web", "docker pull redpandadata/redpanda:v25.2.11", "docker pull haproxy:3.0-alpine",
		"sh -s -- iot-cluster 2379", "saved ", "--entrypoint /app/cluster-init 'iot-platform-api:offline' -bootstrap-postgres -execute", "curl -fsS --max-time 5 http://10.0.0.13:8102/health/ready",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("missing %q in calls:\n%s\n%s", want, log, out)
		}
	}
	// Every node of every stage is started: ssh must not swallow the plan.
	for _, addr := range []string{"10.0.0.11", "10.0.0.12", "10.0.0.13", "10.0.0.14"} {
		if !strings.Contains(log, "ssh -n -o BatchMode=yes -o ConnectTimeout=10 deploy@"+addr+" cd '/opt/iot-cluster' && docker compose -p iot-cluster --env-file .env up -d --no-build --pull never") {
			t.Fatalf("node %s was not started:\n%s", addr, log)
		}
	}
	if strings.Count(log, "saved ") != 4 || !strings.Contains(out, "web          http://10.0.0.11:8080") || strings.Contains(out, first.AdminPassword) || strings.Contains(log, first.PostgresPassword) {
		t.Fatalf("images per node, entries or secret leak:\n%s", out)
	}
	// Re-running upgrades in place: same secrets, previous rendering kept.
	_ = os.Remove(filepath.Join(fake, "calls.log"))
	cmdEnv := env
	env = append(env, "FAKE_RUNNING=12", "FAKE_BUSY= 8081")
	if out, err = up("--no-build"); err != nil {
		t.Fatal(err, out)
	}
	env = cmdEnv
	again, _ := LoadSecrets(secretsPath)
	if again != first {
		t.Fatal("upgrade changed the secrets")
	}
	if _, err = os.Stat(filepath.Join(state, "rendered.prev", "deploy-plan.txt")); err != nil || strings.Contains(calls(), "docker build") {
		t.Fatal("upgrade must keep the previous rendering and skip the build", err)
	}
	// A first deployment onto busy ports or a full disk is refused before any change.
	_ = os.Remove(filepath.Join(fake, "calls.log"))
	env = append(env, "FAKE_BUSY= 5432 8080", "FAKE_DISK=5")
	out, err = up("--no-build")
	if err == nil || !strings.Contains(out, "ports already in use by other programs: 5432 8080") || !strings.Contains(out, "only 5GiB free") || strings.Contains(calls(), "up -d") || strings.Contains(calls(), "saved ") {
		t.Fatalf("preflight must stop the deployment: %v\n%s", err, out)
	}
	// An online machine can save every image for an offline controller.
	env = cmdEnv
	_ = os.Remove(filepath.Join(fake, "calls.log"))
	if out, err = up("--no-build", "--bundle", filepath.Join(fake, "bundle.tar")); err != nil || !strings.Contains(calls(), "saved -o "+filepath.Join(fake, "bundle.tar")+" clickhouse/clickhouse-keeper:25.7-alpine") || strings.Contains(calls(), "ssh ") {
		t.Fatal("bundle", err, out, calls())
	}
}

func TestLocalLoadBalancersReplaceExternalOnes(t *testing.T) {
	inv := example(t)
	if !inv.AutoLB() {
		t.Fatal("the example uses the built-in load balancers")
	}
	files, err := Render(inv, testSecrets())
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(files["n4/lb/haproxy.cfg"])
	for _, want := range []string{"bind 127.0.0.1:18181", "server api-n1 10.0.0.11:8081", "server api-n2 10.0.0.12:8081", "bind 127.0.0.1:18182", "server gateway-n3 10.0.0.13:8082", "option httpchk GET /health/ready"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("haproxy config lacks %q:\n%s", want, cfg)
		}
	}
	for _, n := range inv.Nodes {
		if !strings.Contains(string(files[n.Name+"/compose.yaml"]), "haproxy:3.0-alpine") {
			t.Fatal("every node runs the local load balancer", n.Name)
		}
	}
	n1 := string(files["n1/compose.yaml"])
	if !strings.Contains(n1, "IOT_API_UPSTREAM: 127.0.0.1:18181") || !strings.Contains(n1, "IOT_AI_HARNESS_MCP_URL: http://127.0.0.1:18181/mcp/harness") || !strings.Contains(n1, "IOT_ACCESS_GATEWAY_URL: http://127.0.0.1:18182") {
		t.Fatalf("web, harness callbacks and gateway forwarding must use the local balancer:\n%s", n1)
	}
	plan := string(files["deploy-plan.txt"])
	for _, want := range []string{"# platform-image iot-platform-api:offline", "images n4 10.0.0.14 ", "ports n1 10.0.0.11 ", "entry web http://10.0.0.11:8080", "entry device-tcp 10.0.0.12:26875"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("deploy plan lacks %q:\n%s", want, plan)
		}
	}
	// External load balancers: both URLs set, no HAProxy rendered.
	inv.Platform.InternalURL, inv.Platform.GatewayURL = "http://10.0.0.10:8081", "http://10.0.0.10:8082"
	files, err = Render(inv, testSecrets())
	if err != nil || files["n1/lb/haproxy.cfg"] != nil || strings.Contains(string(files["n3/compose.yaml"]), "haproxy") {
		t.Fatal("explicit load balancers must not render HAProxy", err)
	}
	inv.Platform.GatewayURL = ""
	if _, err = inv.Validate(); err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatal("half-configured load balancers must be rejected", err)
	}
}

func TestEnsureSecretsFillsOnlyMissingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "secrets.yaml")
	generated, err := EnsureSecrets(path)
	if err != nil || len(generated) < 15 {
		t.Fatal(err, generated)
	}
	info, _ := os.Stat(path)
	s, err := LoadSecrets(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatal(err, info.Mode())
	}
	if err = s.validate(example(t)); err != nil {
		t.Fatal("generated secrets must pass validation:", err)
	}
	if key, err := base64.StdEncoding.DecodeString(s.VideoCredentialKey); err != nil || len(key) != 32 || s.DeepSeekAPIKey != "" || s.MinIORootUser != "iotadmin" {
		t.Fatal("video key, optional and fixed values", err)
	}
	// Operator-set values survive; placeholders and gaps are filled.
	body, _ := os.ReadFile(path)
	body = []byte(strings.Replace(strings.Replace(string(body), "adminPassword: "+s.AdminPassword, "adminPassword: Operator-Chosen-1", 1), "redisPassword: "+s.RedisPassword, "redisPassword: change-me", 1) + "deepseekApiKey: sk-test\n")
	body = []byte(strings.Replace(string(body), "deepseekApiKey: \"\"\n", "", 1))
	_ = os.WriteFile(path, body, 0o600)
	generated, err = EnsureSecrets(path)
	again, _ := LoadSecrets(path)
	if err != nil || !reflect.DeepEqual(generated, []string{"redisPassword"}) || again.AdminPassword != "Operator-Chosen-1" || again.RedisPassword == "change-me" || again.DeepSeekAPIKey != "sk-test" || again.JWTSecret != s.JWTSecret {
		t.Fatal(err, generated, again.AdminPassword)
	}
	if generated, _ = EnsureSecrets(path); generated != nil {
		t.Fatal("a complete file is left untouched", generated)
	}
}
