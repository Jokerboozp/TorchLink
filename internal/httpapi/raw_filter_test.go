package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestRawFilterValidation(t *testing.T) {
	for _, query := range []string{"start=abc", "end=-1", "start=20&end=10", "parseStatus=ANY", "messageType=unknown"} {
		q, _ := url.ParseQuery(query)
		if _, err := parseRawFilter(q); err == nil {
			t.Fatalf("accepted invalid query %s", query)
		}
	}
	q, _ := url.ParseQuery("deviceId=+d+&parseStatus=parsed&messageType=alarm_report&start=10&end=20")
	f, err := parseRawFilter(q)
	if err != nil || f.DeviceID != "d" || f.ParseStatus != "PARSED" || f.MessageType != "ALARM_REPORT" || f.Start != 10 || f.End != 20 {
		t.Fatalf("bad normalized filter %+v %v", f, err)
	}
}

func TestRawFiltersHTTP(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	for _, row := range []model.RawArchiveIndex{
		{TenantID: "t", MessageID: "failed", DeviceID: "d", ProductID: "p", Protocol: "tcp", PayloadFormat: "hex", ReceivedAt: 2000, ParseError: "invalid"},
		{TenantID: "t", MessageID: "parsed", DeviceID: "d", ProductID: "p", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1000},
		{TenantID: "other", MessageID: "foreign", DeviceID: "d", ProductID: "p", Protocol: "json", PayloadFormat: "json", ReceivedAt: 3000},
	} {
		if _, err := repo.SaveRawIndex(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveStandardMessage(ctx, model.StandardMessage{TenantID: "t", MessageID: "s", RawMessageID: "parsed", DeviceID: "d", MessageType: model.AlarmReport, Parser: "json_parser"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "raw-filter-http-test"
	cfg.AdminTenants = []string{"t"}
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	token, err := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"deviceId=d&productId=p&messageId=parsed", "parseStatus=PARSED&messageType=ALARM_REPORT&parser=json_parser&protocol=JSON&payloadFormat=JSON&start=1000&end=1000"} {
		result := requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/raw-messages?"+query, token, nil, 200)
		items := result["items"].([]any)
		if result["total"].(float64) != 1 || len(items) != 1 || items[0].(map[string]any)["messageId"] != "parsed" || items[0].(map[string]any)["parsed"] != true {
			t.Fatalf("filtered response %+v", result)
		}
	}
	requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/raw-messages?start=no", token, nil, 400)
}
