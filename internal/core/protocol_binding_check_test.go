package core

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

// With the platform parsers, only standard messages are parsed without a
// protocol binding; a JSON payload of an unbound template is a recorded parse
// failure, and the binding check lists that template.
func TestUnboundTemplatesAreNotGuessed(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.Product{
		{ID: "json_sensor", TenantID: "t1", Name: "未绑定 JSON", ProtocolPackageID: "json@1.0.0"},
		{ID: "std", TenantID: "t1", Name: "标准", ProtocolPackageID: parser.StandardProtocolID + "@1.0.0"},
		{ID: "pkg", TenantID: "t1", Name: "协议包", ProtocolPackageID: "published"},
		{ID: "bound", TenantID: "t1", Name: "已绑定", ProtocolPackageID: "x"},
	} {
		if err = repo.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err = repo.SaveProtocolPackage(ctx, model.ProtocolPackage{ID: "published", TenantID: "t1", Status: "PUBLISHED", ParserType: "configurable_json_parser"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t1", ProtocolID: "p", Version: "1", Status: "PUBLISHED", ParserType: "custom_json_parser"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t1", ProductID: "bound", ProtocolID: "p", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	raw := model.RawMessage{MessageID: "raw_unbound", TenantID: "t1", ProductID: "json_sensor", DeviceID: "d1", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1, Payload: json.RawMessage(`{"properties":{"temperature":20}}`)}
	if _, _, err = e.IngestRaw(ctx, raw); err != nil {
		t.Fatal(err)
	}
	idx, err := repo.GetRawIndex(ctx, "t1", "raw_unbound")
	if err != nil || !strings.Contains(idx.ParseError, "未绑定") {
		t.Fatalf("unbound JSON must fail to parse: %+v %v", idx, err)
	}
	bound := model.RawMessage{MessageID: "raw_bound", TenantID: "t1", ProductID: "bound", DeviceID: "d2", Protocol: "json", PayloadFormat: "json", ReceivedAt: 2, Payload: json.RawMessage(`{"properties":{"temperature":20}}`)}
	if _, _, err = e.IngestRaw(ctx, bound); err != nil {
		t.Fatal(err)
	}
	if idx, err = repo.GetRawIndex(ctx, "t1", "raw_bound"); err != nil || idx.ParseError != "" {
		t.Fatalf("explicitly bound parser must still parse: %+v %v", idx, err)
	}
	unbound, err := e.UnboundProducts(ctx, "t1")
	if err != nil || len(unbound) != 1 || unbound[0].ID != "json_sensor" || unbound[0].Reason == "" {
		t.Fatalf("unbound products %+v %v", unbound, err)
	}
}
