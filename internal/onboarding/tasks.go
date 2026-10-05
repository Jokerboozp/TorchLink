package onboarding

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
)

const DraftKind = "device-draft"
const BatchKind = "device-batch"
const maxBatchRows = 1000

type TaskOwner struct {
	Username       string `json:"username"`
	Managed        bool   `json:"managed"`
	SessionVersion int64  `json:"sessionVersion"`
}

// TaskService deliberately retains no browser request or plaintext credential.
// Authorize reloads the persisted owner's permissions before each mutation.
type TaskService struct {
	Service                *Service
	Authorize              func(context.Context, string, TaskOwner) (context.Context, error)
	TemplateDraftAuthorize func(context.Context, string, TaskOwner) (context.Context, error)
	Fingerprint            func(context.Context, string, string) (string, error)
	Key                    string
}

func (s *TaskService) authorize(ctx context.Context, tenant string, owner TaskOwner) (context.Context, error) {
	if tenant == "" || owner.Username == "" || s.Authorize == nil {
		return ctx, &EnrollError{Status: 403, Message: "接入任务身份或权限不可用"}
	}
	return s.Authorize(ctx, tenant, owner)
}

type DeviceDraft struct {
	Revision  int64         `json:"revision,omitempty"`
	Step      string        `json:"step"`
	ProductID string        `json:"productId,omitempty"`
	Request   EnrollRequest `json:"request"`
}

func (s *TaskService) SaveDraft(ctx context.Context, tenant string, owner TaskOwner, id string, d DeviceDraft) (model.OnboardingRecord, error) {
	ctx, err := s.authorizeDraft(ctx, tenant, owner, d)
	if err != nil {
		return model.OnboardingRecord{}, err
	}
	if !segment.MatchString(id) || len(d.Step) > 32 || d.Revision < 0 {
		return model.OnboardingRecord{}, invalid("草稿标识、步骤或版本无效")
	}
	expected := d.Revision
	if expected > 0 {
		old, e := s.Service.Repo.GetOnboardingRecord(ctx, tenant, id)
		if e != nil {
			return model.OnboardingRecord{}, e
		}
		if old.OwnerID != owner.Username || old.Kind != DraftKind {
			return model.OnboardingRecord{}, model.ErrNotFound
		}
		var previous DeviceDraft
		if e = json.Unmarshal(old.Body, &previous); e != nil {
			return model.OnboardingRecord{}, e
		}
		if _, e = s.authorizeDraft(ctx, tenant, owner, previous); e != nil {
			return model.OnboardingRecord{}, e
		}
	}
	d.Revision = 0
	data, err := json.Marshal(d)
	if err != nil || len(data) > 256<<10 {
		return model.OnboardingRecord{}, invalid("接入草稿过大或格式无效")
	}
	var content any
	_ = json.Unmarshal(data, &content)
	if containsCredential(content) {
		return model.OnboardingRecord{}, invalid("接入草稿不能保存密码、令牌或密钥")
	}
	return s.Service.Repo.SaveOnboardingRecord(ctx, model.OnboardingRecord{TenantID: tenant, ID: id, OwnerID: owner.Username, Kind: DraftKind, Status: "DRAFT", Body: data}, expected)
}

func templateDraft(d DeviceDraft) bool { return strings.HasPrefix(d.Step, "preparation:") }
func (s *TaskService) authorizeDraft(ctx context.Context, tenant string, owner TaskOwner, d DeviceDraft) (context.Context, error) {
	if !templateDraft(d) {
		return s.authorize(ctx, tenant, owner)
	}
	if d.Request.NewProduct == nil || d.Request.NewProduct.Metadata == nil || d.Request.NewProduct.Metadata["preparationDraft"] == nil {
		return ctx, invalid("模板准备草稿缺少准备内容")
	}
	if tenant == "" || owner.Username == "" || s.TemplateDraftAuthorize == nil {
		return ctx, &EnrollError{Status: 403, Message: "当前账号不能保存模板准备草稿"}
	}
	return s.TemplateDraftAuthorize(ctx, tenant, owner)
}

