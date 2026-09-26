package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const maxVideoMediaBytes = 256 << 20

func (e *Engine) archiveVideoMedia(ctx context.Context, v model.VideoAlarmEvent) (model.VideoAlarmEvent, error) {
	var err error
	if strings.HasPrefix(v.SnapshotURL, "http://") || strings.HasPrefix(v.SnapshotURL, "https://") {
		v.SnapshotURL, err = e.transferVideoURL(ctx, v, v.SnapshotURL, "snapshot")
		if err != nil {
			return v, err
		}
	}
	if strings.HasPrefix(v.VideoClipURL, "http://") || strings.HasPrefix(v.VideoClipURL, "https://") {
		v.VideoClipURL, err = e.transferVideoURL(ctx, v, v.VideoClipURL, "clip")
		if err != nil {
			return v, err
		}
	}
	return v, nil
}
func isExternalMedia(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}
func (e *Engine) validateVideoMediaURLs(v model.VideoAlarmEvent) error {
	for _, rawURL := range []string{v.SnapshotURL, v.VideoClipURL} {
		if !isExternalMedia(rawURL) {
			continue
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		if !e.videoHostAllowed(u.Hostname()) {
			return fmt.Errorf("video media host %q is not allowlisted", u.Hostname())
		}
	}
	return nil
}
func (e *Engine) processVideoMedia(ctx context.Context, original model.VideoAlarmEvent) {
	updated, err := e.archiveVideoMedia(ctx, original)
	if updated.Raw == nil {
		updated.Raw = map[string]any{}
	}
	if err != nil {
		updated.Raw["mediaTransferStatus"] = "FAILED"
		updated.Raw["mediaTransferError"] = err.Error()
		if e.Metrics != nil {
			e.Metrics.Inc("video_media_transfer_failed_total")
		}
	} else {
		updated.Raw["mediaTransferStatus"] = "STORED"
		delete(updated.Raw, "mediaTransferError")
		if e.Metrics != nil {
			e.Metrics.Inc("video_media_transfer_success_total")
		}
	}
	_ = e.Repo.UpdateVideoEvent(ctx, updated)
	if err == nil {
		e.updateVideoAlarmMedia(ctx, updated)
	}
}
func (e *Engine) updateVideoAlarmMedia(ctx context.Context, event model.VideoAlarmEvent) {
	alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: event.TenantID, Status: "ACTIVE", Limit: 1000})
	if err != nil {
		return
	}
	for _, alarm := range alarms {
		if alarm.Source != "video" || alarm.DeviceID != event.CameraID || alarm.RuleID != "video:"+event.AlarmType {
			continue
		}
		if alarm.Details == nil {
			alarm.Details = map[string]any{}
		}
		alarm.Details["videoEvent"] = event
		_ = e.Repo.UpdateAlarm(ctx, alarm)
	}
}
func (e *Engine) retryPendingVideoMedia(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			items, err := e.Repo.ListPendingVideoEvents(ctx, 100)
			if err != nil {
				continue
			}
			for _, item := range items {
				e.processVideoMedia(ctx, item)
			}
		}
	}
}
func (e *Engine) transferVideoURL(ctx context.Context, v model.VideoAlarmEvent, rawURL, kind string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if !e.videoHostAllowed(u.Hostname()) {
		return "", fmt.Errorf("video media host %q is not allowlisted", u.Hostname())
	}
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("too many redirects")
		}
		if !e.videoHostAllowed(req.URL.Hostname()) {
			return fmt.Errorf("redirect host %q is not allowlisted", req.URL.Hostname())
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download video media: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("download video media: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxVideoMediaBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxVideoMediaBytes {
		return "", fmt.Errorf("video media exceeds %d bytes", maxVideoMediaBytes)
	}
	ext := path.Ext(u.Path)
	if len(ext) > 10 || strings.ContainsAny(ext, "/\\") {
		ext = ""
	}
	bucket := "video-alarm"
	key := fmt.Sprintf("%s/%s/%s/%s-%s%s", safeSegment(v.TenantID), time.UnixMilli(v.EventTime).UTC().Format("2006/01/02"), safeSegment(v.EventID), kind, safeSegment(v.CameraID), ext)
	stored, err := e.Archive.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), resp.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	return stored, nil
}
func (e *Engine) videoHostAllowed(host string) bool {
	for _, allowed := range e.VideoMediaAllowedHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return true
		}
	}
	return false
}
func safeSegment(v string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(v)
}
