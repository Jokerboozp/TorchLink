package response

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	ProcedureKind  = "RESPONSE_PROCEDURE"
	ExecutionKind  = "RESPONSE_EXECUTION"
	CorrectiveKind = "CORRECTIVE_ACTION"
	AttachmentKind = "RESPONSE_ATTACHMENT"
	DutyLinkKind   = "DUTY_ACTION_LINK"
)

type Catalog interface {
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetAlarm(context.Context, string, string) (model.Alarm, error)
}
type AIReports interface {
	List(context.Context, analytics.Actor, string, string, model.AnalysisFilter) ([]model.AnalysisAIRevision, int, error)
}
type Service struct {
	AI       AIReports
	Analysis *analytics.Service
	Facts    ports.AnalyticsFactStore
	Catalog  Catalog
	Archive  ports.Archive
	// ValidateStaff checks enabled membership and current full device scope of
	// the assignee. Being on duty is deliberately not required.
	ValidateStaff   func(context.Context, string, string, []string) error
	ResolveEvidence func(context.Context, analytics.Actor, model.ResponseEvidenceReference) (model.ResponseEvidenceReference, error)
	// EvidenceKind is set by trusted startup wiring for the shared canonical
	// source reader. It is never decoded from a request.
	EvidenceKind string
	Now          func() time.Time
}
type RevisionRequest struct {
	ResourceID      string          `json:"resourceId"`
	ExpectedVersion int64           `json:"expectedVersion"`
	DeviceIDs       []string        `json:"deviceIds"`
	IdempotencyKey  string          `json:"idempotencyKey"`
	Body            json.RawMessage `json:"body"`
}
type ActionRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
	Action          string `json:"action"`
	Reason          string `json:"reason,omitempty"`
}

