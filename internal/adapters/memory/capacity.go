package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"iot-platform/internal/model"
)

func capacityJobRunning(status string) bool {
	return slices.Contains([]string{"running", "pending", "processing", "queued"}, strings.ToLower(status))
}

func capacityRunOwned(q model.CapacityCleanupBatch, run string) bool {
	return run != "" && (q.AllRuns || run == q.RunID)
}

func (r *Repository) ListCapacityFixtureProducts(_ context.Context, t string) ([]model.CapacityFixtureProduct, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.CapacityFixtureProduct{}
	for _, p := range r.products {
		if p.TenantID != t || !model.IsCapacityFixture(p) {
			continue
		}
		v := model.CapacityFixtureProduct{ProductID: p.ID, Name: p.Name}
		for _, d := range r.devices {
			if d.TenantID == t && d.ProductID == p.ID {
				v.DeviceCount++
			}
		}
		for _, raw := range r.raw {
			if raw.TenantID == t && raw.ProductID == p.ID {
				v.RawMessages++
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProductID < out[j].ProductID })
	return out, nil
}

func (r *Repository) ListCapacityFixtureDevices(_ context.Context, t, product, after string, limit int) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.products[key(t, product)]
	if !ok {
		return []string{}, nil
	}
	if !model.IsCapacityFixture(p) {
		return nil, model.ErrResourceInUse
	}
	ids := []string{}
	for _, d := range r.devices {
		if d.TenantID == t && d.ProductID == product && d.ID > after && model.CapacityFixtureDevice(p, d) {
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)
	return ids[:min(len(ids), max(1, limit))], nil
}

// capacityCheckLocked rejects devices that are not tool-owned or are still in
// use, before any deletion takes place.
func (r *Repository) capacityCheckLocked(t string, q model.CapacityCleanupBatch) (model.Product, bool, error) {
	if q.Product == "" {
		if len(q.Devices) > 0 || q.RemoveProduct {
			return model.Product{}, false, model.ErrResourceInUse
		}
		return model.Product{}, false, nil
	}
	p, exists := r.products[key(t, q.Product)]
	if !exists {
		if len(q.Devices) > 0 {
			for _, id := range q.Devices {
				if _, ok := r.devices[key(t, id)]; ok {
					return p, false, model.ErrResourceInUse
				}
			}
		}
		return p, false, nil
	}
	if !model.IsCapacityFixture(p) {
		return p, false, model.ErrResourceInUse
	}
	remove := func(tenant, id string) bool { return tenant == t && slices.Contains(q.Devices, id) }
	for _, id := range q.Devices {
		if d, ok := r.devices[key(t, id)]; ok && !model.CapacityFixtureDevice(p, d) {
			return p, true, model.ErrResourceInUse
		}
	}
	for _, v := range r.devices {
		if remove(v.TenantID, v.GatewayID) {
			return p, true, model.ErrResourceInUse
		}
	}
	for _, v := range r.accessProfiles {
		if remove(v.TenantID, v.DeviceID) {
			return p, true, model.ErrResourceInUse
		}
	}
	for _, v := range r.videoMappings {
		if remove(v.TenantID, v.DeviceID) || slices.ContainsFunc(v.RelatedDeviceIDs, func(d string) bool { return remove(v.TenantID, d) }) {
			return p, true, model.ErrResourceInUse
		}
	}
	for _, relations := range r.videoRelations {
		for _, v := range relations {
			if v.RelationType == "device" && remove(v.TenantID, v.TargetID) {
				return p, true, model.ErrResourceInUse
			}
		}
	}
	for k, v := range r.standard {
		if remove(v.TenantID, v.DeviceID) && !r.standardProcessed[k] {
			return p, true, model.ErrResourceInUse
		}
	}
	for _, v := range r.raw {
		if remove(v.TenantID, v.DeviceID) && v.ParseAttemptedAt == 0 {
			return p, true, model.ErrResourceInUse
		}
	}
	for _, job := range r.analysisJobs {
		if job.TenantID == t && capacityJobRunning(job.Status) && remove(t, r.alarms[key(t, job.AlarmID)].DeviceID) {
			return p, true, model.ErrResourceInUse
		}
	}
	return p, true, nil
}

func (r *Repository) CleanupCapacityData(_ context.Context, t string, q model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n model.CapacityCleanupCounts
	product, exists, err := r.capacityCheckLocked(t, q)
	if err != nil {
		return n, err
	}
	for _, job := range r.inspectionJobs[t] {
		if capacityRunOwned(q, job.CapacityRunID) && capacityJobRunning(job.Status) {
			return n, model.ErrResourceInUse
		}
	}
	for _, job := range r.replays {
		if job.TenantID == t && capacityRunOwned(q, job.CapacityRunID) && capacityJobRunning(job.Status) {
			return n, model.ErrResourceInUse
		}
	}
	var prunedAccess []byte
	if len(q.Devices) > 0 {
		if body, ok := r.accessStates[t]; ok {
			prunedAccess, n.AccessReferences, err = model.PruneCapacityAccessReferences(body, q.Devices)
			if err != nil {
				return n, err
			}
			if n.AccessReferences > 0 {
				var state map[string]json.RawMessage
				if err = json.Unmarshal(prunedAccess, &state); err != nil {
					return n, err
				}
				var revision int64
				_ = json.Unmarshal(state["revision"], &revision)
				state["revision"], _ = json.Marshal(revision + 1)
				prunedAccess, _ = json.Marshal(state)
			}
		}
	}
	remove := func(tenant, id string) bool { return tenant == t && slices.Contains(q.Devices, id) }
	rawIDs, alarmIDs, removedJobs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for k, v := range r.standard {
		if remove(v.TenantID, v.DeviceID) {
			delete(r.standard, k)
			delete(r.standardProcessed, k)
			delete(r.claims, k)
			n.Standard++
		}
	}
	for k, v := range r.raw {
		if remove(v.TenantID, v.DeviceID) {
			rawIDs[v.MessageID] = true
			delete(r.raw, k)
			delete(r.rawMessages, k)
			delete(r.rawReservations, k)
			n.Raw++
		}
	}
	for k, v := range r.alarms {
		if remove(v.TenantID, v.DeviceID) {
			alarmIDs[v.ID] = true
			delete(r.alarms, k)
			n.Alarms++
		}
	}
	for k, v := range r.ai {
		if v.TenantID == t && (alarmIDs[v.AlarmID] || capacityRunOwned(q, v.CapacityRunID)) {
			delete(r.ai, k)
		}
	}
	for k, v := range r.analysisJobs {
		if v.TenantID != t {
			continue
		}
		if alarmIDs[v.AlarmID] {
			delete(r.analysisJobs, k)
		} else if capacityRunOwned(q, v.CapacityRunID) && !capacityJobRunning(v.Status) {
			delete(r.analysisJobs, k)
			n.Resources++
		}
	}
	for k, c := range r.componentAlarms {
		if strings.HasPrefix(k, t+"\x00") && alarmIDs[c.AlarmID] {
			delete(r.componentAlarms, k)
		}
	}
	for k, v := range r.states {
		if remove(v.TenantID, v.DeviceID) {
			delete(r.states, k)
		}
	}
	for k := range r.rulePending {
		if parts := strings.Split(k, "\x00"); len(parts) == 3 && remove(parts[0], parts[2]) {
			delete(r.rulePending, k)
		}
	}
	r.stateEvents = slices.DeleteFunc(r.stateEvents, func(v model.DeviceStateEvent) bool { return remove(v.State.TenantID, v.State.DeviceID) })
	for k, v := range r.commands {
		if remove(v.TenantID, v.DeviceID) {
			delete(r.commands, k)
		}
	}
	for k, v := range r.revocations {
		if remove(v.TenantID, v.DeviceID) {
			delete(r.revocations, k)
		}
	}
	for k, v := range r.devices {
		if exists && v.ProductID == q.Product && remove(v.TenantID, v.ID) {
			delete(r.devices, k)
			n.Devices++
		}
	}
	if r.inspectionJobs == nil {
		r.inspectionJobs = map[string][]model.HealthInspectionJob{}
	}
	inspectionAudits := map[string]bool{}
	r.inspectionJobs[t] = slices.DeleteFunc(r.inspectionJobs[t], func(v model.HealthInspectionJob) bool {
		if capacityRunOwned(q, v.CapacityRunID) && !capacityJobRunning(v.Status) {
			removedJobs[v.ID] = true
			inspectionAudits[fmt.Sprintf("inspection_%d", v.Report.GeneratedAt)] = true
			n.Resources++
			return true
		}
		return false
	})
	for k, v := range r.replays {
		if v.TenantID == t && capacityRunOwned(q, v.CapacityRunID) && !capacityJobRunning(v.Status) {
			removedJobs[v.ID] = true
			delete(r.replays, k)
			n.Resources++
		}
	}
	if n.AccessReferences > 0 {
		r.accessStates[t] = prunedAccess
	}
	productRemoved := false
	if q.RemoveProduct && exists {
		remaining := slices.ContainsFunc(mapValues(r.devices), func(d model.ManagedDevice) bool { return d.TenantID == t && d.ProductID == q.Product })
		referenced := slices.ContainsFunc(mapValues(r.rules), func(v model.AlarmRule) bool {
			return v.TenantID == t && v.ProductID == q.Product && v.AlarmType != "CAPACITY_TEST"
		}) || slices.ContainsFunc(mapValues(r.accessProfiles), func(v model.DeviceAccessProfile) bool { return v.TenantID == t && v.ProductID == q.Product })
		switch {
		case remaining:
			n.Warnings = append(n.Warnings, "测试产品仍有保留运行使用的设备，产品已保留")
		case referenced:
			n.Warnings = append(n.Warnings, "测试产品仍被规则或接入配置引用，产品已保留")
		default:
			for k, v := range r.rules {
				if v.TenantID == t && v.ProductID == q.Product && v.AlarmType == "CAPACITY_TEST" {
					delete(r.rules, k)
					n.Rules++
				}
			}
			delete(r.protocolBindings, key(t, q.Product))
			delete(r.products, key(t, product.ID))
			n.Products++
			productRemoved = true
		}
	}
	r.audits = slices.DeleteFunc(r.audits, func(v model.AuditLog) bool {
		if v.TenantID != t {
			return false
		}
		device, _ := v.Details["deviceId"].(string)
		match := (v.TargetType == "device" && slices.Contains(q.Devices, v.TargetID)) || (v.TargetType == "alarm" && alarmIDs[v.TargetID]) || (v.TargetType == "raw-message" && rawIDs[v.TargetID]) || slices.Contains(q.Devices, device) || (productRemoved && v.TargetType == "product" && v.TargetID == q.Product) || (slices.Contains([]string{"replay", "inspection", "health-inspection", "device-health"}, v.TargetType) && (removedJobs[v.TargetID] || inspectionAudits[v.TargetID]))
		if match {
			n.Audits++
		}
		return match
	})
	return n, nil
}

func mapValues[K comparable, V any](m map[K]V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
