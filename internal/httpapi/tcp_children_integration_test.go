package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"                                 /* 执行当前语句并推进处理流程。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"encoding/json"                         /* 执行当前语句并推进处理流程。 */
	"fmt"                                   /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolruntime" /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"mime/multipart"                        /* 执行当前语句并推进处理流程。 */
	"net"                                   /* 执行当前语句并推进处理流程。 */
	"net/http"                              /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                     /* 执行当前语句并推进处理流程。 */
	"os"                                    /* 执行当前语句并推进处理流程。 */
	"os/exec"                               /* 执行当前语句并推进处理流程。 */
	"path/filepath"                         /* 执行当前语句并推进处理流程。 */
	"strings"                               /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Real uploaded Go functions, authenticated APIs, TCP sockets, archive, parser,
// registration and command routing. Bytes below are a teaching test protocol.
func TestTCPParentChildSourceChain(t *testing.T) { /* 定义 TestTCPParentChildSourceChain 函数。 */
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                           /* 安排函数结束时执行清理。 */
	root := t.TempDir()                                                      /* 更新 root 的值。 */
	repo := memory.NewRepository()                                           /* 更新 repo 的值。 */
	archive, err := local.NewArchive(root)                                   /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                         /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	if err = engine.Start(ctx); err != nil {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                          /* 更新 cfg 的值。 */
	cfg.DataDir = root                                                                                            /* 更新 cfg.DataDir 的值。 */
	api := New(cfg, engine, metrics.New(), log)                                                                   /* 更新 api 的值。 */
	ingested := make(chan model.RawMessage, 64)                                                                   /* 更新 ingested 的值。 */
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error { /* 更新 listeners 的值。 */
		_, _, e := engine.IngestRaw(ctx, raw) /* 更新 e 的值。 */
		if e == nil {                         /* 判断条件并选择处理分支。 */
			ingested <- raw /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return e /* 返回当前处理结果。 */
	}, log) /* 结束当前表达式或代码块。 */
	listeners.SetConnectionReporter(engine.ReportConnection)                                    /* 执行当前语句并推进处理流程。 */
	api.SetProtocolListeners(listeners)                                                         /* 执行当前语句并推进处理流程。 */
	token, _ := api.auth.Issue("tester", "tenant", "operator", nil, time.Hour)                  /* 更新 _ 的值。 */
	other, _ := api.auth.Issue("tester", "other", "operator", nil, time.Hour)                   /* 更新 _ 的值。 */
	request := func(method, path, auth string, body any, want int) *httptest.ResponseRecorder { /* 更新 request 的值。 */
		t.Helper()                                                      /* 执行当前语句并推进处理流程。 */
		data, _ := json.Marshal(body)                                   /* 更新 _ 的值。 */
		req := httptest.NewRequest(method, path, bytes.NewReader(data)) /* 更新 req 的值。 */
		req.Header.Set("Authorization", "Bearer "+auth)                 /* 执行当前语句并推进处理流程。 */
		req.Header.Set("Content-Type", "application/json")              /* 执行当前语句并推进处理流程。 */
		w := httptest.NewRecorder()                                     /* 更新 w 的值。 */
		api.Handler().ServeHTTP(w, req)                                 /* 执行当前语句并推进处理流程。 */
		if w.Code != want {                                             /* 判断条件并选择处理分支。 */
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, want, w.Body.String()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return w /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, id := range []string{"parent", "sensor"} { /* 循环处理当前数据。 */
		repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: id, Name: id, Status: "ENABLED"}) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	upload := func(id, source string) { /* 更新 upload 的值。 */
		t.Helper()                                                                            /* 执行当前语句并推进处理流程。 */
		var body bytes.Buffer                                                                 /* 声明 body。 */
		form := multipart.NewWriter(&body)                                                    /* 更新 form 的值。 */
		f, _ := form.CreateFormFile("file", "protocol.go")                                    /* 更新 _ 的值。 */
		f.Write([]byte(source))                                                               /* 执行当前语句并推进处理流程。 */
		form.WriteField("productId", id)                                                      /* 执行当前语句并推进处理流程。 */
		form.WriteField("version", "1")                                                       /* 执行当前语句并推进处理流程。 */
		form.Close()                                                                          /* 执行当前语句并推进处理流程。 */
		req := httptest.NewRequest("POST", "/api/v2/protocols/"+id+"/source-releases", &body) /* 更新 req 的值。 */
		req.Header.Set("Authorization", "Bearer "+token)                                      /* 执行当前语句并推进处理流程。 */
		req.Header.Set("Content-Type", form.FormDataContentType())                            /* 执行当前语句并推进处理流程。 */
		w := httptest.NewRecorder()                                                           /* 更新 w 的值。 */
		api.Handler().ServeHTTP(w, req)                                                       /* 执行当前语句并推进处理流程。 */
		if w.Code != 201 {                                                                    /* 判断条件并选择处理分支。 */
			t.Fatalf("upload %s: %d %s", id, w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	upload("sensor", tcpChildSource)                  /* 执行当前语句并推进处理流程。 */
	upload("parent", tcpParentSource)                 /* 执行当前语句并推进处理流程。 */
	profiles := []model.DeviceAccessProfile{}         /* 更新 profiles 的值。 */
	peers := []net.Conn{}                             /* 更新 peers 的值。 */
	for i, mode := range []string{"listen", "dial"} { /* 循环处理当前数据。 */
		socket, e := net.Listen("tcp", "127.0.0.1:0") /* 更新 e 的值。 */
		if e != nil {                                 /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		p := model.DeviceAccessProfile{ID: mode, TenantID: "tenant", ProductID: "parent", ProtocolID: "parent", ProtocolVersion: "1", Mode: "listener", Network: "tcp", ConnectionMode: mode, Host: "127.0.0.1", Port: socket.Addr().(*net.TCPAddr).Port, Enabled: true, AutoRegister: true, TimeoutMs: 3000, ChildProducts: []model.ChildProductBinding{{Type: "smoke", ProductID: "sensor"}}} /* 更新 p 的值。 */
		if mode == "listen" {                                                                                                                                                                                                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
			socket.Close() /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			defer socket.Close()                                                                                                                                                                        /* 安排函数结束时执行清理。 */
			p.DeviceID = fmt.Sprintf("main-%d", i+1)                                                                                                                                                    /* 更新 p.DeviceID 的值。 */
			repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: p.DeviceID, Name: p.DeviceID, ProductID: "parent", Status: "ENABLED", DeviceRole: "DIRECT", AccessKey: p.DeviceID}) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		request("POST", "/api/v2/device-access-profiles", token, p, 201) /* 执行当前语句并推进处理流程。 */
		profiles = append(profiles, p)                                   /* 更新 profiles 的值。 */
		if mode == "listen" {                                            /* 判断条件并选择处理分支。 */
			listeners.Start(ctx) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var peer net.Conn     /* 声明 peer。 */
		if mode == "listen" { /* 判断条件并选择处理分支。 */
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) { /* 循环处理当前数据。 */
				peer, e = net.DialTimeout("tcp", net.JoinHostPort(p.Host, fmt.Sprint(p.Port)), 100*time.Millisecond) /* 更新 e 的值。 */
				if e == nil {                                                                                        /* 判断条件并选择处理分支。 */
					break /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} else { /* 结束当前表达式或代码块。 */
			socket.(*net.TCPListener).SetDeadline(time.Now().Add(4 * time.Second)) /* 执行当前语句并推进处理流程。 */
			peer, e = socket.Accept()                                              /* 更新 e 的值。 */
		} /* 结束当前表达式或代码块。 */
		if e != nil || peer == nil { /* 判断条件并选择处理分支。 */
			t.Fatal("connection", e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer peer.Close()                                 /* 安排函数结束时执行清理。 */
		peers = append(peers, peer)                        /* 更新 peers 的值。 */
		peer.SetDeadline(time.Now().Add(10 * time.Second)) /* 执行当前语句并推进处理流程。 */
		// Child information before the registration handshake must not be ACKed.
		if mode == "listen" { /* 判断条件并选择处理分支。 */
			bad, e := net.Dial("tcp", peer.RemoteAddr().String()) /* 更新 e 的值。 */
			if e != nil {                                         /* 判断条件并选择处理分支。 */
				t.Fatal(e) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			bad.SetDeadline(time.Now().Add(time.Second)) /* 执行当前语句并推进处理流程。 */
			bad.Write([]byte{1, 0, 9})                   /* 执行当前语句并推进处理流程。 */
			buf := make([]byte, 1)                       /* 更新 buf 的值。 */
			if n, _ := bad.Read(buf); n != 0 {           /* 判断条件并选择处理分支。 */
				t.Fatal("bad credential ACKed") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			bad.Close()                                                          /* 执行当前语句并推进处理流程。 */
			if _, e = repo.GetManagedDevice(ctx, "tenant", "main-9"); e == nil { /* 判断条件并选择处理分支。 */
				t.Fatal("bad credential registered") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		id := byte(i + 1)                                                  /* 更新 id 的值。 */
		peer.Write([]byte{1, 0x5a, id})                                    /* 执行当前语句并推进处理流程。 */
		reply := make([]byte, 1)                                           /* 更新 reply 的值。 */
		if _, e = io.ReadFull(peer, reply); e != nil || reply[0] != 0x81 { /* 判断条件并选择处理分支。 */
			t.Fatal("register reply", reply, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		// Same child address under two different parents must create different rows.
		peer.Write([]byte{2, id, 7, 42})                                   /* 执行当前语句并推进处理流程。 */
		if _, e = io.ReadFull(peer, reply); e != nil || reply[0] != 0x82 { /* 判断条件并选择处理分支。 */
			t.Fatal("child ACK", reply, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		parentID := fmt.Sprintf("main-%d", id)                                      /* 更新 parentID 的值。 */
		childID := model.ChildDeviceID("tenant", parentID, "7")                     /* 更新 childID 的值。 */
		child, e := repo.GetManagedDevice(ctx, "tenant", childID)                   /* 更新 e 的值。 */
		if e != nil || child.GatewayID != parentID || child.ProductID != "sensor" { /* 判断条件并选择处理分支。 */
			t.Fatal(child, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		message, e := repo.GetLatestMessage(ctx, "tenant", childID)       /* 更新 e 的值。 */
		if e != nil || message.Properties["temperature"] != float64(42) { /* 判断条件并选择处理分支。 */
			t.Fatal(message, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		// Duplicate registration updates the same child; the state is not inherited from parent.
		peer.Write([]byte{2, id, 7, 43})               /* 执行当前语句并推进处理流程。 */
		if _, e = io.ReadFull(peer, reply); e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		children, total, e := repo.ListManagedDeviceChildren(ctx, "tenant", parentID, 20, 0) /* 更新 e 的值。 */
		if e != nil || len(children) != 1 || total != 1 {                                    /* 判断条件并选择处理分支。 */
			t.Fatal(children, total, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		result := request("GET", "/api/v1/device-registry/"+parentID+"/children", token, nil, 200)                                /* 更新 result 的值。 */
		if !strings.Contains(result.Body.String(), childID) || !strings.Contains(result.Body.String(), `"protocolId":"sensor"`) { /* 判断条件并选择处理分支。 */
			t.Fatal(result.Body.String()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		request("GET", "/api/v1/device-registry/"+parentID+"/children", other, nil, 404)  /* 执行当前语句并推进处理流程。 */
		request("GET", "/api/v2/products/sensor/protocol-binding", other, nil, 404)       /* 执行当前语句并推进处理流程。 */
		request("GET", "/api/v1/device-registry/"+childID+"/connection", token, nil, 200) /* 执行当前语句并推进处理流程。 */
		// A real child codec creates the inner command; the main codec wraps it.
		done := make(chan error, 1) /* 更新 done 的值。 */
		go func() {                 /* 执行当前语句并推进处理流程。 */
			q := make([]byte, 4)                                     /* 更新 q 的值。 */
			_, e := io.ReadFull(peer, q)                             /* 更新 e 的值。 */
			if e == nil && !bytes.Equal(q, []byte{5, id, 7, 0x44}) { /* 判断条件并选择处理分支。 */
				e = fmt.Errorf("wrong child command %x", q) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if e == nil && mode == "listen" { /* 判断条件并选择处理分支。 */
				release, getErr := repo.GetProtocolRelease(ctx, "tenant", "sensor", "1") /* 更新 getErr 的值。 */
				e = getErr                                                               /* 更新 e 的值。 */
				if e == nil {                                                            /* 判断条件并选择处理分支。 */
					release.Version = "2"                        /* 更新 release.Version 的值。 */
					e = repo.CreateProtocolRelease(ctx, release) /* 更新 e 的值。 */
				} /* 结束当前表达式或代码块。 */
				if e == nil { /* 判断条件并选择处理分支。 */
					e = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "sensor", ProtocolID: "sensor", Version: "2"}) /* 更新 e 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if e == nil { /* 判断条件并选择处理分支。 */
				_, e = peer.Write([]byte{6, id, 7, 44}) /* 更新 e 的值。 */
			} /* 结束当前表达式或代码块。 */
			done <- e /* 执行当前语句并推进处理流程。 */
		}() /* 结束当前表达式或代码块。 */
		cmd := request("POST", "/api/v2/device-access-profiles/"+mode+"/devices/"+childID+"/commands", token, map[string]any{"type": "read", "confirmed": true}, 200) /* 更新 cmd 的值。 */
		if !strings.Contains(cmd.Body.String(), "acknowledged") {                                                                                                     /* 判断条件并选择处理分支。 */
			t.Fatal(cmd.Body.String()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if e := <-done; e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if mode == "listen" { /* 判断条件并选择处理分支。 */
				var raw model.RawMessage /* 声明 raw。 */
		waitPinned: /* 执行当前语句并推进处理流程。 */
			for { /* 循环处理当前数据。 */
				select { /* 根据条件选择处理路径。 */
				case raw = <-ingested: /* 处理当前分支。 */
					if raw.DeviceID == childID && string(raw.Payload) == "\"AA012C\"" { /* 判断条件并选择处理分支。 */
						break waitPinned /* 执行当前语句并推进处理流程。 */
					} /* 结束当前表达式或代码块。 */
				case <-time.After(time.Second): /* 处理当前分支。 */
					t.Fatal("missing pinned child reply") /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if raw.ProtocolVersion != "1" { /* 判断条件并选择处理分支。 */
				t.Fatal("pending child command changed parser version", raw.ProtocolVersion) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if e := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "sensor", ProtocolID: "sensor", Version: "1"}); e != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(e) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		p.Queries = []model.ProtocolQuery{{Type: "read-main", IntervalSec: 60}}              /* 更新 p.Queries 的值。 */
		request("PUT", "/api/v2/device-access-profiles/"+mode, token, p, 201)                /* 执行当前语句并推进处理流程。 */
		query := make([]byte, 2)                                                             /* 更新 query 的值。 */
		if _, e = io.ReadFull(peer, query); e != nil || !bytes.Equal(query, []byte{3, id}) { /* 判断条件并选择处理分支。 */
			t.Fatal("real scheduled query", query, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
			peer.Write([]byte{4, id, 0}) /* 执行当前语句并推进处理流程。 */
	waitQuery: /* 执行当前语句并推进处理流程。 */
		for { /* 循环处理当前数据。 */
			select { /* 根据条件选择处理路径。 */
			case raw := <-ingested: /* 处理当前分支。 */
				if string(raw.Payload) == fmt.Sprintf("\"04%02X00\"", id) { /* 判断条件并选择处理分支。 */
					break waitQuery /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			case <-time.After(time.Second): /* 处理当前分支。 */
				t.Fatal("query response not ingested") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		p.Queries = nil                                                       /* 更新 p.Queries 的值。 */
		request("PUT", "/api/v2/device-access-profiles/"+mode, token, p, 201) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	t.Run("browser", func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
		if os.Getenv("IOT_TEST_BROWSER") == "" { /* 判断条件并选择处理分支。 */
			t.Skip("IOT_TEST_BROWSER is not configured") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))          /* 更新 assets 的值。 */
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
			if strings.HasPrefix(r.URL.Path, "/api/") { /* 判断条件并选择处理分支。 */
				api.Handler().ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
			} else { /* 结束当前表达式或代码块。 */
				assets.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		})) /* 结束当前表达式或代码块。 */
		defer server.Close()                                                                                                          /* 安排函数结束时执行清理。 */
		cmd := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "tcp-children-check.mjs")) /* 更新 cmd 的值。 */
		cmd.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)                                        /* 更新 cmd.Env 的值。 */
		if out, e := cmd.CombinedOutput(); e != nil {                                                                                 /* 判断条件并选择处理分支。 */
			t.Fatalf("browser: %v %s", e, out) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	// Disable retains children but prevents any further device command.
	profiles[0].Enabled = false                                                      /* 更新 profiles[0].Enabled 的值。 */
	request("PUT", "/api/v2/device-access-profiles/listen", token, profiles[0], 201) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

const tcpChildSource = `package main
import "errors"
func Protocol() Definition{return Definition{
 Decode:func(b []byte,c Context)(Message,error){if len(b)!=3||b[0]!=0xaa{return Message{},errors.New("invalid sensor data")};return properties(map[string]any{"temperature":int(b[2])}),nil},
 Encode:func(q Command,c Context)(Frame,error){if q.Type!="read"{return Frame{},errors.New("unsupported command")};return Frame{Reply:[]byte{0x44},CorrelationID:"read"},nil},
 Samples:[]Sample{{Data:[]byte{0xaa,1,42},Want:properties(map[string]any{"temperature":42})}},
 Operations:[]OperationSample{{Operation:"encode",Command:Command{Type:"read"},Want:Frame{Reply:[]byte{0x44},CorrelationID:"read"}}},
}}
`

const tcpParentSource = `package main
import("fmt";"errors";"strconv")
func Protocol() Definition{return Definition{
 Transport:"TCP",
 Decode:func(b []byte,c Context)(Message,error){if len(b)<3{return Message{},errors.New("short frame")};return Message{MessageType:"EVENT_REPORT",Event:map[string]any{"type":"gatewayFrame"}},nil},
 Ingress:func(b []byte,c Context)(Frame,error){
  if len(b)<3{return Frame{NeedMore:true},nil}
  if b[0]==1{if b[1]!=0x5a{return Frame{},errors.New("invalid registration credential")};id:=fmt.Sprintf("main-%d",b[2]);return Frame{Consumed:3,DeviceID:id,Reply:[]byte{0x81},State:map[string]any{"id":int(b[2])}},nil}
  id,ok:=c.State["id"].(float64);if !ok||byte(id)!=b[1]{return Frame{},errors.New("registration required")}
  if b[0]==4{return Frame{Consumed:3,DeviceID:fmt.Sprintf("main-%d",int(id)),State:c.State,CorrelationID:"read-main"},nil}
  if len(b)<4{return Frame{NeedMore:true},nil}
  if b[0]!=2 && b[0]!=6{return Frame{},errors.New("unexpected frame")}
  f:=Frame{Consumed:4,DeviceID:fmt.Sprintf("main-%d",int(id)),State:c.State,Children:[]Child{{Address:strconv.Itoa(int(b[2])),Type:"smoke",Name:"烟感探测器",Data:[]byte{0xaa,1,b[3]}}}}
  if b[0]==2{f.Reply=[]byte{0x82}}else{f.CorrelationID="child-"+strconv.Itoa(int(b[2]))};return f,nil
 },
 Encode:func(q Command,c Context)(Frame,error){id,ok:=c.State["id"].(float64);if !ok{return Frame{},errors.New("registration required")};if q.Type=="read-main"{return Frame{Reply:[]byte{3,byte(id)},State:c.State,CorrelationID:"read-main"},nil};if q.Type!="child"{return Frame{},errors.New("unsupported command")};address,_:=q.Params["address"].(string);n,e:=strconv.Atoi(address);if e!=nil||q.Params["payload"]!="44"{return Frame{},errors.New("invalid child envelope")};return Frame{Reply:[]byte{5,byte(id),byte(n),0x44},State:c.State,CorrelationID:"child-"+address},nil},
 Samples:[]Sample{{Data:[]byte{1,0x5a,1},Want:Message{MessageType:"EVENT_REPORT",Event:map[string]any{"type":"gatewayFrame"}}}},
 Operations:[]OperationSample{
 {Operation:"ingress",Data:[]byte{1,0x5a,1},Want:Frame{Consumed:3,DeviceID:"main-1",Reply:[]byte{0x81},State:map[string]any{"id":1}}},
 {Operation:"ingress",Data:[]byte{2,1,7,42},Context:Context{State:map[string]any{"id":1}},Want:Frame{Consumed:4,DeviceID:"main-1",Reply:[]byte{0x82},State:map[string]any{"id":1},Children:[]Child{{Address:"7",Type:"smoke",Name:"烟感探测器",Data:[]byte{0xaa,1,42}}}}},
 {Operation:"encode",Command:Command{Type:"child",Params:map[string]any{"address":"7","payload":"44"}},Context:Context{State:map[string]any{"id":1}},Want:Frame{Reply:[]byte{5,1,7,0x44},State:map[string]any{"id":1},CorrelationID:"child-7"}},
 },
}}
`
