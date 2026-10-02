package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
)

func (r *Repository) capacityFixtureLocked(p model.Product) model.CapacityFixtureProduct {
	v := model.CapacityFixtureProduct{ProductID: p.ID, Name: p.Name, Source: model.CapacityFixtureSource(p)}
	body, _ := json.Marshal(p)
	h := sha256.New()
	_, _ = h.Write(body)
	var devices []model.ManagedDevice
	for _, d := range r.devices {
		if d.TenantID == p.TenantID && d.ProductID == p.ID {
			devices = append(devices, d)
			if !model.CapacityFixtureDevice(p, d) {
				v.BlockedReason = "产品包含已改作其他用途的设备"
			}
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	for _, d := range devices {
		fmt.Fprintf(h, "\x00%x:%d", d.ID, d.UpdatedAt)
	}
	v.DeviceCount = int64(len(devices))
	v.Fingerprint = hex.EncodeToString(h.Sum(nil))
	owned := func(t, id string) bool {
		return t == p.TenantID && slices.ContainsFunc(devices, func(d model.ManagedDevice) bool { return d.ID == id })
	}
	for _, raw := range r.raw {
		if raw.TenantID == p.TenantID && raw.ProductID == p.ID {
			v.RawMessages++
			if raw.ParseAttemptedAt == 0 {
				v.BlockedReason = "报文仍在处理"
			}
		}
	}
	for k, message := range r.standard {
		if message.TenantID == p.TenantID && message.ProductID == p.ID && !r.standardProcessed[k] {
			v.BlockedReason = "报文仍在处理"
		}
	}
	for _, d := range r.devices {
		if owned(d.TenantID, d.GatewayID) {
			v.BlockedReason = "设备仍被网关关联引用"
		}
	}
	for _, profile := range r.accessProfiles {
		if profile.TenantID != p.TenantID {
			continue
		}
		if owned(profile.TenantID, profile.DeviceID) || (profile.ProductID == p.ID && (!strings.HasPrefix(v.Source, "legacy-cap") || profile.Enabled || profile.DeviceID != "")) {
			v.BlockedReason = "接入配置仍引用测试产品或设备"
		}
		for _, child := range profile.ChildProducts {
			if child.ProductID == p.ID {
				v.BlockedReason = "接入配置仍引用测试产品"
			}
		}
		for _, d := range r.devices {
			if d.TenantID == p.TenantID && d.ProductID != p.ID && d.ConnectorProfileID == profile.ID && profile.ProductID == p.ID {
				v.BlockedReason = "接入配置被其他设备共享"
			}
		}
	}
	for _, c := range r.videoMappings {
		if owned(c.TenantID, c.DeviceID) {
			v.BlockedReason = "摄像头仍引用测试设备"
		}
		for _, id := range c.RelatedDeviceIDs {
			if owned(c.TenantID, id) {
				v.BlockedReason = "摄像头仍引用测试设备"
			}
		}
	}
	for _, relations := range r.videoRelations {
		for _, c := range relations {
			if c.RelationType == "device" && owned(c.TenantID, c.TargetID) {
				v.BlockedReason = "摄像头仍引用测试设备"
			}
		}
	}
	for _, doc := range r.knowledge {
		if doc.TenantID == p.TenantID && doc.ProductID == p.ID {
			v.BlockedReason = "知识文档仍引用测试产品"
		}
	}
	for _, binding := range r.workflowKnowledge {
		if binding.TenantID == p.TenantID && slices.Contains(binding.ProductIDs, p.ID) {
			v.BlockedReason = "知识配置仍引用测试产品"
		}
	}
	for _, rule := range r.rules {
		if rule.TenantID == p.TenantID && rule.ProductID == p.ID && rule.AlarmType != "CAPACITY_TEST" {
			v.BlockedReason = "业务规则仍引用测试产品"
		}
	}
	for _, job := range r.inspectionJobs[p.TenantID] {
		if capacityJobRunning(job.Status) {
			v.BlockedReason = "巡检仍在运行"
		}
	}
	for _, job := range r.replays {
		if job.TenantID == p.TenantID && capacityJobRunning(job.Status) && (job.ProductID == p.ID || owned(job.TenantID, job.DeviceID) || (job.ProductID == "" && job.DeviceID == "")) {
			v.BlockedReason = "回放仍在运行"
		}
	}
	for _, job := range r.analysisJobs {
		if job.TenantID == p.TenantID && capacityJobRunning(job.Status) && owned(job.TenantID, r.alarms[key(p.TenantID, job.AlarmID)].DeviceID) {
			v.BlockedReason = "研判仍在运行"
		}
	}
	if v.Source == "legacy-cap-gb26875" {
		id, _, ok := strings.Cut(p.ProtocolPackageID, "@")
		pkg, exists := r.protocols[key(p.TenantID, p.ProtocolPackageID)]
		private := ok && exists && pkg.Protocol == id && (pkg.ParserType == "gb26875" || pkg.ParserType == "go_protocol_parser")
		for _, other := range r.protocols {
			if other.TenantID == p.TenantID && other.ID != p.ProtocolPackageID && (other.Protocol == id || strings.HasPrefix(other.ID, id+"@")) {
				private = false
			}
		}
		for _, other := range r.products {
			if other.TenantID == p.TenantID && other.ID != p.ID && strings.HasPrefix(other.ProtocolPackageID, id+"@") {
				private = false
			}
		}
		for _, b := range r.protocolBindings {
			if b.TenantID == p.TenantID && b.ProductID != p.ID && (b.ProtocolID == id || b.PreviousProtocolID == id) {
				private = false
			}
		}
		for _, profile := range r.accessProfiles {
			if profile.TenantID == p.TenantID && profile.ProductID != p.ID && profile.ProtocolID == id {
				private = false
			}
		}
		if private {
			v.ProtocolID = id
		} else {
			v.BlockedReason = "历史协议包不存在或被其他配置共享"
		}
	}
	return v
}

func (r *Repository) ListCapacityFixtureProducts(_ context.Context, tenant, after string, limit int) ([]model.CapacityFixtureProduct, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.CapacityFixtureProduct{}
	for _, p := range r.products {
		if p.TenantID == tenant && p.ID > after && model.CapacityFixtureSource(p) != "" {
			out = append(out, r.capacityFixtureLocked(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProductID < out[j].ProductID })
	return out[:min(len(out), max(1, min(limit, 100)))], nil
}

func (r *Repository) ListCapacityFixtureDevices(_ context.Context, tenant, product, after string, limit int) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.products[key(tenant, product)]
	if !ok {
		return nil, model.ErrNotFound
	}
	if model.CapacityFixtureSource(p) == "" {
		return nil, model.ErrResourceInUse
	}
	out := []string{}
	for _, d := range r.devices {
		if d.ID > after && model.CapacityFixtureDevice(p, d) {
			out = append(out, d.ID)
		}
	}
	sort.Strings(out)
	return out[:min(len(out), max(1, min(limit, 1000)))], nil
}

func (r *Repository) PrepareCapacityFixture(_ context.Context, tenant, product, fingerprint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.products[key(tenant, product)]
	if !ok {
		return model.ErrNotFound
	}
	if model.CapacityFixtureSource(p) == "" {
		return model.ErrResourceInUse
	}
	v := r.capacityFixtureLocked(p)
	if v.BlockedReason != "" || fingerprint == "" || v.Fingerprint != fingerprint {
		return model.ErrResourceInUse
	}
	p.Status = "DISABLED"
	p.UpdatedAt = time.Now().UnixMilli()
	r.products[key(tenant, product)] = p
	return nil
}
