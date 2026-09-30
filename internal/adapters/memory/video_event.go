package memory

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"sort"
	"strconv"
	"strings"
)

func (r *Repository) videoEventSource(v model.VideoAlarmEvent) ports.VideoEventSource {
	d, _ := v.Raw["governanceDeviceId"].(string)
	current := r.videoMappings[key(v.TenantID, v.CameraID)].DeviceID
	return ports.VideoEventSource{Event: cloneVideoEvent(v), DeviceID: d, CurrentDeviceID: current}
}
func (r *Repository) GetGovernanceVideoEvent(_ context.Context, tenant, id string) (ports.VideoEventSource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.video[key(tenant, id)]
	if !ok {
		return ports.VideoEventSource{}, model.ErrNotFound
	}
	return r.videoEventSource(v), nil
}
func (r *Repository) ListGovernanceVideoEvents(_ context.Context, tenant string, f ports.VideoEventFilter) ([]ports.VideoEventSource, error) {
	if f.Start < 0 || f.End <= f.Start || f.Limit < 1 || f.Limit > 1000 {
		return nil, model.ErrGovernanceInvalid
	}
	var at int64 = -1
	var id string
	if f.Cursor != "" {
		p := strings.SplitN(f.Cursor, ":", 2)
		if len(p) != 2 {
			return nil, model.ErrGovernanceInvalid
		}
		var err error
		at, err = strconv.ParseInt(p[0], 10, 64)
		if err != nil {
			return nil, model.ErrGovernanceInvalid
		}
		id = p[1]
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []ports.VideoEventSource{}
	for _, v := range r.video {
		if v.TenantID != tenant || v.EventTime < f.Start || v.EventTime >= f.End || v.EventTime < at || v.EventTime == at && v.EventID <= id {
			continue
		}
		source := r.videoEventSource(v)
		if !f.AllDevices && (source.DeviceID == "" || !slices.Contains(f.DeviceIDs, source.DeviceID) || source.CurrentDeviceID != "" && !slices.Contains(f.DeviceIDs, source.CurrentDeviceID)) {
			continue
		}
		if f.AllDevices && len(f.DeviceIDs) > 0 && !slices.Contains(f.DeviceIDs, source.DeviceID) && !slices.Contains(f.DeviceIDs, source.CurrentDeviceID) {
			continue
		}
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Event, out[j].Event
		return a.EventTime < b.EventTime || a.EventTime == b.EventTime && a.EventID < b.EventID
	})
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}
func (r *Repository) bindVideoEvent(v model.VideoAlarmEvent, preserve bool) model.VideoAlarmEvent {
	v = cloneVideoEvent(v)
	if v.Raw == nil {
		v.Raw = map[string]any{}
	}
	delete(v.Raw, "governanceDeviceId")
	if preserve {
		if old, ok := r.video[key(v.TenantID, v.EventID)]; ok {
			if d, ok := old.Raw["governanceDeviceId"].(string); ok && d != "" {
				v.Raw["governanceDeviceId"] = d
			}
		}
	} else if d := r.videoMappings[key(v.TenantID, v.CameraID)].DeviceID; d != "" {
		v.Raw["governanceDeviceId"] = d
	}
	return v
}

var _ ports.VideoEventReader = (*Repository)(nil)
