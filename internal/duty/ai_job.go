package duty

import (
	"context"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Service) job(tx ports.DutyTx, a Actor, c Command) (any, error) {
	if c.Operation != "stop" {
		return nil, invalid("未知AI任务操作")
	}
	doc, j, e := read[AIJob](tx, model.DutyAIJobKind, c.ID)
	if e != nil {
		return nil, e
	}
	if e = checkVersion(doc, c.ExpectedVersion); e != nil {
		return nil, e
	}
	if j.RequesterID != a.Username && !a.Admin {
		return nil, ErrForbidden
	}
	if !a.covers(j.DeviceIDs) {
		return nil, ErrForbidden
	}
	if j.Status == "RUNNING" {
		j.Status = "STOP_REQUESTED"
		j.Stage = "等待停止"
	} else if j.Status == "QUEUED" {
		j.Status = "CANCELLED"
		j.Stage = "已停止"
		j.FinishedAt = s.now()
	} else {
		return nil, invalid("任务已结束或正在停止")
	}
	return put(tx, doc, j, c.ExpectedVersion)
}

// ClaimJob also recovers expired leases. A lease owner is a fencing token: a
// stale process cannot complete an interrupted or superseded generation.
func (s *Service) ClaimJob(ctx context.Context, tenant, jobID, owner string, lease time.Duration) (model.DutyDocument, bool, error) {
	var out model.DutyDocument
	claimed := false
	if owner == "" || lease <= 0 {
		return out, false, invalid("无效任务租约")
	}
	err := s.Store.DutyTransaction(ctx, tenant, func(tx ports.DutyTx) error {
		out = model.DutyDocument{}
		claimed = false
		doc, j, e := read[AIJob](tx, model.DutyAIJobKind, jobID)
		if e != nil {
			return e
		}
		if j.Status == "STOP_REQUESTED" && j.LeaseUntil <= s.now() {
			j.Status = "CANCELLED"
			j.Stage = "已停止"
			j.FinishedAt = s.now()
			out, e = put(tx, doc, j, doc.Version)
			return e
		}
		if j.Status != "QUEUED" && (j.Status != "RUNNING" || j.LeaseUntil > s.now()) {
			out = doc
			return nil
		}
		_, h, e := read[Handover](tx, model.DutyHandoverKind, j.HandoverID)
		if e != nil {
			return e
		}
		if h.CurrentRevisionID != j.RevisionID || (h.Status != "DRAFT" && h.Status != "RETURNED") {
			j.Status = "INTERRUPTED"
			j.Stage = "交接版本已变化"
			j.FinishedAt = s.now()
			out, e = put(tx, doc, j, doc.Version)
			return e
		}
		j.Status = "RUNNING"
		j.Stage = "整理交接事实"
		j.LeaseOwner = owner
		j.LeaseUntil = s.now() + lease.Milliseconds()
		j.Attempt++
		if j.StartedAt == 0 {
			j.StartedAt = s.now()
		}
		out, e = put(tx, doc, j, doc.Version)
		claimed = e == nil
		return e
	})
	return out, claimed, err
}
func (s *Service) HeartbeatJob(ctx context.Context, tenant, jobID, owner, stage string, lease time.Duration) (bool, error) {
	ok := false
	err := s.Store.DutyTransaction(ctx, tenant, func(tx ports.DutyTx) error {
		ok = false
		doc, j, e := read[AIJob](tx, model.DutyAIJobKind, jobID)
		if e != nil {
			return e
		}
		if j.LeaseOwner != owner {
			return nil
		}
		if j.Status == "STOP_REQUESTED" {
			j.Status = "CANCELLED"
			j.Stage = "已停止"
			j.FinishedAt = s.now()
			_, e = put(tx, doc, j, doc.Version)
			return e
		}
		if j.Status != "RUNNING" || j.LeaseUntil < s.now() {
			return nil
		}
		j.Stage = stage
		j.LeaseUntil = s.now() + lease.Milliseconds()
		_, e = put(tx, doc, j, doc.Version)
		ok = e == nil
		return e
	})
	return ok, err
}

