package externaldata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type schedule struct {
	Watermark   int64  `json:"watermark"`
	JobID       string `json:"jobId,omitempty"`
	LastSuccess int64  `json:"lastSuccess,omitempty"`
	Error       string `json:"error,omitempty"`
}
type eventState struct {
	Event           Event  `json:"event"`
	Hash            string `json:"hash"`
	Result          Result `json:"result"`
	RecordID        string `json:"recordId"`
	PendingHash     string `json:"pendingHash,omitempty"`
	PendingRecordID string `json:"pendingRecordId,omitempty"`
}

func (s *Service) ensureSchedule(ctx context.Context, tenant string, ep Endpoint) error {
	e, err := s.Store.Get(ctx, tenant, "schedule", ep.ID)
	if errors.Is(err, ErrNotFound) {
		from := ep.StartAt
		if from == 0 {
			from = time.Now().UnixMilli()
		}
		_, err = s.Store.Put(ctx, Entry{TenantID: tenant, Kind: "schedule", ID: ep.ID, SourceID: ep.SourceID, EndpointID: ep.ID, Status: "PENDING", Body: body(schedule{Watermark: from})}, 0)
		if errors.Is(err, ErrConflict) {
			return nil
		}
		return err
	}
	if err != nil {
		return err
	}
	if e.Status == "RUNNING" {
		return nil
	}
	e.DueAt = time.Now().UnixMilli()
	e.Status = "PENDING"
	_, err = s.Store.Put(ctx, e, e.Revision)
	return err
}

func (s *Service) Pull(ctx context.Context, tenant string, ep Endpoint, from, to int64) (Entry, error) {
	if ep.Mode != "pull" {
		return Entry{}, invalid("该接口不是拉取接口")
	}
	src, err := s.Source(ctx, tenant, ep.SourceID)
	if err != nil {
		return Entry{}, err
	}
	if !src.Enabled || !ep.Enabled {
		return Entry{}, invalid("请先启用外部系统和接口")
	}
	if _, err = s.Authorize(ctx, tenant, src, ep); err != nil {
		return Entry{}, err
	}
	if to == 0 {
		to = time.Now().UnixMilli()
	}
	if from == 0 {
		from = ep.StartAt
		if from == 0 {
			from = to - 3600000
		}
	}
	if from < 0 || to <= from || to > time.Now().Add(time.Minute).UnixMilli() {
		return Entry{}, invalid("拉取开始时间必须早于结束时间，结束时间不能晚于当前时间")
	}
	j := Job{Manual: true, From: from, To: to, Page: ep.Pagination.Start, ConfigRevision: ep.Revision}
	return s.Store.Put(ctx, Entry{TenantID: tenant, Kind: "job", ID: uuid.NewString(), SourceID: ep.SourceID, EndpointID: ep.ID, Status: "PENDING", Body: body(j)}, 0)
}

func (s *Service) FetchPreview(ctx context.Context, tenant string, ep Endpoint, from, to int64) (map[string]any, error) {
	src, err := s.Source(ctx, tenant, ep.SourceID)
	if err != nil {
		return nil, err
	}
	ctx, err = s.Authorize(ctx, tenant, src, ep)
	if err != nil {
		return nil, err
	}
	if to == 0 {
		to = time.Now().UnixMilli()
	}
	if from == 0 {
		from = to - 3600000
	}
	r, err := s.fetch(ctx, tenant, src, ep, Job{From: from, To: to, Page: ep.Pagination.Start}, true)
	if err != nil {
		return nil, err
	}
	preview, err := s.Preview(ep, r.Body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"raw": json.RawMessage(r.Body), "items": preview, "done": r.Done, "nextCursor": r.NextCursor}, nil
}

// Run belongs to the jobs process. Database claims coordinate all replicas.
func (s *Service) Run(ctx context.Context, log *slog.Logger) {
	for i := 0; i < 4; i++ {
		go func() {
			owner := uuid.NewString()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					for _, kind := range []string{"receipt", "schedule", "job", "record"} {
						for n := 0; n < 8; n++ {
							worked, err := s.Step(ctx, kind, owner)
							if err != nil {
								log.Warn("external data worker", "kind", kind, "error", err)
							}
							if !worked || ctx.Err() != nil {
								break
							}
						}
					}
				}
			}
		}()
	}
}

