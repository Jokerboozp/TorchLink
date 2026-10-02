package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/model"
)

// Run resumes durable tasks after restart. At most two batches run in this
// process; each batch executes one row at a time and holds a renewable lease.
func (s *TaskService) Run(ctx context.Context) {
	owner := "onboarding-" + uuid.NewString()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		_ = s.ExpireCredentialDeliveries(ctx)
		jobs, err := s.Service.Repo.ListPendingOnboardingRecords(ctx, BatchKind, 20)
		if err == nil {
			var wg sync.WaitGroup
			slots := make(chan struct{}, 2)
			for _, job := range jobs {
				select {
				case slots <- struct{}{}:
				case <-ctx.Done():
					wg.Wait()
					return
				}
				wg.Add(1)
				go func(job model.OnboardingRecord) {
					defer wg.Done()
					defer func() { <-slots }()
					_ = s.RunBatch(ctx, job.TenantID, job.ID, owner)
				}(job)
			}
			wg.Wait()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *TaskService) saveBatch(ctx context.Context, v model.OnboardingRecord, b batchBody, status string) (model.OnboardingRecord, error) {
	v.Status = status
	v.Body, _ = json.Marshal(b)
	return s.Service.Repo.SaveOnboardingRecord(ctx, v, v.Revision)
}

func (s *TaskService) RunBatch(ctx context.Context, tenant, id, worker string) error {
	lease, ok, err := s.Service.Repo.AcquireExecutionLease(ctx, tenant, "onboarding/"+id, worker, "", 30*time.Second)
	if err != nil || !ok {
		return err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Service.Repo.ReleaseExecutionLease(releaseCtx, lease)
	}()
	v, err := s.Service.Repo.GetOnboardingRecord(ctx, tenant, id)
	if err != nil {
		return err
	}
	if v.Kind != BatchKind || (v.Status != "INITIALIZING" && v.Status != "QUEUED" && v.Status != "RUNNING") {
		return nil
	}
	var b batchBody
	if err = json.Unmarshal(v.Body, &b); err != nil {
		return err
	}
	if b.Owner.Username != v.OwnerID {
		return errors.New("batch owner mismatch")
	}
	if _, err = s.authorize(ctx, tenant, b.Owner); err != nil {
		b.Error = "账户、会话或设备范围已变化，任务已暂停"
		_, saveErr := s.saveBatch(ctx, v, b, "PAUSED")
		return saveErr
	}
	if b.Input != nil {
		for i := range b.Input.Rows {
			if err = ctx.Err(); err != nil {
				return err
			}
			if i%25 == 0 {
				lease, ok, err = s.Service.Repo.AcquireExecutionLease(ctx, tenant, "onboarding/"+id, worker, "", 30*time.Second)
				if err != nil || !ok {
					return err
				}
			}
			q := batchEnroll(*b.Input, i)
			row := BatchRow{Index: i, DeviceID: q.Device.ID, Name: q.Device.Name, Status: "PENDING", CredentialStatus: "NONE", Request: q}
			data, _ := json.Marshal(row)
			_, err = s.Service.Repo.SaveOnboardingRecord(ctx, model.OnboardingRecord{TenantID: tenant, ID: batchRowID(id, i), OwnerID: v.OwnerID, Kind: batchRowKind(id), Status: "PENDING", Body: data}, 0)
			if err != nil && !errors.Is(err, model.ErrOnboardingChanged) {
				return err
			}
		}
		b.Input = nil
	}
	for _, index := range b.RetryIndices {
		r, e := s.Service.Repo.GetOnboardingRecord(ctx, tenant, batchRowID(id, index))
		if e != nil {
			return e
		}
		var row BatchRow
		if e = json.Unmarshal(r.Body, &row); e != nil {
			return e
		}
		if row.Status == "FAILED" {
			row.Status, row.Error = "PENDING", ""
			r.Status = "PENDING"
			r.Body, _ = json.Marshal(row)
			if _, e = s.Service.Repo.SaveOnboardingRecord(ctx, r, r.Revision); e != nil {
				return e
			}
		}
	}
	b.RetryIndices = nil
	b.Error = ""
	b.Succeeded, b.Failed, b.Pending = 0, 0, 0
	// Recount once on every resume: a process may have committed a row but not
	// its summary. Row records are the durable source of aggregate counts.
	for i := 0; i < b.Total; i++ {
		if i%25 == 0 {
			lease, ok, err = s.Service.Repo.AcquireExecutionLease(ctx, tenant, "onboarding/"+id, worker, "", 30*time.Second)
			if err != nil || !ok {
				return err
			}
		}
		r, e := s.Service.Repo.GetOnboardingRecord(ctx, tenant, batchRowID(id, i))
		if e != nil {
			return e
		}
		// Enrollment and the row result have separate commits. Recover only an
		// already persisted matching device before checking template readiness;
		// a later template upgrade must not hide a completed registration.
		r, e = s.recoverCommittedBatchRow(ctx, v, b, i, r)
		if e != nil {
			return e
		}
		switch r.Status {
		case "SUCCEEDED":
			b.Succeeded++
		case "FAILED":
			b.Failed++
		default:
			b.Pending++
		}
	}
	current, err := s.templateFingerprint(ctx, tenant, b.ProductID)
	if err != nil || current != b.TemplateFingerprint {
		status := "PAUSED"
		b.Error = "模板配置已变化，任务已暂停；请重新预检剩余设备"
		if b.Pending == 0 {
			status, b.Error = "COMPLETED", ""
			if b.Failed > 0 {
				status = "PARTIAL_FAILED"
			}
			b.ActiveIndices = nil
		}
		_, saveErr := s.saveBatch(ctx, v, b, status)
		return saveErr
	}
	v, err = s.saveBatch(ctx, v, b, "RUNNING")
	if err != nil {
		return err
	}
	for i := 0; i < b.Total; i++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		if len(b.ActiveIndices) > 0 && !slices.Contains(b.ActiveIndices, i) {
			continue
		}
		lease, ok, err = s.Service.Repo.AcquireExecutionLease(ctx, tenant, "onboarding/"+id, worker, "", 30*time.Second)
		if err != nil || !ok {
			return err
		}
		r, e := s.Service.Repo.GetOnboardingRecord(ctx, tenant, batchRowID(id, i))
		if e != nil {
			return e
		}
		if r.Status == "SUCCEEDED" || r.Status == "FAILED" {
			continue
		}
		rowCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		authorized, e := s.authorize(rowCtx, tenant, b.Owner)
		if e != nil {
			cancel()
			b.Error = "账户、会话或设备范围已变化，任务已暂停"
			_, e = s.saveBatch(ctx, v, b, "PAUSED")
			return e
		}
		fingerprint, e := s.templateFingerprint(authorized, tenant, b.ProductID)
		if e != nil || fingerprint != b.TemplateFingerprint {
			cancel()
			b.Error = "模板配置已变化，任务已暂停；请重新预检剩余设备"
			_, e = s.saveBatch(ctx, v, b, "PAUSED")
			return e
		}
		var row BatchRow
		if e = json.Unmarshal(r.Body, &row); e != nil {
			cancel()
			return e
		}
		row.Status = "RUNNING"
		r.Status = "RUNNING"
		r.Body, _ = json.Marshal(row)
		r, e = s.Service.Repo.SaveOnboardingRecord(authorized, r, r.Revision)
		if e != nil {
			cancel()
			return e
		}
		result, enrollErr := s.Service.enroll(authorized, tenant, row.Request, b.TemplateFingerprint)
		if enrollErr != nil {
			row.Status, row.Error = "FAILED", publicTaskError(enrollErr)
			b.Failed++
		} else {
			row.Status, row.Error, row.Mode = "SUCCEEDED", "", result.Mode
			row.CredentialStatus = "NONE"
			if result.Profile != nil {
				row.ProfileID = result.Profile.ID
			}
			if result.Credential.Secret != "" {
				row.Delivery, e = s.sealCredential(r, result.Credential)
				if e != nil {
					row.CredentialStatus = "REISSUE_REQUIRED"
				} else {
					row.CredentialStatus = "AVAILABLE"
					if e = s.scheduleCredentialExpiry(authorized, r, row.Delivery.ExpiresAt); e != nil {
						cancel()
						return e
					}
				}
			} else if result.Device.UsesPlatformCredentials(result.Product) {
				row.CredentialStatus = "REISSUE_REQUIRED"
			}
			b.Succeeded++
		}
		r.Status = row.Status
		r.Body, _ = json.Marshal(row)
		_, e = s.Service.Repo.SaveOnboardingRecord(authorized, r, r.Revision)
		cancel()
		if e != nil {
			return e
		}
		b.Pending--
		v, e = s.saveBatch(ctx, v, b, "RUNNING")
		if e != nil {
			return e
		}
	}
	status := "COMPLETED"
	if b.Failed > 0 {
		status = "PARTIAL_FAILED"
	}
	if b.Pending > 0 {
		status = "PAUSED"
		b.Error = "本次选中行已处理，其余待处理行可继续"
	}
	b.ActiveIndices = nil
	_, err = s.saveBatch(ctx, v, b, status)
	return err
}

