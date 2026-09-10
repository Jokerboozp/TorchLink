package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestImportModbusTCPV2RequiresGoPackage(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusTCPParser{}), log)
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = t.TempDir()
	cfg.JWTSecret = "protocol-v2-test-secret-at-least-32"
	cfg.AdminTenants = []string{"tenant_001"}
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)
	token, err := api.auth.Issue("tester", "tenant_001", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	csv := []byte("标识,名称,功能码,地址,数据类型,倍率\ntemperature,温度,03,40001,int16,0.1\n")
	doImport := func() int {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "points.csv")
		_, _ = part.Write(csv)
		for key, value := range map[string]string{"protocolId": "pump-modbus", "version": "1.0.0", "name": "消防泵 Modbus", "productId": "pump-product", "deviceId": "pump-01", "host": "127.0.0.1"} {
			_ = writer.WriteField(key, value)
		}
		_ = writer.Close()
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v2/modbus-tcp/import", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		response, requestErr := server.Client().Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := doImport(); status != http.StatusUnprocessableEntity {
		t.Fatalf("new builtin import status=%d", status)
	}
	if _, err := repo.GetProtocolRelease(context.Background(), "tenant_001", "pump-modbus", "1.0.0"); err == nil {
		t.Fatal("legacy release unexpectedly created")
	}

}

func TestProtocolPackageV2RejectsTraversal(t *testing.T) {
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	entry, err := writer.Create("../artifact")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	_ = writer.Close()
	reader, err := zip.NewReader(bytes.NewReader(body.Bytes()), int64(body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = inspectProtocolPackageV2(reader); err == nil {
		t.Fatal("expected traversal package to be rejected")
	}
}

func TestProtocolPackageV2RejectsDuplicateNormalizedEntry(t *testing.T) {
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	for _, name := range []string{"workers/artifact", "workers/./artifact"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte(name))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(body.Bytes()), int64(body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = inspectProtocolPackageV2(reader); err == nil {
		t.Fatal("expected duplicate normalized entry to be rejected")
	}
}

func TestRemovedFieldProfilesCannotRunOnCentre(t *testing.T) {
	base := model.DeviceAccessProfile{ID: "p", TenantID: "t", DeviceID: "d", ProductID: "product", ProtocolID: "protocol", ProtocolVersion: "1", Host: "127.0.0.1", Port: 502, UnitID: 1, TimeoutMs: 1000, Mode: "poll", Network: "tcp"}
	if err := validateAccessProfile(base); err != nil {
		t.Fatal(err)
	}
	for _, network := range []string{"serial", "opc_ua", "snmp", "bacnet", "onvif"} {
		p := base
		p.Network = network
		if err := validateAccessProfile(p); err == nil {
			t.Fatalf("removed network %s accepted", network)
		}
	}
	for _, mode := range []string{"poll", "listener"} {
		p := base
		p.Mode = mode
		p.EdgeNodeID = "legacy-node"
		if err := validateAccessProfile(p); err == nil {
			t.Fatalf("legacy %s assignment accepted", mode)
		}
	}
}
