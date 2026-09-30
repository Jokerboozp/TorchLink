package alarmgovernance

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"strings"
	"time"
)

type attachmentUpload struct {
	AttachmentID string `json:"attachmentId"`
	StorageKey   string `json:"storageKey"`
	StartedAt    int64  `json:"startedAt"`
}

// Persist before remote I/O, so a process death or failed compensation leaves
// a recoverable, server-owned attempt. Pending rows are never public evidence.
func (s *Service) trackUpload(ctx context.Context, a Actor, parent model.GovernanceDocument, id, key string) error {
	return s.Store.GovernanceTransaction(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		if s.ResolveTx != nil {
			fresh, e := s.ResolveTx(tx, a)
			if e != nil || !fresh.menu() || !fresh.can("record") || !sourceMenus(fresh, parent.Kind) {
				return ErrForbidden
			}
			a = fresh
		}
		current, e := tx.Get(parent.Kind, parent.ID)
		if e != nil {
			return e
		}
		if e = s.scope(current, a); e != nil {
			return e
		}
		if !attachmentMutable(current) || current.Version != parent.Version {
			return model.ErrGovernanceConflict
		}
		_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceUploadAttemptKind, ID: "upload_" + model.GovernanceHash(key), Status: "UPLOAD_PENDING", DeviceIDs: current.DeviceIDs, CreatedBy: "SYSTEM"}, attachmentUpload{id, key, s.Now().UnixMilli()}, 0)
		return e
	})
}

func (s *Service) finishUpload(ctx context.Context, tenant, key string) error {
	return s.Store.GovernanceTransaction(ctx, tenant, func(tx ports.AlarmGovernanceTx) error {
		d, v, e := read[attachmentUpload](tx, model.GovernanceUploadAttemptKind, "upload_"+model.GovernanceHash(key))
		if e != nil {
			return e
		}
		attachment, e := tx.Get(model.GovernanceAttachmentKind, v.AttachmentID)
		if e != nil {
			return e
		}
		a, e := model.GovernanceBody[model.GovernanceAttachment](attachment)
		if e != nil {
			return e
		}
		if a.StorageKey != key {
			return model.ErrGovernanceConflict
		}
		if d.Status == "UPLOAD_REFERENCED" {
			return nil
		}
		d.Status = "UPLOAD_REFERENCED"
		_, e = put(tx, d, v, d.Version)
		return e
	})
}

// CleanupAttachmentUploads deletes only an expired known attempt with no
// metadata reference. Formal/withdrawn/damaged objects all remain referenced.
// Upload I/O is bounded to two minutes; ten-minute grace excludes live writes.
func (s *Service) CleanupAttachmentUploads(ctx context.Context, tenant string, archive ports.Archive) (cleaned int, err error) {
	deleter, ok := archive.(ports.ObjectDeleter)
	if !ok {
		return 0, nil
	}
	err = s.Store.GovernanceTransaction(ctx, tenant, func(tx ports.AlarmGovernanceTx) error {
		rows, _, e := tx.List(model.GovernanceFilter{Kind: model.GovernanceUploadAttemptKind, Status: "UPLOAD_PENDING", AllDevices: true, Limit: 100})
		if e != nil {
			return e
		}
		for _, d := range rows {
			v, e := model.GovernanceBody[attachmentUpload](d)
			if e != nil {
				return e
			}
			if v.StartedAt <= 0 || v.StartedAt > s.Now().Add(-10*time.Minute).UnixMilli() {
				continue
			}
			if d.ID != "upload_"+model.GovernanceHash(v.StorageKey) || !strings.HasPrefix(v.StorageKey, attachmentKey(tenant, v.AttachmentID)+"/") {
				return invalid("补偿任务存储身份无效")
			}
			attachment, e := tx.Get(model.GovernanceAttachmentKind, v.AttachmentID)
			if e == nil {
				a, e := model.GovernanceBody[model.GovernanceAttachment](attachment)
				if e != nil {
					return e
				}
				if a.StorageKey == v.StorageKey {
					d.Status = "UPLOAD_REFERENCED"
					if _, e = put(tx, d, v, d.Version); e != nil {
						return e
					}
					continue
				}
			} else if !errors.Is(e, model.ErrNotFound) {
				return e
			}
			cleanup, cancel := context.WithTimeout(ctx, 30*time.Second)
			e = deleter.DeleteObject(cleanup, AttachmentBucket, v.StorageKey)
			cancel()
			if e != nil {
				continue
			}
			d.Status = "UPLOAD_CLEANED"
			if _, e = put(tx, d, v, d.Version); e != nil {
				return e
			}
			cleaned++
		}
		return nil
	})
	return
}
