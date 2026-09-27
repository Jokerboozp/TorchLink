package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func (s *Server) runAIAlarmAnalysis(w http.ResponseWriter, r *http.Request) {
	job, err := s.startAIAnalysisJob(r.Context(), claims(r).TenantID, r.PathValue("alarmId"), claims(r).Username, alarmAnalysisRunScope(r.Context()), aiRunIdentity(r.Context(), claims(r))) /* 按发起人角色决定是否引用知识库。 */
	if err != nil {
		problem(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	write(w, http.StatusAccepted, aiAnalysisJobView(job))
}

func (s *Server) healthInspection(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	report, err := s.engine.InspectDeviceHealth(aiRunContext(ctx, claims(r)), claims(r).TenantID)
	if err != nil {
		problem(w, http.StatusBadGateway, err.Error())
		return
	}
	job, err := s.rememberHealthInspection(ctx, claims(r).TenantID, claims(r).Username, report)
	if err != nil {
		problem(w, 503, "保存巡检报告失败")
		return
	}
	job.Report.Items = job.Report.Items[:min(100, len(job.Report.Items))]
	write(w, http.StatusOK, job.Report)
}

// A PDF download never starts a new inspection or loads all device rows.
func (s *Server) healthInspectionPDF(w http.ResponseWriter, r *http.Request) {
	select {
	case s.inspectionRequests <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "3")
		problem(w, 429, "巡检报告下载繁忙，请稍后重试")
		return
	}
	defer func() { <-s.inspectionRequests }()
	ctx := r.Context()
	tenant := claims(r).TenantID
	var job model.HealthInspectionJob
	var err error
	if id := r.URL.Query().Get("jobId"); id != "" {
		job, err = s.engine.Repo.HealthInspectionPage(ctx, tenant, id, 1, 0)
	} else {
		job, err = s.engine.Repo.LatestHealthInspectionSummary(ctx, tenant, "succeeded")
	}
	if errors.Is(err, model.ErrNotFound) {
		problem(w, 409, "请先完成一次智能巡检再下载报告")
		return
	}
	if err != nil {
		problem(w, 503, "读取巡检报告失败")
		return
	}
	if job.Status != "succeeded" {
		problem(w, 409, "智能巡检尚未完成")
		return
	}
	data, err := s.inspectionPDFs.load(ctx, tenant, job.ID, func(loadCtx context.Context) (model.DeviceHealthReport, error) {
		page, e := s.engine.Repo.HealthInspectionPage(loadCtx, tenant, job.ID, core.InspectionPDFMaxDevices, 0)
		if e == nil && page.Report.TotalItems > len(page.Report.Items) {
			page.Report.Warnings = append(page.Report.Warnings, fmt.Sprintf("PDF 仅展示前 %d 台设备明细，完整 %d 台设备请在巡检页面分页查看。", len(page.Report.Items), page.Report.TotalItems))
		}
		return page.Report, e
	})
	if errors.Is(err, errPDFBusy) {
		w.Header().Set("Retry-After", "3")
		problem(w, 429, "巡检报告生成繁忙，请稍后重试")
		return
	}
	if err != nil {
		problem(w, 500, "生成巡检报告失败")
		return
	}
	filename := fmt.Sprintf("health-inspection-%d.pdf", job.Report.GeneratedAt)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filename, url.QueryEscape("智能巡检结果.pdf")))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	s.audit(r, "ai.health-inspection.download", "device-health", job.ID, map[string]any{"format": "pdf", "bytes": len(data)})
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func (s *Server) rememberHealthInspection(ctx context.Context, tenantID, actor string, report model.DeviceHealthReport) (model.HealthInspectionJob, error) {
	now := time.Now().UnixMilli()
	job := model.HealthInspectionJob{ID: "inspection_job_" + randomHex(10), TenantID: tenantID, Actor: actor, Status: "succeeded", Stage: "completed", Message: "智能巡检已完成", Progress: 100, StartedAt: now, UpdatedAt: now, FinishedAt: now, Report: report}
	job.Report.ReportID, job.Report.TotalItems = job.ID, len(report.Items)
	_, err := s.engine.Repo.CreateHealthInspectionJob(ctx, job)
	return job, err
}

func (s *Server) healthInspectionPage(w http.ResponseWriter, r *http.Request) {
	limit, offset := 100, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 100 {
			problem(w, 400, "limit 必须为 1 到 100")
			return
		}
		limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 0 {
			problem(w, 400, "offset 必须是非负整数")
			return
		}
		offset = n
	}
	job, err := s.engine.Repo.HealthInspectionPage(r.Context(), claims(r).TenantID, r.PathValue("jobId"), limit, offset)
	if errors.Is(err, model.ErrNotFound) {
		problem(w, 404, "巡检报告不存在")
		return
	}
	if err != nil {
		problem(w, 503, "读取巡检报告失败")
		return
	}
	if job.Status != "succeeded" {
		problem(w, 409, "巡检尚未完成")
		return
	}
	write(w, 200, job.Report)
}

