package alarmgovernance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	minio "github.com/minio/minio-go/v7"
	"io"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const AttachmentBucket = "iot-alarm-governance-attachments"
const AttachmentMax = 16 << 20

func attachmentFile(name string, data []byte) (string, string, error) {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || len(name) > 240 || len(data) == 0 || len(data) > AttachmentMax {
		return "", "", invalid("附件名称或大小无效，单文件最大16MiB")
	}
	mime := http.DetectContentType(data)
	ext := strings.ToLower(filepath.Ext(name))
	allowed := map[string][]string{"image/jpeg": {".jpg", ".jpeg"}, "image/png": {".png"}, "image/webp": {".webp"}, "application/pdf": {".pdf"}}
	if !slices.Contains(allowed[mime], ext) {
		return "", "", invalid("只允许实际MIME与扩展名一致的JPEG/PNG/WebP/PDF")
	}
	return name, mime, nil
}
func attachmentKey(tenant, id string) string {
	return "governance/" + model.GovernanceHash(tenant) + "/" + id
}
func attachmentMutable(d model.GovernanceDocument) bool {
	return d.Kind == model.GovernanceCaseKind || d.Status == "DRAFT"
}
func (s *Service) UploadAttachment(ctx context.Context, a Actor, archive ports.Archive, kind, parent, name, key string, data []byte) (out model.GovernanceDocument, err error) {
	a, err = s.actor(ctx, a, "record")
	if err != nil {
		return
	}
	if !slices.Contains([]string{model.GovernanceCaseKind, model.GovernanceVerificationKind, model.GovernanceActivityKind}, kind) || len(key) == 0 || len(key) > 200 {
		return out, invalid("附件归属或幂等键无效")
	}
	name, mime, err := attachmentFile(name, data)
	if err != nil {
		return out, err
	}
	doc, err := s.Get(ctx, a, kind, parent)
	if err != nil {
		return out, err
	}
	if !attachmentMutable(doc) {
		return out, invalid("正式核实或活动的附件使用新更正记录追加")
	}
	if archive == nil {
		return out, invalid("附件存储不可用")
	}
	deleter, ok := archive.(ports.ObjectDeleter)
	if !ok {
		return out, invalid("附件存储未提供失败补偿能力")
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	id := "gatt_" + model.GovernanceHash([]string{a.Username, kind, parent, key})[:32]
	existing, e := s.Get(ctx, a, model.GovernanceAttachmentKind, id)
	if e == nil {
		v, e := model.GovernanceBody[model.GovernanceAttachment](existing)
		if e != nil {
			return out, e
		}
		if v.SHA256 != hash || v.Name != name || v.Availability != "AVAILABLE" {
			return out, model.ErrGovernanceConflict
		}
		return existing, nil
	}
	if !errors.Is(e, model.ErrNotFound) {
		return out, e
	}
	storage := attachmentKey(a.TenantID, id) + "/" + uuid.NewString()
	if err = s.trackUpload(ctx, a, doc, id, storage); err != nil {
		return out, err
	}
	uploadCtx, cancelUpload := context.WithTimeout(ctx, 2*time.Minute)
	_, err = archive.PutObject(uploadCtx, AttachmentBucket, storage, bytes.NewReader(data), int64(len(data)), mime)
	cancelUpload()
	if err != nil {
		_ = deleter.DeleteObject(context.WithoutCancel(ctx), AttachmentBucket, storage)
		return out, err
	}
	// Every grant and parent relationship is checked again after remote upload.
	fresh, e := s.actor(ctx, a, "record")
	if e != nil {
		err = e
	} else {
		postUpload, e := s.Get(ctx, fresh, kind, parent)
		if e != nil {
			_ = deleter.DeleteObject(context.WithoutCancel(ctx), AttachmentBucket, storage)
			return out, e
		}
		err = s.Store.GovernanceTransaction(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
			if s.ResolveTx != nil {
				current, e := s.ResolveTx(tx, fresh)
				if e != nil || !current.menu() || !current.can("record") || !sourceMenus(current, kind) || current.AccessVersion != fresh.AccessVersion {
					return ErrForbidden
				}
				fresh = current
			}
			latest, e := tx.Get(kind, parent)
			if e != nil {
				return e
			}
			if e = s.scope(latest, fresh); e != nil {
				return e
			}
			if !attachmentMutable(latest) || latest.Version != postUpload.Version || kind != model.GovernanceCaseKind && latest.Version != doc.Version {
				return model.ErrGovernanceConflict
			}
			v := model.GovernanceAttachment{ParentKind: kind, ParentID: parent, DeviceIDs: latest.DeviceIDs, Name: name, MIME: mime, Size: int64(len(data)), SHA256: hash, StorageKey: storage, UploadedBy: a.Username, UploadedAt: s.Now().UnixMilli(), Source: "USER_UPLOAD", Availability: "AVAILABLE", IdempotencyKey: key}
			d := model.GovernanceDocument{Kind: model.GovernanceAttachmentKind, ID: id, ParentID: parent, DeviceIDs: latest.DeviceIDs, CaseID: latest.CaseID, RoundID: latest.RoundID, Status: "AVAILABLE", CreatedBy: a.Username}
			if kind == model.GovernanceCaseKind {
				d.CaseID = parent
			}
			out, e = put(tx, d, v, 0)
			if e != nil {
				return e
			}
			return s.attachmentInputChanged(tx, d, v)
		})
	}
	if err != nil {
		if del, ok := archive.(ports.ObjectDeleter); ok {
			cleanup := context.WithoutCancel(ctx)
			_ = del.DeleteObject(cleanup, AttachmentBucket, storage)
		}
	}
	if errors.Is(err, model.ErrGovernanceConflict) {
		if existing, e := s.Get(ctx, a, model.GovernanceAttachmentKind, id); e == nil {
			v, e := model.GovernanceBody[model.GovernanceAttachment](existing)
			if e == nil && v.SHA256 == hash && v.Name == name && v.Availability == "AVAILABLE" {
				return existing, nil
			}
		}
	}
	if err == nil {
		_ = s.finishUpload(context.WithoutCancel(ctx), a.TenantID, storage)
	}
	return
}
func (s *Service) DownloadAttachment(ctx context.Context, a Actor, archive ports.Archive, id string) (v model.GovernanceAttachment, data []byte, err error) {
	d, err := s.Get(ctx, a, model.GovernanceAttachmentKind, id)
	if err != nil {
		return v, nil, err
	}
	v, err = model.GovernanceBody[model.GovernanceAttachment](d)
	if err != nil {
		return v, nil, err
	}
	suffix := strings.TrimPrefix(v.StorageKey, attachmentKey(a.TenantID, id)+"/")
	if _, e := uuid.Parse(suffix); e != nil {
		return v, nil, invalid("附件存储身份无效")
	}
	if !strings.HasPrefix(v.StorageKey, attachmentKey(a.TenantID, id)+"/") || v.Size < 1 || v.Size > AttachmentMax {
		return v, nil, invalid("附件存储身份无效")
	}
	if v.Availability != "AVAILABLE" || archive == nil {
		return v, nil, invalid("附件不可复查")
	}
	reader, err := archive.GetObject(ctx, AttachmentBucket, v.StorageKey)
	if err != nil {
		if attachmentMissing(err) {
			if e := s.attachmentAvailability(ctx, a, id, d.Version, "MISSING", "对象已不存在"); e != nil {
				return v, nil, e
			}
		}
		return v, nil, err
	}
	defer reader.Close()
	data, err = io.ReadAll(io.LimitReader(reader, AttachmentMax+1))
	if err != nil {
		if attachmentMissing(err) {
			if e := s.attachmentAvailability(ctx, a, id, d.Version, "MISSING", "对象已不存在"); e != nil {
				return v, nil, e
			}
		}
		return v, nil, err
	}
	h := sha256.Sum256(data)
	if int64(len(data)) != v.Size || hex.EncodeToString(h[:]) != v.SHA256 {
		if e := s.attachmentAvailability(ctx, a, id, d.Version, "DAMAGED", "实读大小或SHA256与登记值不符"); e != nil {
			return v, nil, e
		}
		return v, nil, fmt.Errorf("%w: 附件缺失或hash不符，无法复查", model.ErrGovernanceInvalid)
	}
	latest, e := s.Get(ctx, a, model.GovernanceAttachmentKind, id)
	if e != nil {
		return v, nil, e
	}
	current, e := model.GovernanceBody[model.GovernanceAttachment](latest)
	if e != nil {
		return v, nil, e
	}
	if latest.Version != d.Version || current.Availability != "AVAILABLE" || current.SHA256 != v.SHA256 || current.StorageKey != v.StorageKey {
		return v, nil, model.ErrGovernanceConflict
	}
	if err != nil {
		return v, nil, err
	}
	v.StorageKey = ""
	return v, data, nil
}
func (s *Service) WithdrawAttachment(ctx context.Context, a Actor, id string, expected int64, reason string) (out model.GovernanceDocument, err error) {
	a, err = s.actor(ctx, a, "record")
	if err != nil {
		return
	}
	if strings.TrimSpace(reason) == "" {
		return out, invalid("须填写附件撤回理由")
	}
	err = s.Store.GovernanceTransaction(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		if s.ResolveTx != nil {
			fresh, e := s.ResolveTx(tx, a)
			if e != nil || !fresh.menu() || !fresh.can("record") || !sourceMenus(fresh, model.GovernanceAttachmentKind) {
				return ErrForbidden
			}
			a = fresh
		}
		d, e := tx.Get(model.GovernanceAttachmentKind, id)
		if e != nil {
			return e
		}
		if e = s.scope(d, a); e != nil {
			return e
		}
		if d.Version != expected {
			return model.ErrGovernanceConflict
		}
		v, e := model.GovernanceBody[model.GovernanceAttachment](d)
		if e != nil {
			return e
		}
		v.Availability = "WITHDRAWN"
		v.AvailabilityReason = reason
		v.AvailabilityChangedAt = s.Now().UnixMilli()
		v.AvailabilityChangedBy = a.Username
		d.Status = "WITHDRAWN"
		out, e = put(tx, d, v, expected)
		if e != nil {
			return e
		}
		if e = s.attachmentInputChanged(tx, d, v); e != nil {
			return e
		}
		event := model.GovernanceEvent{CaseID: d.CaseID, RoundID: d.RoundID, ResourceID: d.ID, ResourceVersion: out.Version, Action: "attachment:withdraw", Actor: a.Username, OccurredAt: s.Now().UnixMilli(), RecordedAt: s.Now().UnixMilli(), Reason: reason}
		_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceEventKind, ID: "event_" + model.GovernanceHash([]any{id, out.Version}), CaseID: d.CaseID, RoundID: d.RoundID, DeviceIDs: d.DeviceIDs, CreatedBy: a.Username, OccurredAt: event.OccurredAt}, event, 0)
		return e
	})
	return
}
func attachmentMissing(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NoSuchObject" || code == "NotFound"
}
func (s *Service) attachmentAvailability(ctx context.Context, a Actor, id string, expected int64, status, reason string) error {
	fresh, e := s.actor(ctx, a, "")
	if e != nil {
		return e
	}
	return s.Store.GovernanceTransaction(ctx, fresh.TenantID, func(tx ports.AlarmGovernanceTx) error {
		if s.ResolveTx != nil {
			current, e := s.ResolveTx(tx, fresh)
			if e != nil || !current.menu() || !sourceMenus(current, model.GovernanceAttachmentKind) {
				return ErrForbidden
			}
			fresh = current
		}
		d, v, e := read[model.GovernanceAttachment](tx, model.GovernanceAttachmentKind, id)
		if e != nil {
			return e
		}
		if e = s.scope(d, fresh); e != nil {
			return e
		}
		if d.Version != expected || v.Availability != "AVAILABLE" {
			return model.ErrGovernanceConflict
		}
		v.Availability = status
		v.AvailabilityReason = reason
		v.AvailabilityChangedAt = s.Now().UnixMilli()
		v.AvailabilityChangedBy = fresh.Username
		d.Status = status
		out, e := put(tx, d, v, expected)
		if e != nil {
			return e
		}
		if e = s.attachmentInputChanged(tx, d, v); e != nil {
			return e
		}
		event := model.GovernanceEvent{CaseID: d.CaseID, RoundID: d.RoundID, ResourceID: id, ResourceVersion: out.Version, Action: "attachment:" + strings.ToLower(status), Actor: fresh.Username, OccurredAt: s.Now().UnixMilli(), RecordedAt: s.Now().UnixMilli(), Reason: reason}
		_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceEventKind, ID: "event_" + model.GovernanceHash([]any{id, out.Version}), CaseID: d.CaseID, RoundID: d.RoundID, DeviceIDs: d.DeviceIDs, CreatedBy: fresh.Username, OccurredAt: event.OccurredAt}, event, 0)
		return e
	})
}
func (s *Service) attachmentInputChanged(tx ports.AlarmGovernanceTx, d model.GovernanceDocument, v model.GovernanceAttachment) error {
	if d.CaseID != "" {
		if e := s.bumpCase(tx, d.CaseID); e != nil {
			return e
		}
	}
	if v.ParentKind == model.GovernanceVerificationKind {
		_, b, e := read[model.FieldVerification](tx, v.ParentKind, v.ParentID)
		if e != nil {
			return e
		}
		return tx.BumpSourceVersions(versionRange(b.GovernancePoint, "VERIFICATION", b.VerifiedAt, b.VerifiedAt))
	}
	if v.ParentKind == model.GovernanceActivityKind {
		_, b, e := read[model.FieldActivityRevision](tx, v.ParentKind, v.ParentID)
		if e != nil {
			return e
		}
		keys := []model.GovernanceSourceVersion{}
		for _, id := range b.DeviceIDs {
			keys = append(keys, versionRange(model.GovernancePoint{DeviceID: id}, "ACTIVITY", b.StartAt, b.EndAt)...)
		}
		return tx.BumpSourceVersions(keys)
	}
	return nil
}
