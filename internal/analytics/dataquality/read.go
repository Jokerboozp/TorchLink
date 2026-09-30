package dataquality

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/quality"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type SeriesResponse struct {
	Items          []model.MeasurementFact        `json:"items"`
	Complete       bool                           `json:"complete"`
	Coverage       []model.AnalysisSourceCoverage `json:"coverage"`
	Limitations    []string                       `json:"limitations"`
	OriginalCount  int                            `json:"originalCount"`
	AvailableCount int                            `json:"availableCount"`
	ReturnedCount  int                            `json:"returnedCount"`
	Downsampled    bool                           `json:"downsampled"`
}

func (s *Service) Series(ctx context.Context, a analytics.Actor, runID, device, attribute string, limit int) (SeriesResponse, error) {
	run, err := s.Analysis.Get(ctx, a, analytics.KindDataQuality, runID)
	if err != nil {
		return SeriesResponse{}, err
	}
	if _, err = s.authorize(ctx, a, "", run.DeviceIDs); err != nil {
		return SeriesResponse{}, err
	}
	if !run.InputsFrozen {
		return SeriesResponse{}, model.ErrAnalysisConflict
	}
	if !slices.Contains(run.DeviceIDs, device) || attribute == "" {
		return SeriesResponse{}, analytics.ErrForbidden
	}
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		return SeriesResponse{}, invalid("曲线最多返回5000个点")
	}
	manifest, _, err := s.loadManifest(ctx, run)
	if err != nil {
		return SeriesResponse{}, err
	}
	members := []manifestMember{}
	for _, m := range manifest.Members {
		if m.DeviceID == device && m.Property == attribute {
			members = append(members, m)
		}
	}
	if !slices.Contains(manifest.Parameters.AttributeIDs, attribute) {
		return SeriesResponse{}, analytics.ErrForbidden
	}
	samples, issues, err := s.readMembers(ctx, run, members)
	if err != nil {
		return SeriesResponse{}, err
	}
	slices.SortFunc(samples, func(a, b quality.Sample) int {
		if a.EventAt.Before(b.EventAt) {
			return -1
		}
		if a.EventAt.After(b.EventAt) {
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	meta := map[string]model.MeasurementFact{}
	for _, m := range members {
		meta[m.Metadata.ID] = m.Metadata
	}
	response := SeriesResponse{Items: []model.MeasurementFact{}, Complete: len(issues) == 0, OriginalCount: len(members), AvailableCount: len(samples), Coverage: manifest.Sources, Limitations: append(slices.Clone(manifest.Limitations), issues...)}
	for _, source := range manifest.Sources {
		if !source.Complete {
			response.Complete = false
		}
	}
	// Evenly spaced representative points preserve first/last values. Source
	// sample counts remain explicit and never use plotted points as a denominator.
	chosen := samples
	if len(samples) > limit {
		response.Downsampled = true
		response.Limitations = append(response.Limitations, "曲线为按序均匀抽样，完整分母见冻结指标")
		chosen = make([]quality.Sample, limit)
		if limit == 1 {
			chosen[0] = samples[0]
		} else {
			for i := range chosen {
				chosen[i] = samples[i*(len(samples)-1)/(limit-1)]
			}
		}
	}
	for _, v := range chosen {
		fact := meta[v.ID]
		fact.Value = v.Value
		response.Items = append(response.Items, fact)
	}
	response.ReturnedCount = len(response.Items)
	return response, nil
}
func (s *Service) Export(ctx context.Context, a analytics.Actor, runID string) (json.RawMessage, error) {
	run, err := s.Analysis.Get(ctx, a, analytics.KindDataQuality, runID)
	if err != nil {
		return nil, err
	}
	if _, err = s.authorize(ctx, a, "GET /api/v1/data-quality/runs/:id/export", run.DeviceIDs); err != nil {
		return nil, err
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindDataQuality, runID)
	if err != nil {
		return nil, err
	}
	outputs := []model.AnalysisOutput{}
	evidence := []model.AnalysisEvidence{}
	reviews := []model.AnalysisReview{}
	for _, kind := range []string{"metrics", "findings"} {
		for offset := 0; ; {
			page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, a.TenantID, model.AnalysisFilter{RunID: run.ID, Kind: kind, Limit: 100, Offset: offset})
			if err != nil {
				return nil, err
			}
			outputs = append(outputs, page...)
			offset += len(page)
			if offset >= total {
				break
			}
		}
	}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisEvidence(ctx, a.TenantID, model.AnalysisFilter{RunID: run.ID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, page...)
		offset += len(page)
		if offset >= total {
			break
		}
	}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisReviews(ctx, a.TenantID, model.AnalysisFilter{RunID: run.ID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, page...)
		offset += len(page)
		if offset >= total {
			break
		}
	}
	// Snapshot and outputs are immutable. Reviews explicitly carry their own
	// versions/times; exporting later never rewrites the original analysis.
	run.LeaseOwner = ""
	run.LeaseToken = 0
	return json.Marshal(map[string]any{"format": "torchlink-data-quality-v1", "run": run, "snapshot": snapshot, "outputs": outputs, "evidence": evidence, "reviews": reviews, "limitations": []string{"数据质量线索不代表消防风险或传感器故障；现场核实另行记录", "源记录可能过期，历史指标使用冻结版本"}})
}
func (s *Service) UploadAttachment(ctx context.Context, a analytics.Actor, devices []string, scope, name, contentType string, size int64, reader io.Reader) (model.QualityAttachment, error) {
	scope = strings.ToUpper(scope)
	if scope == "" {
		scope = "PERSONAL"
	}
	devices = slices.Clone(devices)
	slices.Sort(devices)
	devices = slices.Compact(devices)
	if len(devices) == 0 || len(devices) > s.Analysis.Limits.MaxDevices || (scope != "PERSONAL" && scope != "SHARED") {
		return model.QualityAttachment{}, invalid("附件范围或设备集合无效")
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/data-quality/calibrations/attachments", devices); err != nil {
		return model.QualityAttachment{}, err
	}
	if scope == "SHARED" {
		if _, err := s.authorize(ctx, a, "POST /api/v1/data-quality/profiles/publish", devices); err != nil {
			return model.QualityAttachment{}, err
		}
	}
	if s.Catalog == nil {
		return model.QualityAttachment{}, analytics.ErrUnsupported
	}
	for _, id := range devices {
		if _, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, id); err != nil {
			return model.QualityAttachment{}, analytics.ErrForbidden
		}
	}
	if size <= 0 || size > AttachmentMax {
		return model.QualityAttachment{}, invalid("附件大小须在1字节至16MiB之间")
	}
	if s.Archive == nil {
		return model.QualityAttachment{}, analytics.ErrUnsupported
	}
	data, err := io.ReadAll(io.LimitReader(reader, AttachmentMax+1))
	if err != nil {
		return model.QualityAttachment{}, err
	}
	if int64(len(data)) != size {
		return model.QualityAttachment{}, invalid("附件实际大小与请求不一致")
	}
	digest := sha256.Sum256(data)
	id := uuid.NewString()
	key := a.TenantID + "/" + id
	if _, err := s.Archive.PutObject(ctx, AttachmentBucket, key, bytes.NewReader(data), size, contentType); err != nil {
		return model.QualityAttachment{}, err
	}
	record := model.QualityAttachmentRecord{QualityAttachment: model.QualityAttachment{ID: id, Name: name, Size: size, ContentType: contentType, SHA256: hex.EncodeToString(digest[:])}, StorageKey: key}
	body, _ := json.Marshal(record)
	_, err = s.Analysis.Store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: id, TenantID: a.TenantID, Kind: model.DataQualityAttachmentKind, ResourceID: id, Scope: scope, DeviceIDs: devices, Creator: a.Username, Body: body}, 0)
	if err != nil {
		if deleter, ok := s.Archive.(ports.ObjectDeleter); ok {
			_ = deleter.DeleteObject(ctx, AttachmentBucket, key)
		}
		return model.QualityAttachment{}, err
	}
	return record.QualityAttachment, nil
}
func (s *Service) DownloadAttachment(ctx context.Context, a analytics.Actor, id string) (model.QualityAttachment, io.ReadCloser, error) {
	revision, err := s.GetConfig(ctx, a, model.DataQualityAttachmentKind, id)
	if err != nil {
		return model.QualityAttachment{}, nil, err
	}
	if _, err = s.authorize(ctx, a, "GET /api/v1/data-quality/calibrations/attachments/:id", revision.DeviceIDs); err != nil {
		return model.QualityAttachment{}, nil, err
	}
	var record model.QualityAttachmentRecord
	if err = json.Unmarshal(revision.Body, &record); err != nil {
		return model.QualityAttachment{}, nil, err
	}
	if s.Archive == nil {
		return record.QualityAttachment, nil, analytics.ErrUnsupported
	}
	if record.StorageKey != a.TenantID+"/"+revision.ID {
		return record.QualityAttachment, nil, fmt.Errorf("invalid attachment storage identity")
	}
	reader, err := analytics.OpenVerifiedAnalysisObject(ctx, s.Analysis.Store, s.Archive, a.TenantID, AttachmentBucket, record.StorageKey, record.SHA256, record.Size, AttachmentMax)
	return record.QualityAttachment, reader, err
}
