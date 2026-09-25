package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"       /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"fmt"               /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"mime/multipart"    /* 执行当前语句并推进处理流程。 */
	"net"               /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"os"                /* 执行当前语句并推进处理流程。 */
	"os/exec"           /* 执行当前语句并推进处理流程。 */
	"path/filepath"     /* 执行当前语句并推进处理流程。 */
	"strconv"           /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolbuild"   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolruntime" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker"  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestGoFunctionsUploadAndListener(t *testing.T) { /* 定义 TestGoFunctionsUploadAndListener 函数。 */
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute) /* 更新 cancel 的值。 */
	defer cancel()                                                          /* 安排函数结束时执行清理。 */
	root := t.TempDir()                                                     /* 更新 root 的值。 */
	repo := memory.NewRepository()                                          /* 更新 repo 的值。 */
	archive, err := local.NewArchive(root)                                  /* 更新 err 的值。 */
	if err != nil {                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                           /* 更新 log 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	if err := engine.Start(ctx); err != nil {                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                          /* 更新 cfg 的值。 */
	cfg.DataDir = root                                                                                            /* 更新 cfg.DataDir 的值。 */
	api := New(cfg, engine, metrics.New(), log)                                                                   /* 更新 api 的值。 */
	ingested := make(chan model.RawMessage, 16)                                                                   /* 更新 ingested 的值。 */
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error { /* 更新 listeners 的值。 */
		_, _, err := engine.IngestRaw(ctx, raw) /* 更新 err 的值。 */
		if err == nil {                         /* 判断条件并选择处理分支。 */
			ingested <- raw /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return err /* 返回当前处理结果。 */
	}, log) /* 结束当前表达式或代码块。 */
	api.SetProtocolListeners(listeners)                                                          /* 执行当前语句并推进处理流程。 */
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))          /* 更新 assets 的值。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if strings.HasPrefix(r.URL.Path, "/api/") { /* 判断条件并选择处理分支。 */
			api.Handler().ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			assets.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close()                                                                                                                /* 安排函数结束时执行清理。 */
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)                                                        /* 更新 _ 的值。 */
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)                                                           /* 更新 _ 的值。 */
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Name: "Go 函数产品", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	upload := func(auth, name string, code []byte, fields map[string]string, want int) model.ProtocolRelease { /* 更新 upload 的值。 */
		t.Helper()                                /* 执行当前语句并推进处理流程。 */
		var body bytes.Buffer                     /* 声明 body。 */
		form := multipart.NewWriter(&body)        /* 更新 form 的值。 */
		f, _ := form.CreateFormFile("file", name) /* 更新 _ 的值。 */
		f.Write(code)                             /* 执行当前语句并推进处理流程。 */
		form.WriteField("productId", "product")   /* 执行当前语句并推进处理流程。 */
		for k, v := range fields {                /* 循环处理当前数据。 */
			form.WriteField(k, v) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		form.Close()                                                                             /* 执行当前语句并推进处理流程。 */
		req := httptest.NewRequest("POST", "/api/v2/protocols/functions/source-releases", &body) /* 更新 req 的值。 */
		req.Header.Set("Authorization", "Bearer "+auth)                                          /* 执行当前语句并推进处理流程。 */
		req.Header.Set("Content-Type", form.FormDataContentType())                               /* 执行当前语句并推进处理流程。 */
		w := httptest.NewRecorder()                                                              /* 更新 w 的值。 */
		api.Handler().ServeHTTP(w, req)                                                          /* 执行当前语句并推进处理流程。 */
		if w.Code != want {                                                                      /* 判断条件并选择处理分支。 */
			t.Fatalf("upload %s: %d want %d: %s", name, w.Code, want, w.Body.String()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		var result struct{ Release model.ProtocolRelease } /* 声明 result。 */
		json.Unmarshal(w.Body.Bytes(), &result)            /* 执行当前语句并推进处理流程。 */
		return result.Release                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// No runtime, JSON samples, metadata or module required for a single Go file.
	upload(viewer, "protocol.go", []byte(protocolbuild.FunctionTemplate), nil, 403)         /* 执行当前语句并推进处理流程。 */
	first := upload(token, "protocol.go", []byte(protocolbuild.FunctionTemplate), nil, 201) /* 更新 first 的值。 */
	// The platform adapter serves repeated requests, so the release stays resident.
	if !strings.HasPrefix(first.Version, "auto-") || first.Artifact["runtime"] != protocolworker.Runtime || first.Artifact["workerMode"] != parser.WorkerModeServe { /* 判断条件并选择处理分支。 */
		t.Fatalf("release %+v", first) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raw := model.RawMessage{MessageID: "raw_functions", TenantID: "tenant", ProductID: "product", DeviceID: "device", Protocol: "functions", PayloadFormat: "hex", Payload: json.RawMessage(`"AA012A"`)} /* 更新 raw 的值。 */
	if _, _, err := engine.IngestRaw(ctx, raw); err != nil {                                                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID) /* 更新 err 的值。 */
	if err != nil || message.Properties["temperature"] != float64(42) {        /* 判断条件并选择处理分支。 */
		t.Fatalf("message %+v %v", message, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	bad := strings.Replace(protocolbuild.FunctionTemplate, "int(data[2])", "99", 1)                                                                   /* 更新 bad 的值。 */
	upload(token, "protocol.go", []byte(bad), nil, 422)                                                                                               /* 执行当前语句并推进处理流程。 */
	upload(token, "protocol.go", []byte(strings.Replace(protocolbuild.FunctionTemplate, "Samples: []Sample{{", "Samples: []Sample{/*", 1)), nil, 422) /* 执行当前语句并推进处理流程。 */
	upload(token, "protocol.go", []byte(protocolbuild.FunctionTemplate), map[string]string{"capabilities": `["decode"]`}, 422)                        /* 执行当前语句并推进处理流程。 */
	// Go samples are mandatory; supplied expected event/tag/time fields must
	// actually be checked rather than silently accepted as metadata.
	upload(token, "protocol.go", []byte(`package main
func Protocol() Definition {return Definition{Decode:func([]byte,Context)(Message,error){return properties(map[string]any{}),nil}}}`), nil, 422)
	for _, field := range []string{`Event:map[string]any{"type":"WRONG"}`, `Tags:map[string]string{"site":"WRONG"}`, `Timestamp:123`} { /* 循环处理当前数据。 */
		code := `package main
func Protocol() Definition {return Definition{
 Decode:func([]byte,Context)(Message,error){return properties(map[string]any{"temperature":42}),nil},
 Samples:[]Sample{{Data:[]byte{1},Want:Message{MessageType:"PROPERTY_REPORT",` + field + `}}},
}}`
		upload(token, "protocol.go", []byte(code), nil, 422) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "product") /* 更新 err 的值。 */
	if err != nil || binding.Version != first.Version {                      /* 判断条件并选择处理分支。 */
		t.Fatalf("failed upload replaced binding %+v %v", binding, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Downloaded template is a complete independently buildable Go project.
	req := httptest.NewRequest("GET", "/api/v2/protocol-source-template?format=go-functions&kind=tcp", nil) /* 更新 req 的值。 */
	req.Header.Set("Authorization", "Bearer "+token)                                                        /* 执行当前语句并推进处理流程。 */
	w := httptest.NewRecorder()                                                                             /* 更新 w 的值。 */
	api.Handler().ServeHTTP(w, req)                                                                         /* 执行当前语句并推进处理流程。 */
	if w.Code != 200 {                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	sources, err := protocolbuild.Sources("template.zip", w.Body.Bytes()) /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	project := t.TempDir()            /* 更新 project 的值。 */
	var wrapped bytes.Buffer          /* 声明 wrapped。 */
	zw := zip.NewWriter(&wrapped)     /* 更新 zw 的值。 */
	for name, data := range sources { /* 循环处理当前数据。 */
		if strings.HasSuffix(name, ".json") { /* 判断条件并选择处理分支。 */
			t.Fatal("template requires JSON", name) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		os.WriteFile(filepath.Join(project, name), data, 0600) /* 执行当前语句并推进处理流程。 */
		f, _ := zw.Create("my-protocol/" + name)               /* 更新 _ 的值。 */
		f.Write(data)                                          /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	zw.Close()                                                 /* 执行当前语句并推进处理流程。 */
	command := exec.CommandContext(ctx, "go", "test", "./...") /* 更新 command 的值。 */
	command.Dir = project                                      /* 更新 command.Dir 的值。 */
	if output, err := command.CombinedOutput(); err != nil {   /* 判断条件并选择处理分支。 */
		t.Fatalf("standalone template: %v %s", err, output) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second := upload(token, "project.zip", wrapped.Bytes(), map[string]string{"version": "2.0.0"}, 201) /* 更新 second 的值。 */
	if len(second.Capabilities) != 3 {                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(second.Capabilities) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	upload(token, "project.zip", wrapped.Bytes(), map[string]string{"version": "2.0.0"}, 409) /* 执行当前语句并推进处理流程。 */
	// Full template's samples exercise ingress, decode, ACK matching and encode.
	// Also prove real sockets still use inventory, archive and StandardMessage.
	free, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	port := free.Addr().(*net.TCPAddr).Port                                                                                                                                                                                                                        /* 更新 port 的值。 */
	free.Close()                                                                                                                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	profile := model.DeviceAccessProfile{ID: "functions-tcp", ProductID: "product", ProtocolID: "functions", ProtocolVersion: second.Version, Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, TimeoutMs: 2000, Enabled: true, AutoRegister: true} /* 更新 profile 的值。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, profile, 201)                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	listeners.Start(ctx)                                                                                                                                                                                                                                           /* 执行当前语句并推进处理流程。 */
	var conn net.Conn                                                                                                                                                                                                                                              /* 声明 conn。 */
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {                                                                                                                                                                                /* 循环处理当前数据。 */
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Millisecond*100) /* 更新 err 的值。 */
		if err == nil {                                                                           /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(20 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer conn.Close()                                           /* 安排函数结束时执行清理。 */
	frame := []byte{0xAA, 1, 7, 42, 0xDC}                        /* 更新 frame 的值。 */
	conn.Write(frame[:2])                                        /* 执行当前语句并推进处理流程。 */
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) /* 执行当前语句并推进处理流程。 */
	reply := make([]byte, 5)                                     /* 更新 reply 的值。 */
	if _, err := conn.Read(reply); err == nil {                  /* 判断条件并选择处理分支。 */
		t.Fatal("half frame received ACK") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	conn.Write(append(frame[2:], frame...))               /* 执行当前语句并推进处理流程。 */
	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) /* 执行当前语句并推进处理流程。 */
	for i := 0; i < 2; i++ {                              /* 循环处理当前数据。 */
		if _, err := io.ReadFull(conn, reply); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if !bytes.Equal(reply, []byte{0xAA, 2, 7, 42, 0xDD}) { /* 判断条件并选择处理分支。 */
			t.Fatalf("ACK %x", reply) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		select { /* 根据条件选择处理路径。 */
		case raw := <-ingested: /* 处理当前分支。 */
			message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID)                     /* 更新 err 的值。 */
			if err != nil || message.DeviceID != "7" || message.Properties["temperature"] != float64(42) { /* 判断条件并选择处理分支。 */
				t.Fatalf("TCP message %+v %v", message, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		case <-time.After(5 * time.Second): /* 处理当前分支。 */
			t.Fatal("no archived frame") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	// Downlink is encoded by the uploaded Go function, sent on the live socket,
	// then acknowledged only when the matching device response arrives.
	commandDone := make(chan error, 1) /* 更新 commandDone 的值。 */
	go func() {                        /* 执行当前语句并推进处理流程。 */
		result, err := listeners.Command(ctx, "tenant", profile.ID, "7", map[string]any{"type": "ping"}) /* 更新 err 的值。 */
		if err == nil && result["status"] != "acknowledged" {                                            /* 判断条件并选择处理分支。 */
			err = fmt.Errorf("command status %v", result) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		commandDone <- err /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) /* 执行当前语句并推进处理流程。 */
	if _, err := io.ReadFull(conn, reply); err != nil {   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !bytes.Equal(reply, []byte{0xAA, 3, 7, 1, 0xB5}) { /* 判断条件并选择处理分支。 */
		t.Fatalf("downlink %x", reply) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	conn.Write([]byte{0xAA, 2, 7, 1, 0xB4}) /* 执行当前语句并推进处理流程。 */
	select {                                /* 根据条件选择处理路径。 */
	case err := <-commandDone: /* 处理当前分支。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	case <-time.After(5 * time.Second): /* 处理当前分支。 */
		t.Fatal("command not acknowledged") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case raw := <-ingested: /* 处理当前分支。 */
		message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID) /* 更新 err 的值。 */
		if err != nil || message.MessageType != model.CommandReply {               /* 判断条件并选择处理分支。 */
			t.Fatalf("ACK message %+v %v", message, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	case <-time.After(5 * time.Second): /* 处理当前分支。 */
		t.Fatal("ACK not archived") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Invalid checksum must not be acknowledged or archived.
	frame[4] = 0                                          /* 更新 frame[4] 的值。 */
	conn.Write(frame)                                     /* 执行当前语句并推进处理流程。 */
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) /* 执行当前语句并推进处理流程。 */
	if _, err := conn.Read(reply); err == nil {           /* 判断条件并选择处理分支。 */
		t.Fatal("bad checksum acknowledged") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case <-ingested: /* 处理当前分支。 */
		t.Fatal("bad checksum archived") /* 验证实际结果符合预期。 */
	default: /* 处理当前分支。 */
	} /* 结束当前表达式或代码块。 */
	t.Run("browser", func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
		if os.Getenv("IOT_TEST_BROWSER") == "" { /* 判断条件并选择处理分支。 */
			t.Skip("Chrome not configured") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		source := filepath.Join(t.TempDir(), "protocol.go")                                                                               /* 更新 source 的值。 */
		os.WriteFile(source, []byte(protocolbuild.FunctionTemplate), 0600)                                                                /* 执行当前语句并推进处理流程。 */
		os.WriteFile(source+".invalid.go", []byte("package main\nfunc Protocol() Definition { invalid }"), 0600)                          /* 执行当前语句并推进处理流程。 */
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "go-functions-check.mjs")) /* 更新 command 的值。 */
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token, "IOT_TEST_SOURCE_GO="+source)          /* 更新 command.Env 的值。 */
		if output, err := command.CombinedOutput(); err != nil {                                                                          /* 判断条件并选择处理分支。 */
			t.Fatalf("browser: %v %s", err, output) /* 验证实际结果符合预期。 */
		} else { /* 结束当前表达式或代码块。 */
			t.Log(string(output)) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	if _, err := repo.GetProtocolRelease(ctx, "tenant", "functions-browser", "invalid-browser"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("browser compile failure published") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