func NewService(analysis *analytics.Service, facts ports.AnalyticsFactStore) *Service {
	return &Service{Analysis: analysis, Facts: facts, Now: time.Now}
}
func invalid(reason string) error { return fmt.Errorf("%w: %s", model.ErrAnalysisInvalid, reason) }
func decode(body json.RawMessage, result any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(result); err != nil {
		return invalid(err.Error())
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return invalid("只允许一个 JSON 对象")
	}
	return nil
}
func (s *Service) now() int64 {
	if s.Now != nil {
		return s.Now().UTC().UnixMilli()
	}
	return time.Now().UTC().UnixMilli()
}
func (s *Service) authorize(ctx context.Context, a analytics.Actor, operation string, devices []string) (analytics.Actor, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil || !current.Allows(analytics.KindResponse, operation, devices) {
		return current, analytics.ErrForbidden
	}
	return current, nil
}
func resourcePath(kind string) string {
	switch kind {
	case ProcedureKind:
		return "/api/v1/response-procedures"
	case ExecutionKind:
		return "/api/v1/response-runs"
	case CorrectiveKind:
		return "/api/v1/corrective-actions"
	case AttachmentKind:
		return "/api/v1/response-runs/attachments"
	case DutyLinkKind:
		return "/api/v1/corrective-actions/duty-links"
	}
	return ""
}
func (s *Service) Latest(ctx context.Context, a analytics.Actor, kind, resourceID string) (model.AnalysisConfigRevision, error) {
	if resourcePath(kind) == "" || resourceID == "" {
		return model.AnalysisConfigRevision{}, model.ErrNotFound
	}
	rows, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: kind, ResourceID: resourceID, Limit: 100})
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if total == 0 {
		return model.AnalysisConfigRevision{}, model.ErrNotFound
	}
	var latest model.AnalysisConfigRevision
	for offset := 0; ; {
		for _, v := range rows {
			if v.Scope == "SHARED" && v.Version > latest.Version {
				latest = v
			}
		}
		offset += len(rows)
		if offset >= total {
			break
		}
		if offset > 10000 || len(rows) == 0 {
			return latest, invalid("资源版本超过读取保护范围")
		}
		rows, total, err = s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: kind, ResourceID: resourceID, Limit: 100, Offset: offset})
		if err != nil {
			return latest, err
		}
	}
	if latest.ID == "" {
		return latest, model.ErrNotFound
	}
	if _, err = s.authorize(ctx, a, "", latest.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	return latest, nil
}
func (s *Service) Revision(ctx context.Context, a analytics.Actor, kind, id string) (model.AnalysisConfigRevision, error) {
	v, err := s.Analysis.Store.GetAnalysisConfig(ctx, a.TenantID, id)
	if err != nil {
		return v, err
	}
	if v.Kind != kind || v.Scope != "SHARED" {
		return model.AnalysisConfigRevision{}, model.ErrNotFound
	}
	if _, err = s.authorize(ctx, a, "", v.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	return v, nil
}
func (s *Service) List(ctx context.Context, a analytics.Actor, kind string, f model.AnalysisFilter) ([]model.AnalysisConfigRevision, int, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil {
		return nil, 0, err
	}
	ids := current.DeviceIDs
	if current.AllDevices {
		ids = []string{"catalog-read"}
	}
	if !current.Allows(analytics.KindResponse, "", ids) || resourcePath(kind) == "" {
		return nil, 0, analytics.ErrForbidden
	}
	latest := map[string]model.AnalysisConfigRevision{}
	filter := model.AnalysisFilter{Kind: kind, ResourceID: f.ResourceID, Limit: 100}
	for {
		page, total, e := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, filter)
		if e != nil {
			return nil, 0, e
		}
		for _, v := range page {
			if v.Scope == "SHARED" && v.Version > latest[v.ResourceID].Version {
				latest[v.ResourceID] = v
			}
		}
		filter.Offset += len(page)
		if filter.Offset >= total {
			break
		}
		if filter.Offset > 10000 || len(page) == 0 {
			return nil, 0, invalid("请缩小资源筛选范围")
		}
	}
	visible := []model.AnalysisConfigRevision{}
	for _, v := range latest {
		if current.Allows(analytics.KindResponse, "", v.DeviceIDs) {
			visible = append(visible, v)
		}
	}
	slices.SortFunc(visible, func(a, b model.AnalysisConfigRevision) int {
		if a.CreatedAt > b.CreatedAt {
			return -1
		}
		if a.CreatedAt < b.CreatedAt {
			return 1
		}
		return strings.Compare(a.ResourceID, b.ResourceID)
	})
	total := len(visible)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.AnalysisConfigRevision{}, total, nil
	}
	return visible[offset:min(total, offset+limit)], total, nil
}
func receipts(body any) *[]model.ResponseRequestReceipt {
	switch v := body.(type) {
	case *model.ResponseProcedure:
		return &v.Requests
	case *model.ResponseExecution:
		return &v.Requests
	case *model.CorrectiveAction:
		return &v.Requests
	case *model.ResponseAttachment:
		return &v.Requests
	}
	return nil
}

