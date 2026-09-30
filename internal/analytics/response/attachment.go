package response

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

const AttachmentBucket = "iot-response-attachments"
const AttachmentMaxBytes = 16 << 20

// UploadAttachment binds the object to the full execution scope. Objects have
// no public URL; downloads re-resolve current permissions before opening them.
func (s *Service) UploadAttachment(ctx context.Context, a analytics.Actor, executionID, key, name, contentType string, data []byte) (model.AnalysisConfigRevision, error) {
	execution, err := s.Latest(ctx, a, ExecutionKind, executionID)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	current, err := s.authorize(ctx, a, "POST /api/v1/response-runs/:id/attachments", execution.DeviceIDs)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var exec model.ResponseExecution
	if err = json.Unmarshal(execution.Body, &exec); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if !slices.Contains([]string{"DRAFT", "PUBLISHED", "RUNNING", "RECORDING", "ENDED", "REVIEWED"}, exec.Status) {
		return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
	}
	if s.Archive == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if len(data) < 1 || len(data) > AttachmentMaxBytes || name == "" || name == "." || len(name) > 240 || key == "" || len(key) > 200 {
		return model.AnalysisConfigRevision{}, invalid("附件大小、名称或幂等键无效")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if len(contentType) > 200 || strings.ContainsAny(contentType, "\r\n") {
		return model.AnalysisConfigRevision{}, invalid("附件类型无效")
	}
	digest := sha256.Sum256(data)
	request := struct {
		ExecutionID, Key, Name, ContentType, SHA256 string
		Size                                        int
	}{executionID, key, name, contentType, hex.EncodeToString(digest[:]), len(data)}
	identity, _ := analytics.AnalysisHash([]string{a.TenantID, executionID, current.Username, key})
	resourceID := "response-attachment-" + identity
	old, err := s.Latest(ctx, current, AttachmentKind, resourceID)
	if err == nil {
		var previous model.ResponseAttachment
		if err = json.Unmarshal(old.Body, &previous); err != nil {
			return old, err
		}
		return s.commit(ctx, current, AttachmentKind, resourceID, execution.DeviceIDs, 0, key, request, &previous)
	}
	if !errors.Is(err, model.ErrNotFound) {
		return model.AnalysisConfigRevision{}, err
	}
	objectKey := a.TenantID + "/" + executionID + "/" + resourceID + "/" + request.SHA256
	if _, err = s.Archive.PutObject(ctx, AttachmentBucket, objectKey, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	attachment := model.ResponseAttachment{ExecutionID: executionID, Name: name, ContentType: contentType, Size: int64(len(data)), SHA256: request.SHA256, ObjectKey: objectKey, RecordedAt: s.now(), Author: current.Username}
	saved, err := s.commit(ctx, current, AttachmentKind, resourceID, execution.DeviceIDs, 0, key, request, &attachment)
	// A losing replica must not remove the winning replica's identical object.
	// Unreferenced objects from changed-payload races are safe for later GC.
	return saved, err
}

func (s *Service) DownloadAttachment(ctx context.Context, a analytics.Actor, revisionID string) (model.ResponseAttachment, io.ReadCloser, error) {
	v, err := s.Revision(ctx, a, AttachmentKind, revisionID)
	if err != nil {
		return model.ResponseAttachment{}, nil, err
	}
	if _, err = s.authorize(ctx, a, "GET /api/v1/response-attachments/:id", v.DeviceIDs); err != nil {
		return model.ResponseAttachment{}, nil, err
	}
	var attachment model.ResponseAttachment
	if err = json.Unmarshal(v.Body, &attachment); err != nil {
		return attachment, nil, err
	}
	if s.Archive == nil {
		return attachment, nil, analytics.ErrUnsupported
	}
	reader, err := analytics.OpenVerifiedAnalysisObject(ctx, s.Analysis.Store, s.Archive, a.TenantID, AttachmentBucket, attachment.ObjectKey, attachment.SHA256, attachment.Size, AttachmentMaxBytes)
	attachment.ObjectKey, attachment.Requests = "", nil
	return attachment, reader, err
}

// AttachmentEvidence returns server-owned metadata. The uploaded bytes prove
// the existence of an attachment, not a field action or its occurrence time.
func (s *Service) AttachmentEvidence(ctx context.Context, a analytics.Actor, ref model.ResponseEvidenceReference) (model.ResponseEvidenceReference, error) {
	v, err := s.Revision(ctx, a, AttachmentKind, ref.SourceID)
	if err != nil {
		return model.ResponseEvidenceReference{}, err
	}
	if !slices.Contains(v.DeviceIDs, ref.DeviceID) {
		return model.ResponseEvidenceReference{}, analytics.ErrForbidden
	}
	var attachment model.ResponseAttachment
	if err = json.Unmarshal(v.Body, &attachment); err != nil {
		return model.ResponseEvidenceReference{}, err
	}
	if s.Archive == nil {
		return model.ResponseEvidenceReference{}, analytics.ErrUnsupported
	}
	reader, err := analytics.OpenVerifiedAnalysisObject(ctx, s.Analysis.Store, s.Archive, a.TenantID, AttachmentBucket, attachment.ObjectKey, attachment.SHA256, attachment.Size, AttachmentMaxBytes)
	if err != nil {
		return model.ResponseEvidenceReference{}, err
	}
	defer reader.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(reader, AttachmentMaxBytes+1))
	if err != nil || n != attachment.Size || hex.EncodeToString(hash.Sum(nil)) != attachment.SHA256 {
		return model.ResponseEvidenceReference{}, invalid("附件原件缺失、过期或摘要不匹配")
	}
	return model.ResponseEvidenceReference{ID: v.ID, Kind: "ATTACHMENT", SourceID: v.ID, DeviceID: ref.DeviceID, ResourceID: attachment.ExecutionID, Description: attachment.Name, RecordedAt: attachment.RecordedAt, Classification: "UNCLASSIFIED"}, nil
}