func (s *Server) generateProtocolAssistant(w http.ResponseWriter, r *http.Request) {
	const maxDocumentBytes = 32 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			problem(w, http.StatusRequestEntityTooLarge, "protocol document exceeds 32 MiB")
			return
		}
		problem(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	document, err := readProtocolAssistantDocument(r, maxDocumentBytes)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pointTable := strings.TrimSpace(r.FormValue("pointTable"))
	if document.Text == "" && pointTable == "" && strings.TrimSpace(r.FormValue("samplePayload")) == "" {
		problem(w, http.StatusUnprocessableEntity, "protocol document or point table is required")
		return
	}
	input := core.ProtocolAssistantInput{
		InputKind:        r.FormValue("inputKind"),
		Name:             strings.TrimSpace(r.FormValue("name")),
		Protocol:         strings.TrimSpace(r.FormValue("protocol")),
		Transport:        strings.TrimSpace(r.FormValue("transport")),
		PayloadFormat:    strings.TrimSpace(r.FormValue("payloadFormat")),
		DocumentText:     document.Text,
		PointTable:       pointTable,
		SamplePayload:    strings.TrimSpace(r.FormValue("samplePayload")),
		DocumentFilename: document.Filename,
		DocumentData:     document.Data,
	}
	if input.InputKind != "" && input.InputKind != "sample" && input.InputKind != "point-table" {
		problem(w, 422, "unsupported upload type")
		return
	}
	if len(input.SamplePayload) > 1<<20 {
		problem(w, 422, "sample payload exceeds 1 MiB")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	draft, err := s.engine.GenerateProtocolAssistant(aiRunContext(ctx, claims(r)), claims(r).TenantID, input)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, core.ErrProtocolInput) {
			status = http.StatusUnprocessableEntity
		}
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		problem(w, status, err.Error())
		return
	}
	s.audit(r, "ai.protocol-assistant.generate", "protocol-draft", "draft", map[string]any{"filename": protocolAssistantFilename(r), "fields": len(draft.Fields), "payloadFormat": draft.PayloadFormat})
	write(w, http.StatusOK, draft)
}