// recoverCommittedBatchRow cannot register or modify a device. The original
// request digest must match the existing device before the unfinished row can
// become registered; its lost one-time secret is never regenerated.
func (s *TaskService) recoverCommittedBatchRow(ctx context.Context, task model.OnboardingRecord, batch batchBody, index int, record model.OnboardingRecord) (model.OnboardingRecord, error) {
	if record.Status != "RUNNING" {
		return record, nil
	}
	var row BatchRow
	if err := json.Unmarshal(record.Body, &row); err != nil {
		return record, err
	}
	if record.OwnerID != task.OwnerID || record.Kind != batchRowKind(task.ID) || row.Index != index || row.DeviceID != row.Request.Device.ID || row.Request.ProductID != batch.ProductID || row.Request.RequestID != "batch-"+Hash(task.ID + "/" + fmt.Sprint(index))[:32] {
		return record, errors.New("batch recovery request identity mismatch")
	}
	authorized, err := s.authorize(ctx, task.TenantID, batch.Owner)
	if err != nil {
		return record, err
	}
	device, err := s.Service.Repo.GetManagedDevice(authorized, task.TenantID, row.DeviceID)
	if errors.Is(err, model.ErrNotFound) {
		return record, nil
	}
	if err != nil {
		return record, err
	}
	encoded, err := json.Marshal(normalizeEnroll(row.Request))
	if err != nil {
		return record, err
	}
	if device.TenantID != task.TenantID || device.ID != row.DeviceID || device.ProductID != batch.ProductID || device.OnboardingRequestHash != Hash(string(encoded)) {
		return record, nil
	}
	row.Status, row.Error, row.Mode = "SUCCEEDED", "", row.Request.Connection.Mode
	row.ProfileID = device.ConnectorProfileID
	row.Delivery, row.CredentialStatus = nil, "NONE"
	if device.UsesPlatformCredentials(model.Product{}) {
		row.CredentialStatus = "REISSUE_REQUIRED"
	}
	record.Status = row.Status
	record.Body, _ = json.Marshal(row)
	return s.Service.Repo.SaveOnboardingRecord(authorized, record, record.Revision)
}

