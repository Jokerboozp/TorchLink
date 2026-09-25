package memory

import (
	"context"
	"sort"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// ListManagedDevicesFiltered keeps the same ordering as the database adapter:
// newest update first, then device ID.
func (r *Repository) ListManagedDevicesFiltered(_ context.Context, f ports.DeviceFilter, limit, offset int) ([]model.ManagedDevice, int, error) {
	r.mu.RLock()
	out := []model.ManagedDevice{}
	for _, v := range r.devices {
		var state *model.DeviceState
		if s, ok := r.states[key(v.TenantID, v.ID)]; ok {
			state = &s
		}
		if f.Matches(v, state) {
			out = append(out, cloneManaged(v))
		}
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID > out[j].ID
	})
	return page(out, offset, limit), len(out), nil
}
