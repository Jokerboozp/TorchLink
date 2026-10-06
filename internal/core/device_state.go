package core

import (
	"context"
	"encoding/json"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func (e *Engine) touchState(ctx context.Context, msg model.StandardMessage) error {
	return e.applyMessageState(ctx, msg, false)
}

// Reconcile after alarm processing so one message persists only its final
// state. Raw archive/claim/alarm ordering and the processed marker are retained.
func (e *Engine) applyMessageState(ctx context.Context, msg model.StandardMessage, reconcile bool) error {
	unlock := e.lockDeviceState(msg.TenantID, msg.DeviceID)
	defer unlock()
	before, after, written, err := e.mutateDeviceState(ctx, msg.TenantID, msg.DeviceID, func(state *model.DeviceState, found bool) (bool, error) {
		open := false
		if reconcile {
			var err error
			if open, err = e.Repo.HasOpenAlarm(ctx, msg.TenantID, msg.DeviceID); err != nil {
				return false, err
			}
		}
		if !found {
			state.ReportIntervalSec, state.OfflineToleranceSec = e.deviceTiming(ctx, msg.TenantID, msg.ProductID, msg.DeviceID)
		}
		return nextMessageState(state, found, msg, reconcile, open), nil
	})
	if err == nil && written {
		e.publishStateChange(ctx, before, after)
	}
	return err
}

// nextMessageState applies one processed message to the device state and
// reports whether it must be written. With reconcile the business status
// follows the device's open alarms.
func nextMessageState(state *model.DeviceState, found bool, msg model.StandardMessage, reconcile, open bool) bool {
	if !found {
		interval, tolerance := state.ReportIntervalSec, state.OfflineToleranceSec
		if interval <= 0 {
			interval, tolerance = model.DefaultReportIntervalSec, model.DefaultOfflineToleranceSec
		}
		*state = model.DeviceState{TenantID: msg.TenantID, ProductID: msg.ProductID, DeviceID: msg.DeviceID, ReportIntervalSec: interval, OfflineToleranceSec: tolerance, ConnectionStatus: "UNKNOWN"}
	}
	// Late retransmissions remain archived but must not roll back current state.
	late := msg.Timestamp < state.LastSeenAt
	if late && !reconcile {
		return false
	}
	previous := *state
	old := state.BusinessStatus
	state.DataStatus = "ACTIVE"
	if msg.MessageType == model.AlarmReport || strings.EqualFold(old, "ALARM") {
		state.BusinessStatus = "ALARM"
	} else {
		state.BusinessStatus = "ONLINE"
	}
	state.LastSeenAt = msg.Timestamp
	state.LastMessageID = msg.MessageID
	state.StatusSource = "RAW_MESSAGE"
	if msg.MessageType == model.StateChange && msg.Parser == parser.StandardParserName {
		if status, ok := msg.Properties["connectionStatus"].(string); ok && (status == "CONNECTED" || status == "DISCONNECTED" || status == "UNKNOWN") {
			state.ConnectionStatus = status
			if status == "CONNECTED" {
				state.LastConnectAt = msg.Timestamp
			} else if status == "DISCONNECTED" {
				state.LastDisconnectAt = msg.Timestamp
			}
		}
	}
	if late {
		*state = previous
	}
	if reconcile {
		if open {
			state.BusinessStatus = "ALARM"
			state.StatusSource = "ACTIVE_ALARM"
			state.Reason = "存在活动告警"
		} else if !late || state.BusinessStatus == "ALARM" {
			state.BusinessStatus = "ONLINE"
			state.StatusSource = "RAW_MESSAGE"
			state.Reason = ""
		}
	}
	return true
}

func (e *Engine) syncDeviceBusinessStatus(ctx context.Context, tenant, product, device string) error {
	unlock := e.lockDeviceState(tenant, device)
	defer unlock()
	before, after, written, err := e.mutateDeviceState(ctx, tenant, device, func(state *model.DeviceState, found bool) (bool, error) {
		if !found {
			return false, nil
		}
		open, err := e.hasOpenAlarm(ctx, tenant, device)
		if err != nil {
			return false, err
		}
		nextStatus, source, reason := "ONLINE", "RAW_MESSAGE", ""
		if open {
			nextStatus, source, reason = "ALARM", "ACTIVE_ALARM", "存在活动告警"
		}
		if state.ProductID == "" {
			state.ProductID = product
		}
		state.BusinessStatus, state.StatusSource, state.Reason = nextStatus, source, reason
		return true, nil
	})
	if err == nil && written {
		e.publishStateChange(ctx, before, after)
	}
	return err
}

func (e *Engine) hasOpenAlarm(ctx context.Context, tenant, device string) (bool, error) {
	return e.Repo.HasOpenAlarm(ctx, tenant, device)
}

func (e *Engine) handleState(ctx context.Context, b []byte) error {
	var state model.DeviceState
	if err := json.Unmarshal(b, &state); err == nil && state.DeviceID != "" {
		return e.UpdateDeviceState(ctx, state)
	}
	var msg model.StandardMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		return model.Permanent(err)
	}
	return e.touchState(ctx, msg)
}

func (e *Engine) UpdateDeviceState(ctx context.Context, state model.DeviceState) error {
	unlock := e.lockDeviceState(state.TenantID, state.DeviceID)
	defer unlock()
	return e.updateDeviceState(ctx, state)
}

// updateDeviceState replaces the stored state with state (keeping the last
// seen time when state has none), version-checked against concurrent writers.
func (e *Engine) updateDeviceState(ctx context.Context, state model.DeviceState) error {
	before, after, written, err := e.mutateDeviceState(ctx, state.TenantID, state.DeviceID, func(current *model.DeviceState, _ bool) (bool, error) {
		next := state
		if next.LastSeenAt == 0 {
			next.LastSeenAt = current.LastSeenAt
		}
		*current = next
		return true, nil
	})
	if err == nil && written {
		e.publishStateChange(ctx, before, after)
	}
	return err
}

// ScanOffline marks silent devices offline. Each device is re-evaluated on
// its freshest state inside a version-checked write, so a report that arrived
// after the listing is never overwritten by the stale snapshot.
func (e *Engine) ScanOffline(ctx context.Context) error {
	now := e.Clock.Now().UnixMilli()
	for ctx.Err() == nil {
		// Only devices whose check time has passed are read, in batches.
		states, err := e.Repo.ListOfflineDue(ctx, now, offlineScanBatch)
		if err != nil {
			return err
		}
		for _, listed := range states {
			if ctx.Err() != nil {
				break
			}
			unlock := e.lockDeviceState(listed.TenantID, listed.DeviceID)
			before, after, written, err := e.mutateDeviceState(ctx, listed.TenantID, listed.DeviceID, func(s *model.DeviceState, found bool) (bool, error) {
				check := s.OfflineCheckAt()
				if !found || check == 0 || check >= now {
					return false, nil
				}
				s.DataStatus, s.BusinessStatus = "SILENT", s.OfflineStatus()
				s.OfflineAt = s.OfflineDeadline()
				s.OfflineDetectedAt = now
				s.StatusSource = "RAW_MESSAGE_TIMEOUT"
				return true, nil
			})
			unlock()
			if err == nil && written {
				e.publishStateChange(ctx, before, after)
			}
		}
		if len(states) < offlineScanBatch {
			return nil
		}
	}
	return ctx.Err()
}

const offlineScanBatch = 1000
