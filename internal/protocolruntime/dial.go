package protocolruntime

import (
	"context"
	"net"
	"strconv"
	"time"
)

// SetAllowedCIDRs uses the same outbound network policy as central Modbus.
func (r *Listeners) SetAllowedCIDRs(cidrs []string) { r.allowedCIDRs = append([]string(nil), cidrs...) }

func (h *protocolListener) dial() {
	backoff := time.Second
	for h.ctx.Err() == nil {
		p := h.snapshot()
		ctx, cancel := context.WithTimeout(h.ctx, time.Duration(max(1, p.TimeoutMs))*time.Millisecond)
		host, err := ResolveAllowedTarget(ctx, p.Host, h.owner.allowedCIDRs)
		var conn net.Conn
		if err == nil {
			conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(p.Port)))
		}
		cancel()
		if err == nil {
			s := h.newSession(conn.RemoteAddr().String(), conn, nil)
			h.mu.Lock()
			if h.ctx.Err() != nil {
				h.mu.Unlock()
				conn.Close()
				return
			}
			h.lastError = ""
			h.sessions[s.remote] = s
			h.mu.Unlock()
			go s.poll()
			s.receiveTCP()
			backoff = time.Second
		} else {
			h.mu.Lock()
			h.lastError = limitError(err.Error(), 512)
			h.mu.Unlock()
		}
		timer := time.NewTimer(backoff)
		select {
		case <-h.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(30*time.Second, backoff*2)
	}
}

func (s *listenerSession) poll() {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	last := map[string]time.Time{}
	for {
		select {
		case <-s.done:
			return
		case <-s.host.ctx.Done():
			return
		case now := <-tick.C:
			p := s.host.snapshot()
			s.mu.Lock()
			device := s.deviceID
			s.mu.Unlock()
			if device == "" && p.ConnectionMode == "dial" {
				device = p.DeviceID
			}
			if device == "" {
				continue
			}
			for _, query := range p.Queries {
				if now.Sub(last[query.Type]) < time.Duration(query.IntervalSec)*time.Second {
					continue
				}
				command := map[string]any{}
				for k, v := range query.Params {
					command[k] = v
				}
				command["type"] = query.Type
				command["_scheduled"] = true
				last[query.Type] = now
				_, err := s.host.owner.Command(s.host.ctx, p.TenantID, p.ID, device, command)
				if err != nil {
					s.host.owner.warn("scheduled protocol query", err)
				}
				select {
				case <-s.done:
					return
				default:
				}
			}
		}
	}
}
