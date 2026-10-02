package externaldata

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Authorize func(context.Context, string, Source, Endpoint) (context.Context, error)
type Deliver func(context.Context, string, Source, Endpoint, Event, Binding, Result) (Result, error)

type Service struct {
	Store     Store
	cipher    *Cipher
	client    *Client
	Authorize Authorize
	Deliver   Deliver
}

func New(store Store, key string, authorize Authorize, deliver Deliver) (*Service, error) {
	c, err := NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &Service{Store: store, cipher: c, client: NewHTTPClient(), Authorize: authorize, Deliver: deliver}, nil
}

func invalid(detail string) error { return fmt.Errorf("%w: %s", ErrInvalid, detail) }
func key(parts ...string) string {
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func body(v any) json.RawMessage     { b, _ := json.Marshal(v); return b }
func read[T any](e Entry) (T, error) { var v T; err := json.Unmarshal(e.Body, &v); return v, err }
func (s *Service) authSecret(a *Auth, seal bool) error {
	if a == nil || a.Secret == "" {
		return nil
	}
	var err error
	if seal {
		a.Secret, err = s.cipher.Seal(a.Secret)
	} else {
		a.Secret, err = s.cipher.Open(a.Secret)
	}
	return err
}
func publicAuth(a *Auth) {
	if a != nil {
		a.SecretSet = a.Secret != ""
		a.Secret = ""
		a.ClearSecret = false
	}
}
func mergeAuth(a *Auth, old *Auth) {
	if a == nil {
		return
	}
	if a.ClearSecret {
		a.Secret = ""
	} else if a.Secret == "" && old != nil {
		a.Secret = old.Secret
	}
	a.ClearSecret = false
	a.SecretSet = false
}

func (s *Service) Source(ctx context.Context, tenant, id string) (Source, error) {
	e, err := s.Store.Get(ctx, tenant, "source", id)
	if err != nil {
		return Source{}, err
	}
	v, err := read[Source](e)
	v.Revision = e.Revision
	if err == nil {
		err = s.authSecret(&v.Auth, false)
	}
	return v, err
}

func (s *Service) SourceInfo(ctx context.Context, tenant, id string) (Source, error) {
	e, err := s.Store.Get(ctx, tenant, "source", id)
	if err != nil {
		return Source{}, err
	}
	v, err := read[Source](e)
	v.Revision = e.Revision
	return PublicSource(v), err
}
func (s *Service) Endpoint(ctx context.Context, tenant, id string) (Endpoint, error) {
	e, err := s.Store.Get(ctx, tenant, "endpoint", id)
	if err != nil {
		return Endpoint{}, err
	}
	v, err := read[Endpoint](e)
	v.Revision = e.Revision
	if err == nil {
		err = s.authSecret(v.Auth, false)
	}
	return v, err
}
func PublicSource(v Source) Source { publicAuth(&v.Auth); return v }
func PublicEndpoint(v Endpoint) Endpoint {
	publicAuth(v.Auth)
	v.PushKeySet = v.PushKey != ""
	v.PushKey = ""
	return v
}

func (s *Service) SaveSource(ctx context.Context, tenant string, v Source) (Source, error) {
	v.Runtime = nil
	if v.ID == "" {
		v.ID = uuid.NewString()
	}
	if v.Revision > 0 {
		e, err := s.Store.Get(ctx, tenant, "source", v.ID)
		if err != nil {
			return v, err
		}
		old, err := read[Source](e)
		if err != nil {
			return v, err
		}
		if v.Auth.Secret == "" && !v.Auth.ClearSecret {
			if err = s.authSecret(&old.Auth, false); err != nil {
				return v, err
			}
		}
		mergeAuth(&v.Auth, &old.Auth)
	} else {
		mergeAuth(&v.Auth, nil)
	}
	if err := ValidateSource(v); err != nil {
		return v, err
	}
	stored := v
	if err := s.authSecret(&stored.Auth, true); err != nil {
		return v, err
	}
	e, err := s.Store.Put(ctx, Entry{TenantID: tenant, Kind: "source", ID: v.ID, Body: body(stored)}, v.Revision)
	v.Revision = e.Revision
	return PublicSource(v), err
}
func (s *Service) SaveEndpoint(ctx context.Context, tenant string, v Endpoint) (Endpoint, error) {
	v.Runtime = nil
	if v.ID == "" {
		v.ID = uuid.NewString()
	}
	source, err := s.SourceInfo(ctx, tenant, v.SourceID)
	if err != nil {
		return v, err
	}
	v.PushKey = ""
	if v.Revision > 0 {
		e, err := s.Store.Get(ctx, tenant, "endpoint", v.ID)
		if err != nil {
			return v, err
		}
		old, err := read[Endpoint](e)
		if err != nil {
			return v, err
		}
		if v.Auth != nil && v.Auth.Secret == "" && !v.Auth.ClearSecret {
			if err = s.authSecret(old.Auth, false); err != nil {
				return v, err
			}
		}
		if old.SourceID != v.SourceID || old.Kind != v.Kind || old.Mode != v.Mode {
			return v, invalid("已有接口不能更换外部系统、接入方式或数据用途，请新建接口")
		}
		mergeAuth(v.Auth, old.Auth)
		v.PushKey = old.PushKey
	}
	if v.TimeoutSeconds == 0 {
		v.TimeoutSeconds = 20
	}
	if v.MaxAttempts == 0 {
		v.MaxAttempts = 8
	}
	if v.ResponseStatus == 0 {
		v.ResponseStatus = 202
	}
	if len(v.ResponseBody) == 0 {
		v.ResponseBody = body(map[string]any{"accepted": true})
	}
	if err := ValidateEndpoint(v); err != nil {
		return v, err
	}
	effectiveAuth := source.Auth
	if v.Auth != nil {
		effectiveAuth = *v.Auth
	}
	if v.Mode == "push" && effectiveAuth.Type == "token" {
		return v, invalid("推送接口请覆盖来源认证，选择接收密钥或签名；登录获取 Token 仅供主动拉取")
	}
	stored := v
	if stored.Auth != nil {
		a := *stored.Auth
		stored.Auth = &a
	}
	if err := s.authSecret(stored.Auth, true); err != nil {
		return v, err
	}
	e, err := s.Store.Put(ctx, Entry{TenantID: tenant, Kind: "endpoint", ID: v.ID, SourceID: v.SourceID, Body: body(stored)}, v.Revision)
	v.Revision = e.Revision
	if err == nil && v.Mode == "pull" {
		err = s.ensureSchedule(ctx, tenant, v)
	}
	return PublicEndpoint(v), err
}

func (s *Service) SaveBinding(ctx context.Context, tenant string, v Binding) (Binding, error) {
	if _, err := s.Source(ctx, tenant, v.SourceID); err != nil {
		return v, err
	}
	if v.Kind != "device" && v.Kind != "camera" || strings.TrimSpace(v.ExternalID) == "" || strings.TrimSpace(v.TargetID) == "" {
		return v, invalid("请选择对象类型并填写外部编号与平台对象")
	}
	id := key(v.SourceID, v.Kind, v.ExternalID)
	if v.ID != "" && v.ID != id {
		return v, invalid("对应关系的外部编号不能修改，请新建对应关系")
	}
	v.ID = id
	e, err := s.Store.Put(ctx, Entry{TenantID: tenant, Kind: "binding", ID: id, SourceID: v.SourceID, Body: body(v)}, v.Revision)
	v.Revision = e.Revision
	return v, err
}

func (s *Service) List(ctx context.Context, q Query) ([]any, int, error) {
	rows, total, err := s.Store.List(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]any, 0, len(rows))
	for _, e := range rows {
		switch q.Kind {
		case "source":
			v, err := read[Source](e)
			if err != nil {
				return nil, 0, err
			}
			v.Revision = e.Revision
			v.Runtime, err = s.summary(ctx, e.TenantID, e.ID, "")
			if err != nil {
				return nil, 0, err
			}
			out = append(out, PublicSource(v))
		case "endpoint":
			v, err := read[Endpoint](e)
			if err != nil {
				return nil, 0, err
			}
			v.Revision = e.Revision
			v.Runtime, err = s.summary(ctx, e.TenantID, e.SourceID, e.ID)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, PublicEndpoint(v))
		case "binding":
			v, err := read[Binding](e)
			if err != nil {
				return nil, 0, err
			}
			v.Revision = e.Revision
			out = append(out, v)
		case "job":
			v, err := read[Job](e)
			if err != nil {
				return nil, 0, err
			}
			v.Processed, v.Failed, v.Pending, err = s.recordCounts(ctx, Query{TenantID: e.TenantID, Kind: "record", JobID: e.ID, Limit: 1})
			if err != nil {
				return nil, 0, err
			}
			e.Body = body(v)
			out = append(out, e)
		default:
			out = append(out, e)
		}
	}
	return out, total, nil
}

