package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"iot-platform/internal/externaldata"
	"iot-platform/internal/model"
)

// External event identity is stamped by trusted ingress metadata, never taken
// from arbitrary device payload fields. Each external event owns its lifecycle;
// separate events must not share a recoverable alarm merely by alarm type.
func externalAlarmEvent(msg model.StandardMessage) bool {
	return msg.MessageType == model.AlarmReport && msg.Tags["externalEventKey"] != ""
}

func externalAlarmIdentity(a *model.Alarm, msg model.StandardMessage) {
	if !externalAlarmEvent(msg) {
		return
	}
	h := sha256.Sum256([]byte(msg.Tags["externalEventKey"] + "\x00" + a.RuleID))
	a.ID = "external_" + hex.EncodeToString(h[:])
	a.RuleID += ":external:" + msg.Tags["externalEventKey"]
	a.FirstTriggeredAt = msg.Timestamp
	a.LastTriggeredAt = msg.Timestamp
	a.Content, _ = msg.Event["content"].(string)
	a.Details["externalSourceId"] = msg.Tags["externalSourceId"]
	a.Details["externalEventId"] = msg.Tags["externalEventId"]
	if encoded := msg.Tags["externalVideoEvent"]; encoded != "" {
		var v model.VideoAlarmEvent
		if json.Unmarshal([]byte(encoded), &v) == nil {
			a.Source = "video"
			a.DeviceType = "video_ai"
			a.Confidence = v.Confidence
			a.Details["videoEvent"] = v
			if v.Confidence < 0.6 {
				a.Details["requiresVerification"] = true
			}
			cameras := a.Cameras[:0]
			for _, c := range a.Cameras {
				if c.CameraID == v.CameraID {
					cameras = append(cameras, c)
				}
			}
			a.Cameras = cameras
		}
	}
}

func (e *Engine) upsertReportedAlarm(ctx context.Context, a model.Alarm, msg model.StandardMessage) (model.Alarm, bool, bool, error) {
	externalAlarmIdentity(&a, msg)
	if externalAlarmEvent(msg) {
		return e.Repo.UpsertExternalAlarm(ctx, a)
	}
	saved, created, err := e.Repo.UpsertAlarm(ctx, a)
	return saved, created, created || saved.TriggerID != msg.MessageID, err
}

func (e *Engine) saveExternalDelivery(ctx context.Context, msg model.StandardMessage, ids []string) error {
	if msg.Tags["externalEventKey"] == "" {
		return nil
	}
	result := externaldata.Result{DeviceID: msg.DeviceID, MessageID: msg.RawMessageID, AlarmIDs: ids}
	if len(ids) > 0 {
		result.AlarmID = ids[0]
	}
	if encoded := msg.Tags["externalVideoEvent"]; encoded != "" {
		var video model.VideoAlarmEvent
		if err := json.Unmarshal([]byte(encoded), &video); err != nil {
			return err
		}
		result.CameraID = video.CameraID
		if video.Raw == nil {
			video.Raw = map[string]any{}
		}
		if isExternalMedia(video.SnapshotURL) || isExternalMedia(video.VideoClipURL) {
			video.Raw["mediaTransferStatus"] = "PENDING"
		}
		if err := e.ValidateExternalVideoMedia(ctx, video); err != nil {
			video.Raw["mediaTransferStatus"] = "FAILED"
			video.Raw["mediaTransferError"] = err.Error()
		}
		if _, err := e.Repo.SaveVideoEvent(ctx, video); err != nil {
			return err
		}
		// Media is processed by the existing durable retry job, independently
		// of alarm creation and the receipt's completion status.
	}
	b, _ := json.Marshal(result)
	_, err := e.Repo.ExternalDataStore().Put(ctx, externaldata.Entry{TenantID: msg.TenantID, Kind: "delivery", ID: msg.RawMessageID, SourceID: msg.Tags["externalSourceId"], Status: "PROCESSED", Body: b}, 0)
	if errors.Is(err, externaldata.ErrConflict) {
		return nil
	}
	return err
}

func (e *Engine) ValidateExternalVideoMedia(ctx context.Context, v model.VideoAlarmEvent) error {
	return e.validateExternalVideoMediaURLs(ctx, v)
}

func (e *Engine) applyExternalComponentAlarm(ctx context.Context, a model.Alarm, msg model.StandardMessage, active bool) (model.Alarm, string, error) {
	if active {
		saved, created, _, err := e.upsertReportedAlarm(ctx, a, msg)
		event := ""
		if created {
			event = "raised"
		}
		return saved, event, err
	}
	externalAlarmIdentity(&a, msg)
	old, err := e.Repo.GetAlarm(ctx, a.TenantID, a.ID)
	if errors.Is(err, model.ErrNotFound) {
		return model.Alarm{}, "", nil
	}
	if err != nil {
		return old, "", err
	}
	err = e.RecoverExternalAlarms(ctx, a.TenantID, []string{a.ID}, "外部部件状态")
	return old, "", err // SetAlarmStatus publishes the recovery transition.
}

// External recovery is event-specific and keeps an operator's terminal state.
func (e *Engine) RecoverExternalAlarms(ctx context.Context, tenant string, ids []string, actor string) error {
	for _, id := range ids {
		a, err := e.Repo.GetAlarm(ctx, tenant, id)
		if err != nil {
			return err
		}
		if !strings.Contains(a.RuleID, ":external:") {
			return errors.New("告警不属于外部事件")
		}
		if a.Status == "CLOSED" || a.Status == "RECOVERED" {
			continue
		}
		if _, err = e.SetAlarmStatus(ctx, tenant, id, "RECOVERED", actor); err != nil {
			return err
		}
	}
	return nil
}
