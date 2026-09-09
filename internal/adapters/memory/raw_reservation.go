package memory

import (
	"context"
	"iot-platform/internal/model"
)

type rawReservation struct {
	Metadata model.RawMessage
	Hash     string
}

func (r *Repository) ReserveRawMessage(_ context.Context, v model.RawMessage) (model.RawMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rawReservations == nil {
		r.rawReservations = map[string]rawReservation{}
	}
	k := key(v.TenantID, v.MessageID)
	if old, ok := r.rawReservations[k]; ok {
		if old.Hash != v.PayloadHash() || old.Metadata.ProductID != v.ProductID || old.Metadata.DeviceID != v.DeviceID {
			return v, model.ErrRawConflict
		}
		canonical := clone(old.Metadata)
		canonical.Payload = append(canonical.Payload[:0], v.Payload...)
		return canonical, nil
	}
	metadata := clone(v)
	metadata.Payload = nil
	r.rawReservations[k] = rawReservation{Metadata: metadata, Hash: v.PayloadHash()}
	return v, nil
}
