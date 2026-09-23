package memory

import (
	"context"
	"sort"

	"iot-platform/internal/model"
)

func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func (r *Repository) GetProductsByIDs(_ context.Context, tenant string, ids []string) (map[string]model.Product, error) {
	out := make(map[string]model.Product, len(ids))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, id := range ids {
		if value, ok := r.products[key(tenant, id)]; ok {
			out[id] = clone(value)
		}
	}
	return out, nil
}

func (r *Repository) GetDeviceStatesByIDs(_ context.Context, tenant string, ids []string) (map[string]model.DeviceState, error) {
	out := make(map[string]model.DeviceState, len(ids))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, id := range ids {
		if value, ok := r.states[key(tenant, id)]; ok {
			out[id] = clone(value)
		}
	}
	return out, nil
}

func (r *Repository) GetStandardMessagesByRawIDs(_ context.Context, tenant string, ids []string) (map[string]model.StandardMessage, error) {
	wanted := idSet(ids)
	out := make(map[string]model.StandardMessage, len(ids))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, value := range r.standard {
		if value.TenantID != tenant || !wanted[value.RawMessageID] {
			continue
		}
		if previous, ok := out[value.RawMessageID]; !ok || value.Timestamp > previous.Timestamp {
			out[value.RawMessageID] = clone(value)
		}
	}
	return out, nil
}

func (r *Repository) ListVideoCameraMappingsByDeviceIDs(_ context.Context, tenant string, ids []string) (map[string][]model.VideoCameraMapping, error) {
	wanted := idSet(ids)
	out := make(map[string][]model.VideoCameraMapping, len(ids))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, relations := range r.videoRelations {
		for _, relation := range relations {
			if relation.TenantID != tenant || relation.RelationType != "device" || !wanted[relation.TargetID] {
				continue
			}
			if mapping, ok := r.videoMappings[key(tenant, relation.CameraID)]; ok {
				out[relation.TargetID] = append(out[relation.TargetID], clone(mapping))
			}
		}
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool { return out[id][i].CameraID < out[id][j].CameraID })
	}
	return out, nil
}
