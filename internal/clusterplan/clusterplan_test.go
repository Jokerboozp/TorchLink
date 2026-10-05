package clusterplan

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

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
		field := v.Type().Field(i)
		if field.Tag.Get("yaml") == "-" || strings.HasPrefix(field.Name, "Kafka") || field.Name == "MQTTPublicURL" || strings.HasPrefix(field.Name, "TLS") || field.Name == "AlertWebhookURL" {
			continue
		}
		v.Field(i).SetString("s3cret-" + strings.ToLower(v.Type().Field(i).Name) + "-0123456789abcdefghij")
	}
	return v.Interface().(Secrets)
}

func testKafkaSecrets(t *testing.T) (Secrets, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	s := testSecrets()
	s.KafkaSASLUsername, s.KafkaSASLPassword, s.KafkaSASLMechanism = "platform-writer", "platform-kafka-password", "SCRAM-SHA-512"
	s.KafkaTLS, s.KafkaTLSCAFile = "true", "kafka-ca.pem"
	s.KafkaAdminURL, s.KafkaAdminUsername, s.KafkaAdminPassword = "https://admin.example.test:9644", "topic-admin", "kafka-admin-password"
	s.KafkaPublicBrokers, s.MQTTPublicURL = "kafka.example.test:9093,[2001:db8::1]:9093", "ssl://mqtt.example.test:8883"
	dir := t.TempDir()
	body, err := yaml.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "secrets.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, s.KafkaTLSCAFile), ca, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err = LoadSecrets(filepath.Join(dir, "secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return s, ca
}

func TestKafkaAndMQTTSettingsReachEveryRoleAndInit(t *testing.T) {
	inv := example(t)
	s, ca := testKafkaSecrets(t)
	files, err := Render(inv, s)
	if err != nil {
		t.Fatal(err)
	}
	wantEnv := map[string]string{
		"IOT_KAFKA_SASL_USERNAME": s.KafkaSASLUsername, "IOT_KAFKA_SASL_PASSWORD": s.KafkaSASLPassword,
		"IOT_KAFKA_SASL_MECHANISM": "SCRAM-SHA-512", "IOT_KAFKA_TLS": "true", "IOT_KAFKA_TLS_CA_FILE": "/app/kafka/ca.pem",
		"IOT_KAFKA_ADMIN_URL": s.KafkaAdminURL, "IOT_KAFKA_ADMIN_USERNAME": s.KafkaAdminUsername,
		"IOT_KAFKA_ADMIN_PASSWORD": s.KafkaAdminPassword, "IOT_KAFKA_PUBLIC_BROKERS": s.KafkaPublicBrokers,
		"IOT_DEVICE_MQTT_PUBLIC_URL": s.MQTTPublicURL,
	}
	for _, role := range RoleNames {
		for _, node := range inv.Platform.Roles[role].Nodes {
			var compose struct {
				Services map[string]struct {
					Environment map[string]string `yaml:"environment"`
					Volumes     []string          `yaml:"volumes"`
				} `yaml:"services"`
			}
			if err := yaml.Unmarshal(files[node+"/compose.yaml"], &compose); err != nil {
				t.Fatal(err)
			}
			svc := compose.Services["iot-"+role]
			for k, v := range wantEnv {
				if svc.Environment[k] != "${"+k+"}" || !strings.Contains(string(files[node+"/.env"]), k+"="+v+"\n") {
					t.Fatalf("%s/%s lacks setting %s or its variable reference", node, role, k)
				}
			}
			if !strings.Contains(strings.Join(svc.Volumes, "\n"), "./kafka/ca.pem:/app/kafka/ca.pem:ro") || string(files[node+"/kafka/ca.pem"]) != string(ca) {
				t.Fatalf("%s/%s cannot read the rendered CA", node, role)
			}
		}
	}
	for k, v := range wantEnv {
		if !strings.Contains(string(files["init.env"]), k+"="+v+"\n") {
			t.Fatalf("init.env lacks %s", k)
		}
	}
	if string(files["kafka/ca.pem"]) != string(ca) {
		t.Fatal("initialization CA missing")
	}
	for name, body := range files {
		if !strings.HasSuffix(name, ".env") && (strings.Contains(string(body), s.KafkaSASLPassword) || strings.Contains(string(body), s.KafkaAdminPassword)) {
			t.Fatalf("Kafka credential leaked into %s", name)
		}
	}
	baselineSecrets := testSecrets()
	baselineSecrets.ServiceAdminUser, baselineSecrets.ServiceAdminPassword = "", ""
	baseline, err := Render(inv, baselineSecrets)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(baseline["init.env"]), "IOT_KAFKA_TLS=false\n") || !strings.Contains(string(baseline["init.env"]), "IOT_KAFKA_SASL_PASSWORD=admin123\n") || baseline["kafka/ca.pem"] != nil {
		t.Fatalf("new cluster defaults: TLS=%t identity=%t CA=%t", strings.Contains(string(baseline["init.env"]), "IOT_KAFKA_TLS=false\n"), strings.Contains(string(baseline["init.env"]), "IOT_KAFKA_SASL_PASSWORD=admin123\n"), baseline["kafka/ca.pem"] != nil)
	}
}

