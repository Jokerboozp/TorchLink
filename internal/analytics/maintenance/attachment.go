package maintenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"path/filepath"
	"slices"
	"strings"
)

const AttachmentMaxBytes = 16 << 20
const AttachmentBucket = "iot-maintenance-attachments"

func (s *Service) UploadAttachment(ctx context.Context, a analytics.Actor, devices []string, key, name, contentType string, data []byte) (model.AnalysisConfigRevision, error) {
	current, err := s.authorize(ctx, a, "POST /api/v1/maintenance-attachments", devices)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if err = s.validateDevices(ctx, current, devices); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if s.Archive == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(c rune) rune {
		if c < 32 || c == 127 {
			return -1
		}
		return c
	}, name)
	if name == "" || name == "." || len(name) > 240 || len(data) < 1 || len(data) > AttachmentMaxBytes || key == "" || len(key) > 200 {
		return model.AnalysisConfigRevision{}, invalid("附件大小、名称、幂等键无效")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if len(contentType) > 200 || strings.ContainsAny(contentType, "\r\n") {
		return model.AnalysisConfigRevision{}, invalid("附件类型无效")
	}
	digest := sha256.Sum256(data)
	request := struct {
		Name, ContentType, SHA256, Key string
		DeviceIDs                      []string
		Size                           int
	}{name, contentType, hex.EncodeToString(digest[:]), key, devices, len(data)}
	id, _ := analytics.AnalysisHash([]any{a.TenantID, current.Username, key, devices})
	resource := "maintenance-attachment-" + id
	old, err := s.Latest(ctx, current, AttachmentKind, resource)
	if err == nil {
		var b model.MaintenanceAttachment
		decode(old.Body, &b)
		return s.commit(ctx, current, AttachmentKind, resource, devices, 0, key, request, &b)
	}
	if !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	objectKey := a.TenantID + "/" + resource + "/" + request.SHA256
	if _, err = s.Archive.PutObject(ctx, AttachmentBucket, objectKey, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		return old, err
	}
	body := model.MaintenanceAttachment{DeviceIDs: devices, Name: name, ContentType: contentType, SHA256: request.SHA256, Size: int64(len(data)), ObjectKey: objectKey, Author: current.Username, RecordedAt: s.now()}
	return s.commit(ctx, current, AttachmentKind, resource, devices, 0, key, request, &body)
}
func (s *Service) DownloadAttachment(ctx context.Context, a analytics.Actor, id string) (model.MaintenanceAttachment, io.ReadCloser, error) {
	v, err := s.Revision(ctx, a, AttachmentKind, id)
	if err != nil {
		return model.MaintenanceAttachment{}, nil, err
	}
	if _, err = s.authorize(ctx, a, "GET /api/v1/maintenance-attachments/:id", v.DeviceIDs); err != nil {
		return model.MaintenanceAttachment{}, nil, err
	}
	var b model.MaintenanceAttachment
	if err = json.Unmarshal(v.Body, &b); err != nil {
		return b, nil, err
	}
	if s.Archive == nil {
		return b, nil, analytics.ErrUnsupported
	}
	reader, err := analytics.OpenVerifiedAnalysisObject(ctx, s.Analysis.Store, s.Archive, a.TenantID, AttachmentBucket, b.ObjectKey, b.SHA256, b.Size, AttachmentMaxBytes)
	b.ObjectKey = ""
	b.Requests = nil
	return b, reader, err
}
func (s *Service) AttachmentEvidence(ctx context.Context, a analytics.Actor, ref model.ResponseEvidenceReference) (model.ResponseEvidenceReference, error) {
	v, err := s.Revision(ctx, a, AttachmentKind, ref.SourceID)
	if err != nil {
		return model.ResponseEvidenceReference{}, err
	}
	if !slices.Contains(v.DeviceIDs, ref.DeviceID) {
		return model.ResponseEvidenceReference{}, analytics.ErrForbidden
	}
	b, reader, err := s.DownloadAttachment(ctx, a, v.ID)
	if err != nil {
		return model.ResponseEvidenceReference{}, err
	}
	defer reader.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(reader, AttachmentMaxBytes+1))
	if err != nil || n != b.Size || hex.EncodeToString(hash.Sum(nil)) != b.SHA256 {
		return model.ResponseEvidenceReference{}, invalid("附件原件缺失、过期或摘要不匹配")
	}
	return model.ResponseEvidenceReference{ID: v.ID, Kind: "ATTACHMENT", SourceID: v.ID, DeviceID: ref.DeviceID, Description: b.Name, RecordedAt: b.RecordedAt, Classification: "HUMAN_RECORDED_ATTACHMENT"}, nil
}
