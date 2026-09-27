package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"iot-platform/internal/model"
)

// processingLease bounds how long a crashed worker's claim blocks another
// worker from taking over a standard message.
const processingLease = 60 * time.Second

// casAttempts bounds optimistic retries of one read-modify-write.
const casAttempts = 8

// claimStandard claims msg for this process. When another live worker holds
// it (for example the previous partition owner during a rebalance), it waits
// for that claim to finish or expire instead of processing concurrently.
func (e *Engine) claimStandard(ctx context.Context, msg model.StandardMessage) (model.StandardClaim, error) {
	deadline := time.Now().Add(processingLease + 10*time.Second)
	for {
		claim, err := e.Repo.ClaimStandardMessage(ctx, msg, e.Identity(), processingLease)
		if err != nil || !claim.Busy {
			return claim, err
		}
		if time.Now().After(deadline) {
			return claim, fmt.Errorf("standard message %s is still claimed by %s", msg.MessageID, claim.HeldBy)
		}
		e.count("standard_claim_wait_total")
		wait := min(max(claim.RetryAfter, 50*time.Millisecond), 5*time.Second)
		select {
		case <-ctx.Done():
			return claim, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// mutateDeviceState applies fn to the freshest stored state and writes it
// only if nobody wrote in between; conflicts re-read and retry. fn reports
// whether to write. It returns the state before and after the change.
func (e *Engine) mutateDeviceState(ctx context.Context, tenant, device string, fn func(state *model.DeviceState, found bool) (bool, error)) (before, after model.DeviceState, written bool, err error) {
	for attempt := 0; attempt < casAttempts; attempt++ {
		current, getErr := e.Repo.GetDeviceStateFresh(ctx, tenant, device)
		found := getErr == nil
		if getErr != nil && !errors.Is(getErr, model.ErrNotFound) {
			return before, after, false, getErr
		}
		if !found {
			current = model.DeviceState{}
		}
		next := current
		write, fnErr := fn(&next, found)
		if fnErr != nil || !write {
			return current, current, false, fnErr
		}
		next.TenantID, next.DeviceID, next.Version = tenant, device, current.Version
		ok, writeErr := e.Repo.UpsertDeviceStateIf(ctx, next)
		if writeErr != nil {
			return current, current, false, writeErr
		}
		if ok {
			next.Version++
			return current, next, true, nil
		}
		e.count("device_state_conflict_total")
		backoff(ctx, attempt)
	}
	return before, after, false, fmt.Errorf("device %s state: %w", device, model.ErrConcurrentUpdate)
}

// mutateAlarm applies fn to the freshest stored alarm under optimistic
// concurrency. fn reports whether to write.
func (e *Engine) mutateAlarm(ctx context.Context, tenant, alarmID string, fn func(alarm *model.Alarm) (bool, error)) (model.Alarm, bool, error) {
	for attempt := 0; attempt < casAttempts; attempt++ {
		current, err := e.Repo.GetAlarm(ctx, tenant, alarmID)
		if err != nil {
			return current, false, err
		}
		next := current
		write, err := fn(&next)
		if err != nil || !write {
			return current, false, err
		}
		next.Version = current.Version
		ok, err := e.Repo.UpdateAlarmIf(ctx, next)
		if err != nil {
			return current, false, err
		}
		if ok {
			next.Version++
			return next, true, nil
		}
		e.count("alarm_conflict_total")
		backoff(ctx, attempt)
	}
	return model.Alarm{}, false, fmt.Errorf("alarm %s: %w", alarmID, model.ErrConcurrentUpdate)
}

func backoff(ctx context.Context, attempt int) {
	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(attempt+1) * 5 * time.Millisecond):
	}
}

func (e *Engine) count(name string) {
	if e.Metrics != nil {
		e.Metrics.Inc(name)
	}
}

// publishStateChange records and publishes a changed business or connection
// status.
func (e *Engine) publishStateChange(ctx context.Context, before, after model.DeviceState) {
	if before.BusinessStatus == after.BusinessStatus && before.ConnectionStatus == after.ConnectionStatus {
		return
	}
	_ = e.Repo.SaveDeviceStateEvent(ctx, after)
	payload := mustJSON(after)
	_ = e.Realtime.Publish(ctx, fmt.Sprintf("/iot/device/state/%s/%s/%s", after.TenantID, after.ProductID, after.DeviceID), payload, 1, true)
}