func TestKafkaClusterSettingsFailBeforeDeployment(t *testing.T) {
	valid, _ := testKafkaSecrets(t)
	cases := map[string]func(*Secrets){
		"SASL pair":                  func(s *Secrets) { s.KafkaSASLPassword = "" },
		"mechanism":                  func(s *Secrets) { s.KafkaSASLMechanism = "PLAIN" },
		"TLS boolean":                func(s *Secrets) { s.KafkaTLS = "maybe" },
		"CA without TLS":             func(s *Secrets) { s.KafkaTLS = "false" },
		"missing CA":                 func(s *Secrets) { s.KafkaTLSCAFile = "missing.pem" },
		"admin pair":                 func(s *Secrets) { s.KafkaAdminUsername = "" },
		"admin embedded credentials": func(s *Secrets) { s.KafkaAdminURL = "https://user:password@example.test" },
		"public broker address":      func(s *Secrets) { s.KafkaPublicBrokers = "https://kafka.example.test:9093" },
		"public MQTT credentials":    func(s *Secrets) { s.MQTTPublicURL = "tcp://user:password@example.test:1883" },
		"environment injection":      func(s *Secrets) { s.KafkaSASLPassword = "secret\nINJECT=true" },
		"environment comment":        func(s *Secrets) { s.KafkaSASLPassword = "secret #truncated" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := valid
			mutate(&s)
			if _, err := Render(example(t), s); err == nil {
				t.Fatal("invalid Kafka configuration accepted")
			} else if strings.Contains(err.Error(), valid.KafkaSASLPassword) || strings.Contains(err.Error(), valid.KafkaAdminPassword) {
				t.Fatal("validation error exposes credentials")
			}
		})
	}
	inv := example(t)
	inv.Env["IOT_KAFKA_TLS"] = "false"
	if _, err := Render(inv, valid); err == nil || !strings.Contains(err.Error(), "secrets file") {
		t.Fatal("inventory must not silently override secured client settings", err)
	}
	for _, body := range []string{"not a certificate", "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----"} {
		path := filepath.Join(t.TempDir(), "bad-ca.pem")
		_ = os.WriteFile(path, []byte(body), 0o600)
		s := valid
		s.KafkaTLSCAFile = path
		if _, err := Render(example(t), s); err == nil {
			t.Fatal("invalid certificate accepted")
		}
	}
}

func TestToolDefaultsPreserveIndependentInternalSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if _, err := EnsureSecretsWith(path, SecretInputs{}); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"serviceAdminUser": s.ServiceAdminUser, "kafkaSaslUsername": s.KafkaSASLUsername, "kafkaAdminUsername": s.KafkaAdminUsername, "mqttToolUsername": s.MQTTToolUsername, "minioRootUser": s.MinIORootUser} {
		if value != "admin" {
			t.Fatalf("%s must default to admin", key)
		}
	}
	for key, value := range map[string]string{"serviceAdminPassword": s.ServiceAdminPassword, "kafkaSaslPassword": s.KafkaSASLPassword, "kafkaAdminPassword": s.KafkaAdminPassword, "mqttToolPassword": s.MQTTToolPassword, "postgresPassword": s.PostgresPassword, "redisPassword": s.RedisPassword, "clickhousePassword": s.ClickHousePassword, "minioRootPassword": s.MinIORootPassword} {
		if value != "admin123" {
			t.Fatalf("%s must default to admin123", key)
		}
	}
	seen := map[string]bool{}
	for _, value := range []string{s.JWTSecret, s.HarnessToken, s.BackupToken, s.PostgresSuperuserPassword, s.PostgresReplicationPassword, s.EMQXCookie, s.EMQXAPISecret} {
		if len(value) < 32 || seen[value] {
			t.Fatal("internal credentials must stay independent and random")
		}
		seen[value] = true
	}
	if s.KafkaTLS != "" || s.KafkaTLSCAFile != "" || s.KafkaPublicBrokers != "" || s.MQTTPublicURL != "" {
		t.Fatal("optional listener and TLS settings must not be invented in secrets")
	}
	// Filling a different missing secret must preserve a portable relative CA path.
	s, _ = testKafkaSecrets(t)
	path = filepath.Join(s.baseDir, "secrets.yaml")
	body, _ := os.ReadFile(path)
	body = []byte(strings.Replace(string(body), "adminPassword: "+s.AdminPassword, "adminPassword: change-me", 1))
	_ = os.WriteFile(path, body, 0o600)
	if _, err = EnsureSecretsWith(path, SecretInputs{}); err != nil {
		t.Fatal(err)
	}
	again, err := LoadSecrets(path)
	if err != nil || again.KafkaTLSCAFile != "kafka-ca.pem" || again.KafkaSASLPassword != s.KafkaSASLPassword {
		t.Fatal("broker settings changed while completing secrets", err)
	}
	if _, err = Render(example(t), again); err != nil {
		t.Fatal(err)
	}
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
			want := "host"
			if name == "protocol-runner" {
				// Uploaded protocol code gets no network at all.
				want = "none"
			}
			if svc["network_mode"] != want {
				t.Fatal(n.Name, name, "network_mode", svc["network_mode"])
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
	// Knowledge uses the replicated database and the embedding / reranker
	// services running beside each API on loopback ports.
	if !strings.Contains(n1, "iot-platform-postgres-ha:17-pgvector-0.8.1") || !strings.Contains(n1, "IOT_EMBEDDING_URL: http://127.0.0.1:18093/v1") || !strings.Contains(n1, "IOT_RERANK_URL: http://127.0.0.1:18094") || !strings.Contains(n1, "IOT_EMBEDDING_DIMENSIONS: \"1024\"") {
		t.Fatal("platform must use pgvector and the bundled vector services")
	}
	apiNodes := map[string]bool{}
	for _, node := range inv.Platform.Roles["api"].Nodes {
		apiNodes[node] = true
	}
	for _, node := range inv.Nodes {
		compose := string(files[node.Name+"/compose.yaml"])
		local := strings.Contains(compose, "image: iot-local-ai:offline") && strings.Contains(compose, "LLAMA_ARG_HOST: 127.0.0.1") && strings.Contains(compose, "bge-reranker-v2-m3-Q8_0.gguf")
		if local != apiNodes[node.Name] {
			t.Fatalf("node %s: bundled vector services=%v, api node=%v", node.Name, local, apiNodes[node.Name])
		}
	}
	backup := string(files[inv.Backup.Node+"/compose.yaml"])
	for _, node := range inv.Harness.Nodes {
		n, _ := inv.node(node)
		if !strings.Contains(backup, "http://"+n.Address+":8091/v1/backup/snapshot") {
			t.Fatal("backup snapshot does not cover every Harness instance")
		}
	}
	if !strings.Contains(string(files[inv.Backup.Node+"/.env"]), "IOT_AI_HARNESS_TOKEN="+s.HarnessToken) || !strings.Contains(string(files[inv.Backup.Node+"/.env"]), "IOT_BACKUP_RESTORE_MINIO_SECRET_KEY="+s.BackupRestoreMinIOSecretKey) {
		t.Fatal("backup node is missing server-side snapshot/DR credentials")
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
		"unknown node":                                func(i *Inventory) { i.RustFS.Node = "n9" },
		"secrets come from the secrets file":          func(i *Inventory) { i.Env["IOT_JWT_SECRET"] = "x" },
		"needs at least one node":                     func(i *Inventory) { delete(i.Platform.Roles, "processor") },
		"unknown platform role":                       func(i *Inventory) { i.Platform.Roles["unknown"] = RoleSpec{Nodes: []string{"n3", "n4"}, PoolMax: 4} },
		"distributed rustfs needs at least 4 nodes":   func(i *Inventory) { i.RustFS = PoolSpec{Nodes: []string{"n1", "n2", "n3"}} },

		// Pools take node or nodes.
		"not both":                          func(i *Inventory) { i.Video.Nodes = []string{"n3", "n4"} },
		"monitoring.nodes needs at least 2": func(i *Inventory) { i.Monitoring = PoolSpec{Nodes: []string{"n4"}} },
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
	secrets, _ := testKafkaSecrets(t)
	files, err := Render(inv, secrets)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, body, 0o600)
	}
	if _, err := exec.LookPath("docker"); err == nil && exec.Command("docker", "compose", "version").Run() == nil {
		for _, node := range inv.Nodes {
			cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, node.Name, "compose.yaml"), "--env-file", filepath.Join(dir, node.Name, ".env"), "config", "--quiet")
			if err := cmd.Run(); err != nil {
				t.Fatalf("secured node %s failed Compose validation: %v", node.Name, err)
			}
		}
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
		if !(idx("-bootstrap-postgres -execute") < idx("run --rm --no-deps postgres-tool-admin") && idx("run --rm --no-deps clickhouse-tool-admin") < idx("iot-processor")) {
			t.Fatal("tool accounts must initialize after databases and before workers")
		}
		if !strings.Contains(out, "http://10.0.0.13:8102/health/ready") {
			t.Fatal("processor readiness not checked")
		}
		if strings.Contains(out, secrets.JWTSecret) || strings.Contains(out, secrets.KafkaSASLPassword) || strings.Contains(out, secrets.KafkaAdminPassword) {
			t.Fatal("secret printed")
		}
		for _, want := range []string{".init-kafka-ca.pem", ":/app/kafka/ca.pem:ro", "--env-file .init.env"} {
			if !strings.Contains(out, want) {
				t.Fatalf("cluster-init cannot use the rendered CA: missing %s", want)
			}
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
		// A local initializer runs on the controller, where the container CA
		// path does not exist. Stub only node IO and inspect the actual child env.
		fake := t.TempDir()
		for _, name := range []string{"ssh", "scp"} {
			if err := os.WriteFile(filepath.Join(fake, name), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		init := filepath.Join(fake, "test-init")
		if err := os.WriteFile(init, []byte("#!/bin/sh\n[ \"$IOT_KAFKA_TLS_CA_FILE\" = \"$EXPECTED_CA\" ] && [ -r \"$IOT_KAFKA_TLS_CA_FILE\" ] || exit 7\nprintf 'local init CA verified\\n'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", filepath.Join(root, "scripts", "cluster-deploy.sh"), "--rendered", dir, "--stage", "init", "--cluster-init", init, "--init-attempts", "1")
		cmd.Env = append(os.Environ(), "PATH="+fake+string(os.PathListSeparator)+os.Getenv("PATH"), "EXPECTED_CA="+filepath.Join(dir, "kafka", "ca.pem"), "IOT_KAFKA_TLS_CA_FILE=/unused/old-ca.pem")
		out, err = cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "local init CA verified") {
			t.Fatal("local cluster-init did not receive its readable CA path", err, string(out))
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
      for img in "$IOT_PLATFORM_WEB_IMAGE" "$IOT_DEEPSEEK_HARNESS_IMAGE" "$IOT_BACKUP_IMAGE" "$IOT_ZLMEDIAKIT_IMAGE" "$IOT_LOCAL_AI_IMAGE"; do [ -n "$img" ] && touch "$FAKE/images/$(key "$img")"; done
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
        --rm|-i) shift;;
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
    if [ "$entry" = /app/cluster-ssh ]; then
      nodes="" key=""
      for ((j=0; j<${#args[@]}; j++)); do
        [ "${args[$j]}" = -nodes ] && nodes="${args[$((j+1))]}"
        [ "${args[$j]}" = -key ] && key="${args[$((j+1))]}"
      done
      if [ "${args[0]}" = check ]; then
        [ -f "$key" ] || { for n in ${nodes//,/ }; do echo "fail $n: no deployment key yet"; done; exit 1; }
        for n in ${nodes//,/ }; do echo "ok $n"; done; exit 0
      fi
      cat >> "$FAKE/ssh-stdin.log"
      echo "-----BEGIN OPENSSH PRIVATE KEY-----" > "$key"; chmod 600 "$key"
      for n in ${nodes//,/ }; do echo "ok $n"; done; exit 0
    fi
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
	env := append(os.Environ(), "FAKE="+fake, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TORCHLINK_SSH_PASSWORD=Fake-Ssh-Pass", "TORCHLINK_SERVICE_PASSWORD=", "TORCHLINK_FORCE_INTERACTIVE=")
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
		if !strings.Contains(log, "deploy@"+addr+" cd '/opt/iot-cluster' && docker compose -p iot-cluster --env-file .env up -d --no-build --pull never") || !strings.Contains(log, "ssh -n -o BatchMode=yes -o ConnectTimeout=10 -i "+filepath.Join(state, "deploy_key")+" -o IdentitiesOnly=yes -o UserKnownHostsFile="+filepath.Join(state, "known_hosts")) {
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
	if out, err = up("--no-build", "--bundle", filepath.Join(fake, "bundle.tar")); err != nil || !strings.Contains(calls(), "saved -o "+filepath.Join(fake, "bundle.tar")+" clickhouse/clickhouse-keeper:25.7-alpine") || strings.Contains(calls(), "ssh ") || strings.Contains(calls(), "cluster-ssh") {
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
	generated, err := EnsureSecretsWith(path, SecretInputs{})
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
	if key, err := base64.StdEncoding.DecodeString(s.VideoCredentialKey); err != nil || len(key) != 32 || s.DeepSeekAPIKey != "" || s.MinIORootUser != "admin" {
		t.Fatal("video key, optional and fixed values", err)
	}
	// Operator-set values survive; placeholders and gaps are filled.
	body, _ := os.ReadFile(path)
	body = []byte(strings.Replace(strings.Replace(string(body), "adminPassword: "+s.AdminPassword, "adminPassword: Operator-Chosen-1", 1), "redisPassword: "+s.RedisPassword, "redisPassword: change-me", 1) + "deepseekApiKey: sk-test\n")
	body = []byte(strings.Replace(string(body), "deepseekApiKey: \"\"\n", "", 1))
	_ = os.WriteFile(path, body, 0o600)
	generated, err = EnsureSecretsWith(path, SecretInputs{})
	again, _ := LoadSecrets(path)
	if err != nil || !reflect.DeepEqual(generated, []string{"redisPassword"}) || again.AdminPassword != "Operator-Chosen-1" || again.RedisPassword == "change-me" || again.DeepSeekAPIKey != "sk-test" || again.JWTSecret != s.JWTSecret {
		t.Fatal(err, generated, again.AdminPassword)
	}
	if generated, _ = EnsureSecretsWith(path, SecretInputs{}); generated != nil {
		t.Fatal("a complete file is left untouched", generated)
	}
}

func TestClusterUpWizardAsksNodesPasswordsAndServicePassword(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil || runtime.GOOS == "windows" {
		t.Skip("bash shims")
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	fake, env := fakeTools(t, root)
	state := filepath.Join(fake, "state")
	env = append(env, "TORCHLINK_SSH_PASSWORD=", "TORCHLINK_FORCE_INTERACTIVE=1")
	up := func(stdin string, extra ...string) (string, error) {
		cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts", "cluster-up.sh"), "--state-dir", state, "--yes"}, extra...)...)
		cmd.Env, cmd.Dir, cmd.Stdin = env, root, strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	answers := strings.Join([]string{
		"",       // cluster name: torchlink
		"2", "3", // two nodes are refused, then three
		"10.0.0.1", "10.0.0.1", "10.0.0.2", "bad", "10.0.0.3", // duplicate and invalid addresses are asked again
		"n",    // no video module
		"y",    // capacity module on
		"", "", // SSH user root, port 22
		"2", // one password per node
		"Node-One-Pw", "Node-Two-Pw", "Node-Three-Pw",
		"short",                            // rejected service password
		"Svc-Unified-42", "Svc-Unified-42", // unified service password, confirmed
		"", // no DeepSeek key
	}, "\n") + "\n"
	out, err := up(answers)
	if err != nil {
		t.Fatal(err, out)
	}
	for _, want := range []string{"2 个节点无法形成", "该 IP 已输入过", "不是有效的 IP 地址", "密码不符合要求"} {
		if !strings.Contains(out, want) {
			t.Errorf("wizard output lacks %q", want)
		}
	}
	inv, err := Load(filepath.Join(state, "inventory.yaml"))
	if err != nil || inv.Name != "torchlink" || len(inv.Nodes) != 3 || inv.Nodes[2].Address != "10.0.0.3" || inv.Video.Node != "" || inv.Capacity.Node != "n3" {
		t.Fatalf("generated inventory: %v %+v", err, inv)
	}
	stdin, _ := os.ReadFile(filepath.Join(fake, "ssh-stdin.log"))
	if string(stdin) != "10.0.0.1=Node-One-Pw\n10.0.0.2=Node-Two-Pw\n10.0.0.3=Node-Three-Pw\n" {
		t.Fatalf("per-node passwords on stdin: %q", stdin)
	}
	s, err := LoadSecrets(filepath.Join(state, "secrets.yaml"))
	if err != nil || s.PostgresPassword != "Svc-Unified-42" || s.RedisPassword != "Svc-Unified-42" || s.ClickHousePassword != "Svc-Unified-42" || s.MinIORootPassword != "Svc-Unified-42" || s.AdminPassword != "Svc-Unified-42" || s.EMQXDashboardPassword != "Svc-Unified-42" || s.JWTSecret == "Svc-Unified-42" || len(s.JWTSecret) < 32 {
		t.Fatalf("service passwords: %v %+v", err, s)
	}
	conf, _ := os.ReadFile(filepath.Join(state, "ssh.conf"))
	if string(conf) != "user=root\nport=22\n" {
		t.Fatalf("ssh.conf %q", conf)
	}
	calls, _ := os.ReadFile(filepath.Join(fake, "calls.log"))
	for _, secret := range []string{"Node-One-Pw", "Svc-Unified-42"} {
		if strings.Contains(string(calls), secret) || strings.Contains(out, secret) {
			t.Fatalf("%s leaked into a command line or the output", secret)
		}
	}
	if !strings.Contains(string(calls), "deploy@10.0.0.2 cd '/opt/torchlink'") && !strings.Contains(string(calls), "root@10.0.0.2 cd '/opt/torchlink'") {
		t.Fatalf("nodes were not deployed as root:\n%s", calls)
	}
	// Upgrading the same cluster asks nothing: the key logs in, secrets exist.
	_ = os.Remove(filepath.Join(fake, "ssh-stdin.log"))
	env = append(env, "TORCHLINK_FORCE_INTERACTIVE=")
	if out, err = up("", "--name", "torchlink", "--no-build"); err != nil {
		t.Fatal(err, out)
	}
	if _, err = os.Stat(filepath.Join(fake, "ssh-stdin.log")); err == nil {
		t.Fatal("an upgrade must not ask for SSH passwords again")
	}
	again, _ := LoadSecrets(filepath.Join(state, "secrets.yaml"))
	if again != s {
		t.Fatal("upgrade changed the secrets")
	}
	// The module switch edits the inventory and re-renders without questions.
	if out, err = up("", "--name", "torchlink", "--no-build", "--capacity", "off"); err != nil {
		t.Fatal(err, out)
	}
	if inv, _ = Load(filepath.Join(state, "inventory.yaml")); inv.Capacity.Node != "" {
		t.Fatal("capacity module still on")
	}
	if b, _ := os.ReadFile(filepath.Join(state, "rendered", "n3", "compose.yaml")); strings.Contains(string(b), "capacity-test") {
		t.Fatal("capacity service still rendered")
	}
	// Unattended: a new cluster from --nodes needs the passwords in the environment.
	other := filepath.Join(fake, "other")
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "cluster-up.sh"), "--name", "second", "--state-dir", other, "--nodes", "10.1.0.1,10.1.0.2,10.1.0.3", "--yes", "--no-build")
	cmd.Env, cmd.Dir = append(env, "TORCHLINK_SSH_PASSWORD="), root
	if b, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(b), "TORCHLINK_SSH_PASSWORD") {
		t.Fatal("unattended run without SSH password must explain what is missing", string(b))
	}
}

func TestGenerateInventoryLaysOutValidClusters(t *testing.T) {
	if !reflect.DeepEqual(DefaultImages(), example(t).Images) {
		t.Fatal("generated inventories must use the example's pinned images")
	}
	for n := 3; n <= 8; n++ {
		var addrs []string
		for i := 1; i <= n; i++ {
			addrs = append(addrs, fmt.Sprintf("192.168.1.%d", 10+i))
		}
		inv, err := GenerateInventory(GenerateOptions{Name: "torchlink", Addresses: addrs, Video: true, Capacity: true})
		if err != nil {
			t.Fatalf("%d nodes: %v", n, err)
		}
		files, err := Render(inv, testSecrets())
		if err != nil {
			t.Fatalf("%d nodes render: %v", n, err)
		}
		wantReplicas := map[bool]int{true: 3, false: 2}[n == 3 || n >= 6]
		if len(inv.ClickHouse.Shards[0]) != wantReplicas || inv.Video.Node != fmt.Sprintf("n%d", n) || len(files) == 0 {
			t.Fatalf("%d nodes layout: %+v", n, inv.ClickHouse)
		}
		b, _ := MarshalInventory(inv)
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "i.yaml"), b, 0o600)
		back, err := Load(filepath.Join(dir, "i.yaml"))
		if err != nil || !reflect.DeepEqual(back.Nodes, inv.Nodes) || back.Platform.Roles["processor"].Nodes == nil {
			t.Fatalf("%d nodes round trip: %v", n, err)
		}
	}
	for _, bad := range [][]string{{"10.0.0.1", "10.0.0.2"}, {"10.0.0.1", "10.0.0.1", "10.0.0.2"}, {"10.0.0.1", "10.0.0.2", "host"}} {
		if _, err := GenerateInventory(GenerateOptions{Name: "x", Addresses: bad}); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestUnifiedServicePasswordRules(t *testing.T) {
	for _, bad := range []string{"short", "has space1", "quote'pass1", "dollar$pass1"} {
		if ValidateServicePassword(bad) == nil {
			t.Fatal("accepted", bad)
		}
	}
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if _, err := EnsureSecretsWith(path, SecretInputs{ServicePassword: "jokerboozp", DeepSeekAPIKey: "sk-abc"}); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadSecrets(path)
	if s.PostgresSuperuserPassword == "jokerboozp" || s.PostgresReplicationPassword == "jokerboozp" || s.ServiceAdminPassword != "jokerboozp" || s.DeepSeekAPIKey != "sk-abc" || s.BackupToken == "jokerboozp" || s.HarnessToken == "jokerboozp" {
		t.Fatal("custom tool password must not replace internal credentials")
	}
	// The same password again is fine; a different one would not reach the
	// deployed databases, so it is refused.
	if _, err := EnsureSecretsWith(path, SecretInputs{ServicePassword: "jokerboozp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSecretsWith(path, SecretInputs{ServicePassword: "another-pass"}); !errors.Is(err, ErrServicePasswordConflict) {
		t.Fatal("a different service password must be refused", err)
	}
}

func TestCapacityModuleRendersBesideThePlatform(t *testing.T) {
	inv := example(t)
	s := testSecrets()
	if inv.Capacity.Node != "n4" {
		t.Fatal("the example deploys the capacity module by default")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "inv.yaml")
	src, _ := os.ReadFile(filepath.Join("..", "..", "deploy", "cluster", "inventory.example.yaml"))
	_ = os.WriteFile(path, src, 0o600)
	if node, err := SetCapacity(path, false); err != nil || node != "" {
		t.Fatal(node, err)
	}
	off, err := Load(path)
	if err != nil || off.Capacity.Node != "" {
		t.Fatal("switch off", err)
	}
	files, err := Render(off, s)
	if err != nil || strings.Contains(string(files["n4/compose.yaml"]), "capacity-test") || strings.Contains(string(files["n1/compose.yaml"]), "IOT_OPS_CAPACITY_URL") {
		t.Fatal("a switched-off module must not render", err)
	}
	if node, err := SetCapacity(path, true); err != nil || node != "n4" {
		t.Fatal(node, err)
	}
	edited, _ := os.ReadFile(path)
	if !strings.Contains(string(edited), "# Cluster inventory: the single source") || !strings.HasSuffix(string(edited), "capacity: {node: n4}\n") || strings.Count(string(edited), "capacity:") != 1 {
		t.Fatal("switch must keep comments and hold one entry")
	}
	on, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	files, err = Render(on, s)
	if err != nil {
		t.Fatal(err)
	}
	n4 := string(files["n4/compose.yaml"])
	for _, want := range []string{"/app/capacity-test", "--self", "IOT_CAPACITY_SERVICE_TOKEN: ${IOT_OPS_CAPACITY_TOKEN}", "api@api-n1=http://10.0.0.11:8081/metrics", "processor@processor-n3=http://10.0.0.13:8102/metrics", "n4=http://10.0.0.14:9100/metrics", "IOT_CAPACITY_API_URL: http://127.0.0.1:18181"} {
		if !strings.Contains(n4, want) {
			t.Fatalf("capacity service lacks %q:\n%s", want, n4)
		}
	}
	if !strings.Contains(string(files["n1/compose.yaml"]), "IOT_OPS_CAPACITY_URL: http://10.0.0.14:7080") || !strings.Contains(string(files["n1/.env"]), "IOT_OPS_CAPACITY_TOKEN="+s.CapacityToken) || !strings.Contains(string(files["n4/.env"]), "IOT_OPS_CAPACITY_TOKEN=") {
		t.Fatal("api instances need the module address and token")
	}
	if strings.Contains(n4, s.CapacityToken) || strings.Contains(n4, s.PostgresPassword) {
		t.Fatal("secrets leaked into compose.yaml")
	}
	if _, err = SetCapacity(path, false); err != nil {
		t.Fatal(err)
	}
	if off, _ := Load(path); off.Capacity.Node != "" {
		t.Fatal("switch off")
	}
}

func TestClusterRendersAlertingAndEntryTLS(t *testing.T) {
	inv := example(t)
	s := testSecrets()
	files, err := Render(inv, s)
	if err != nil {
		t.Fatal(err)
	}
	mon := inv.Monitoring.Node
	prom := string(files[mon+"/prometheus/prometheus.yml"])
	rules := string(files[mon+"/prometheus/alerts.yml"])
	if !strings.Contains(prom, "rule_files: [/etc/prometheus/alerts.yml]") || !strings.Contains(prom, `targets: ["127.0.0.1:9093"]`) {
		t.Fatalf("prometheus config lacks rules or alertmanager:\n%s", prom)
	}
	if !strings.Contains(rules, "DeadLetterPublished") || !strings.Contains(rules, `up{job="platform"}`) || strings.Contains(rules, `job="iot-platform"`) {
		t.Fatal("cluster rules must be the platform rules with the cluster job name")
	}
	if c := string(files[mon+"/compose.yaml"]); !strings.Contains(c, "prom/alertmanager:v0.34.1") || strings.Contains(string(files[mon+"/alertmanager/alertmanager.yml"]), "webhook") {
		t.Fatal("alertmanager without a webhook keeps alerts local")
	}
	if !strings.Contains(string(files[inv.Platform.Roles["api"].Nodes[0]+"/compose.yaml"]), "IOT_OPS_ALERTMANAGER_URL: http://") {
		t.Fatal("platform must reach alertmanager")
	}
	web := inv.Platform.Web.Nodes[0]
	if _, ok := files[web+"/tls/tls.crt"]; ok || strings.Contains(string(files[web+"/compose.yaml"]), "8443") {
		t.Fatal("TLS must stay off without a certificate")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"iot.example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "tls.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "tls.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	s.TLSCertFile, s.TLSKeyFile, s.AlertWebhookURL = filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), "https://alerts.example.test/hook"
	files, err = Render(inv, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(files[web+"/tls/tls.key"]) == 0 || !strings.Contains(string(files[web+"/compose.yaml"]), "./tls:/etc/torchlink/tls:ro") || !strings.Contains(string(files[web+"/compose.yaml"]), "IOT_WEB_HTTPS_PORT: \"8443\"") {
		t.Fatal("web nodes must get HTTPS")
	}
	emqx := inv.EMQX.Nodes[0]
	if len(files[emqx+"/tls/tls.crt"]) == 0 || !strings.Contains(string(files[emqx+"/compose.yaml"]), "./tls:/opt/emqx/etc/torchlink-tls:ro") {
		t.Fatal("EMQX nodes must get the certificate for MQTTS and WSS")
	}
	if !strings.Contains(string(files["deploy-plan.txt"]), "entry mqtts ssl://") || !strings.Contains(string(files["deploy-plan.txt"]), "entry web-https https://") {
		t.Fatal("deploy plan must list the TLS entry points")
	}
	if string(files[mon+"/alertmanager/webhook-url"]) != s.AlertWebhookURL || !strings.Contains(string(files[mon+"/alertmanager/alertmanager.yml"]), "url_file: /etc/alertmanager/webhook-url") {
		t.Fatal("webhook receiver")
	}
	s.TLSKeyFile = filepath.Join(dir, "tls.crt")
	if _, err = Render(inv, s); err == nil || !strings.Contains(err.Error(), "matching") {
		t.Fatalf("mismatched key accepted: %v", err)
	}
	s.TLSKeyFile, s.AlertWebhookURL = filepath.Join(dir, "tls.key"), "ftp://x"
	if _, err = Render(inv, s); err == nil {
		t.Fatal("invalid webhook accepted")
	}
}

// RustFS, Prometheus with Alertmanager and the media servers can each run on
// several nodes: distributed RustFS behind the local load balancers,
// independent Prometheus replicas feeding one Alertmanager cluster, and
// standby media servers the live module and the HLS proxy fail over to.
func TestHighAvailabilityPlacementsRender(t *testing.T) {
	inv := example(t)
	inv.RustFS = PoolSpec{Nodes: []string{"n1", "n2", "n3", "n4"}}
	inv.Monitoring = PoolSpec{Nodes: []string{"n3", "n4"}}
	inv.Video = PoolSpec{Nodes: []string{"n4", "n3"}}
	files, err := Render(inv, testSecrets())
	if err != nil {
		t.Fatal(err)
	}
	n3 := string(files["n3/compose.yaml"])
	for _, want := range []string{
		"RUSTFS_VOLUMES: http://rustfs{1...4}:9002/data", "- rustfs1:10.0.0.11", "- rustfs4:10.0.0.14", "rustfs-data:/data",
		"--cluster.peer=10.0.0.14:9094", "--cluster.advertise-address=10.0.0.13:9094",
		"IOT_VIDEO_MEDIA_SERVER_ID: iot-cluster-media-2",
		// Spilo's monitor stays off the web port.
		"bg_mon.port: 8009",
		// Patroni members advertise the node address, not 127.0.1.1.
		"connect_address: 10.0.0.13:5432", "connect_address: 10.0.0.13:8008",
	} {
		if !strings.Contains(n3, want) {
			t.Fatalf("n3 compose lacks %q:\n%s", want, n3)
		}
	}
	platform := string(files["n1/compose.yaml"])
	for _, want := range []string{
		"IOT_MINIO_ENDPOINT: 127.0.0.1:18183", "IOT_OPS_PROMETHEUS_URL: http://127.0.0.1:18190", "IOT_OPS_ALERTMANAGER_URL: http://127.0.0.1:18193",
		"IOT_VIDEO_MEDIA_API_URL: http://10.0.0.14:80", "IOT_VIDEO_MEDIA_SERVER_ID: iot-cluster-media-1",
		"IOT_VIDEO_MEDIA_STANDBY_URLS: http://10.0.0.13:80", "IOT_VIDEO_MEDIA_STANDBY_IDS: iot-cluster-media-2", "IOT_GB28181_MEDIA_STANDBY_IPS: 10.0.0.13",
	} {
		if !strings.Contains(platform, want) {
			t.Fatalf("platform env lacks %q", want)
		}
	}
	prom := string(files["n3/prometheus/prometheus.yml"])
	for _, want := range []string{"replica: n3", "regex: replica", `"10.0.0.13:9093", "10.0.0.14:9093"`} {
		if !strings.Contains(prom, want) {
			t.Fatalf("prometheus config lacks %q:\n%s", want, prom)
		}
	}
	var parsed struct {
		Global struct {
			ExternalLabels map[string]string `yaml:"external_labels"`
		} `yaml:"global"`
		Alerting struct {
			Relabel       []map[string]string `yaml:"alert_relabel_configs"`
			Alertmanagers []struct {
				StaticConfigs []struct {
					Targets []string `yaml:"targets"`
				} `yaml:"static_configs"`
			} `yaml:"alertmanagers"`
		} `yaml:"alerting"`
	}
	if err = yaml.Unmarshal([]byte(prom), &parsed); err != nil || parsed.Global.ExternalLabels["replica"] != "n3" || len(parsed.Alerting.Relabel) != 1 || len(parsed.Alerting.Alertmanagers[0].StaticConfigs[0].Targets) != 2 {
		t.Fatalf("prometheus config does not parse as intended: %+v %v", parsed, err)
	}
	lb := string(files["n2/lb/haproxy.cfg"])
	for _, want := range []string{"bind 127.0.0.1:18183", "server rustfs-n4 10.0.0.14:9002", "option httpchk GET /health", "bind 127.0.0.1:18190", "bind 127.0.0.1:18180", "balance first", "server video-n4 10.0.0.14:80\n  server video-n3 10.0.0.13:80"} {
		if !strings.Contains(lb, want) {
			t.Fatalf("haproxy lacks %q:\n%s", want, lb)
		}
	}
	for _, n := range inv.Nodes {
		if !strings.Contains(string(files[n.Name+"/compose.yaml"]), "IOT_VIDEO_UPSTREAM: 127.0.0.1:18180") && strings.Contains(string(files[n.Name+"/compose.yaml"]), "IOT_VIDEO_UPSTREAM") {
			t.Fatalf("%s: HLS must follow the media failover", n.Name)
		}
	}
	// The local balancers are rendered for these pools even with external
	// API and gateway balancers.
	inv.Platform.InternalURL, inv.Platform.GatewayURL = "http://10.0.0.10:8081", "http://10.0.0.10:8082"
	files, err = Render(inv, testSecrets())
	if err != nil || !strings.Contains(string(files["n1/lb/haproxy.cfg"]), "bind 127.0.0.1:18183") || strings.Contains(string(files["n1/lb/haproxy.cfg"]), "bind 127.0.0.1:18181") {
		t.Fatalf("pool balancers with external API balancers: %v\n%s", err, files["n1/lb/haproxy.cfg"])
	}
}