func (s *Server) previewProtocolAssistant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Draft         model.ProtocolAssistantDraft `json:"draft"`
		Source        string                       `json:"source"`
		Payload       json.RawMessage              `json:"payload"`
		PayloadFormat string                       `json:"payloadFormat"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	draft := in.Draft
	if strings.TrimSpace(in.Source) != "" {
		draft.Source = in.Source
	}
	if strings.TrimSpace(in.PayloadFormat) != "" {
		draft.PayloadFormat = strings.TrimSpace(in.PayloadFormat)
	}
	payload, err := assistantPayloadText(in.Payload, draft.PayloadFormat)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	message, err := core.PreviewProtocolAssistant(draft, claims(r).TenantID, payload)
	if err != nil {
		write(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true, "standardMessage": message})
}

func (s *Server) publishProtocolAssistant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID            string                       `json:"id"`
		Version       string                       `json:"version"`
		Status        string                       `json:"status"`
		Payload       json.RawMessage              `json:"payload"`
		PayloadFormat string                       `json:"payloadFormat"`
		Draft         model.ProtocolAssistantDraft `json:"draft"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if generatedMapping(in.Draft.ParserType) {
		s.saveGeneratedProtocol(w, r, in.ID, in.Version, in.Draft, in.Payload, in.PayloadFormat)
		return
	}
	draft := in.Draft
	if strings.TrimSpace(in.PayloadFormat) != "" {
		draft.PayloadFormat = strings.TrimSpace(in.PayloadFormat)
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "protocol_ai_" + randomHex(6)
	}
	if len(id) > 128 || strings.ContainsAny(id, "/\\") {
		problem(w, http.StatusUnprocessableEntity, "invalid protocol package id")
		return
	}
	status := strings.ToUpper(strings.TrimSpace(in.Status))
	if status == "" {
		status = "DRAFT"
	}
	if status != "DRAFT" && status != "PUBLISHED" {
		problem(w, http.StatusUnprocessableEntity, "status must be DRAFT or PUBLISHED")
		return
	}
	if status == "PUBLISHED" {
		problem(w, 422, "专用协议请到 Go 源码接入上传、测试并发布；协议助手只保存映射草稿")
		return
	}
	parserType := strings.TrimSpace(draft.ParserType)
	if parserType != parser.ModbusCoilParserName && parserType != parser.GoProtocolParserName {
		problem(w, http.StatusUnprocessableEntity, "协议助手只支持 Go 协议映射草稿")
		return
	}
	if draft.Config == nil {
		draft.Config = map[string]any{}
	}
	if parserType == parser.ModbusCoilParserName {
		if draft.MessageType == "" {
			draft.MessageType = model.PropertyReport
		}
		draft.Config["messageType"] = string(draft.MessageType)
		if err := parser.ValidateModbusCoilConfig(draft.Config); err != nil {
			problem(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}

	var message *model.StandardMessage
	var err error
	if len(in.Payload) > 0 && string(in.Payload) != "null" {
		payload, payloadErr := assistantPayloadText(in.Payload, draft.PayloadFormat)
		if payloadErr != nil {
			problem(w, http.StatusUnprocessableEntity, payloadErr.Error())
			return
		}
		if parserType == parser.ModbusCoilParserName {
			message, err = core.PreviewProtocolAssistant(draft, claims(r).TenantID, payload)
			if err != nil {
				problem(w, http.StatusUnprocessableEntity, "发布前解析校验失败："+err.Error())
				return
			}
		}
	}
	now := time.Now().UnixMilli()
	pkg := model.ProtocolPackage{ID: id, TenantID: claims(r).TenantID, Name: draft.Name, Version: in.Version, Protocol: draft.Protocol, Transport: draft.Transport, PayloadFormat: draft.PayloadFormat, ParserType: parserType, Status: status, Description: draft.Description, Config: draft.Config, CreatedAt: now, UpdatedAt: now}
	if pkg.Name == "" {
		pkg.Name = "AI 协议解析包"
	}
	if pkg.Version == "" {
		pkg.Version = "1.0.0"
	}
	if pkg.Protocol == "" {
		pkg.Protocol = "custom-go-worker"
	}
	if pkg.Transport == "" {
		pkg.Transport = "MQTT"
	}
	if pkg.PayloadFormat == "" {
		pkg.PayloadFormat = "json"
	}
	if old, getErr := s.engine.Repo.GetProtocolPackage(r.Context(), pkg.TenantID, pkg.ID); getErr == nil {
		pkg.CreatedAt = old.CreatedAt
	}
	if err = s.engine.Repo.SaveProtocolPackage(r.Context(), pkg); err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "ai.protocol-assistant.publish", "protocolPackage", pkg.ID, map[string]any{"version": pkg.Version, "status": pkg.Status, "fields": len(draft.Fields)})
	response := map[string]any{"package": pkg}
	if message != nil {
		response["standardMessage"] = message
	}
	write(w, http.StatusCreated, response)
}

type protocolAssistantDocument struct {
	Filename string
	Data     []byte
	Text     string
}

func readProtocolAssistantDocument(r *http.Request, maximum int) (protocolAssistantDocument, error) {
	f, header, err := r.FormFile("file")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			return protocolAssistantDocument{}, nil
		}
		return protocolAssistantDocument{}, fmt.Errorf("read protocol document: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maximum)+1))
	if err != nil {
		return protocolAssistantDocument{}, err
	}
	if len(data) > maximum {
		return protocolAssistantDocument{}, errors.New("protocol document exceeds 32 MiB")
	}
	if r.FormValue("inputKind") == "sample" {
		if len(data) > 1<<20 {
			return protocolAssistantDocument{}, errors.New("报文样本不能超过 1 MiB")
		}
		text := string(data)
		switch strings.ToLower(filepath.Ext(header.Filename)) {
		case ".bin":
			text = hex.EncodeToString(data)
		case ".json", ".txt", ".hex":
		default:
			return protocolAssistantDocument{}, errors.New("报文文件支持 JSON / TXT / HEX / BIN")
		}
		return protocolAssistantDocument{Filename: header.Filename, Data: data, Text: text}, nil
	}
	text, err := core.ExtractKnowledgeText(header.Filename, data)
	if err != nil {
		return protocolAssistantDocument{}, fmt.Errorf("extract protocol document: %w", err)
	}
	return protocolAssistantDocument{Filename: header.Filename, Data: data, Text: text}, nil
}

func protocolAssistantFilename(r *http.Request) string {
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) == 0 {
		return ""
	}
	return r.MultipartForm.File["file"][0].Filename
}

func assistantPayloadText(raw json.RawMessage, format string) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("sample payload is required")
	}
	if strings.EqualFold(format, "hex") {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("hex sample payload must be a JSON string")
		}
		return value, nil
	}
	if !json.Valid(raw) {
		return "", errors.New("JSON sample payload is invalid")
	}
	return string(raw), nil
}
