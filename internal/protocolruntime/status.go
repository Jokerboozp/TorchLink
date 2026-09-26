package protocolruntime

// Sessions returns a tenant-scoped snapshot without exposing protocol state.
func (r *Listeners) Sessions(tenant, profile string) []map[string]any {
	r.mu.Lock()
	h := r.hosts[listenerKey(tenant, profile)]
	r.mu.Unlock()
	out := []map[string]any{}
	if h == nil {
		return out
	}
	h.mu.Lock()
	sessions := make([]*listenerSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		if !s.closed {
			last := int64(0)
			if !s.lastSeen.IsZero() {
				last = s.lastSeen.UnixMilli()
			}
			out = append(out, map[string]any{"deviceId": s.deviceID, "remoteAddress": s.remote, "lastSeenAt": last, "protocolId": s.release.ProtocolID, "protocolVersion": s.release.Version})
		}
		s.mu.Unlock()
	}
	return out
}
