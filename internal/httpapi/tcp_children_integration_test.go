package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolruntime"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real uploaded Go functions, authenticated APIs, TCP sockets, archive, parser,
// registration and command routing. Bytes below are a teaching test protocol.
func TestTCPParentChildSourceChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	ingested := make(chan model.RawMessage, 64)
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error {
		_, _, e := engine.IngestRaw(ctx, raw)
		if e == nil {
			ingested <- raw
		}
		return e
	}, log)
	listeners.SetConnectionReporter(engine.ReportConnection)
	api.SetProtocolListeners(listeners)
	token, _ := api.auth.Issue("tester", "tenant", "operator", nil, time.Hour)
	other, _ := api.auth.Issue("tester", "other", "operator", nil, time.Hour)
	request := func(method, path, auth string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	for _, id := range []string{"parent", "sensor"} {
		repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: id, Name: id, Status: "ENABLED"})
	}
	upload := func(id, source string) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		f, _ := form.CreateFormFile("file", "protocol.go")
		f.Write([]byte(source))
		form.WriteField("productId", id)
		form.WriteField("version", "1")
		form.Close()
		req := httptest.NewRequest("POST", "/api/v2/protocols/"+id+"/source-releases", &body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != 201 {
			t.Fatalf("upload %s: %d %s", id, w.Code, w.Body.String())
		}
	}
	upload("sensor", tcpChildSource)
	upload("parent", tcpParentSource)
	profiles := []model.DeviceAccessProfile{}
	peers := []net.Conn{}
	for i, mode := range []string{"listen", "dial"} {
		socket, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		p := model.DeviceAccessProfile{ID: mode, TenantID: "tenant", ProductID: "parent", ProtocolID: "parent", ProtocolVersion: "1", Mode: "listener", Network: "tcp", ConnectionMode: mode, Host: "127.0.0.1", Port: socket.Addr().(*net.TCPAddr).Port, Enabled: true, AutoRegister: true, TimeoutMs: 3000, ChildProducts: []model.ChildProductBinding{{Type: "smoke", ProductID: "sensor"}}}
		if mode == "listen" {
			socket.Close()
		} else {
			defer socket.Close()
			p.DeviceID = fmt.Sprintf("main-%d", i+1)
			repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: p.DeviceID, Name: p.DeviceID, ProductID: "parent", Status: "ENABLED", DeviceRole: "DIRECT", AccessKey: p.DeviceID})
		}
		request("POST", "/api/v2/device-access-profiles", token, p, 201)
		profiles = append(profiles, p)
		if mode == "listen" {
			listeners.Start(ctx)
		}
		var peer net.Conn
		if mode == "listen" {
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
				peer, e = net.DialTimeout("tcp", net.JoinHostPort(p.Host, fmt.Sprint(p.Port)), 100*time.Millisecond)
				if e == nil {
					break
				}
			}
		} else {
			socket.(*net.TCPListener).SetDeadline(time.Now().Add(4 * time.Second))
			peer, e = socket.Accept()
		}
		if e != nil || peer == nil {
			t.Fatal("connection", e)
		}
		defer peer.Close()
		peers = append(peers, peer)
		peer.SetDeadline(time.Now().Add(10 * time.Second))
		// Child information before the registration handshake must not be ACKed.
		if mode == "listen" {
			bad, e := net.Dial("tcp", peer.RemoteAddr().String())
			if e != nil {
				t.Fatal(e)
			}
			bad.SetDeadline(time.Now().Add(time.Second))
			bad.Write([]byte{1, 0, 9})
			buf := make([]byte, 1)
			if n, _ := bad.Read(buf); n != 0 {
				t.Fatal("bad credential ACKed")
			}
			bad.Close()
			if _, e = repo.GetManagedDevice(ctx, "tenant", "main-9"); e == nil {
				t.Fatal("bad credential registered")
			}
		}
		id := byte(i + 1)
		peer.Write([]byte{1, 0x5a, id})
		reply := make([]byte, 1)
		if _, e = io.ReadFull(peer, reply); e != nil || reply[0] != 0x81 {
			t.Fatal("register reply", reply, e)
		}
		// Same child address under two different parents must create different rows.
		peer.Write([]byte{2, id, 7, 42})
		if _, e = io.ReadFull(peer, reply); e != nil || reply[0] != 0x82 {
			t.Fatal("child ACK", reply, e)
		}
		parentID := fmt.Sprintf("main-%d", id)
		childID := model.ChildDeviceID("tenant", parentID, "7")
		child, e := repo.GetManagedDevice(ctx, "tenant", childID)
		if e != nil || child.GatewayID != parentID || child.ProductID != "sensor" {
			t.Fatal(child, e)
		}
		message, e := repo.GetLatestMessage(ctx, "tenant", childID)
		if e != nil || message.Properties["temperature"] != float64(42) {
			t.Fatal(message, e)
		}
		// Duplicate registration updates the same child; the state is not inherited from parent.
		peer.Write([]byte{2, id, 7, 43})
		if _, e = io.ReadFull(peer, reply); e != nil {
			t.Fatal(e)
		}
		children, total, e := repo.ListManagedDeviceChildren(ctx, "tenant", parentID, 20, 0)
		if e != nil || len(children) != 1 || total != 1 {
			t.Fatal(children, total, e)
		}
		result := request("GET", "/api/v1/device-registry/"+parentID+"/children", token, nil, 200)
		if !strings.Contains(result.Body.String(), childID) || !strings.Contains(result.Body.String(), `"protocolId":"sensor"`) {
			t.Fatal(result.Body.String())
		}
		request("GET", "/api/v1/device-registry/"+parentID+"/children", other, nil, 404)
		request("GET", "/api/v2/products/sensor/protocol-binding", other, nil, 404)
		request("GET", "/api/v1/device-registry/"+childID+"/connection", token, nil, 200)
		// A real child codec creates the inner command; the main codec wraps it.
		done := make(chan error, 1)
		go func() {
			q := make([]byte, 4)
			_, e := io.ReadFull(peer, q)
			if e == nil && !bytes.Equal(q, []byte{5, id, 7, 0x44}) {
				e = fmt.Errorf("wrong child command %x", q)
			}
			if e == nil && mode == "listen" {
				release, getErr := repo.GetProtocolRelease(ctx, "tenant", "sensor", "1")
				e = getErr
				if e == nil {
					release.Version = "2"
					e = repo.CreateProtocolRelease(ctx, release)
				}
				if e == nil {
					e = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "sensor", ProtocolID: "sensor", Version: "2"})
				}
			}
			if e == nil {
				_, e = peer.Write([]byte{6, id, 7, 44})
			}
			done <- e
		}()
		cmd := request("POST", "/api/v2/device-access-profiles/"+mode+"/devices/"+childID+"/commands", token, map[string]any{"type": "read", "confirmed": true}, 200)
		if !strings.Contains(cmd.Body.String(), "acknowledged") {
			t.Fatal(cmd.Body.String())
		}
		if e := <-done; e != nil {
			t.Fatal(e)
		}
		if mode == "listen" {
			var raw model.RawMessage
		waitPinned:
			for {
				select {
				case raw = <-ingested:
					if raw.DeviceID == childID && string(raw.Payload) == "\"AA012C\"" {
						break waitPinned
					}
				case <-time.After(time.Second):
					t.Fatal("missing pinned child reply")
				}
			}
			if raw.ProtocolVersion != "1" {
				t.Fatal("pending child command changed parser version", raw.ProtocolVersion)
			}
			if e := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "sensor", ProtocolID: "sensor", Version: "1"}); e != nil {
				t.Fatal(e)
			}
		}
		p.Queries = []model.ProtocolQuery{{Type: "read-main", IntervalSec: 60}}
		request("PUT", "/api/v2/device-access-profiles/"+mode, token, p, 201)
		query := make([]byte, 2)
		if _, e = io.ReadFull(peer, query); e != nil || !bytes.Equal(query, []byte{3, id}) {
			t.Fatal("real scheduled query", query, e)
		}
		peer.Write([]byte{4, id, 0})
	waitQuery:
		for {
			select {
			case raw := <-ingested:
				if string(raw.Payload) == fmt.Sprintf("\"04%02X00\"", id) {
					break waitQuery
				}
			case <-time.After(time.Second):
				t.Fatal("query response not ingested")
			}
		}
		p.Queries = nil
		request("PUT", "/api/v2/device-access-profiles/"+mode, token, p, 201)
	}
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("IOT_TEST_BROWSER is not configured")
		}
		assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				api.Handler().ServeHTTP(w, r)
			} else {
				assets.ServeHTTP(w, r)
			}
		}))
		defer server.Close()
		cmd := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "tcp-children-check.mjs"))
		cmd.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("browser: %v %s", e, out)
		}
	})
	// Disable retains children but prevents any further device command.
	profiles[0].Enabled = false
	request("PUT", "/api/v2/device-access-profiles/listen", token, profiles[0], 201)
}

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
