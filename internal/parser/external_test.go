package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"os/exec"       /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"runtime"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestExternalGoProtocolParserRunsWorkerAndEnforcesEnvelopeIdentity(t *testing.T) { /* 定义 TestExternalGoProtocolParserRunsWorkerAndEnforcesEnvelopeIdentity 函数。 */
	root := t.TempDir() /* 更新 root 的值。 */
	worker := buildExternalTestWorker(t, root, `package main
import("encoding/json";"os")
func main(){var request struct{Version int; Operation string; Raw struct{Payload json.RawMessage `+"`json:\"payload\"`"+`}};if json.NewDecoder(os.Stdin).Decode(&request)!=nil || request.Version!=2 || request.Operation!="decode"{os.Exit(2)};var body map[string]any;_ = json.Unmarshal(request.Raw.Payload,&body);_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"standardMessage":map[string]any{"messageType":"PROPERTY_REPORT","tenantId":"evil","deviceId":"evil","properties":map[string]any{"temperature":body["temperature"]}}})}`)
	digest := fileDigest(t, worker)                                                                                                                                                                                   /* 更新 digest 的值。 */
	r := NewRegistry(ExternalParser{Root: root})                                                                                                                                                                      /* 更新 r 的值。 */
	m, err := r.ParseWithConfig(GoProtocolParserName, map[string]any{"artifact": map[string]any{"path": filepath.Base(worker), "sha256": digest, "runtime": "go-protocol-v2"}, "timeoutMs": 10000}, model.RawMessage{ /* 更新 err 的值。 */
		MessageID: "raw_external", TenantID: "tenant_001", ProductID: "product_1", DeviceID: "device_1", PayloadFormat: "json", ReceivedAt: 1234, /* 执行当前语句并推进处理流程。 */
		Payload: json.RawMessage(`{"temperature":42}`), /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.MessageType != model.PropertyReport || m.Properties["temperature"] != float64(42) { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected external result: %#v", m) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.TenantID != "tenant_001" || m.DeviceID != "device_1" || m.Parser != GoProtocolParserName { /* 判断条件并选择处理分支。 */
		t.Fatalf("worker changed protected identity or parser metadata: %#v", m) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestExternalGoProtocolParserRejectsChecksumMismatch(t *testing.T) { /* 定义 TestExternalGoProtocolParserRejectsChecksumMismatch 函数。 */
	root := t.TempDir() /* 更新 root 的值。 */
	worker := buildExternalTestWorker(t, root, `package main
import("encoding/json";"os")
func main(){_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"messageType":"PROPERTY_REPORT"})}`)
	_, err := (ExternalParser{Root: root}).ParseWithConfig(model.RawMessage{}, map[string]any{"artifact": map[string]any{"path": filepath.Base(worker), "sha256": "00", "runtime": "go-protocol-v2"}}) /* 更新 err 的值。 */
	if err == nil || !containsAny(err.Error(), "checksum mismatch") {                                                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatalf("expected checksum mismatch, got %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestExternalV2DecodeUsesArchivedSessionState(t *testing.T) { /* 定义 TestExternalV2DecodeUsesArchivedSessionState 函数。 */
	root := t.TempDir() /* 更新 root 的值。 */
	worker := buildExternalTestWorker(t, root, `package main
import("encoding/json";"os")
func main(){var in map[string]any;if json.NewDecoder(os.Stdin).Decode(&in)!=nil{os.Exit(2)};if in["operation"]!="decode" || in["version"]!=float64(2){os.Exit(3)};_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"standardMessage":map[string]any{"messageType":"PROPERTY_REPORT","properties":in["state"]}})}`)
	config := map[string]any{"artifact": map[string]any{"path": filepath.Base(worker), "sha256": fileDigest(t, worker), "runtime": "go-protocol-v2"}} /* 更新 config 的值。 */
	raw := model.RawMessage{MessageID: "raw_state", Metadata: map[string]any{"protocolState": map[string]any{"sequence": float64(17)}}}               /* 更新 raw 的值。 */
	// The persisted JSON representation must reproduce the same context.
	archived, _ := json.Marshal(raw)                        /* 更新 _ 的值。 */
	var replay model.RawMessage                             /* 声明 replay。 */
	_ = json.Unmarshal(archived, &replay)                   /* 更新 _ 的值。 */
	for _, input := range []model.RawMessage{raw, replay} { /* 循环处理当前数据。 */
		message, err := (ExternalParser{Root: root}).ParseWithConfig(input, config) /* 更新 err 的值。 */
		if err != nil || message.Properties["sequence"] != float64(17) {            /* 判断条件并选择处理分支。 */
			t.Fatalf("state decode %+v %v", message, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func buildExternalTestWorker(t *testing.T, root, source string) string { /* 定义 buildExternalTestWorker 函数。 */
	t.Helper()                                                              /* 执行当前语句并推进处理流程。 */
	sourcePath := filepath.Join(root, "worker.go")                          /* 更新 sourcePath 的值。 */
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	name := "worker"               /* 更新 name 的值。 */
	if runtime.GOOS == "windows" { /* 判断条件并选择处理分支。 */
		name += ".exe" /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	path := filepath.Join(root, name)                                       /* 更新 path 的值。 */
	cmd := exec.Command("go", "build", "-trimpath", "-o", path, sourcePath) /* 更新 cmd 的值。 */
	if output, err := cmd.CombinedOutput(); err != nil {                    /* 判断条件并选择处理分支。 */
		t.Fatalf("build external test worker: %v\n%s", err, output) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return path /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func fileDigest(t *testing.T, path string) string { /* 定义 fileDigest 函数。 */
	t.Helper()                  /* 执行当前语句并推进处理流程。 */
	b, err := os.ReadFile(path) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	h := sha256.Sum256(b)           /* 更新 h 的值。 */
	return hex.EncodeToString(h[:]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func containsAny(value string, expected ...string) bool { /* 定义 containsAny 函数。 */
	for _, item := range expected { /* 循环处理当前数据。 */
		if strings.Contains(value, item) { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestExternalRejectsLegacyContract(t *testing.T) { /* 定义 TestExternalRejectsLegacyContract 函数。 */
	for _, runtime := range []string{"", "go-json-lines-v1"} { /* 循环处理当前数据。 */
		_, err := (ExternalParser{}).ParseWithConfig(model.RawMessage{}, map[string]any{"artifact": map[string]any{"path": "worker", "runtime": runtime}}) /* 更新 err 的值。 */
		if err == nil || !strings.Contains(err.Error(), "runtime must be go-protocol-v2") {                                                                /* 判断条件并选择处理分支。 */
			t.Fatalf("legacy runtime %q: %v", runtime, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := decodeExternalMessage([]byte(`{"messageType":"PROPERTY_REPORT"}`)); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("unwrapped legacy response accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