const deliveryExpiryKind = "device-credential-expiry"

type deliveryExpiry struct {
	RowID     string `json:"rowId"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (s *TaskService) scheduleCredentialExpiry(ctx context.Context, row model.OnboardingRecord, expiresAt int64) error {
	body, _ := json.Marshal(deliveryExpiry{RowID: row.ID, ExpiresAt: expiresAt})
	_, err := s.Service.Repo.SaveOnboardingRecord(ctx, model.OnboardingRecord{TenantID: row.TenantID, ID: "expiry-" + Hash(row.Kind + "\x00" + row.ID)[:32], OwnerID: row.OwnerID, Kind: deliveryExpiryKind, Status: "QUEUED", Body: body}, 0)
	if errors.Is(err, model.ErrOnboardingChanged) {
		return nil
	}
	return err
}

// ExpireCredentialDeliveries erases ciphertext after its delivery window even
// when the browser never returns. The queue contains only row IDs and deadlines.
func (s *TaskService) ExpireCredentialDeliveries(ctx context.Context) error {
	jobs, err := s.Service.Repo.ListPendingOnboardingRecords(ctx, deliveryExpiryKind, 100)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		var expiry deliveryExpiry
		if err = json.Unmarshal(job.Body, &expiry); err != nil {
			return err
		}
		if expiry.ExpiresAt > time.Now().UnixMilli() {
			continue
		}
		r, e := s.Service.Repo.GetOnboardingRecord(ctx, job.TenantID, expiry.RowID)
		if e != nil && !errors.Is(e, model.ErrNotFound) {
			return e
		}
		if e == nil {
			if r.OwnerID != job.OwnerID {
				return errors.New("credential expiry owner mismatch")
			}
			var row BatchRow
			if e = json.Unmarshal(r.Body, &row); e != nil {
				return e
			}
			if row.Delivery != nil && row.Delivery.ExpiresAt <= time.Now().UnixMilli() {
				row.Delivery = nil
				row.CredentialStatus = "EXPIRED"
				r.Body, _ = json.Marshal(row)
				if _, e = s.Service.Repo.SaveOnboardingRecord(ctx, r, r.Revision); e != nil {
					if errors.Is(e, model.ErrOnboardingChanged) {
						continue
					}
					return e
				}
			}
		}
		job.Status = "COMPLETED"
		if _, e = s.Service.Repo.SaveOnboardingRecord(ctx, job, job.Revision); e != nil && !errors.Is(e, model.ErrOnboardingChanged) {
			return e
		}
	}
	return nil
}

func (s *TaskService) RetryBatch(ctx context.Context, tenant string, owner TaskOwner, id string, revision int64, indices []int) (BatchSummary, error) {
	v, err := s.OwnedRecord(ctx, tenant, owner, id, BatchKind)
	if err != nil {
		return BatchSummary{}, err
	}
	if v.Revision != revision {
		return BatchSummary{}, model.ErrOnboardingChanged
	}
	if v.Status != "PARTIAL_FAILED" && v.Status != "PAUSED" {
		return BatchSummary{}, conflict("任务仍在运行或没有失败项")
	}
	if len(indices) == 0 || len(indices) > maxBatchRows {
		return BatchSummary{}, invalid("请选择需要重试的失败行")
	}
	var b batchBody
	if err = json.Unmarshal(v.Body, &b); err != nil {
		return BatchSummary{}, err
	}
	fingerprint, err := s.templateFingerprint(ctx, tenant, b.ProductID)
	if err != nil {
		return BatchSummary{}, err
	}
	if fingerprint != b.TemplateFingerprint {
		return BatchSummary{}, conflict("模板配置已变化，请新建任务并重新预检剩余设备")
	}
	seen := map[int]bool{}
	for _, index := range indices {
		if index < 0 || index >= b.Total || seen[index] {
			return BatchSummary{}, invalid("重试行号无效或重复")
		}
		seen[index] = true
		r, e := s.Service.Repo.GetOnboardingRecord(ctx, tenant, batchRowID(id, index))
		if errors.Is(e, model.ErrNotFound) && v.Status == "PAUSED" && b.Input != nil && index < len(b.Input.Rows) {
			continue
		}
		if e != nil {
			return BatchSummary{}, e
		}
		if r.Status != "FAILED" && !(v.Status == "PAUSED" && (r.Status == "PENDING" || r.Status == "RUNNING")) {
			return BatchSummary{}, invalid(fmt.Sprintf("第 %d 行不是失败或暂停的待处理行", index+1))
		}
	}
	b.Owner = owner
	b.RetryIndices = indices
	b.ActiveIndices = indices
	b.Error = ""
	v, err = s.saveBatch(ctx, v, b, "QUEUED")
	return summarizeBatch(v, b), err
}
