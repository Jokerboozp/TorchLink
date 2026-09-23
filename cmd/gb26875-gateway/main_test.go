package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bufio"             /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/binary"   /* 执行当前语句并推进处理流程。 */
	"encoding/hex"      /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"net"               /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestReadFramePreservesCompleteGB26875Message(t *testing.T) { /* 定义 TestReadFramePreservesCompleteGB26875Message 函数。 */
	want := parser.BuildGB26875ComponentStatusFrame(9, [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}, 128, 1, 23, 1, 2, 1<<1, "gateway test", time.Now()) /* 更新 want 的值。 */
	got, err := readFrame(bufio.NewReader(bytes.NewReader(append([]byte("noise"), want...))))                                                           /* 更新 err 的值。 */
	if err != nil {                                                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !bytes.Equal(got, want) { /* 判断条件并选择处理分支。 */
		t.Fatalf("frame changed in gateway\nwant %X\n got %X", want, got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGatewayConfirmationFrameIsParseable(t *testing.T) { /* 定义 TestGatewayConfirmationFrameIsParseable 函数。 */
	ack := parser.BuildGB26875AckFrame(9, [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}, time.Now())                                            /* 更新 ack 的值。 */
	payload, _ := json.Marshal(hex.EncodeToString(ack))                                                                                       /* 更新 _ 的值。 */
	message, err := (parser.GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if message.MessageType != model.CommandReply || message.Event["type"] != "ACK" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected confirmation: %#v", message) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGatewayAnswersDeviceTimeSyncRequest(t *testing.T) { /* 定义 TestGatewayAnswersDeviceTimeSyncRequest 函数。 */
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 platform 的值。 */
		w.WriteHeader(http.StatusCreated) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	defer platform.Close()                                                                                                 /* 安排函数结束时执行清理。 */
	c := &platformClient{baseURL: platform.URL, tenant: "tenant_001", http: platform.Client(), devices: map[string]bool{}} /* 更新 c 的值。 */
	source := [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}                                                                  /* 更新 source 的值。 */
	request := parser.BuildGB26875TimeSyncRequestFrame(3, source, time.Now())                                              /* 更新 request 的值。 */
	response, deviceID, err := processFrame(context.Background(), c, request, "TCP", "127.0.0.1:26875")                    /* 更新 err 的值。 */
	if err != nil {                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if deviceID != "gb26875_123456789012" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected device id %s", deviceID) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(response) != 38 { /* 判断条件并选择处理分支。 */
		t.Fatalf("expected 38-byte time sync response, got %d", len(response)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	payload, _ := json.Marshal(hex.EncodeToString(response))                                                                                  /* 更新 _ 的值。 */
	message, err := (parser.GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if message.Event["type"] != "TIME_SYNC" || message.MessageType != model.CommandReply { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected time sync response: %#v", message) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGatewayControlAPICompletesTimeSyncRequestResponse(t *testing.T) { /* 定义 TestGatewayControlAPICompletesTimeSyncRequestResponse 函数。 */
	registry := newSessionRegistry()                      /* 更新 registry 的值。 */
	deviceConn, gatewayConn := net.Pipe()                 /* 更新 gatewayConn 的值。 */
	defer deviceConn.Close()                              /* 安排函数结束时执行清理。 */
	defer gatewayConn.Close()                             /* 安排函数结束时执行清理。 */
	source := [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12} /* 更新 source 的值。 */
	deviceID := "gb26875_123456789012"                    /* 更新 deviceID 的值。 */
	registry.registerTCP(deviceID, source, gatewayConn)   /* 执行当前语句并推进处理流程。 */
	go func() {                                           /* 执行当前语句并推进处理流程。 */
		reader := bufio.NewReader(deviceConn) /* 更新 reader 的值。 */
		request, err := readFrame(reader)     /* 更新 err 的值。 */
		if err != nil {                       /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		sequence := binary.LittleEndian.Uint16(request[2:4])             /* 更新 sequence 的值。 */
		ack := parser.BuildGB26875AckFrame(sequence, source, time.Now()) /* 更新 ack 的值。 */
		registry.deliver(deviceID, sequence, ack)                        /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
	server := httptest.NewServer(controlHandler{sessions: registry, token: "secret"})                                            /* 更新 server 的值。 */
	defer server.Close()                                                                                                         /* 安排函数结束时执行清理。 */
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/devices/"+deviceID+"/time-sync", strings.NewReader("")) /* 更新 err 的值。 */
	if err != nil {                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	request.Header.Set("X-Gateway-Token", "secret") /* 执行当前语句并推进处理流程。 */
	response, err := server.Client().Do(request)    /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer response.Body.Close()               /* 安排函数结束时执行清理。 */
	if response.StatusCode != http.StatusOK { /* 判断条件并选择处理分支。 */
		t.Fatalf("control API status=%d", response.StatusCode) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var body map[string]any                                              /* 声明 body。 */
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if body["deviceId"] != deviceID || body["request"] != "time-sync" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected control response: %#v", body) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