func ValidateAIResult(rev Revision, result *model.DutyAIResult) error {
	if result == nil {
		return invalid("AI没有返回结果")
	}
	ids := map[string]bool{}
	for _, ev := range rev.Evidence {
		ids[ev.ID] = true
	}
	for _, section := range [][]model.DutyAIStatement{result.Highlights, result.Suggestions, result.Missing} {
		for _, statement := range section {
			if strings.TrimSpace(statement.Text) == "" {
				return invalid("AI结果包含空内容")
			}
			if len(statement.EvidenceIDs) == 0 {
				return invalid("AI说明缺少证据引用")
			}
			for _, id := range statement.EvidenceIDs {
				if !ids[id] {
					return invalid("AI引用了无效证据 %s", id)
				}
			}
		}
	}
	if result.InputCount < 0 || result.TotalCount < result.InputCount {
		return invalid("AI输入统计无效")
	}
	return nil
}
func (s *Service) FinishJob(ctx context.Context, tenant, jobID, owner string, result *model.DutyAIResult, modelName, harnessRunID, failure string) (model.DutyDocument, error) {
	var out model.DutyDocument
	err := s.Store.DutyTransaction(ctx, tenant, func(tx ports.DutyTx) error {
		out = model.DutyDocument{}
		doc, j, e := read[AIJob](tx, model.DutyAIJobKind, jobID)
		if e != nil {
			return e
		}
		out = doc
		if j.LeaseOwner != owner || j.Status != "RUNNING" || j.LeaseUntil < s.now() {
			if j.Status == "STOP_REQUESTED" && j.LeaseOwner == owner {
				j.Status = "CANCELLED"
				j.Stage = "已停止"
				j.FinishedAt = s.now()
				out, e = put(tx, doc, j, doc.Version)
				return e
			}
			return ErrConflict
		}
		j.FinishedAt = s.now()
		j.Model = modelName
		j.HarnessRunID = harnessRunID
		j.LeaseUntil = 0
		if failure != "" {
			j.Status = "FAILED"
			j.Stage = "生成失败"
			j.Error = failure
			out, e = put(tx, doc, j, doc.Version)
			return e
		}
		hdoc, h, e := read[Handover](tx, model.DutyHandoverKind, j.HandoverID)
		if e != nil {
			return e
		}
		_, rev, e := read[Revision](tx, model.DutyRevisionKind, j.RevisionID)
		if e != nil {
			return e
		}
		if h.CurrentRevisionID != j.RevisionID || (h.Status != "DRAFT" && h.Status != "RETURNED") {
			j.Status = "INTERRUPTED"
			j.Stage = "交接已修改，结果未覆盖"
			out, e = put(tx, doc, j, doc.Version)
			return e
		}
		if e = ValidateAIResult(rev, result); e != nil {
			j.Status = "FAILED"
			j.Stage = "结果校验失败"
			j.Error = e.Error()
			out, e = put(tx, doc, j, doc.Version)
			return e
		}
		rev.Number = h.RevisionNumber + 1
		rev.AI = result
		rev.AIJobID = jobID
		rev.AuthorID = j.RequesterID
		newRevision, e := create(tx, model.DutyRevisionKind, rev)
		if e != nil {
			return e
		}
		h.CurrentRevisionID = newRevision.ID
		h.RevisionNumber++
		if _, e = put(tx, hdoc, h, hdoc.Version); e != nil {
			return e
		}
		j.Status = "SUCCEEDED"
		j.Stage = "已完成"
		j.Result = result
		out, e = put(tx, doc, j, doc.Version)
		return e
	})
	return out, err
}
func (s *Service) ApplyAIResult(ctx context.Context, tenant, jobID, owner string, result *model.DutyAIResult, modelName, harnessRunID string) (model.DutyDocument, error) {
	return s.FinishJob(ctx, tenant, jobID, owner, result, modelName, harnessRunID, "")
}
