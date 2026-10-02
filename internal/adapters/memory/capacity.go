package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/model"
	"slices"
	"strings"
)

func capacityMatch(t, p, d, raw string, tenant string, q model.CapacityCleanupBatch) bool {
	return t == tenant && p == q.Product && slices.Contains(q.Devices, d) && (slices.Contains(q.RawIDs, raw) || slices.Contains(q.RemoveDevices, d))
}
func (r *Repository) CapacityMessageIDs(_ context.Context, t string, q model.CapacityCleanupBatch) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capacityMessageIDsLocked(t, q)
}
func (r *Repository) capacityMessageIDsLocked(t string, q model.CapacityCleanupBatch) ([]string, error) {
	for _, id := range q.RemoveDevices {
		if !slices.Contains(q.Devices, id) {
			return nil, model.ErrResourceInUse
		}
		if d, ok := r.devices[key(t, id)]; ok && d.ProductID != q.Product {
			return nil, model.ErrResourceInUse
		}
	}
	if q.Historical {
		p, ok := r.products[key(t, q.Product)]
		if !ok || !strings.EqualFold(p.Status, "DISABLED") || model.CapacityFixtureSource(p) == "" || r.capacityFixtureLocked(p).BlockedReason != "" {
			return nil, model.ErrResourceInUse
		}
	}
	if len(q.RemoveDevices) > 0 {
		for _, job := range r.inspectionJobs[t] {
			if capacityJobRunning(job.Status) {
				return nil, model.ErrResourceInUse
			}
		}
		for _, job := range r.replays {
			if job.TenantID == t && capacityJobRunning(job.Status) && (slices.Contains(q.RemoveDevices, job.DeviceID) || job.ProductID == q.Product || (job.ProductID == "" && job.DeviceID == "")) {
				return nil, model.ErrResourceInUse
			}
		}
	}
	var ids []string
	messages := map[string]bool{}
	for k, v := range r.standard {
		if capacityMatch(v.TenantID, v.ProductID, v.DeviceID, v.RawMessageID, t, q) {
			if !r.standardProcessed[k] {
				return nil, model.ErrResourceInUse
			}
			messages[v.MessageID] = true
			if slices.Contains(q.RawIDs, v.RawMessageID) {
				ids = append(ids, v.MessageID)
			}
		}
	}
	for _, v := range r.raw {
		if capacityMatch(v.TenantID, v.ProductID, v.DeviceID, v.MessageID, t, q) && v.ParseAttemptedAt == 0 {
			return nil, model.ErrResourceInUse
		}
	}
	for _, job := range r.analysisJobs {
		if job.TenantID != t || !capacityJobRunning(job.Status) {
			continue
		}
		alarm := r.alarms[key(t, job.AlarmID)]
		if alarm.TenantID == t && slices.Contains(q.Devices, alarm.DeviceID) && (slices.Contains(q.RemoveDevices, alarm.DeviceID) || messages[alarm.TriggerID]) {
			return nil, model.ErrResourceInUse
		}
	}
	for _, resource := range q.Resources {
		switch resource.Kind {
		case "inspection":
			for _, v := range r.inspectionJobs[t] {
				if v.ID == resource.ID && v.CapacityRunID == q.RunID && capacityJobRunning(v.Status) {
					return nil, model.ErrResourceInUse
				}
			}
		case "alarm-analysis":
			for _, v := range r.analysisJobs {
				if v.TenantID == t && v.ID == resource.ID && v.CapacityRunID == q.RunID && capacityJobRunning(v.Status) {
					return nil, model.ErrResourceInUse
				}
			}
		case "replay":
			v := r.replays[resource.ID]
			if v.TenantID == t && v.CapacityRunID == q.RunID && capacityJobRunning(v.Status) {
				return nil, model.ErrResourceInUse
			}
		}
	}
	return ids, nil
}
func (r *Repository) CleanupCapacityData(_ context.Context, t string, q model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n model.CapacityCleanupCounts
	if _, err := r.capacityMessageIDsLocked(t, q); err != nil {
		return n, err
	}
	if r.inspectionJobs == nil {
		r.inspectionJobs = map[string][]model.HealthInspectionJob{}
	}
	var product model.Product
	var fixture model.CapacityFixtureProduct
	if q.RemoveProduct {
		var ok bool
		product, ok = r.products[key(t, q.Product)]
		if ok {
			fixture = r.capacityFixtureLocked(product)
			if fixture.Source == "" || fixture.BlockedReason != "" {
				return n, model.ErrResourceInUse
			}
			for _, d := range r.devices {
				if d.TenantID == t && d.ProductID == q.Product && !slices.Contains(q.RemoveDevices, d.ID) {
					return n, model.ErrResourceInUse
				}
			}
		}
	}
	var prunedAccess []byte
	if len(q.RemoveDevices) > 0 {
		if body, ok := r.accessStates[t]; ok {
			var err error
			prunedAccess, n.AccessReferences, err = model.PruneCapacityAccessReferences(body, q.RemoveDevices)
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
	remove := func(tenant, d string) bool { return tenant == t && slices.Contains(q.RemoveDevices, d) }
	for _, v := range r.devices {
		if remove(v.TenantID, v.GatewayID) {
			return n, model.ErrResourceInUse
		}
	}
	for _, v := range r.accessProfiles {
		if remove(v.TenantID, v.DeviceID) {
			return n, model.ErrResourceInUse
		}
	}
	for _, v := range r.videoMappings {
		if remove(v.TenantID, v.DeviceID) {
			return n, model.ErrResourceInUse
		}
		for _, d := range v.RelatedDeviceIDs {
			if remove(v.TenantID, d) {
				return n, model.ErrResourceInUse
			}
		}
	}
	for _, relations := range r.videoRelations {
		for _, v := range relations {
			if v.RelationType == "device" && remove(v.TenantID, v.TargetID) {
				return n, model.ErrResourceInUse
			}
		}
	}
	ids := map[string]bool{}
	rawIDs := map[string]bool{}
	alarmIDs := map[string]bool{}
	removedJobs := map[string]string{}
	inspectionAudits := map[string]bool{}
	for k, v := range r.standard {
		if capacityMatch(v.TenantID, v.ProductID, v.DeviceID, v.RawMessageID, t, q) {
			ids[v.MessageID] = true
			delete(r.standard, k)
			delete(r.standardProcessed, k)
			delete(r.claims, k)
			n.Standard++
		}
	}
	for k, v := range r.raw {
		if capacityMatch(v.TenantID, v.ProductID, v.DeviceID, v.MessageID, t, q) {
			rawIDs[v.MessageID] = true
			delete(r.raw, k)
			delete(r.rawMessages, k)
			delete(r.rawReservations, k)
			n.Raw++
		}
	}
	for k, v := range r.alarms {
		if v.TenantID == t && (remove(v.TenantID, v.DeviceID) || ids[v.TriggerID]) {
			alarmIDs[v.ID] = true
			delete(r.alarms, k)
			for j, analysis := range r.ai {
				if analysis.TenantID == t && analysis.AlarmID == v.ID {
					delete(r.ai, j)
				}
			}
			for j, job := range r.analysisJobs {
				if job.TenantID == t && job.AlarmID == v.ID {
					delete(r.analysisJobs, j)
				}
			}
			for j, c := range r.componentAlarms {
				if strings.HasPrefix(j, t+"\x00") && c.AlarmID == v.ID {
					delete(r.componentAlarms, j)
				}
			}
			n.Alarms++
		}
	}
	for k, v := range r.states {
		if v.TenantID == t && (remove(v.TenantID, v.DeviceID) || ids[v.LastMessageID]) {
			delete(r.states, k)
		}
	}
	for k := range r.rulePending {
		parts := strings.Split(k, "\x00")
		if len(parts) == 3 && remove(parts[0], parts[2]) {
			delete(r.rulePending, k)
		}
	}
	r.stateEvents = slices.DeleteFunc(r.stateEvents, func(v model.DeviceStateEvent) bool {
		return v.State.TenantID == t && (remove(v.State.TenantID, v.State.DeviceID) || ids[v.State.LastMessageID])
	})
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
		if v.ProductID == q.Product && remove(v.TenantID, v.ID) {
			delete(r.devices, k)
			n.Devices++
		}
	}
	for _, res := range q.Resources {
		switch res.Kind {
		case "inspection":
			r.inspectionJobs[t] = slices.DeleteFunc(r.inspectionJobs[t], func(v model.HealthInspectionJob) bool {
				if v.ID == res.ID && v.CapacityRunID == q.RunID && !capacityJobRunning(v.Status) && !slices.ContainsFunc(v.Report.Items, func(item model.DeviceHealthItem) bool { return item.ProductID != q.Product }) {
					removedJobs[v.ID] = "inspection"
					inspectionAudits[fmt.Sprintf("inspection_%d", v.Report.GeneratedAt)] = true
					n.Resources++
					return true
				}
				return false
			})
		case "alarm-analysis":
			for k, v := range r.analysisJobs {
				if v.TenantID == t && v.ID == res.ID && v.CapacityRunID == q.RunID && !capacityJobRunning(v.Status) {
					for k, analysis := range r.ai {
						if analysis.TenantID == t && analysis.AlarmID == v.AlarmID && analysis.KnowledgeScope == v.KnowledgeScope && analysis.CapacityRunID == q.RunID {
							delete(r.ai, k)
						}
					}
					delete(r.analysisJobs, k)
					n.Resources++
				}
			}
		case "replay":
			v, ok := r.replays[res.ID]
			if ok && v.TenantID == t && v.CapacityRunID == q.RunID && !capacityJobRunning(v.Status) {
				removedJobs[v.ID] = "replay"
				delete(r.replays, res.ID)
				n.Resources++
			}
		}
	}
	if len(q.RemoveDevices) > 0 || q.RemoveProduct {
		for _, job := range r.inspectionJobs[t] {
			if slices.ContainsFunc(job.Report.Items, func(item model.DeviceHealthItem) bool { return slices.Contains(q.RemoveDevices, item.DeviceID) }) && slices.ContainsFunc(job.Report.Items, func(item model.DeviceHealthItem) bool { return item.ProductID != q.Product }) {
				n.Warnings = append(n.Warnings, "含非测试设备的混合巡检报告已完整保留")
				break
			}
		}
		r.inspectionJobs[t] = slices.DeleteFunc(r.inspectionJobs[t], func(v model.HealthInspectionJob) bool {
			if capacityJobRunning(v.Status) || len(v.Report.Items) == 0 {
				return false
			}
			for _, item := range v.Report.Items {
				if !slices.Contains(q.RemoveDevices, item.DeviceID) && !(q.RemoveProduct && item.ProductID == q.Product && (item.DeviceName == "容量测试 "+item.DeviceID || item.DeviceName == "压测设备 "+item.DeviceID || item.DeviceName == "GB26875 设备 "+strings.TrimPrefix(item.DeviceID, "gb26875_"))) {
					return false
				}
			}
			removedJobs[v.ID] = "inspection"
			inspectionAudits[fmt.Sprintf("inspection_%d", v.Report.GeneratedAt)] = true
			n.Resources++
			return true
		})
		for k, v := range r.replays {
			if v.TenantID == t && !capacityJobRunning(v.Status) && (slices.Contains(q.RemoveDevices, v.DeviceID) || (q.RemoveProduct && v.ProductID == q.Product && v.DeviceID == "")) {
				removedJobs[v.ID] = "replay"
				delete(r.replays, k)
				n.Resources++
			}
		}
	}
	if n.AccessReferences > 0 {
		r.accessStates[t] = prunedAccess
	}
	ruleIDs := map[string]bool{}
	for k, v := range r.rules {
		if v.TenantID == t && v.ProductID == q.Product && v.AlarmType == "CAPACITY_TEST" && (v.ID == q.RemoveRule || q.RemoveProduct) {
			ruleIDs[v.ID] = true
			if q.RemoveProduct {
				delete(r.rules, k)
				n.Rules++
			}
		}
	}
	if q.RemoveProduct && fixture.Source != "" {
		if q.Historical && strings.HasPrefix(fixture.Source, "legacy-cap") {
			for k, v := range r.accessProfiles {
				if v.TenantID == t && v.ProductID == q.Product && v.DeviceID == "" && !v.Enabled {
					delete(r.accessProfiles, k)
					n.Profiles++
				}
			}
		}
		delete(r.protocolBindings, key(t, q.Product))
		if fixture.ProtocolID != "" && !q.KeepProtocol {
			delete(r.protocolDefinitions, key(t, fixture.ProtocolID))
			for k, v := range r.protocolReleases {
				if v.TenantID == t && v.ProtocolID == fixture.ProtocolID {
					delete(r.protocolReleases, k)
				}
			}
			for k, v := range r.pointTables {
				if v.TenantID == t && v.ProtocolID == fixture.ProtocolID {
					delete(r.pointTables, k)
				}
			}
			if _, ok := r.protocols[key(t, product.ProtocolPackageID)]; ok {
				delete(r.protocols, key(t, product.ProtocolPackageID))
				n.Protocols++
			}
		}
		delete(r.products, key(t, q.Product))
		n.Products++
	}
	r.audits = slices.DeleteFunc(r.audits, func(v model.AuditLog) bool {
		if v.TenantID != t {
			return false
		}
		match := (v.TargetType == "device" && slices.Contains(q.RemoveDevices, v.TargetID)) || (v.TargetType == "alarm" && alarmIDs[v.TargetID]) || (v.TargetType == "raw-message" && rawIDs[v.TargetID]) || (v.TargetType == "rule" && ruleIDs[v.TargetID]) || (q.RemoveProduct && v.TargetType == "product" && v.TargetID == q.Product) || (v.TargetType == "replay" && removedJobs[v.TargetID] == "replay") || (slices.Contains([]string{"inspection", "health-inspection", "device-health"}, v.TargetType) && (removedJobs[v.TargetID] == "inspection" || inspectionAudits[v.TargetID]))
		device, _ := v.Details["deviceId"].(string)
		p, _ := v.Details["productId"].(string)
		match = match || slices.Contains(q.RemoveDevices, device) || (q.RemoveProduct && p == q.Product)
		if match {
			n.Audits++
		}
		return match
	})
	return n, nil
}

func capacityJobRunning(status string) bool {
	return slices.Contains([]string{"running", "pending", "processing", "queued"}, strings.ToLower(status))
}
