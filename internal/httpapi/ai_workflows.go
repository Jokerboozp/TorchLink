package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/core"   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (s *Server) runAIAlarmAnalysis(w http.ResponseWriter, r *http.Request) { /* 定义 runAIAlarmAnalysis 函数。 */
	job, err := s.startAIAnalysisJob(r.Context(), claims(r).TenantID, r.PathValue("alarmId"), claims(r).Username, alarmAnalysisRunScope(r.Context()), aiRunIdentity(r.Context(), claims(r))) /* 按发起人角色决定是否引用知识库。 */
	if err != nil {
		problem(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	write(w, http.StatusAccepted, aiAnalysisJobView(job)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) healthInspection(w http.ResponseWriter, r *http.Request) { /* 定义 healthInspection 函数。 */
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)                                /* 更新 cancel 的值。 */
	defer cancel()                                                                                /* 安排函数结束时执行清理。 */
	report, err := s.engine.InspectDeviceHealth(aiRunContext(ctx, claims(r)), claims(r).TenantID) /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadGateway, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.rememberHealthInspection(ctx, claims(r).TenantID, claims(r).Username, report) /* 执行当前语句并推进处理流程。 */
	write(w, http.StatusOK, report)                                                 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) healthInspectionPDF(w http.ResponseWriter, r *http.Request) { /* 定义 healthInspectionPDF 函数。 */
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute) /* 更新 cancel 的值。 */
	defer cancel()                                                 /* 安排函数结束时执行清理。 */
	tenantID := claims(r).TenantID                                 /* 更新 tenantID 的值。 */
	report, ok := s.recentHealthInspection(ctx, tenantID)          /* 更新 ok 的值。 */
	if !ok {                                                       /* 判断条件并选择处理分支。 */
		if job, found, _ := s.loadHealthInspectionJob(ctx, tenantID); found && job.Status == "running" { /* 判断条件并选择处理分支。 */
			problem(w, http.StatusConflict, "智能巡检仍在进行，请等待任务完成后再下载报告") /* 执行当前语句并推进处理流程。 */
			return                                                    /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var err error                                                                      /* 声明 err。 */
		report, err = s.engine.InspectDeviceHealth(aiRunContext(ctx, claims(r)), tenantID) /* 更新 err 的值。 */
		if err != nil {                                                                    /* 判断条件并选择处理分支。 */
			problem(w, http.StatusBadGateway, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                                         /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		s.rememberHealthInspection(ctx, tenantID, claims(r).Username, report) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	data, err := core.RenderHealthInspectionPDF(report) /* 更新 err 的值。 */
	if err != nil {                                     /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	filename := fmt.Sprintf("health-inspection-%d.pdf", report.GeneratedAt)                                                                                             /* 更新 filename 的值。 */
	w.Header().Set("Content-Type", "application/pdf")                                                                                                                   /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filename, url.QueryEscape("智能巡检结果.pdf")))                       /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))                                                                                                           /* 执行当前语句并推进处理流程。 */
	s.audit(r, "ai.health-inspection.download", "device-health", fmt.Sprintf("inspection_%d", report.GeneratedAt), map[string]any{"format": "pdf", "bytes": len(data)}) /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(http.StatusOK)                                                                                                                                        /* 执行当前语句并推进处理流程。 */
	_, _ = w.Write(data)                                                                                                                                                /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */

// rememberHealthInspection stores a report produced outside a background job
// (synchronous inspection or PDF regeneration) as a completed job, so the PDF
// download on any replica reuses it.
func (s *Server) rememberHealthInspection(ctx context.Context, tenantID, actor string, report model.DeviceHealthReport) { /* 定义 rememberHealthInspection 函数。 */
	now := time.Now().UnixMilli()
	job := model.HealthInspectionJob{ID: "inspection_job_" + randomHex(10), TenantID: tenantID, Actor: actor, Status: "succeeded", Stage: "completed", Message: "智能巡检已完成", Progress: 100, StartedAt: now, UpdatedAt: now, FinishedAt: now, Report: report}
	if _, err := s.engine.Repo.CreateHealthInspectionJob(ctx, job); err != nil && s.log != nil {
		s.log.Warn("save health inspection report failed", "tenant", tenantID, "error", err)
	}
} /* 结束当前表达式或代码块。 */

// recentHealthInspection returns the newest completed report that is still
// fresh enough to download without inspecting again.
func (s *Server) recentHealthInspection(ctx context.Context, tenantID string) (model.DeviceHealthReport, bool) { /* 定义 recentHealthInspection 函数。 */
	job, err := s.engine.Repo.LatestHealthInspectionJob(ctx, tenantID, "succeeded")
	if err != nil || time.Since(time.UnixMilli(job.FinishedAt)) > healthInspectionCacheTTL {
		return model.DeviceHealthReport{}, false
	}
	return job.Report, true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) generateProtocolAssistant(w http.ResponseWriter, r *http.Request) { /* 定义 generateProtocolAssistant 函数。 */
	const maxDocumentBytes = 32 << 20                                 /* 声明 maxDocumentBytes。 */
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentBytes+(1<<20)) /* 更新 r.Body 的值。 */
	if err := r.ParseMultipartForm(1 << 20); err != nil {             /* 判断条件并选择处理分支。 */
		var maxErr *http.MaxBytesError /* 声明 maxErr。 */
		if errors.As(err, &maxErr) {   /* 判断条件并选择处理分支。 */
			problem(w, http.StatusRequestEntityTooLarge, "protocol document exceeds 32 MiB") /* 执行当前语句并推进处理流程。 */
			return                                                                           /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, http.StatusBadRequest, "invalid multipart form") /* 执行当前语句并推进处理流程。 */
		return                                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if r.MultipartForm != nil { /* 判断条件并选择处理分支。 */
		defer r.MultipartForm.RemoveAll() /* 安排函数结束时执行清理。 */
	} /* 结束当前表达式或代码块。 */
	document, err := readProtocolAssistantDocument(r, maxDocumentBytes) /* 更新 err 的值。 */
	if err != nil {                                                     /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pointTable := strings.TrimSpace(r.FormValue("pointTable"))                                            /* 更新 pointTable 的值。 */
	if document.Text == "" && pointTable == "" && strings.TrimSpace(r.FormValue("samplePayload")) == "" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "protocol document or point table is required") /* 执行当前语句并推进处理流程。 */
		return                                                                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	input := core.ProtocolAssistantInput{ /* 更新 input 的值。 */
		InputKind:        r.FormValue("inputKind"),                        /* 执行当前语句并推进处理流程。 */
		Name:             strings.TrimSpace(r.FormValue("name")),          /* 执行当前语句并推进处理流程。 */
		Protocol:         strings.TrimSpace(r.FormValue("protocol")),      /* 执行当前语句并推进处理流程。 */
		Transport:        strings.TrimSpace(r.FormValue("transport")),     /* 执行当前语句并推进处理流程。 */
		PayloadFormat:    strings.TrimSpace(r.FormValue("payloadFormat")), /* 执行当前语句并推进处理流程。 */
		DocumentText:     document.Text,                                   /* 执行当前语句并推进处理流程。 */
		PointTable:       pointTable,                                      /* 执行当前语句并推进处理流程。 */
		SamplePayload:    strings.TrimSpace(r.FormValue("samplePayload")), /* 执行当前语句并推进处理流程。 */
		DocumentFilename: document.Filename,                               /* 执行当前语句并推进处理流程。 */
		DocumentData:     document.Data,                                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if input.InputKind != "" && input.InputKind != "sample" && input.InputKind != "point-table" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "unsupported upload type") /* 执行当前语句并推进处理流程。 */
		return                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(input.SamplePayload) > 1<<20 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "sample payload exceeds 1 MiB") /* 执行当前语句并推进处理流程。 */
		return                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)                                            /* 更新 cancel 的值。 */
	defer cancel()                                                                                            /* 安排函数结束时执行清理。 */
	draft, err := s.engine.GenerateProtocolAssistant(aiRunContext(ctx, claims(r)), claims(r).TenantID, input) /* 更新 err 的值。 */
	if err != nil {                                                                                           /* 判断条件并选择处理分支。 */
		status := http.StatusBadGateway            /* 更新 status 的值。 */
		if errors.Is(err, core.ErrProtocolInput) { /* 判断条件并选择处理分支。 */
			status = http.StatusUnprocessableEntity /* 更新 status 的值。 */
		} /* 结束当前表达式或代码块。 */
		if errors.Is(err, context.DeadlineExceeded) { /* 判断条件并选择处理分支。 */
			status = http.StatusGatewayTimeout /* 更新 status 的值。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, status, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "ai.protocol-assistant.generate", "protocol-draft", "draft", map[string]any{"filename": protocolAssistantFilename(r), "fields": len(draft.Fields), "payloadFormat": draft.PayloadFormat}) /* 执行当前语句并推进处理流程。 */
	write(w, http.StatusOK, draft)                                                                                                                                                                       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) previewProtocolAssistant(w http.ResponseWriter, r *http.Request) { /* 定义 previewProtocolAssistant 函数。 */
	var in struct { /* 声明 in。 */
		Draft         model.ProtocolAssistantDraft `json:"draft"`         /* 执行当前语句并推进处理流程。 */
		Source        string                       `json:"source"`        /* 执行当前语句并推进处理流程。 */
		Payload       json.RawMessage              `json:"payload"`       /* 执行当前语句并推进处理流程。 */
		PayloadFormat string                       `json:"payloadFormat"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	draft := in.Draft                       /* 更新 draft 的值。 */
	if strings.TrimSpace(in.Source) != "" { /* 判断条件并选择处理分支。 */
		draft.Source = in.Source /* 更新 draft.Source 的值。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(in.PayloadFormat) != "" { /* 判断条件并选择处理分支。 */
		draft.PayloadFormat = strings.TrimSpace(in.PayloadFormat) /* 更新 draft.PayloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	payload, err := assistantPayloadText(in.Payload, draft.PayloadFormat) /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	message, err := core.PreviewProtocolAssistant(draft, claims(r).TenantID, payload) /* 更新 err 的值。 */
	if err != nil {                                                                   /* 判断条件并选择处理分支。 */
		write(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()}) /* 执行当前语句并推进处理流程。 */
		return                                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, http.StatusOK, map[string]any{"success": true, "standardMessage": message}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) publishProtocolAssistant(w http.ResponseWriter, r *http.Request) { /* 定义 publishProtocolAssistant 函数。 */
	var in struct { /* 声明 in。 */
		ID            string                       `json:"id"`            /* 执行当前语句并推进处理流程。 */
		Version       string                       `json:"version"`       /* 执行当前语句并推进处理流程。 */
		Status        string                       `json:"status"`        /* 执行当前语句并推进处理流程。 */
		Payload       json.RawMessage              `json:"payload"`       /* 执行当前语句并推进处理流程。 */
		PayloadFormat string                       `json:"payloadFormat"` /* 执行当前语句并推进处理流程。 */
		Draft         model.ProtocolAssistantDraft `json:"draft"`         /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if generatedMapping(in.Draft.ParserType) { /* 判断条件并选择处理分支。 */
		s.saveGeneratedProtocol(w, r, in.ID, in.Version, in.Draft, in.Payload, in.PayloadFormat) /* 执行当前语句并推进处理流程。 */
		return                                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	draft := in.Draft                              /* 更新 draft 的值。 */
	if strings.TrimSpace(in.PayloadFormat) != "" { /* 判断条件并选择处理分支。 */
		draft.PayloadFormat = strings.TrimSpace(in.PayloadFormat) /* 更新 draft.PayloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	id := strings.TrimSpace(in.ID) /* 更新 id 的值。 */
	if id == "" {                  /* 判断条件并选择处理分支。 */
		id = "protocol_ai_" + randomHex(6) /* 更新 id 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(id) > 128 || strings.ContainsAny(id, "/\\") { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "invalid protocol package id") /* 执行当前语句并推进处理流程。 */
		return                                                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	status := strings.ToUpper(strings.TrimSpace(in.Status)) /* 更新 status 的值。 */
	if status == "" {                                       /* 判断条件并选择处理分支。 */
		status = "DRAFT" /* 更新 status 的值。 */
	} /* 结束当前表达式或代码块。 */
	if status != "DRAFT" && status != "PUBLISHED" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "status must be DRAFT or PUBLISHED") /* 执行当前语句并推进处理流程。 */
		return                                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if status == "PUBLISHED" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "专用协议请到 Go 源码接入上传、测试并发布；协议助手只保存映射草稿") /* 执行当前语句并推进处理流程。 */
		return                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parserType := strings.TrimSpace(draft.ParserType)                                           /* 更新 parserType 的值。 */
	if parserType != parser.ModbusCoilParserName && parserType != parser.GoProtocolParserName { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "协议助手只支持 Go 协议映射草稿") /* 执行当前语句并推进处理流程。 */
		return                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if draft.Config == nil { /* 判断条件并选择处理分支。 */
		draft.Config = map[string]any{} /* 更新 draft.Config 的值。 */
	} /* 结束当前表达式或代码块。 */
	if parserType == parser.ModbusCoilParserName { /* 判断条件并选择处理分支。 */
		if draft.MessageType == "" { /* 判断条件并选择处理分支。 */
			draft.MessageType = model.PropertyReport /* 更新 draft.MessageType 的值。 */
		} /* 结束当前表达式或代码块。 */
		draft.Config["messageType"] = string(draft.MessageType)               /* 执行当前语句并推进处理流程。 */
		if err := parser.ValidateModbusCoilConfig(draft.Config); err != nil { /* 判断条件并选择处理分支。 */
			problem(w, http.StatusUnprocessableEntity, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                                                  /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	var message *model.StandardMessage                       /* 声明 message。 */
	var err error                                            /* 声明 err。 */
	if len(in.Payload) > 0 && string(in.Payload) != "null" { /* 判断条件并选择处理分支。 */
		payload, payloadErr := assistantPayloadText(in.Payload, draft.PayloadFormat) /* 更新 payloadErr 的值。 */
		if payloadErr != nil {                                                       /* 判断条件并选择处理分支。 */
			problem(w, http.StatusUnprocessableEntity, payloadErr.Error()) /* 执行当前语句并推进处理流程。 */
			return                                                         /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if parserType == parser.ModbusCoilParserName { /* 判断条件并选择处理分支。 */
			message, err = core.PreviewProtocolAssistant(draft, claims(r).TenantID, payload) /* 更新 err 的值。 */
			if err != nil {                                                                  /* 判断条件并选择处理分支。 */
				problem(w, http.StatusUnprocessableEntity, "发布前解析校验失败："+err.Error()) /* 执行当前语句并推进处理流程。 */
				return                                                               /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                                                                                                                                                                                                                                                                     /* 更新 now 的值。 */
	pkg := model.ProtocolPackage{ID: id, TenantID: claims(r).TenantID, Name: draft.Name, Version: in.Version, Protocol: draft.Protocol, Transport: draft.Transport, PayloadFormat: draft.PayloadFormat, ParserType: parserType, Status: status, Description: draft.Description, Config: draft.Config, CreatedAt: now, UpdatedAt: now} /* 更新 pkg 的值。 */
	if pkg.Name == "" {                                                                                                                                                                                                                                                                                                               /* 判断条件并选择处理分支。 */
		pkg.Name = "AI 协议解析包" /* 更新 pkg.Name 的值。 */
	} /* 结束当前表达式或代码块。 */
	if pkg.Version == "" { /* 判断条件并选择处理分支。 */
		pkg.Version = "1.0.0" /* 更新 pkg.Version 的值。 */
	} /* 结束当前表达式或代码块。 */
	if pkg.Protocol == "" { /* 判断条件并选择处理分支。 */
		pkg.Protocol = "custom-go-worker" /* 更新 pkg.Protocol 的值。 */
	} /* 结束当前表达式或代码块。 */
	if pkg.Transport == "" { /* 判断条件并选择处理分支。 */
		pkg.Transport = "MQTT" /* 更新 pkg.Transport 的值。 */
	} /* 结束当前表达式或代码块。 */
	if pkg.PayloadFormat == "" { /* 判断条件并选择处理分支。 */
		pkg.PayloadFormat = "json" /* 更新 pkg.PayloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	if old, getErr := s.engine.Repo.GetProtocolPackage(r.Context(), pkg.TenantID, pkg.ID); getErr == nil { /* 判断条件并选择处理分支。 */
		pkg.CreatedAt = old.CreatedAt /* 更新 pkg.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err = s.engine.Repo.SaveProtocolPackage(r.Context(), pkg); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "ai.protocol-assistant.publish", "protocolPackage", pkg.ID, map[string]any{"version": pkg.Version, "status": pkg.Status, "fields": len(draft.Fields)}) /* 执行当前语句并推进处理流程。 */
	response := map[string]any{"package": pkg}                                                                                                                        /* 更新 response 的值。 */
	if message != nil {                                                                                                                                               /* 判断条件并选择处理分支。 */
		response["standardMessage"] = message /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	write(w, http.StatusCreated, response) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type protocolAssistantDocument struct { /* 定义 protocolAssistantDocument 类型。 */
	Filename string /* 执行当前语句并推进处理流程。 */
	Data     []byte /* 执行当前语句并推进处理流程。 */
	Text     string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func readProtocolAssistantDocument(r *http.Request, maximum int) (protocolAssistantDocument, error) { /* 定义 readProtocolAssistantDocument 函数。 */
	f, header, err := r.FormFile("file") /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		if errors.Is(err, http.ErrMissingFile) { /* 判断条件并选择处理分支。 */
			return protocolAssistantDocument{}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return protocolAssistantDocument{}, fmt.Errorf("read protocol document: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer f.Close()                                              /* 安排函数结束时执行清理。 */
	data, err := io.ReadAll(io.LimitReader(f, int64(maximum)+1)) /* 更新 err 的值。 */
	if err != nil {                                              /* 判断条件并选择处理分支。 */
		return protocolAssistantDocument{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) > maximum { /* 判断条件并选择处理分支。 */
		return protocolAssistantDocument{}, errors.New("protocol document exceeds 32 MiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if r.FormValue("inputKind") == "sample" { /* 判断条件并选择处理分支。 */
		if len(data) > 1<<20 { /* 判断条件并选择处理分支。 */
			return protocolAssistantDocument{}, errors.New("报文样本不能超过 1 MiB") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		text := string(data)                                    /* 更新 text 的值。 */
		switch strings.ToLower(filepath.Ext(header.Filename)) { /* 根据条件选择处理路径。 */
		case ".bin": /* 处理当前分支。 */
			text = hex.EncodeToString(data) /* 更新 text 的值。 */
		case ".json", ".txt", ".hex": /* 处理当前分支。 */
		default: /* 处理当前分支。 */
			return protocolAssistantDocument{}, errors.New("报文文件支持 JSON / TXT / HEX / BIN") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return protocolAssistantDocument{Filename: header.Filename, Data: data, Text: text}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	text, err := core.ExtractKnowledgeText(header.Filename, data) /* 更新 err 的值。 */
	if err != nil {                                               /* 判断条件并选择处理分支。 */
		return protocolAssistantDocument{}, fmt.Errorf("extract protocol document: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return protocolAssistantDocument{Filename: header.Filename, Data: data, Text: text}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func protocolAssistantFilename(r *http.Request) string { /* 定义 protocolAssistantFilename 函数。 */
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) == 0 { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.MultipartForm.File["file"][0].Filename /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func assistantPayloadText(raw json.RawMessage, format string) (string, error) { /* 定义 assistantPayloadText 函数。 */
	if len(raw) == 0 { /* 判断条件并选择处理分支。 */
		return "", errors.New("sample payload is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.EqualFold(format, "hex") { /* 判断条件并选择处理分支。 */
		var value string                                    /* 声明 value。 */
		if err := json.Unmarshal(raw, &value); err != nil { /* 判断条件并选择处理分支。 */
			return "", errors.New("hex sample payload must be a JSON string") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return value, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !json.Valid(raw) { /* 判断条件并选择处理分支。 */
		return "", errors.New("JSON sample payload is invalid") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return string(raw), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