// commit uses the shared PostgreSQL compare-and-swap pointer. Receipts live in
// the same aggregate revision, so idempotency survives restarts and replicas.
func (s *Service) commit(ctx context.Context, a analytics.Actor, kind, resourceID string, devices []string, expected int64, key string, request any, body any) (model.AnalysisConfigRevision, error) {
	if key == "" || len(key) > 200 || resourceID == "" || len(resourceID) > 200 || expected < 0 {
		return model.AnalysisConfigRevision{}, invalid("资源、幂等键或版本无效")
	}
	current, err := s.authorize(ctx, a, "", devices)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	hash, err := analytics.AnalysisHash(request)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	r := receipts(body)
	if r == nil {
		return model.AnalysisConfigRevision{}, invalid("资源不支持事务幂等")
	}
	for _, receipt := range *r {
		if receipt.Key == key {
			if receipt.Hash != hash || receipt.Actor != current.Username {
				return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
			}
			return s.Revision(ctx, a, kind, receipt.ResultID)
		}
	}
	if len(*r) >= 5000 {
		return model.AnalysisConfigRevision{}, invalid("该业务记录已达到操作保护上限，请归档")
	}
	id := uuid.NewString()
	*r = append(*r, model.ResponseRequestReceipt{Key: key, Hash: hash, Actor: current.Username, ResultID: id})
	data, err := json.Marshal(body)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	v := model.AnalysisConfigRevision{ID: id, TenantID: a.TenantID, Kind: kind, ResourceID: resourceID, Scope: "SHARED", Creator: current.Username, DeviceIDs: slices.Clone(devices), Body: data}
	saved, err := s.Analysis.Store.PutAnalysisConfig(ctx, v, expected)
	if errors.Is(err, model.ErrAnalysisConflict) {
		latest, e := s.Latest(ctx, a, kind, resourceID)
		if e == nil {
			var retry any
			switch kind {
			case ProcedureKind:
				retry = &model.ResponseProcedure{}
			case ExecutionKind:
				retry = &model.ResponseExecution{}
			case CorrectiveKind:
				retry = &model.CorrectiveAction{}
			case AttachmentKind:
				retry = &model.ResponseAttachment{}
			}
			if retry != nil && json.Unmarshal(latest.Body, retry) == nil {
				for _, receipt := range *receipts(retry) {
					if receipt.Key == key && receipt.Hash == hash && receipt.Actor == current.Username {
						return s.Revision(ctx, a, kind, receipt.ResultID)
					}
				}
			}
		}
	}
	return saved, err
}
func (s *Service) validateDevices(ctx context.Context, a analytics.Actor, ids []string) error {
	if len(ids) == 0 || len(ids) > s.Analysis.Limits.MaxDevices || len(unique(ids)) != len(ids) {
		return invalid("设备集合须明确且无重复")
	}
	if s.Catalog == nil {
		return analytics.ErrUnsupported
	}
	for _, id := range ids {
		if _, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, id); err != nil {
			return analytics.ErrForbidden
		}
	}
	return nil
}

func (s *Service) SaveProcedure(ctx context.Context, a analytics.Actor, q RevisionRequest) (model.AnalysisConfigRevision, error) {
	if _, err := s.authorize(ctx, a, "POST /api/v1/response-procedures", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if err := s.validateDevices(ctx, a, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var p model.ResponseProcedure
	if err := decode(q.Body, &p); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if p.Published || p.PublishedBy != "" || p.PublishedAt != 0 || len(p.Requests) > 0 {
		return model.AnalysisConfigRevision{}, invalid("发布与操作记录由服务端保存")
	}
	if err := ValidateProcedure(p); err != nil {
		return model.AnalysisConfigRevision{}, invalid(err.Error())
	}
	old, err := s.Latest(ctx, a, ProcedureKind, q.ResourceID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if err == nil {
		var prior model.ResponseProcedure
		_ = json.Unmarshal(old.Body, &prior)
		p.Requests = prior.Requests
		for _, receipt := range p.Requests {
			if receipt.Key == q.IdempotencyKey {
				return s.commit(ctx, a, ProcedureKind, q.ResourceID, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &p)
			}
		}
		if old.Version != q.ExpectedVersion {
			return old, model.ErrAnalysisConflict
		}
		if !slices.Equal(old.DeviceIDs, q.DeviceIDs) {
			return old, invalid("流程版本的设备范围不得隐式改变")
		}
	}
	return s.commit(ctx, a, ProcedureKind, q.ResourceID, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &p)
}
func (s *Service) PublishProcedure(ctx context.Context, a analytics.Actor, id string, q ActionRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, ProcedureKind, id)
	if err != nil {
		return old, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/response-procedures/:id/publish", old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var p model.ResponseProcedure
	_ = json.Unmarshal(old.Body, &p)
	for _, r := range p.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ProcedureKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &p)
		}
	}
	if old.Version != q.ExpectedVersion || p.Published {
		return old, model.ErrAnalysisConflict
	}
	p.Published, p.PublishedBy, p.PublishedAt = true, a.Username, s.now()
	return s.commit(ctx, a, ProcedureKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &p)
}
