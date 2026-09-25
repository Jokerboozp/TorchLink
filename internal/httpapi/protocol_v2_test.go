package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"       /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"mime/multipart"    /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestImportModbusTCPV2RequiresGoPackage(t *testing.T) { /* 定义 TestImportModbusTCPV2RequiresGoPackage 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                                       /* 更新 log 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusTCPParser{}), log) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                                                              /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(context.Background()); err != nil {                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                             /* 更新 cfg 的值。 */
	cfg.DataDir = t.TempDir()                                                        /* 更新 cfg.DataDir 的值。 */
	cfg.JWTSecret = "protocol-v2-test-secret-at-least-32"                            /* 更新 cfg.JWTSecret 的值。 */
	cfg.AdminTenants = []string{"tenant_001"}                                        /* 更新 cfg.AdminTenants 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)                 /* 更新 api 的值。 */
	token, err := api.auth.Issue("tester", "tenant_001", "operator", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	server := httptest.NewServer(api.Handler())                                /* 更新 server 的值。 */
	defer server.Close()                                                       /* 安排函数结束时执行清理。 */
	csv := []byte("标识,名称,功能码,地址,数据类型,倍率\ntemperature,温度,03,40001,int16,0.1\n") /* 更新 csv 的值。 */
	doImport := func() int {                                                   /* 更新 doImport 的值。 */
		var body bytes.Buffer                                                                                                                                                                       /* 声明 body。 */
		writer := multipart.NewWriter(&body)                                                                                                                                                        /* 更新 writer 的值。 */
		part, _ := writer.CreateFormFile("file", "points.csv")                                                                                                                                      /* 更新 _ 的值。 */
		_, _ = part.Write(csv)                                                                                                                                                                      /* 更新 _ 的值。 */
		for key, value := range map[string]string{"protocolId": "pump-modbus", "version": "1.0.0", "name": "消防泵 Modbus", "productId": "pump-product", "deviceId": "pump-01", "host": "127.0.0.1"} { /* 循环处理当前数据。 */
			_ = writer.WriteField(key, value) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		_ = writer.Close()                                                                        /* 更新 _ 的值。 */
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v2/modbus-tcp/import", &body) /* 更新 _ 的值。 */
		req.Header.Set("Content-Type", writer.FormDataContentType())                              /* 执行当前语句并推进处理流程。 */
		req.Header.Set("Authorization", "Bearer "+token)                                          /* 执行当前语句并推进处理流程。 */
		response, requestErr := server.Client().Do(req)                                           /* 更新 requestErr 的值。 */
		if requestErr != nil {                                                                    /* 判断条件并选择处理分支。 */
			t.Fatal(requestErr) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer response.Body.Close() /* 安排函数结束时执行清理。 */
		return response.StatusCode  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if status := doImport(); status != http.StatusUnprocessableEntity { /* 判断条件并选择处理分支。 */
		t.Fatalf("new builtin import status=%d", status) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := repo.GetProtocolRelease(context.Background(), "tenant_001", "pump-modbus", "1.0.0"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("legacy release unexpectedly created") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

} /* 结束当前表达式或代码块。 */

func TestProtocolPackageV2RejectsTraversal(t *testing.T) { /* 定义 TestProtocolPackageV2RejectsTraversal 函数。 */
	var body bytes.Buffer                      /* 声明 body。 */
	writer := zip.NewWriter(&body)             /* 更新 writer 的值。 */
	entry, err := writer.Create("../artifact") /* 更新 err 的值。 */
	if err != nil {                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = entry.Write([]byte("bad"))                                              /* 更新 _ 的值。 */
	_ = writer.Close()                                                             /* 更新 _ 的值。 */
	reader, err := zip.NewReader(bytes.NewReader(body.Bytes()), int64(body.Len())) /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = inspectProtocolPackageV2(reader); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected traversal package to be rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProtocolPackageV2RejectsDuplicateNormalizedEntry(t *testing.T) { /* 定义 TestProtocolPackageV2RejectsDuplicateNormalizedEntry 函数。 */
	var body bytes.Buffer                                                     /* 声明 body。 */
	writer := zip.NewWriter(&body)                                            /* 更新 writer 的值。 */
	for _, name := range []string{"workers/artifact", "workers/./artifact"} { /* 循环处理当前数据。 */
		entry, err := writer.Create(name) /* 更新 err 的值。 */
		if err != nil {                   /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		_, _ = entry.Write([]byte(name)) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := writer.Close(); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	reader, err := zip.NewReader(bytes.NewReader(body.Bytes()), int64(body.Len())) /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = inspectProtocolPackageV2(reader); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected duplicate normalized entry to be rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestRemovedFieldProfilesCannotRunOnCentre(t *testing.T) { /* 定义 TestRemovedFieldProfilesCannotRunOnCentre 函数。 */
	base := model.DeviceAccessProfile{ID: "p", TenantID: "t", DeviceID: "d", ProductID: "product", ProtocolID: "protocol", ProtocolVersion: "1", Host: "127.0.0.1", Port: 502, UnitID: 1, TimeoutMs: 1000, Mode: "poll", Network: "tcp"} /* 更新 base 的值。 */
	if err := validateAccessProfile(base); err != nil {                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, network := range []string{"serial", "opc_ua", "snmp", "bacnet", "onvif"} { /* 循环处理当前数据。 */
		p := base                                        /* 更新 p 的值。 */
		p.Network = network                              /* 更新 p.Network 的值。 */
		if err := validateAccessProfile(p); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("removed network %s accepted", network) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, mode := range []string{"poll", "listener"} { /* 循环处理当前数据。 */
		p := base                                        /* 更新 p 的值。 */
		p.Mode = mode                                    /* 更新 p.Mode 的值。 */
		p.EdgeNodeID = "legacy-node"                     /* 更新 p.EdgeNodeID 的值。 */
		if err := validateAccessProfile(p); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("legacy %s assignment accepted", mode) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
