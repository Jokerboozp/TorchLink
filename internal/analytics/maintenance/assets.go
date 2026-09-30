package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type slotMember struct {
	ResourceID, PhysicalID string
	Start                  int64
	End                    *int64
	BoundaryStatus         string
}
type assetSlot struct{ Members []slotMember }

func slotID(device, component string) string {
	hash, _ := analytics.AnalysisHash([]string{device, component})
	return "physical-slot-" + hash
}
func (s *Service) slot(ctx context.Context, tenant, id string) (model.AnalysisConfigRevision, assetSlot, error) {
	var latest model.AnalysisConfigRevision
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, tenant, model.AnalysisFilter{Kind: AssetSlotKind, ResourceID: id, Limit: 100, Offset: offset})
		if err != nil {
			return latest, assetSlot{}, err
		}
		for _, v := range page {
			if v.Version > latest.Version {
				latest = v
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
		if offset >= 10000 || len(page) == 0 {
			return latest, assetSlot{}, invalid("实物关联索引版本读取超限")
		}
	}
	if latest.ID == "" {
		return latest, assetSlot{Members: []slotMember{}}, nil
	}
	var body assetSlot
	err := json.Unmarshal(latest.Body, &body)
	return latest, body, err
}
func (s *Service) SaveAsset(ctx context.Context, a analytics.Actor, q model.MaintenanceRevisionRequest) (model.AnalysisConfigRevision, error) {
	if v, found, err := s.replay(ctx, a, AssetKind, q.ResourceID, q.IdempotencyKey, q); found || err != nil {
		return v, err
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/assets", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if err := s.validateDevices(ctx, a, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.AssetInstance
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if len(q.DeviceIDs) != 1 || q.DeviceIDs[0] != b.DeviceID || strings.TrimSpace(b.PhysicalID) == "" || strings.TrimSpace(b.Name) == "" || b.EffectiveStart <= 0 || b.Importance < 0 || b.Importance > 9 || !slices.Contains([]string{"UNKNOWN", "CONFIRMED", "DISPUTED"}, b.BoundaryStatus) || len(b.Requests) > 0 {
		return model.AnalysisConfigRevision{}, invalid("须明确实物、设备、实例区间和边界依据，不使用平台注册日期")
	}
	for _, date := range []*int64{b.ManufacturedAt, b.InstalledAt, b.CommissionedAt, b.RetiredAt} {
		if date != nil && (*date <= 0 || *date > s.now()) {
			return model.AnalysisConfigRevision{}, invalid("实际物理日期必须来自已发生且有依据的记录")
		}
	}
	if b.ManufacturedAt != nil && b.InstalledAt != nil && *b.ManufacturedAt > *b.InstalledAt || b.InstalledAt != nil && b.CommissionedAt != nil && *b.InstalledAt > *b.CommissionedAt || b.CommissionedAt != nil && b.RetiredAt != nil && *b.CommissionedAt > *b.RetiredAt || b.EffectiveEnd != nil && *b.EffectiveEnd <= b.EffectiveStart {
		return model.AnalysisConfigRevision{}, invalid("物理日期或实例起止顺序无效")
	}
	if b.BoundaryStatus == "CONFIRMED" && (len(b.Evidence) == 0 || b.EffectiveStart > s.now() || b.EffectiveEnd != nil && *b.EffectiveEnd > s.now()) {
		return model.AnalysisConfigRevision{}, invalid("确认实物边界须有实际依据")
	}
	evidence, err := s.canonicalEvidence(ctx, a, b.Evidence, q.DeviceIDs)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	b.Evidence = evidence
	if b.ComponentID != "" {
		proved := false
		for _, ref := range evidence {
			if ref.Kind != "ALARM" && ref.Kind != "ALARM_REPORT" {
				continue
			}
			alarmID := ref.SourceID
			if ref.ResourceID != "" {
				alarmID = ref.ResourceID
			}
			alarm, err := s.Catalog.GetAlarm(ctx, a.TenantID, alarmID)
			if err == nil && alarm.TenantID == a.TenantID && alarm.DeviceID == b.DeviceID && alarm.ComponentID == b.ComponentID {
				proved = true
			}
		}
		if !proved {
			return model.AnalysisConfigRevision{}, invalid("部件实例必须有同设备生产记录中的稳定componentId，位置自由文本不能建实例")
		}
	}
	old, err := s.Latest(ctx, a, AssetKind, q.ResourceID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if err == nil {
		var prior model.AssetInstance
		if decode(old.Body, &prior) != nil {
			return old, model.ErrAnalysisConflict
		}
		if prior.DeviceID != b.DeviceID || prior.ComponentID != b.ComponentID || prior.PhysicalID != b.PhysicalID {
			return old, invalid("沿用平台ID替换实物须建立新资产资源，并结束旧实例")
		}
		b.Requests = prior.Requests
		for _, r := range b.Requests {
			if r.Key == q.IdempotencyKey {
				return s.prepare(ctx, a, AssetKind, q.ResourceID, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
			}
		}
		if old.Version != q.ExpectedVersion {
			return old, model.ErrAnalysisConflict
		}
	} else if q.ExpectedVersion != 0 {
		return old, model.ErrAnalysisConflict
	}
	index, slot, err := s.slot(ctx, a.TenantID, slotID(b.DeviceID, b.ComponentID))
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	newMember := slotMember{ResourceID: q.ResourceID, PhysicalID: b.PhysicalID, Start: b.EffectiveStart, End: b.EffectiveEnd, BoundaryStatus: b.BoundaryStatus}
	if len(slot.Members) >= 5000 {
		return model.AnalysisConfigRevision{}, invalid("实物槽历史实例达到保护上限")
	}
	members := []slotMember{}
	for _, v := range slot.Members {
		if v.ResourceID == q.ResourceID {
			continue
		}
		if v.PhysicalID == b.PhysicalID {
			return model.AnalysisConfigRevision{}, invalid("同一实物标识已关联该设备或部件")
		}
		end, otherEnd := int64(^uint64(0)>>1), int64(^uint64(0)>>1)
		if newMember.End != nil {
			end = *newMember.End
		}
		if v.End != nil {
			otherEnd = *v.End
		}
		if newMember.BoundaryStatus == "CONFIRMED" && v.BoundaryStatus == "CONFIRMED" && newMember.Start < otherEnd && v.Start < end {
			return model.AnalysisConfigRevision{}, invalid("同设备或部件的已确认实物区间重叠；替换须先结束旧实例")
		}
		members = append(members, v)
	}
	members = append(members, newMember)
	slot.Members = members
	slotBody, _ := json.Marshal(slot)
	revision, err := s.prepare(ctx, a, AssetKind, q.ResourceID, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
	if err != nil || revision.Version > 0 {
		return revision, err
	}
	batch, ok := s.Analysis.Store.(ports.AnalysisConfigBatchStore)
	if !ok {
		return revision, analytics.ErrUnsupported
	}
	slotRevision := model.AnalysisConfigRevision{ID: uuid.NewString(), TenantID: a.TenantID, Kind: AssetSlotKind, ResourceID: slotID(b.DeviceID, b.ComponentID), Scope: "SHARED", DeviceIDs: q.DeviceIDs, Creator: a.Username, Body: slotBody}
	saved, err := batch.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{revision, slotRevision}, []int64{q.ExpectedVersion, index.Version})
	if err != nil {
		if errors.Is(err, model.ErrAnalysisConflict) {
			latest, lookupErr := s.Latest(ctx, a, AssetKind, q.ResourceID)
			if lookupErr == nil {
				var prior model.AssetInstance
				json.Unmarshal(latest.Body, &prior)
				hash, _ := analytics.AnalysisHash(q)
				for _, receipt := range prior.Requests {
					if receipt.Key == q.IdempotencyKey && receipt.Hash == hash && receipt.Actor == a.Username {
						return s.Revision(ctx, a, AssetKind, receipt.ResultID)
					}
				}
			}
		}
		return revision, err
	}
	return saved[0], nil
}
