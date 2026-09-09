package platformapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/connector"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

func TestPlatformProcessHelper(t *testing.T) {
	if role := os.Getenv("IOT_TEST_APP_ROLE"); role != "" {
		Run(role)
	}
}

// The Kafka address MUST point to a disposable broker: production topic and
// consumer-group names are deliberately exercised by the actual startup code.
func TestSplitProcessesPostgresKafkaRecovery(t *testing.T) {
	dsn, broker := os.Getenv("IOT_TEST_POSTGRES_DSN"), os.Getenv("IOT_TEST_DISPOSABLE_KAFKA")
	if dsn == "" || broker == "" {
		t.Skip("requires PostgreSQL and an explicitly disposable Kafka broker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("initialize test database")
	}
	defer admin.Close()
	schema := fmt.Sprintf("gateway_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal("create isolated schema")
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE") }()
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		t.Fatal("test requires URL PostgreSQL DSN")
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	isolatedDSN := u.String()
	address := func() string {
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		value := listener.Addr().String()
		listener.Close()
		return value
	}
	apiAddr, gatewayAddr := address(), address()
	root := t.TempDir()
	baseEnv := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "IOT_") && !strings.HasPrefix(entry, "DEEPSEEK_") {
			baseEnv = append(baseEnv, entry)
		}
	}
	baseEnv = append(baseEnv, "IOT_POSTGRES_DSN="+isolatedDSN, "IOT_KAFKA_BROKERS="+broker, "IOT_JWT_SECRET=isolated-split-process-jwt-key-32", "IOT_ADMIN_USER=test-admin", "IOT_ADMIN_PASSWORD=isolated-process-password", "IOT_ADMIN_TENANTS=t", "IOT_DATA_DIR="+root, "IOT_ACCESS_GATEWAY_URL=http://"+gatewayAddr)
	start := func(role, addr string) func() {
		executable, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		command := exec.CommandContext(ctx, executable, "-test.run=^TestPlatformProcessHelper$")
		command.Env = append(append([]string{}, baseEnv...), "IOT_TEST_APP_ROLE="+role, "IOT_HTTP_ADDR="+addr)
		if role == "gateway" {
			command.Env = append(command.Env, "IOT_AI_PROVIDER=invalid-unused-provider", "IOT_WEAVIATE_URL=http://127.0.0.1:1")
		}
		log, e := os.OpenFile(filepath.Join(root, role+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		command.Stdout, command.Stderr = log, log
		if e = command.Start(); e != nil {
			log.Close()
			t.Fatal(e)
		}
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = command.Process.Kill()
			_ = command.Wait()
			_ = log.Close()
		}
		t.Cleanup(stop)
		client := http.Client{Timeout: time.Second}
		for i := 0; i < 100; i++ {
			response, e := client.Get("http://" + addr + "/health/live")
			if e == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					return stop
				}
			}
			select {
			case <-ctx.Done():
				t.Fatal("process startup deadline")
			case <-time.After(100 * time.Millisecond):
			}
		}
		t.Fatal("process startup failed; inspect isolated process log")
		return stop
	}
	stopGateway := start("gateway", gatewayAddr)
	stopAPI := start("api", apiAddr)
	client := http.Client{Timeout: 15 * time.Second}
	request := func(address, path, token string, body any, headers map[string]string, want int, output any) {
		t.Helper()
		encoded, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		req, e := http.NewRequestWithContext(ctx, "POST", "http://"+address+path, bytes.NewReader(encoded))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		response, e := client.Do(req)
		if e != nil {
			t.Fatalf("request failed: %s", path)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("%s status=%d want=%d", path, response.StatusCode, want)
		}
		if output != nil {
			if e = json.NewDecoder(response.Body).Decode(output); e != nil {
				t.Fatal(e)
			}
		} else {
			_, _ = io.Copy(io.Discard, response.Body)
		}
	}
	var login struct {
		AccessToken string `json:"accessToken"`
	}
	request(apiAddr, "/api/v1/auth/login", "", map[string]string{"username": "test-admin", "password": "wrong", "tenantId": "t"}, nil, 401, nil)
	request(apiAddr, "/api/v1/auth/login", "", map[string]string{"username": "test-admin", "password": "isolated-process-password", "tenantId": "t"}, nil, 200, &login)
	request(gatewayAddr, "/api/v1/auth/login", "", map[string]string{}, nil, 404, nil)
	q := onboarding.Request{ProductID: "p", ProductName: "process product", DeviceID: "d", Name: "process device", Type: connector.HTTP, MessageKind: "property", Payload: json.RawMessage(`{"id":"preview","timestamp":1000,"data":{"temperature":42}}`)}
	var preview connector.Result
	request(apiAddr, "/api/v1/onboarding/test", login.AccessToken, q, nil, 200, &preview)
	if !preview.Success {
		t.Fatal("onboarding preview failed")
	}
	q.TestToken = preview.TestToken
	var created onboarding.Result
	request(apiAddr, "/api/v1/onboarding", login.AccessToken, q, nil, 201, &created)
	headers := map[string]string{"X-Device-Key": created.Credential.AccessKey, "X-Device-Secret": created.Credential.Secret}
	path := "/api/v1/device-ingest/standard/t/p/d/property"
	request(apiAddr, path, "", map[string]any{"id": "bad", "data": map[string]int{"temperature": 42}}, nil, 401, nil)
	var receipt struct {
		MessageID string `json:"messageId"`
	}
	request(apiAddr, path, "", map[string]any{"id": "first", "timestamp": time.Now().UnixMilli(), "data": map[string]int{"temperature": 42}}, headers, 202, &receipt)
	repo, e := postgres.New(ctx, isolatedDSN)
	if e != nil {
		t.Fatal("open test repository")
	}
	waitParsed := func(id string) {
		t.Helper()
		for {
			m, e := repo.GetStandardMessageByRaw(ctx, "t", id)
			if e == nil && m.Properties["temperature"] == float64(42) {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("Kafka parsing deadline")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	waitParsed(receipt.MessageID)
	stopAPI()
	request(gatewayAddr, path, "", map[string]any{"id": "while-api-down", "timestamp": time.Now().UnixMilli(), "data": map[string]int{"temperature": 42}}, headers, 202, &receipt)
	if _, err = repo.GetRawIndex(ctx, "t", receipt.MessageID); err != nil {
		t.Fatal("gateway did not persist raw while API stopped")
	}
	stopAPI = start("api", apiAddr)
	defer stopAPI()
	waitParsed(receipt.MessageID)
	stopGateway()
	request(apiAddr, path, "", map[string]any{"id": "gateway-down", "timestamp": time.Now().UnixMilli(), "data": map[string]int{"temperature": 42}}, headers, 503, nil)
	messages, count, e := repo.ListDeviceMessages(ctx, "t", "d", model.PropertyReport, 10, 0)
	if e != nil || count != 2 || len(messages) != 2 {
		t.Fatal("unexpected message count", count, e)
	}
}