func (s *Service) Step(ctx context.Context, kind, owner string) (bool, error) {
	e, err := s.Store.Claim(ctx, kind, owner, time.Now().UnixMilli(), 180000)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	switch kind {
	case "receipt":
		_, err = s.processReceipt(ctx, e)
	case "schedule":
		err = s.processSchedule(ctx, e)
	case "job":
		err = s.processJob(ctx, e)
	case "record":
		err = s.processRecord(ctx, e)
	}
	return true, err
}

func (s *Service) finish(ctx context.Context, e Entry, status string, due int64, v any) error {
	e.Status = status
	e.DueAt = due
	e.Owner = ""
	e.LeaseUntil = 0
	e.Body = body(v)
	_, err := s.Store.Put(ctx, e, e.Revision)
	return err
}

func (s *Service) processSchedule(ctx context.Context, e Entry) error {
	st, err := read[schedule](e)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	next := now + 10000
	ep, err := s.Endpoint(ctx, e.TenantID, e.EndpointID)
	if err != nil {
		st.Error = "接口不可用"
		return s.finish(ctx, e, "PENDING", now+60000, st)
	}
	src, err := s.Source(ctx, e.TenantID, e.SourceID)
	if err != nil || !src.Enabled || !ep.Enabled || ep.IntervalSeconds == 0 {
		return s.finish(ctx, e, "PENDING", now+60000, st)
	}
	if _, err = s.Authorize(ctx, e.TenantID, src, ep); err != nil {
		st.Error = "执行用户当前没有接入权限"
		return s.finish(ctx, e, "PENDING", now+60000, st)
	}
	if st.JobID != "" {
		jobEntry, err := s.Store.Get(ctx, e.TenantID, "job", st.JobID)
		if err != nil {
			return err
		}
		job, err := read[Job](jobEntry)
		if err != nil {
			return err
		}
		if jobEntry.Status == "COMPLETED" {
			st.Watermark = job.To
			st.LastSuccess = now
			st.JobID = ""
			st.Error = ""
			next = now + int64(ep.IntervalSeconds)*1000
		} else if jobEntry.Status == "FAILED" {
			if job.ConfigRevision != ep.Revision {
				st.JobID = ""
				st.Error = ""
				return s.finish(ctx, e, "PENDING", now, st)
			}
			st.Error = "最近自动拉取失败，请重试该任务"
			next = now + 60000
		}
		return s.finish(ctx, e, "PENDING", next, st)
	}
	from := max(int64(0), st.Watermark-int64(ep.OverlapSeconds)*1000)
	if from >= now {
		return s.finish(ctx, e, "PENDING", now+int64(ep.IntervalSeconds)*1000, st)
	}
	j := Job{From: from, To: now, Page: ep.Pagination.Start, ConfigRevision: ep.Revision}
	id := key(ep.ID, fmt.Sprint(st.Watermark), fmt.Sprint(ep.Revision))
	_, err = s.Store.Put(ctx, Entry{TenantID: e.TenantID, Kind: "job", ID: id, SourceID: ep.SourceID, EndpointID: ep.ID, Status: "PENDING", Body: body(j)}, 0)
	if err != nil && !errors.Is(err, ErrConflict) {
		return err
	}
	st.JobID = id
	return s.finish(ctx, e, "PENDING", next, st)
}

