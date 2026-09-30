package memory

import (
	"context"
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
			delete(r.raw, k)
			delete(r.rawMessages, k)
			delete(r.rawReservations, k)
			n.Raw++
		}
	}
	for k, v := range r.alarms {
		if v.TenantID == t && (remove(v.TenantID, v.DeviceID) || ids[v.TriggerID]) {
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
				if v.ID == res.ID && v.CapacityRunID == q.RunID && !capacityJobRunning(v.Status) {
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
				delete(r.replays, res.ID)
				n.Resources++
			}
		}
	}
	return n, nil
}

func capacityJobRunning(status string) bool {
	return slices.Contains([]string{"running", "pending", "processing"}, strings.ToLower(status))
}
