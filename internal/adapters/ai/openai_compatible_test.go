package aiadapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"iot-platform/internal/aioutput"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestOpenAICompatibleProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case "/chat/completions":
			var request openAIChatRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Model != "test-model" || len(request.Messages) != 2 {
				t.Fatalf("unexpected request %#v", request)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat_test", "choices": []any{map[string]any{"message": map[string]any{"content": "插件连接成功"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewOpenAICompatible("test", "Test Provider", server.URL, "test-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	answer, err := client.Chat(context.Background(), "tenant", "hello")
	if err != nil || answer != "插件连接成功" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
}

func TestOpenAICompatibleAnalyzeAlarmAcceptsStringConfidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"summary":"温度持续升高，需现场复核。","possibleReasons":["传感器异常"],"suggestions":["现场复核设备"],"riskLevel":"HIGH","confidence":"0.86"}`}}},
		})
	}))
	defer server.Close()

	client, err := NewOpenAICompatible("deepseek", "DeepSeek", server.URL, "test-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := client.AnalyzeAlarm(context.Background(), model.Alarm{ID: "alarm-1", AlarmLevel: "HIGH"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Summary == "" || analysis.Model != "test-model" || analysis.Confidence != 0.86 {
		t.Fatalf("unexpected decoded alarm analysis: %#v", analysis)
	}
}

func TestOpenAICompatibleAnalyzeAlarmKeepsSummaryWhenConfidenceIsMalformed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"summary":"告警事实已核对，请人工复核现场。","possibleReasons":["传感器异常"],"suggestions":["现场复核设备"],"riskLevel":"HIGH","confidence":"较高"}`}}},
		})
	}))
	defer server.Close()

	client, err := NewOpenAICompatible("deepseek", "DeepSeek", server.URL, "test-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := client.AnalyzeAlarm(context.Background(), model.Alarm{ID: "alarm-2", AlarmLevel: "HIGH"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Summary == "" || analysis.Confidence != 0 {
		t.Fatalf("malformed confidence should not discard summary: %#v", analysis)
	}
}

func TestOpenAICompatibleBoundsAndSanitizesProviderErrors(t *testing.T) {
	leaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "Authorization: Bearer super-secret-key")
	}))
	defer leaky.Close()
	client, err := NewOpenAICompatible("test", "Test", leaky.URL, "model", "super-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Chat(context.Background(), "tenant", "hello")
	if err == nil || strings.Contains(err.Error(), "super-secret-key") {
		t.Fatalf("provider error was not sanitized: %v", err)
	}

	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`)
		_, _ = io.WriteString(w, strings.Repeat("x", maxAIProviderResponseBytes+1))
		_, _ = io.WriteString(w, `"}}]}`)
	}))
	defer oversized.Close()
	client, err = NewOpenAICompatible("test", "Test", oversized.URL, "model", "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized provider response was accepted: %v", err)
	}
}

func TestOpenAICompatibleRejectsUserinfoAndCrossOriginRedirect(t *testing.T) {
	if _, err := NewOpenAICompatible("test", "Test", "https://secret@example.com", "model", "key"); err == nil {
		t.Fatal("provider URL with userinfo was accepted")
	}
	if _, err := NewOpenAICompatible("test", "Test", "https://example.com?token=secret", "model", "key"); err == nil {
		t.Fatal("provider URL with query credentials was accepted")
	}

	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	client, err := NewOpenAICompatible("test", "Test", redirector.URL, "model", "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil {
		t.Fatal("cross-origin redirect was followed")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load())
	}
}

func TestOpenAICompatibleLimitsSameOriginRedirects(t *testing.T) {
	var requests atomic.Int32
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, serverURL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	serverURL = server.URL
	defer server.Close()
	client, err := NewOpenAICompatible("test", "Test", server.URL, "model", "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil {
		t.Fatal("same-origin redirect loop was not rejected")
	}
	if got := requests.Load(); got > 10 {
		t.Fatalf("redirect loop made %d requests", got)
	}
}

func TestDecodeRuleDraftNormalizesObjectShapedModelOutput(t *testing.T) {
	rule, err := aioutput.DecodeRuleDraft(`{"name":"smoke_detector_high_alarm","alarmType":"smoke","level":"high","match":{"deviceType":"smoke_detector"},"conditions":{"smoke":true},"durationSeconds":0,"recovery":{"event":"smoke_clear"},"actions":{"type":"open_camera","cameraId":"camera-001"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rule.AlarmType != "SMOKE_DETECTED" || rule.Level != "HIGH" || rule.Match != "all" {
		t.Fatalf("unexpected normalized rule: %#v", rule)
	}
	if len(rule.Conditions) != 1 || rule.Conditions[0].Field != "smoke" || rule.Conditions[0].Operator != "eq" || rule.Conditions[0].Value != true {
		t.Fatalf("unexpected conditions: %#v", rule.Conditions)
	}
	if len(rule.Recovery) != 1 || rule.Recovery[0].Field != "event" || rule.Recovery[0].Value != "smoke_clear" {
		t.Fatalf("unexpected recovery: %#v", rule.Recovery)
	}
	if len(rule.Actions) != 1 || rule.Actions[0].Type != "OPEN_CAMERA" || rule.Actions[0].CameraID != "camera-001" {
		t.Fatalf("unexpected actions: %#v", rule.Actions)
	}
}

func TestProviderRegistry(t *testing.T) {
	registry := NewProviderRegistry()
	items := registry.List()
	if len(items) < 4 {
		t.Fatalf("expected built-in provider plugins, got %#v", items)
	}
	for _, item := range items {
		if item.ID != "disabled" && !item.Enabled {
			t.Fatalf("provider %q is not selectable in the test sandbox: %#v", item.ID, item)
		}
	}
	if _, err := registry.Create(ports.AIPluginConfig{Provider: "deepseek"}); err == nil {
		t.Fatal("DeepSeek plugin accepted a missing API key")
	}
	client, err := registry.Create(ports.AIPluginConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if info := client.(ports.AIInspectable).ProviderInfo(); info.ID != "disabled" || info.Enabled {
		t.Fatalf("unexpected fallback plugin %#v", info)
	}
}