func (s *Service) processJob(ctx context.Context, e Entry) error {
	j, err := read[Job](e)
	if err != nil {
		return err
	}
	ep, err := s.Endpoint(ctx, e.TenantID, e.EndpointID)
	if err != nil {
		return s.jobFailure(ctx, e, j, 8, "接口配置不可用")
	}
	src, err := s.Source(ctx, e.TenantID, e.SourceID)
	if err != nil || !src.Enabled || !ep.Enabled {
		return s.finish(ctx, e, "RETRY", time.Now().Add(time.Minute).UnixMilli(), j)
	}
	workCtx, err := s.Authorize(ctx, e.TenantID, src, ep)
	if err != nil {
		return s.jobFailure(ctx, e, j, ep.MaxAttempts, "执行用户没有当前接口或对象权限")
	}
	// A changed configuration must not silently reinterpret a stored cursor.
	if ep.Revision != j.ConfigRevision {
		return s.finish(ctx, e, "FAILED", 0, withJobError(j, "接口配置已变更，请新建补拉任务；原分页进度已保留"))
	}
	r, err := s.fetch(workCtx, e.TenantID, src, ep, j, false)
	if err != nil {
		if len(r.Body) > 0 {
			if saveErr := s.ReceiveFailed(ctx, e.TenantID, ep, r.Body, e.ID, err.Error()); saveErr != nil {
				return s.jobFailure(ctx, e, j, ep.MaxAttempts, "保存失败响应原文时发生错误")
			}
		}
		var throttled *RateLimitError
		if errors.As(err, &throttled) {
			j.Error = throttled.Error()
			return s.finish(ctx, e, "RETRY", throttled.RetryAt, j)
		}
		return s.jobFailure(ctx, e, j, ep.MaxAttempts, err.Error())
	}
	n, err := s.Receive(ctx, e.TenantID, ep, r.Body, e.ID)
	if err != nil {
		return s.jobFailure(ctx, e, j, ep.MaxAttempts, "保存拉取结果失败")
	}
	j.Page, _ = paginationPosition(ep.Pagination, j)
	j.Received += n
	j.Pages++
	j.Attempts = 0
	j.Error = ""
	if r.Done {
		return s.finish(ctx, e, "COMPLETED", 0, j)
	}
	maxPages := ep.Pagination.MaxPages
	if maxPages == 0 {
		maxPages = 1000
	}
	if j.Pages >= maxPages {
		return s.finish(ctx, e, "FAILED", 0, withJobError(j, "已达到分页上限，进度已保留，请检查分页配置"))
	}
	j.Cursor = r.NextCursor
	if ep.Pagination.Mode == "offset" {
		size := ep.Pagination.PageSize
		if size <= 0 {
			size = 100
		}
		j.Page += size
	} else {
		j.Page++
	}
	return s.finish(ctx, e, "PENDING", time.Now().UnixMilli(), j)
}
func withJobError(j Job, err string) Job { j.Error = err; return j }
func (s *Service) jobFailure(ctx context.Context, e Entry, j Job, maxAttempts int, detail string) error {
	j.Attempts++
	j.Error = detail
	status := "RETRY"
	if maxAttempts == 0 {
		maxAttempts = 8
	}
	if j.Attempts >= maxAttempts {
		status = "FAILED"
	}
	return s.finish(ctx, e, status, time.Now().Add(backoff(j.Attempts)).UnixMilli(), j)
}
func backoff(attempt int) time.Duration {
	return time.Duration(min(300, 1<<min(attempt, 8))) * time.Second
}

