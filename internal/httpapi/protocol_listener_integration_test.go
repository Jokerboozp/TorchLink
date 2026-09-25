package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"       /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/hex"      /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"io/fs"             /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"mime/multipart"    /* 执行当前语句并推进处理流程。 */
	"net"               /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"os"                /* 执行当前语句并推进处理流程。 */
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
	"iot-platform/internal/onboarding"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolbuild"   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolruntime" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Uses the separately maintained source package and real child processes,
// HTTP publication, sockets and the normal archive/parser path in one host.
func TestGoProtocolListenerSourceHotSwitch(t *testing.T) { /* 定义 TestGoProtocolListenerSourceHotSwitch 函数。 */
	if !protocolbuild.Available() { /* 判断条件并选择处理分支。 */
		t.Skip("Go compiler unavailable") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	root := t.TempDir()                                     /* 更新 root 的值。 */
	repo := memory.NewRepository()                          /* 更新 repo 的值。 */
	archive, err := local.NewArchive(root)                  /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                           /* 更新 log 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                                                  /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(ctx); err != nil {                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                          /* 更新 cfg 的值。 */
	cfg.DataDir = root                                                                                            /* 更新 cfg.DataDir 的值。 */
	cfg.JWTSecret = "listener-test-secret-at-least-32-characters"                                                 /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)                                              /* 更新 api 的值。 */
	ingested := make(chan model.RawMessage, 32)                                                                   /* 更新 ingested 的值。 */
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error { /* 更新 listeners 的值。 */
		_, _, err := engine.IngestRaw(ctx, raw) /* 更新 err 的值。 */
		if err == nil {                         /* 判断条件并选择处理分支。 */
			ingested <- raw /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return err /* 返回当前处理结果。 */
	}, log) /* 结束当前表达式或代码块。 */
	api.SetProtocolListeners(listeners)                                                                               /* 执行当前语句并推进处理流程。 */
	server := httptest.NewServer(api.Handler())                                                                       /* 更新 server 的值。 */
	defer server.Close()                                                                                              /* 安排函数结束时执行清理。 */
	token, _ := api.auth.Issue("tester", "tenant_001", "operator", nil, time.Hour)                                    /* 更新 _ 的值。 */
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant_001", ID: "gb-product", Name: "GB", Status: "ENABLED"}) /* 更新 _ 的值。 */
	packageRoot := filepath.Join("..", "..", "protocol-packages", "gb26875-dahua")                                    /* 更新 packageRoot 的值。 */
	fixture, err := os.ReadFile(filepath.Join(packageRoot, "samples", "cases.json"))                                  /* 更新 err 的值。 */
	if err != nil {                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var samples []protocolPackageCaseV2                      /* 声明 samples。 */
	if err = json.Unmarshal(fixture, &samples); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var frameHex string                                     /* 声明 frameHex。 */
	_ = json.Unmarshal(samples[0].Input.Payload, &frameHex) /* 更新 _ 的值。 */
	frame, _ := hex.DecodeString(frameHex)                  /* 更新 _ 的值。 */
	upload := func(version string, bad bool, status int) {  /* 更新 upload 的值。 */
		t.Helper()                                                                               /* 执行当前语句并推进处理流程。 */
		var packed bytes.Buffer                                                                  /* 声明 packed。 */
		zw := zip.NewWriter(&packed)                                                             /* 更新 zw 的值。 */
		err := filepath.WalkDir(packageRoot, func(path string, d fs.DirEntry, err error) error { /* 更新 err 的值。 */
			if err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if d.IsDir() { /* 判断条件并选择处理分支。 */
				return nil /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			name, _ := filepath.Rel(packageRoot, path) /* 更新 _ 的值。 */
			data, err := os.ReadFile(path)             /* 更新 err 的值。 */
			if err != nil {                            /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if name == "protocol.json" { /* 判断条件并选择处理分支。 */
				data = bytes.ReplaceAll(data, []byte("1.0.0"), []byte(version)) /* 更新 data 的值。 */
			} /* 结束当前表达式或代码块。 */
			if bad && filepath.ToSlash(name) == "samples/operations.json" { /* 判断条件并选择处理分支。 */
				data = []byte(`[]`) /* 更新 data 的值。 */
			} /* 结束当前表达式或代码块。 */
			if version == "1.1.0" && filepath.ToSlash(name) == "gb26875/codec.go" { /* 判断条件并选择处理分支。 */
				data = bytes.ReplaceAll(data, []byte(`"Dahua"`), []byte(`"Dahua-v2"`)) /* 更新 data 的值。 */
			} /* 结束当前表达式或代码块。 */
			f, err := zw.Create(filepath.ToSlash(name)) /* 更新 err 的值。 */
			if err == nil {                             /* 判断条件并选择处理分支。 */
				_, err = f.Write(data) /* 更新 err 的值。 */
			} /* 结束当前表达式或代码块。 */
			return err /* 返回当前处理结果。 */
		}) /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		_ = zw.Close()                                                                                         /* 更新 _ 的值。 */
		var body bytes.Buffer                                                                                  /* 声明 body。 */
		form := multipart.NewWriter(&body)                                                                     /* 更新 form 的值。 */
		f, _ := form.CreateFormFile("file", "gb.zip")                                                          /* 更新 _ 的值。 */
		_, _ = f.Write(packed.Bytes())                                                                         /* 更新 _ 的值。 */
		_ = form.WriteField("productId", "gb-product")                                                         /* 更新 _ 的值。 */
		_ = form.WriteField("publish", "true")                                                                 /* 更新 _ 的值。 */
		_ = form.Close()                                                                                       /* 更新 _ 的值。 */
		req, _ := http.NewRequest("POST", server.URL+"/api/v2/protocols/gb26875-dahua/source-releases", &body) /* 更新 _ 的值。 */
		req.Header.Set("Authorization", "Bearer "+token)                                                       /* 执行当前语句并推进处理流程。 */
		req.Header.Set("Content-Type", form.FormDataContentType())                                             /* 执行当前语句并推进处理流程。 */
		resp, err := server.Client().Do(req)                                                                   /* 更新 err 的值。 */
		if err != nil {                                                                                        /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer resp.Body.Close()          /* 安排函数结束时执行清理。 */
		data, _ := io.ReadAll(resp.Body) /* 更新 _ 的值。 */
		if resp.StatusCode != status {   /* 判断条件并选择处理分支。 */
			t.Fatalf("upload %s: %d %s", version, resp.StatusCode, data) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	upload("1.0.0", false, 201)                   /* 执行当前语句并推进处理流程。 */
	free, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	port := free.Addr().(*net.TCPAddr).Port                                                                                                                                                                                                                 /* 更新 port 的值。 */
	_ = free.Close()                                                                                                                                                                                                                                        /* 更新 _ 的值。 */
	profile := model.DeviceAccessProfile{ID: "gb-tcp", ProductID: "gb-product", ProtocolID: "gb26875-dahua", ProtocolVersion: "1.0.0", Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, TimeoutMs: 2000, Enabled: true, AutoRegister: true} /* 更新 profile 的值。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, profile, 201)                                                                                                                                               /* 执行当前语句并推进处理流程。 */
	udpFree, err := net.ListenPacket("udp", "127.0.0.1:0")                                                                                                                                                                                                  /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	udpPort := udpFree.LocalAddr().(*net.UDPAddr).Port                                                           /* 更新 udpPort 的值。 */
	_ = udpFree.Close()                                                                                          /* 更新 _ 的值。 */
	udpProfile := profile                                                                                        /* 更新 udpProfile 的值。 */
	udpProfile.ID = "gb-udp"                                                                                     /* 更新 udpProfile.ID 的值。 */
	udpProfile.Network = "udp"                                                                                   /* 更新 udpProfile.Network 的值。 */
	udpProfile.Port = udpPort                                                                                    /* 更新 udpProfile.Port 的值。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, udpProfile, 201) /* 执行当前语句并推进处理流程。 */
	listeners.Start(ctx)                                                                                         /* 执行当前语句并推进处理流程。 */
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))                                                 /* 更新 address 的值。 */
	var conn net.Conn                                                                                            /* 声明 conn。 */
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {                              /* 循环处理当前数据。 */
		conn, err = net.DialTimeout("tcp", address, 100*time.Millisecond) /* 更新 err 的值。 */
		if err == nil {                                                   /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(20 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer conn.Close() /* 安排函数结束时执行清理。 */
	// Add a managed device through the unified service while reusing the live
	// listener. The existing runtime below must accept it without re-registration.
	onboardRequest := onboarding.EnrollRequest{RequestID: "gb-reuse", ProductID: "gb-product", Device: onboarding.EnrollDevice{ID: "gb26875_123456789012", Name: "GB onboarded"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeListener, ProfileID: profile.ID}}
	if _, err = api.onboarding.Enroll(ctx, "tenant_001", onboardRequest); err != nil {
		t.Fatal("reuse listener onboarding", err)
	}
	profilesAfter, _ := repo.ListDeviceAccessProfiles(ctx, "tenant_001") /* 更新 _ 的值。 */
	if len(profilesAfter) != 2 {                                         /* 判断条件并选择处理分支。 */
		t.Fatal("onboarding duplicated the existing listener") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	readFrame := func(c net.Conn) []byte { /* 更新 readFrame 的值。 */
		t.Helper()                                             /* 执行当前语句并推进处理流程。 */
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second)) /* 更新 _ 的值。 */
		head := make([]byte, 27)                               /* 更新 head 的值。 */
		if _, err := io.ReadFull(c, head); err != nil {        /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		length := int(head[24]) + int(head[25])*256     /* 更新 length 的值。 */
		tail := make([]byte, length+3)                  /* 更新 tail 的值。 */
		if _, err := io.ReadFull(c, tail); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return append(head, tail...) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	check := func(version, vendor string) { /* 更新 check 的值。 */
		t.Helper() /* 执行当前语句并推进处理流程。 */
		select {   /* 根据条件选择处理路径。 */
		case raw := <-ingested: /* 处理当前分支。 */
			if raw.ProtocolVersion != version || raw.DeviceID != "gb26875_123456789012" { /* 判断条件并选择处理分支。 */
				t.Fatalf("raw %+v", raw) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			msg, err := repo.GetStandardMessageByRaw(ctx, "tenant_001", raw.MessageID) /* 更新 err 的值。 */
			if err != nil || msg.Tags["terminalVendor"] != vendor {                    /* 判断条件并选择处理分支。 */
				t.Fatalf("parsed %+v %v", msg, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			stored, err := repo.GetRawIndex(ctx, "tenant_001", raw.MessageID) /* 更新 err 的值。 */
			if err != nil {                                                   /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			archived, err := engine.GetRaw(ctx, stored)            /* 更新 err 的值。 */
			if err != nil || archived.ProtocolVersion != version { /* 判断条件并选择处理分支。 */
				t.Fatalf("archive %+v %v", archived, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if raw.Metadata["protocolState"] != nil && archived.Metadata["protocolState"] == nil { /* 判断条件并选择处理分支。 */
				t.Fatal("session state was not archived for replay") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		case <-time.After(5 * time.Second): /* 处理当前分支。 */
			t.Fatal("no ingested frame") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = conn.Write(frame[:12])     /* 更新 _ 的值。 */
	time.Sleep(50 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	select {                          /* 根据条件选择处理路径。 */
	case <-ingested: /* 处理当前分支。 */
		t.Fatal("partial frame was ingested") /* 验证实际结果符合预期。 */
	default: /* 处理当前分支。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = conn.Write(append(append([]byte{}, frame[12:]...), frame...)) /* 更新 _ 的值。 */
	for i := 0; i < 2; i++ {                                             /* 循环处理当前数据。 */
		ack := readFrame(conn) /* 更新 ack 的值。 */
		if ack[26] != 3 {      /* 判断条件并选择处理分支。 */
			t.Fatalf("not ACK: %X", ack) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		check("1.0.0", "Dahua") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	// A real command must wait for a matching reply, without acknowledging ACKs.
	commandDone := make(chan error, 1) /* 更新 commandDone 的值。 */
	go func() {                        /* 执行当前语句并推进处理流程。 */
		result, err := listeners.Command(ctx, "tenant_001", "gb-tcp", "gb26875_123456789012", map[string]any{"type": "time-sync"}) /* 更新 err 的值。 */
		if err == nil && result["status"] != "acknowledged" {                                                                      /* 判断条件并选择处理分支。 */
			err = io.ErrUnexpectedEOF /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
		commandDone <- err /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
	command := readFrame(conn)               /* 更新 command 的值。 */
	ack := append([]byte{}, command[:27]...) /* 更新 ack 的值。 */
	copy(ack[12:18], frame[12:18])           /* 执行当前语句并推进处理流程。 */
	copy(ack[18:24], frame[18:24])           /* 执行当前语句并推进处理流程。 */
	ack[24], ack[25], ack[26] = 0, 0, 3      /* 更新 ack[26] 的值。 */
	var sum byte                             /* 声明 sum。 */
	for _, b := range ack[2:] {              /* 循环处理当前数据。 */
		sum += b /* 更新 sum 的值。 */
	} /* 结束当前表达式或代码块。 */
	ack = append(ack, sum, '#', '#')     /* 更新 ack 的值。 */
	_, _ = conn.Write(ack)               /* 更新 _ 的值。 */
	check("1.0.0", "Dahua")              /* 执行当前语句并推进处理流程。 */
	if err = <-commandDone; err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	udp, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(udpPort))) /* 更新 err 的值。 */
	if err != nil {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer udp.Close()                                                   /* 安排函数结束时执行清理。 */
	_, _ = udp.Write(frame)                                             /* 更新 _ 的值。 */
	_ = udp.SetReadDeadline(time.Now().Add(5 * time.Second))            /* 更新 _ 的值。 */
	buf := make([]byte, 1024)                                           /* 更新 buf 的值。 */
	if n, err := udp.Read(buf); err != nil || n != 30 || buf[26] != 3 { /* 判断条件并选择处理分支。 */
		t.Fatalf("UDP ACK %d %v", n, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	check("1.0.0", "Dahua")                                                                                                                   /* 执行当前语句并推进处理流程。 */
	upload("1.1.0", false, 201)                                                                                                               /* 执行当前语句并推进处理流程。 */
	_, _ = conn.Write(frame)                                                                                                                  /* 更新 _ 的值。 */
	_ = readFrame(conn)                                                                                                                       /* 更新 _ 的值。 */
	check("1.1.0", "Dahua-v2")                                                                                                                /* 执行当前语句并推进处理流程。 */
	upload("1.2.0", true, 422)                                                                                                                /* 执行当前语句并推进处理流程。 */
	_, _ = conn.Write(frame)                                                                                                                  /* 更新 _ 的值。 */
	_ = readFrame(conn)                                                                                                                       /* 更新 _ 的值。 */
	check("1.1.0", "Dahua-v2")                                                                                                                /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/gb-product/protocol-binding/rollback", token, map[string]any{}, 200) /* 执行当前语句并推进处理流程。 */
	_, _ = conn.Write(frame)                                                                                                                  /* 更新 _ 的值。 */
	_ = readFrame(conn)                                                                                                                       /* 更新 _ 的值。 */
	check("1.0.0", "Dahua")                                                                                                                   /* 执行当前语句并推进处理流程。 */
	// A corrupt frame receives no ACK and cannot enter the archive.
	_, _ = conn.Write(append([]byte("noise"), frame...))                                            /* 更新 _ 的值。 */
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))                                       /* 更新 _ 的值。 */
	if n, err := conn.Read(buf); n != 0 || err == nil || strings.Contains(err.Error(), "timeout") { /* 判断条件并选择处理分支。 */
		t.Fatalf("invalid frame not rejected: %d %v", n, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case <-ingested: /* 处理当前分支。 */
		t.Fatal("corrupt frame ingested") /* 验证实际结果符合预期。 */
	default: /* 处理当前分支。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
