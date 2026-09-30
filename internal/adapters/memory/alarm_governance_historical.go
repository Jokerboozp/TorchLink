package memory

import (
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"sort"
)

func (t *governanceTx) GetGovernanceHistoricalMessage(id string) (model.StandardMessage, error) {
	v, ok := t.standard[key(t.tenant, id)]
	if !ok {
		return v, model.ErrNotFound
	}
	return clone(v), nil
}

func (t *governanceTx) ListGovernanceHistoricalMessages(f ports.AlarmObservationFilter) ([]model.StandardMessage, error) {
	out := []model.StandardMessage{}
	for _, v := range t.standard {
		if v.TenantID != t.tenant || !slices.Contains(f.DeviceIDs, v.DeviceID) || v.Timestamp < f.Start || v.Timestamp >= f.End {
			continue
		}
		if f.Cursor != "" && fmt.Sprintf("%019d:%s", v.Timestamp, v.MessageID) <= f.Cursor {
			continue
		}
		out = append(out, clone(v))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp == out[j].Timestamp {
			return out[i].MessageID < out[j].MessageID
		}
		return out[i].Timestamp < out[j].Timestamp
	})
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}