func (s *Service) processRecord(ctx context.Context, e Entry) error {
	r, err := read[Record](e)
	if err != nil {
		return err
	}
	ep, err := s.Endpoint(ctx, e.TenantID, e.EndpointID)
	if err != nil {
		return s.recordFailure(ctx, e, r, 8, "FAILED", "接口配置不可用")
	}
	src, err := s.Source(ctx, e.TenantID, e.SourceID)
	if err != nil || !src.Enabled || !ep.Enabled {
		return s.finish(ctx, e, "RETRY", time.Now().Add(time.Minute).UnixMilli(), r)
	}
	workCtx, err := s.Authorize(ctx, e.TenantID, src, ep)
	if err != nil {
		return s.recordFailure(ctx, e, r, ep.MaxAttempts, "FAILED", "执行用户没有当前接口权限")
	}
	ev, filtered, err := Transform(r.Mapping, r.Raw)
	r.Event = &ev
	if err != nil {
		return s.recordFailure(ctx, e, r, ep.MaxAttempts, "FAILED", err.Error())
	}
	if filtered {
		return s.finish(ctx, e, "FILTERED", 0, r)
	}
	var binding Binding
	if ep.Kind != "event" || ev.ObjectID != "" {
		kind := "device"
		if ep.Kind == "video_alarm" {
			kind = "camera"
		}
		b, err := s.Store.Get(ctx, e.TenantID, "binding", key(src.ID, kind, ev.ObjectID))
		if err != nil {
			return s.recordFailure(ctx, e, r, ep.MaxAttempts, "WAITING_BINDING", "外部编号尚未关联平台对象")
		}
		binding, err = read[Binding](b)
		if err != nil {
			return err
		}
	}
	ledgerID := key(src.ID, ep.Kind, ev.ID)
	le, err := s.Store.Get(ctx, e.TenantID, "event", ledgerID)
	var previous eventState
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil {
		previous, err = read[eventState](le)
		if err != nil {
			return err
		}
		if le.Status == "LOCKED" && le.LeaseUntil > time.Now().UnixMilli() {
			return s.recordFailure(ctx, e, r, ep.MaxAttempts, "RETRY", "同一事件正在处理")
		}
	}
	hash := key(string(body(ev)))
	if previous.Hash != "" {
		if previous.Hash == hash {
			r.DeviceID = previous.Result.DeviceID
			r.CameraID = previous.Result.CameraID
			r.MessageID = previous.Result.MessageID
			r.AlarmID = previous.Result.AlarmID
			return s.finish(ctx, e, "DUPLICATE", 0, r)
		}
		oldOrder, newOrder := previous.Event.Timestamp, ev.Timestamp
		if previous.Event.Version > 0 && ev.Version > 0 {
			oldOrder, newOrder = previous.Event.Version, ev.Version
		}
		if newOrder < oldOrder {
			return s.finish(ctx, e, "IGNORED", 0, r)
		}
		if newOrder == oldOrder {
			return s.recordFailure(ctx, e, r, ep.MaxAttempts, "CONFLICT", "同一事件的版本或时间相同但内容不同，请检查源数据")
		}
	}
	if previous.PendingHash != "" && previous.PendingHash != hash {
		r.Error = "前一版本尚未完成，等待其处理结果后继续"
		return s.finish(ctx, e, "RETRY", time.Now().Add(5*time.Second).UnixMilli(), r)
	}
	// Reserve the event across interfaces and transport modes. Delivery uses a
	// stable business identity so a crash before this ledger commit is replayable.
	if le.ID == "" {
		le = Entry{TenantID: e.TenantID, Kind: "event", ID: ledgerID, SourceID: src.ID, EndpointID: ep.ID}
	}
	le.Status = "LOCKED"
	le.LeaseUntil = time.Now().Add(160 * time.Second).UnixMilli()
	le.Owner = e.Owner
	previous.PendingHash = hash
	previous.PendingRecordID = e.ID
	le.Body = body(previous)
	le, err = s.Store.Put(ctx, le, le.Revision)
	if err != nil {
		return s.recordFailure(ctx, e, r, ep.MaxAttempts, "RETRY", "事件正在由其他任务更新")
	}
	result, deliveryErr := s.Deliver(workCtx, e.TenantID, src, ep, ev, binding, previous.Result)
	if deliveryErr != nil {
		r.DeviceID = result.DeviceID
		r.CameraID = result.CameraID
		r.MessageID = result.MessageID
		r.AlarmID = result.AlarmID
		le.Status = "RETRY"
		le.LeaseUntil = 0
		le.Owner = ""
		// Configuration validation happens before business delivery. A corrected
		// mapping must be able to replace that attempt; an enqueued raw message
		// keeps its reservation until its business result has been observed.
		if errors.Is(deliveryErr, ErrInvalid) {
			previous.PendingHash = ""
			previous.PendingRecordID = ""
			le.Body = body(previous)
		}
		_, _ = s.Store.Put(ctx, le, le.Revision)
		return s.recordFailure(ctx, e, r, ep.MaxAttempts, "RETRY", deliveryErr.Error())
	}
	le.Status = "PROCESSED"
	le.LeaseUntil = 0
	le.Owner = ""
	le.Body = body(eventState{Event: ev, Hash: hash, Result: result, RecordID: e.ID})
	if _, err = s.Store.Put(ctx, le, le.Revision); err != nil {
		return err
	}
	r.DeviceID = result.DeviceID
	r.CameraID = result.CameraID
	r.MessageID = result.MessageID
	r.AlarmID = result.AlarmID
	r.Error = ""
	return s.finish(ctx, e, "PROCESSED", 0, r)
}

func (s *Service) recordFailure(ctx context.Context, e Entry, r Record, maxAttempts int, status, detail string) error {
	r.Attempts++
	r.Error = detail
	if maxAttempts == 0 {
		maxAttempts = 8
	}
	if status == "RETRY" && r.Attempts >= maxAttempts {
		status = "FAILED"
	}
	return s.finish(ctx, e, status, time.Now().Add(backoff(r.Attempts)).UnixMilli(), r)
}
