package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestReadFramePreservesCompleteGB26875Message(t *testing.T) {
	want := parser.BuildGB26875ComponentStatusFrame(9, [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}, 128, 1, 23, 1, 2, 1<<1, "gateway test", time.Now())
	got, err := readFrame(bufio.NewReader(bytes.NewReader(append([]byte("noise"), want...))))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("frame changed in gateway\nwant %X\n got %X", want, got)
	}
}

func TestGatewayConfirmationFrameIsParseable(t *testing.T) {
	ack := parser.BuildGB26875AckFrame(9, [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}, time.Now())
	payload, _ := json.Marshal(hex.EncodeToString(ack))
	message, err := (parser.GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if message.MessageType != model.CommandReply || message.Event["type"] != "ACK" {
		t.Fatalf("unexpected confirmation: %#v", message)
	}
}

func TestGatewayAnswersDeviceTimeSyncRequest(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer platform.Close()
	c := &platformClient{baseURL: platform.URL, tenant: "tenant_001", http: platform.Client(), devices: map[string]bool{}}
	source := [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}
	request := parser.BuildGB26875TimeSyncRequestFrame(3, source, time.Now())
	response, deviceID, err := processFrame(context.Background(), c, request, "TCP", "127.0.0.1:26875")
	if err != nil {
		t.Fatal(err)
	}
	if deviceID != "gb26875_123456789012" {
		t.Fatalf("unexpected device id %s", deviceID)
	}
	if len(response) != 38 {
		t.Fatalf("expected 38-byte time sync response, got %d", len(response))
	}
	payload, _ := json.Marshal(hex.EncodeToString(response))
	message, err := (parser.GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if message.Event["type"] != "TIME_SYNC" || message.MessageType != model.CommandReply {
		t.Fatalf("unexpected time sync response: %#v", message)
	}
}

func TestGatewayControlAPICompletesTimeSyncRequestResponse(t *testing.T) {
	registry := newSessionRegistry()
	deviceConn, gatewayConn := net.Pipe()
	defer deviceConn.Close()
	defer gatewayConn.Close()
	source := [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}
	deviceID := "gb26875_123456789012"
	registry.registerTCP(deviceID, source, gatewayConn)
	go func() {
		reader := bufio.NewReader(deviceConn)
		request, err := readFrame(reader)
		if err != nil {
			return
		}
		sequence := binary.LittleEndian.Uint16(request[2:4])
		ack := parser.BuildGB26875AckFrame(sequence, source, time.Now())
		registry.deliver(deviceID, sequence, ack)
	}()
	server := httptest.NewServer(controlHandler{sessions: registry, token: "secret"})
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/devices/"+deviceID+"/time-sync", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Gateway-Token", "secret")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("control API status=%d", response.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["deviceId"] != deviceID || body["request"] != "time-sync" {
		t.Fatalf("unexpected control response: %#v", body)
	}
}