// ListDrafts filters by the exact draft permission before totals/pagination.
func (s *TaskService) ListDrafts(ctx context.Context, tenant string, owner TaskOwner, limit, offset int, purpose string) ([]model.OnboardingRecord, int, error) {
	if purpose != "" && purpose != "preparation" && purpose != "device" {
		return nil, 0, invalid("草稿用途必须为 preparation 或 device")
	}
	_, regularErr := s.authorize(ctx, tenant, owner)
	templateErr := errors.New("template draft permission unavailable")
	if s.TemplateDraftAuthorize != nil {
		_, templateErr = s.TemplateDraftAuthorize(ctx, tenant, owner)
	}
	if regularErr != nil && templateErr != nil {
		return nil, 0, regularErr
	}
	visible := []model.OnboardingRecord{}
	for page := 0; ; page += 100 {
		rows, total, err := s.Service.Repo.ListOnboardingRecords(ctx, tenant, owner.Username, DraftKind, 100, page)
		if err != nil {
			return nil, 0, err
		}
		for _, v := range rows {
			var d DeviceDraft
			if err = json.Unmarshal(v.Body, &d); err != nil {
				return nil, 0, err
			}
			isTemplate := templateDraft(d)
			purposeMatches := purpose == "" || purpose == "preparation" && isTemplate || purpose == "device" && !isTemplate
			if purpose == "preparation" && d.Step == "preparation:linked" {
				purposeMatches = false
			}
			if purposeMatches && (isTemplate && templateErr == nil || !isTemplate && regularErr == nil) {
				visible = append(visible, v)
			}
		}
		if page+len(rows) >= total || len(rows) == 0 {
			break
		}
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	total := len(visible)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	return visible[offset:end], total, nil
}

func containsCredential(v any) bool {
	switch value := v.(type) {
	case map[string]any:
		for key, item := range value {
			k := strings.ToLower(key)
			if strings.Contains(k, "password") || strings.Contains(k, "secret") || strings.Contains(k, "token") || strings.Contains(k, "密码") || strings.Contains(k, "密钥") || strings.Contains(k, "令牌") || k == "authorization" || k == "credential" || k == "credentials" || k == "privatekey" {
				return true
			}
			if containsCredential(item) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if containsCredential(item) {
				return true
			}
		}
	}
	return false
}

func (s *TaskService) OwnedRecord(ctx context.Context, tenant string, owner TaskOwner, id, kind string) (model.OnboardingRecord, error) {
	var err error
	if kind != DraftKind {
		ctx, err = s.authorize(ctx, tenant, owner)
		if err != nil {
			return model.OnboardingRecord{}, err
		}
	}
	v, err := s.Service.Repo.GetOnboardingRecord(ctx, tenant, id)
	if err != nil {
		return v, err
	}
	if v.OwnerID != owner.Username || v.Kind != kind {
		return model.OnboardingRecord{}, model.ErrNotFound
	}
	if kind == DraftKind {
		var d DeviceDraft
		if err = json.Unmarshal(v.Body, &d); err != nil {
			return model.OnboardingRecord{}, err
		}
		if _, err = s.authorizeDraft(ctx, tenant, owner, d); err != nil {
			return model.OnboardingRecord{}, err
		}
	}
	return v, nil
}

type BatchInputRow struct {
	Device     EnrollDevice      `json:"device"`
	Connection *EnrollConnection `json:"connection,omitempty"`
}
type BatchRequest struct {
	ID          string           `json:"id,omitempty"`
	ProductID   string           `json:"productId"`
	Connection  EnrollConnection `json:"connection"`
	Rows        []BatchInputRow  `json:"rows"`
	Fingerprint string           `json:"fingerprint,omitempty"`
}
type BatchCheck struct {
	Index    int    `json:"index"`
	DeviceID string `json:"deviceId"`
	Valid    bool   `json:"valid"`
	Error    string `json:"error,omitempty"`
}
type BatchPreflight struct {
	Fingerprint string       `json:"fingerprint"`
	Total       int          `json:"total"`
	Rows        []BatchCheck `json:"rows"`
}
type BatchRow struct {
	Index            int                 `json:"index"`
	DeviceID         string              `json:"deviceId"`
	Name             string              `json:"name"`
	Status           string              `json:"status"`
	Error            string              `json:"error,omitempty"`
	Mode             string              `json:"mode,omitempty"`
	ProfileID        string              `json:"profileId,omitempty"`
	CredentialStatus string              `json:"credentialStatus"`
	Request          EnrollRequest       `json:"request"`
	Delivery         *credentialDelivery `json:"delivery,omitempty"`
}
type credentialDelivery struct {
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	ExpiresAt  int64  `json:"expiresAt"`
}
type batchBody struct {
	Owner               TaskOwner     `json:"owner"`
	ProductID           string        `json:"productId"`
	Fingerprint         string        `json:"fingerprint"`
	TemplateFingerprint string        `json:"templateFingerprint"`
	InputHash           string        `json:"inputHash"`
	Input               *BatchRequest `json:"input,omitempty"`
	Total               int           `json:"total"`
	Succeeded           int           `json:"succeeded"`
	Failed              int           `json:"failed"`
	Pending             int           `json:"pending"`
	Error               string        `json:"error,omitempty"`
	RetryIndices        []int         `json:"retryIndices,omitempty"`
	ActiveIndices       []int         `json:"activeIndices,omitempty"`
}
type BatchSummary struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Revision    int64  `json:"revision"`
	ProductID   string `json:"productId"`
	Fingerprint string `json:"fingerprint"`
	Total       int    `json:"total"`
	Succeeded   int    `json:"succeeded"`
	Failed      int    `json:"failed"`
	Pending     int    `json:"pending"`
	Error       string `json:"error,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}
type BatchPublicRow struct {
	Index            int            `json:"index"`
	DeviceID         string         `json:"deviceId"`
	Name             string         `json:"name"`
	Status           string         `json:"status"`
	Error            string         `json:"error,omitempty"`
	Mode             string         `json:"mode,omitempty"`
	ProfileID        string         `json:"profileId,omitempty"`
	CredentialStatus string         `json:"credentialStatus"`
	OnboardingStatus string         `json:"onboardingStatus,omitempty"`
	VerificationAt   int64          `json:"verificationAt,omitempty"`
	AccessInfo       map[string]any `json:"accessInfo,omitempty"`
}

func summarizeBatch(v model.OnboardingRecord, b batchBody) BatchSummary {
	return BatchSummary{ID: v.ID, Status: v.Status, Revision: v.Revision, ProductID: b.ProductID, Fingerprint: b.Fingerprint, Total: b.Total, Succeeded: b.Succeeded, Failed: b.Failed, Pending: b.Pending, Error: b.Error, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func BatchRecordSummary(v model.OnboardingRecord) (BatchSummary, error) {
	var b batchBody
	err := json.Unmarshal(v.Body, &b)
	return summarizeBatch(v, b), err
}
func batchRowKind(id string) string { return "device-batch-row:" + id }
func batchRowID(id string, index int) string {
	return fmt.Sprintf("batch-%s-%04d", Hash(id)[:32], index)
}
func batchEnroll(q BatchRequest, index int) EnrollRequest {
	c := q.Connection
	if q.Rows[index].Connection != nil {
		c = *q.Rows[index].Connection
	}
	return normalizeEnroll(EnrollRequest{RequestID: "batch-" + Hash(q.ID + "/" + fmt.Sprint(index))[:32], ProductID: q.ProductID, Device: q.Rows[index].Device, Connection: c})
}
func batchInputHash(q BatchRequest) string {
	q.ID = ""
	q.Fingerprint = ""
	b, _ := json.Marshal(q)
	return Hash(string(b))
}
func (s *TaskService) templateFingerprint(ctx context.Context, tenant, product string) (string, error) {
	if s.Fingerprint == nil {
		return "", errors.New("template fingerprint service unavailable")
	}
	return s.Fingerprint(ctx, tenant, product)
}

func (s *TaskService) PreflightBatch(ctx context.Context, tenant string, owner TaskOwner, q BatchRequest) (BatchPreflight, error) {
	ctx, err := s.authorize(ctx, tenant, owner)
	if err != nil {
		return BatchPreflight{}, err
	}
	if !segment.MatchString(q.ProductID) || len(q.Rows) == 0 || len(q.Rows) > maxBatchRows {
		return BatchPreflight{}, invalid("请选择设备模板并提供 1 至 1000 行设备")
	}
	encoded, _ := json.Marshal(q)
	var content any
	_ = json.Unmarshal(encoded, &content)
	if len(encoded) > 2<<20 || containsCredential(content) {
		return BatchPreflight{}, invalid("批量内容过大或包含不应保存的凭据")
	}
	fingerprint, err := s.templateFingerprint(ctx, tenant, q.ProductID)
	if err != nil {
		return BatchPreflight{}, err
	}
	out := BatchPreflight{Fingerprint: Hash(fingerprint + "\x00" + batchInputHash(q)), Total: len(q.Rows), Rows: []BatchCheck{}}
	seen := map[string]bool{}
	for i := range q.Rows {
		r := batchEnroll(q, i)
		check := BatchCheck{Index: i, DeviceID: r.Device.ID, Valid: true}
		switch {
		case !segment.MatchString(r.Device.ID) || r.Device.Name == "" || len(r.Device.Name) > 256 || len(r.Device.Description) > 1024 || len(r.Device.Tags) > 32:
			check.Error = "设备编号、名称、备注或标签无效"
		case seen[r.Device.ID]:
			check.Error = "设备编号在本次导入中重复"
		case r.Connection.Listener != nil:
			check.Error = "请先配置共享接入点，再批量选择该接入点"
		default:
			if _, e := s.Service.Repo.GetManagedDevice(ctx, tenant, r.Device.ID); e == nil {
				check.Error = "设备编号已登记"
			} else if !errors.Is(e, model.ErrNotFound) {
				return BatchPreflight{}, e
			} else if _, _, e = s.Service.planEnroll(ctx, tenant, r); e != nil {
				check.Error = publicTaskError(e)
			}
		}
		seen[r.Device.ID] = true
		check.Valid = check.Error == ""
		out.Rows = append(out.Rows, check)
	}
	return out, nil
}

func publicTaskError(err error) string {
	var e *EnrollError
	if errors.As(err, &e) {
		return e.Message
	}
	if errors.Is(err, model.ErrOnboardingChanged) {
		return err.Error()
	}
	return "处理失败，请检查设备参数后重试；持续失败时联系管理员"
}

func (s *TaskService) CreateBatch(ctx context.Context, tenant string, owner TaskOwner, q BatchRequest) (BatchSummary, error) {
	ctx, err := s.authorize(ctx, tenant, owner)
	if err != nil {
		return BatchSummary{}, err
	}
	if !segment.MatchString(q.ID) {
		return BatchSummary{}, invalid("批量任务标识无效")
	}
	if existing, e := s.Service.Repo.GetOnboardingRecord(ctx, tenant, q.ID); e == nil {
		var old batchBody
		_ = json.Unmarshal(existing.Body, &old)
		if existing.Kind != BatchKind || existing.OwnerID != owner.Username || old.InputHash != batchInputHash(q) {
			return BatchSummary{}, conflict("任务标识已被其他请求使用")
		}
		return summarizeBatch(existing, old), nil
	} else if !errors.Is(e, model.ErrNotFound) {
		return BatchSummary{}, e
	}
	checked, err := s.PreflightBatch(ctx, tenant, owner, q)
	if err != nil {
		return BatchSummary{}, err
	}
	if q.Fingerprint == "" || q.Fingerprint != checked.Fingerprint {
		return BatchSummary{}, conflict("模板或导入内容已变化，请重新预检")
	}
	for _, row := range checked.Rows {
		if !row.Valid {
			return BatchSummary{}, invalid(fmt.Sprintf("第 %d 行：%s", row.Index+1, row.Error))
		}
	}
	if _, err = s.aead(); err != nil {
		return BatchSummary{}, err
	}
	template, err := s.templateFingerprint(ctx, tenant, q.ProductID)
	if err != nil {
		return BatchSummary{}, err
	}
	if Hash(template+"\x00"+batchInputHash(q)) != checked.Fingerprint {
		return BatchSummary{}, conflict("模板在预检期间发生变化，请重试")
	}
	b := batchBody{Owner: owner, ProductID: q.ProductID, Fingerprint: q.Fingerprint, TemplateFingerprint: template, InputHash: batchInputHash(q), Input: &q, Total: len(q.Rows), Pending: len(q.Rows)}
	data, _ := json.Marshal(b)
	v, err := s.Service.Repo.SaveOnboardingRecord(ctx, model.OnboardingRecord{TenantID: tenant, ID: q.ID, OwnerID: owner.Username, Kind: BatchKind, Status: "INITIALIZING", Body: data}, 0)
	return summarizeBatch(v, b), err
}

func (s *TaskService) Batch(ctx context.Context, tenant string, owner TaskOwner, id string, limit, offset int) (BatchSummary, []BatchPublicRow, error) {
	v, err := s.OwnedRecord(ctx, tenant, owner, id, BatchKind)
	if err != nil {
		return BatchSummary{}, nil, err
	}
	summary, err := BatchRecordSummary(v)
	if err != nil {
		return summary, nil, err
	}
	var b batchBody
	if err = json.Unmarshal(v.Body, &b); err != nil {
		return summary, nil, err
	}
	var rows []model.OnboardingRecord
	if b.Input != nil {
		// A task can pause before its first worker pass. Its durable input still
		// supplies the pending rows, including those not yet materialized.
		if limit <= 0 {
			limit = 20
		}
		limit, offset = min(limit, 100), max(offset, 0)
		for i := offset; i < min(offset+limit, len(b.Input.Rows)); i++ {
			r, e := s.Service.Repo.GetOnboardingRecord(ctx, tenant, batchRowID(id, i))
			if errors.Is(e, model.ErrNotFound) {
				q := batchEnroll(*b.Input, i)
				r.Body, _ = json.Marshal(BatchRow{Index: i, DeviceID: q.Device.ID, Name: q.Device.Name, Status: "PENDING", CredentialStatus: "NONE"})
			} else if e != nil {
				return summary, nil, e
			}
			rows = append(rows, r)
		}
	} else {
		rows, _, err = s.Service.Repo.ListOnboardingRecords(ctx, tenant, owner.Username, batchRowKind(id), limit, offset)
		if err != nil {
			return summary, nil, err
		}
	}
	out := []BatchPublicRow{}
	for _, record := range rows {
		var row BatchRow
		if err = json.Unmarshal(record.Body, &row); err != nil {
			return summary, nil, err
		}
		status := row.CredentialStatus
		if row.Delivery != nil && row.Delivery.ExpiresAt <= time.Now().UnixMilli() {
			status = "EXPIRED"
		}
		out = append(out, BatchPublicRow{Index: row.Index, DeviceID: row.DeviceID, Name: row.Name, Status: row.Status, Error: row.Error, Mode: row.Mode, ProfileID: row.ProfileID, CredentialStatus: status})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return summary, out, nil
}

func (s *TaskService) aead() (cipher.AEAD, error) {
	if len(s.Key) < 32 {
		return nil, errors.New("接入凭据安全交付密钥未配置")
	}
	h := hmac.New(sha256.New, []byte(s.Key))
	_, _ = h.Write([]byte("torchlink-onboarding-delivery-v1"))
	block, err := aes.NewCipher(h.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func deliveryAAD(v model.OnboardingRecord) []byte {
	return []byte(v.TenantID + "\x00" + v.OwnerID + "\x00" + v.Kind + "\x00" + v.ID)
}
func (s *TaskService) sealCredential(v model.OnboardingRecord, c model.DeviceCredential) (*credentialDelivery, error) {
	aead, err := s.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	plain, _ := json.Marshal(c)
	return &credentialDelivery{Nonce: nonce, Ciphertext: aead.Seal(nil, nonce, plain, deliveryAAD(v)), ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli()}, nil
}

type BatchCredential struct {
	Index      int                    `json:"index"`
	DeviceID   string                 `json:"deviceId"`
	Credential model.DeviceCredential `json:"credential"`
	AccessInfo map[string]any         `json:"accessInfo,omitempty"`
}
type BatchCredentialUnavailable struct {
	Index    int    `json:"index"`
	DeviceID string `json:"deviceId"`
	Reason   string `json:"reason"`
}
type BatchCredentials struct {
	Items       []BatchCredential            `json:"items"`
	Unavailable []BatchCredentialUnavailable `json:"unavailable"`
}

func (s *TaskService) ClaimCredentials(ctx context.Context, tenant string, owner TaskOwner, id string, indices []int) (BatchCredentials, error) {
	result := BatchCredentials{Items: []BatchCredential{}, Unavailable: []BatchCredentialUnavailable{}}
	v, err := s.OwnedRecord(ctx, tenant, owner, id, BatchKind)
	if err != nil {
		return result, err
	}
	var b batchBody
	if err = json.Unmarshal(v.Body, &b); err != nil {
		return result, err
	}
	if len(indices) == 0 {
		for i := 0; i < b.Total; i++ {
			indices = append(indices, i)
		}
	}
	if len(indices) > maxBatchRows {
		return result, invalid("领取行数超限")
	}
	aead, err := s.aead()
	if err != nil {
		return result, err
	}
	seen := map[int]bool{}
	for _, index := range indices {
		if index < 0 || index >= b.Total || seen[index] {
			return result, invalid("领取行号无效或重复")
		}
		seen[index] = true
	}
	for _, index := range indices {
		if _, err = s.authorize(ctx, tenant, owner); err != nil {
			return BatchCredentials{}, err
		}
		r, e := s.Service.Repo.GetOnboardingRecord(ctx, tenant, batchRowID(id, index))
		if e != nil {
			result.Unavailable = append(result.Unavailable, BatchCredentialUnavailable{Index: index, Reason: "尚未生成"})
			continue
		}
		var row BatchRow
		if e = json.Unmarshal(r.Body, &row); e != nil {
			return BatchCredentials{}, e
		}
		if r.OwnerID != owner.Username || r.Kind != batchRowKind(id) {
			return BatchCredentials{}, model.ErrNotFound
		}
		if row.Delivery == nil || row.Delivery.ExpiresAt <= time.Now().UnixMilli() {
			result.Unavailable = append(result.Unavailable, BatchCredentialUnavailable{Index: index, DeviceID: row.DeviceID, Reason: "凭据已领取、过期或不可恢复；需要时显式重新签发"})
			continue
		}
		plain, e := aead.Open(nil, row.Delivery.Nonce, row.Delivery.Ciphertext, deliveryAAD(r))
		if e != nil {
			result.Unavailable = append(result.Unavailable, BatchCredentialUnavailable{Index: index, DeviceID: row.DeviceID, Reason: "安全交付密钥已变化，请显式重新签发"})
			continue
		}
		var credential model.DeviceCredential
		if e = json.Unmarshal(plain, &credential); e != nil {
			return BatchCredentials{}, e
		}
		device, lookupErr := s.Service.Repo.GetManagedDevice(ctx, tenant, row.DeviceID)
		if lookupErr != nil && !errors.Is(lookupErr, model.ErrNotFound) {
			return BatchCredentials{}, lookupErr
		}
		if lookupErr != nil || device.Status != "ENABLED" || device.ProductID != row.Request.ProductID || device.AccessKey != credential.AccessKey || !hmac.Equal([]byte(device.SecretHash), []byte(Hash(credential.Secret))) {
			row.Delivery = nil
			row.CredentialStatus = "REISSUE_REQUIRED"
			r.Body, _ = json.Marshal(row)
			if _, e = s.Service.Repo.SaveOnboardingRecord(ctx, r, r.Revision); e != nil && !errors.Is(e, model.ErrOnboardingChanged) {
				return BatchCredentials{}, e
			}
			result.Unavailable = append(result.Unavailable, BatchCredentialUnavailable{Index: index, DeviceID: row.DeviceID, Reason: "设备或凭据已变化，请查看设备详情并按需重新签发"})
			continue
		}
		row.Delivery = nil
		row.CredentialStatus = "DELIVERED"
		r.Body, _ = json.Marshal(row)
		if _, e = s.Service.Repo.SaveOnboardingRecord(ctx, r, r.Revision); e != nil {
			result.Unavailable = append(result.Unavailable, BatchCredentialUnavailable{Index: index, DeviceID: row.DeviceID, Reason: "凭据已被其他请求领取，请刷新"})
			continue
		}
		result.Items = append(result.Items, BatchCredential{Index: index, DeviceID: row.DeviceID, Credential: credential})
	}
	return result, nil
}