func (s *Service) Delete(ctx context.Context, tenant, kind, id string, revision int64) error {
	if kind == "source" {
		v, err := s.Source(ctx, tenant, id)
		if err != nil {
			return err
		}
		if v.Enabled {
			return invalid("请先停用外部系统再删除")
		}
	}
	if kind == "endpoint" {
		v, err := s.Endpoint(ctx, tenant, id)
		if err != nil {
			return err
		}
		if v.Enabled {
			return invalid("请先停用接口再删除")
		}
	}
	if kind == "source" {
		for _, related := range []string{"endpoint", "binding", "record", "job"} {
			_, n, err := s.Store.List(ctx, Query{TenantID: tenant, Kind: related, SourceID: id, Limit: 1})
			if err != nil {
				return err
			}
			if n > 0 {
				return invalid("外部系统仍有关联配置或历史记录，请停用以保留记录")
			}
		}
	}
	if kind == "endpoint" {
		for _, related := range []string{"record", "job"} {
			_, n, err := s.Store.List(ctx, Query{TenantID: tenant, Kind: related, EndpointID: id, Limit: 1})
			if err != nil {
				return err
			}
			if n > 0 {
				return invalid("接口仍有接收或拉取记录，请停用以保留记录")
			}
		}
	}
	if err := s.Store.Delete(ctx, tenant, kind, id, revision); err != nil {
		return err
	}
	if kind == "endpoint" {
		if entry, err := s.Store.Get(ctx, tenant, "schedule", id); err == nil {
			_ = s.Store.Delete(ctx, tenant, "schedule", id, entry.Revision)
		}
	}
	return nil
}

