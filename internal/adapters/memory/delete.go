package memory

import (
	"context"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

// DeleteResource checks live references under the same lock as the deletion.
func (r *Repository) DeleteResource(_ context.Context, tenant, kind, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, id)
	switch kind {
	case "device":
		if _, ok := r.devices[k]; !ok {
			return model.ErrNotFound
		}
		for _, child := range r.devices {
			if child.TenantID == tenant && child.GatewayID == id {
				return model.ErrResourceInUse
			}
		}
		for _, profile := range r.accessProfiles {
			if profile.TenantID == tenant && profile.DeviceID == id {
				return model.ErrResourceInUse
			}
		}
		for _, alarm := range r.alarms {
			if alarm.TenantID == tenant && alarm.DeviceID == id && (alarm.Status == "ACTIVE" || alarm.Status == "ACKED") {
				return model.ErrResourceInUse
			}
		}
		for _, mapping := range r.videoMappings {
			if mapping.TenantID == tenant && (mapping.DeviceID == id || slices.Contains(mapping.RelatedDeviceIDs, id)) {
				return model.ErrResourceInUse
			}
		}
		for _, relations := range r.videoRelations {
			for _, relation := range relations {
				if relation.TenantID == tenant && relation.RelationType == "device" && relation.TargetID == id {
					return model.ErrResourceInUse
				}
			}
		}
		delete(r.devices, k)
		delete(r.states, k)
		for pendingKey := range r.rulePending {
			if strings.HasSuffix(pendingKey, "\x00"+id) && strings.HasPrefix(pendingKey, tenant+"\x00") {
				delete(r.rulePending, pendingKey)
			}
		}
		for alarmKey := range r.componentAlarms {
			if strings.HasPrefix(alarmKey, key(tenant, id)+"\x00") {
				delete(r.componentAlarms, alarmKey)
			}
		}
	case "product":
		if _, ok := r.products[k]; !ok {
			return model.ErrNotFound
		}
		for _, device := range r.devices {
			if device.TenantID == tenant && device.ProductID == id {
				return model.ErrResourceInUse
			}
		}
		for _, profile := range r.accessProfiles {
			if profile.TenantID == tenant && profile.ProductID == id {
				return model.ErrResourceInUse
			}
			for _, child := range profile.ChildProducts {
				if profile.TenantID == tenant && child.ProductID == id {
					return model.ErrResourceInUse
				}
			}
		}
		for _, doc := range r.knowledge {
			if doc.TenantID == tenant && doc.ProductID == id {
				return model.ErrResourceInUse
			}
		}
		for _, rule := range r.rules {
			if rule.TenantID == tenant && rule.ProductID == id {
				return model.ErrResourceInUse
			}
		}
		for _, binding := range r.workflowKnowledge {
			if binding.TenantID == tenant && slices.Contains(binding.ProductIDs, id) {
				return model.ErrResourceInUse
			}
		}
		delete(r.protocolBindings, k)
		delete(r.products, k)
	case "profile":
		profile, ok := r.accessProfiles[k]
		if !ok {
			return model.ErrNotFound
		}
		if profile.Enabled {
			return model.ErrResourceInUse
		}
		for _, device := range r.devices {
			if device.TenantID == tenant && device.Tags["connectorProfileId"] == id {
				return model.ErrResourceInUse
			}
		}
		delete(r.accessProfiles, k)
	case "protocol":
		if _, ok := r.protocolDefinitions[k]; !ok {
			return model.ErrNotFound
		}
		for _, binding := range r.protocolBindings {
			if binding.TenantID == tenant && (binding.ProtocolID == id || binding.PreviousProtocolID == id) {
				return model.ErrResourceInUse
			}
		}
		for _, profile := range r.accessProfiles {
			if profile.TenantID == tenant && profile.ProtocolID == id {
				return model.ErrResourceInUse
			}
		}
		for _, product := range r.products {
			if product.TenantID == tenant && product.ProtocolPackageID == id {
				return model.ErrResourceInUse
			}
		}
		for releaseKey, release := range r.protocolReleases {
			if release.TenantID == tenant && release.ProtocolID == id {
				delete(r.protocolReleases, releaseKey)
			}
		}
		for pointKey, point := range r.pointTables {
			if point.TenantID == tenant && point.ProtocolID == id {
				delete(r.pointTables, pointKey)
			}
		}
		delete(r.protocolDefinitions, k)
	case "camera":
		if _, ok := r.videoMappings[k]; !ok {
			return model.ErrNotFound
		}
		for _, relations := range r.videoRelations {
			for _, relation := range relations {
				if relation.TenantID == tenant && relation.CameraID == id {
					return model.ErrResourceInUse
				}
			}
		}
		for _, rule := range r.rules {
			if rule.TenantID == tenant {
				for _, action := range rule.Actions {
					if action.CameraID == id {
						return model.ErrResourceInUse
					}
				}
			}
		}
		delete(r.videoMappings, k)
	case "alarm":
		alarm, ok := r.alarms[k]
		if !ok {
			return model.ErrNotFound
		}
		if alarm.Status == "ACTIVE" || alarm.Status == "ACKED" {
			return model.ErrResourceInUse
		}
		delete(r.alarms, k)
		delete(r.ai, k)
		for stateKey, state := range r.componentAlarms {
			if strings.HasPrefix(stateKey, tenant+"\x00") && state.AlarmID == id {
				delete(r.componentAlarms, stateKey)
			}
		}
	case "knowledge":
		if _, ok := r.knowledge[k]; !ok {
			return model.ErrNotFound
		}
		delete(r.knowledge, k)
	default:
		return model.ErrNotFound
	}
	return nil
}