func (s *Service) RotateKey(ctx context.Context, tenant, id string) (string, error) {
	v, err := s.Endpoint(ctx, tenant, id)
	if err != nil {
		return "", err
	}
	if v.Mode != "push" {
		return "", invalid("仅推送接口有接收密钥")
	}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", err
	}
	plain := hex.EncodeToString(b)
	v.PushKey = key(plain)
	if err = s.authSecret(v.Auth, true); err != nil {
		return "", err
	}
	_, err = s.Store.Put(ctx, Entry{TenantID: tenant, Kind: "endpoint", ID: id, SourceID: v.SourceID, Body: body(v)}, v.Revision)
	return plain, err
}

// Receive persists the complete payload before acknowledging it. Re-delivery
// may re-create a receipt, but stable record identities prevent duplicate work.
type Receipt struct {
	Payload  json.RawMessage `json:"payload"`
	Endpoint Endpoint        `json:"endpoint"`
	JobID    string          `json:"jobId,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// ReceiveFailed preserves the received bytes even when HTTP/JSON/pagination
// validation fails. It never lets an error response reach business processing.
func (s *Service) ReceiveFailed(ctx context.Context, tenant string, ep Endpoint, payload []byte, jobID, detail string) error {
	if len(payload) > 1<<20 {
		payload = payload[:1<<20]
	}
	raw := json.RawMessage(payload)
	if !json.Valid(raw) {
		raw = body(string(payload))
	}
	v := Receipt{Payload: raw, Endpoint: Endpoint{ID: ep.ID, SourceID: ep.SourceID, Revision: ep.Revision, Mapping: ep.Mapping}, JobID: jobID, Error: detail}
	e, err := s.Store.Put(ctx, Entry{TenantID: tenant, Kind: "receipt", ID: uuid.NewString(), SourceID: ep.SourceID, EndpointID: ep.ID, Status: "PENDING", Body: body(v)}, 0)
	if err != nil {
		return err
	}
	_, err = s.processReceipt(ctx, e)
	return err
}

func (s *Service) Receive(ctx context.Context, tenant string, ep Endpoint, payload []byte, jobID string) (int, error) {
	if len(payload) > 1<<20 {
		return 0, invalid("接收数据超过 1 MiB")
	}
	if !json.Valid(payload) {
		return 0, invalid("请求内容必须是有效 JSON")
	}
	rid := uuid.NewString()
	// The snapshot intentionally omits credentials and request configuration.
	receipt := Receipt{Payload: append(json.RawMessage(nil), payload...), Endpoint: Endpoint{ID: ep.ID, SourceID: ep.SourceID, Revision: ep.Revision, Mapping: ep.Mapping}, JobID: jobID}
	e, err := s.Store.Put(ctx, Entry{TenantID: tenant, Kind: "receipt", ID: rid, SourceID: ep.SourceID, EndpointID: ep.ID, Status: "PENDING", Body: body(receipt)}, 0)
	if err != nil {
		return 0, err
	}
	return s.processReceipt(ctx, e)
}

func (s *Service) processReceipt(ctx context.Context, e Entry) (int, error) {
	receipt, err := read[Receipt](e)
	if err != nil {
		return 0, err
	}
	ep, payload, jobID := receipt.Endpoint, receipt.Payload, receipt.JobID
	items, extractErr := Extract(ep.Mapping, payload)
	if receipt.Error != "" {
		extractErr = errors.New(receipt.Error)
	}
	if extractErr != nil {
		items = []json.RawMessage{json.RawMessage(payload)}
	}
	identityClass := "item"
	if extractErr != nil {
		identityClass = "extraction_error"
		if receipt.Error != "" {
			identityClass = "response_error"
		}
	}
	count := 0
	for i, item := range items {
		// An unsuccessful HTTP response can contain the same JSON as a later
		// successful retry. Keep its diagnosis without occupying that item's
		// identity; changing error text must not create unbounded duplicates.
		id := key(ep.ID, fmt.Sprint(ep.Revision), jobID, identityClass, string(item), fmt.Sprint(i))
		r := Record{ReceiptID: e.ID, JobID: jobID, Raw: item, Mapping: ep.Mapping, ConfigRevision: ep.Revision}
		status := "PENDING"
		if extractErr != nil {
			status = "FAILED"
			r.Error = extractErr.Error()
			r.ExtractionError = true
		}
		_, err = s.Store.Put(ctx, Entry{TenantID: e.TenantID, Kind: "record", ID: id, SourceID: ep.SourceID, EndpointID: ep.ID, Status: status, Body: body(r)}, 0)
		if errors.Is(err, ErrConflict) {
			count++
			continue
		}
		if err != nil {
			return count, err
		}
		count++
	}
	return count, s.finish(ctx, e, "RECEIVED", 0, receipt)
}

func (s *Service) Preview(ep Endpoint, payload []byte) ([]any, error) {
	items, err := Extract(ep.Mapping, payload)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		ev, filtered, err := Transform(ep.Mapping, item)
		row := map[string]any{"raw": json.RawMessage(item), "event": ev, "filtered": filtered}
		if err != nil {
			row["error"] = err.Error()
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *Service) Retry(ctx context.Context, tenant, kind, id string, revision int64, current bool) (Entry, error) {
	e, err := s.Store.Get(ctx, tenant, kind, id)
	if err != nil {
		return e, err
	}
	if e.Revision != revision {
		return e, ErrConflict
	}
	if e.Status == "RUNNING" || e.Status == "PENDING" || e.Status == "RETRY" {
		return e, invalid("任务正在等待或执行")
	}
	if e.Status == "REPLACED" {
		return e, invalid("原记录已按新配置重新提取，请查看新接收记录")
	}
	if kind == "record" {
		r, err := read[Record](e)
		if err != nil {
			return e, err
		}
		if r.ExtractionError && !current {
			return e, invalid("原始数据提取失败，请修正接口配置后按当前规则重新提取")
		}
		r.Attempts = 0
		r.Error = ""
		if current {
			ep, err := s.Endpoint(ctx, tenant, e.EndpointID)
			if err != nil {
				return e, err
			}
			if r.ExtractionError {
				if ep.Revision == r.ConfigRevision {
					return e, invalid("请先修正接口配置，再按当前规则重新提取")
				}
				receiptEntry, err := s.Store.Get(ctx, tenant, "receipt", r.ReceiptID)
				if err != nil {
					return e, err
				}
				original, err := read[Receipt](receiptEntry)
				if err != nil {
					return e, err
				}
				if original.Error != "" {
					return e, invalid("上游失败响应仅供诊断，请重试拉取任务以重新请求数据")
				}
				if _, err = s.Receive(ctx, tenant, ep, original.Payload, r.JobID); err != nil {
					return e, err
				}
				e.Status = "REPLACED"
				e.Body = body(r)
				return s.Store.Put(ctx, e, e.Revision)
			}
			r.Mapping = ep.Mapping
			r.ConfigRevision = ep.Revision
		}
		e.Body = body(r)
	} else if kind == "job" {
		j, err := read[Job](e)
		if err != nil {
			return e, err
		}
		if e.Status == "COMPLETED" {
			return e, invalid("已完成任务请通过新建补拉再次执行")
		}
		j.Attempts = 0
		j.Error = ""
		e.Body = body(j)
	} else {
		return e, invalid("不支持的重试类型")
	}
	e.Status = "PENDING"
	e.LeaseUntil = 0
	e.Owner = ""
	e.DueAt = time.Now().UnixMilli()
	return s.Store.Put(ctx, e, e.Revision)
}
