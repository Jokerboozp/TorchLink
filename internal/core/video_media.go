package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const maxVideoMediaBytes = 256 << 20

func (e *Engine) archiveVideoMedia(ctx context.Context, v model.VideoAlarmEvent) (model.VideoAlarmEvent, error) {
	// Do not erase a failed URL: it is the durable retry target. Each attachment
	// is independent, so a broken screenshot cannot prevent archiving the clip.
	raw := make(map[string]any, len(v.Raw)+4)
	for key, value := range v.Raw {
		raw[key] = value
	}
	v.Raw = raw
	var failures []error
	for _, item := range []struct {
		url          *string
		kind, status string
	}{{&v.SnapshotURL, "snapshot", "snapshotTransferStatus"}, {&v.VideoClipURL, "clip", "clipTransferStatus"}} {
		if !isExternalMedia(*item.url) {
			if *item.url == "" || externalVideoSourceID(v) == "" {
				continue
			}
			if v.Raw[item.status] == "STORED" && (strings.HasPrefix(*item.url, "local://video-alarm/") || strings.HasPrefix(*item.url, "minio://video-alarm/")) {
				continue
			}
			err := errors.New("媒体地址须为 HTTP/HTTPS URL")
			v.Raw[item.status] = "FAILED"
			v.Raw[item.kind+"TransferError"] = err.Error()
			failures = append(failures, err)
			continue
		}
		stored, err := e.transferVideoURL(ctx, v, *item.url, item.kind)
		if err != nil {
			v.Raw[item.status] = "FAILED"
			v.Raw[item.kind+"TransferError"] = err.Error()
			failures = append(failures, err)
			continue
		}
		*item.url = stored
		v.Raw[item.status] = "STORED"
		delete(v.Raw, item.kind+"TransferError")
	}
	return v, errors.Join(failures...)
}
func isExternalMedia(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}
func (e *Engine) processVideoMedia(ctx context.Context, original model.VideoAlarmEvent) {
	updated, err := e.archiveVideoMedia(ctx, original)
	if updated.Raw == nil {
		updated.Raw = map[string]any{}
	}
	attempts := videoMediaAttemptCount(original) + 1
	updated.Raw["mediaAttempts"] = attempts
	updated.Raw["mediaLastAttemptAt"] = time.Now().UnixMilli()
	if err != nil {
		updated.Raw["mediaTransferStatus"] = "FAILED"
		updated.Raw["mediaTransferError"] = err.Error()
		if e.Metrics != nil {
			e.Metrics.Inc("video_media_transfer_failed_total")
		}
	} else {
		updated.Raw["mediaTransferStatus"] = "STORED"
		delete(updated.Raw, "mediaTransferError")
		delete(updated.Raw, "mediaRetryAt")
		if e.Metrics != nil {
			e.Metrics.Inc("video_media_transfer_success_total")
		}
	}
	if updateErr := e.updateVideoAlarmMedia(ctx, updated); updateErr != nil {
		updated.Raw["mediaTransferStatus"] = "FAILED"
		updated.Raw["mediaTransferError"] = "更新告警媒体信息失败，将自动重试"
	}
	if updated.Raw["mediaTransferStatus"] == "FAILED" {
		updated.Raw["mediaRetryAt"] = time.Now().Add(time.Duration(30*(1<<min(attempts-1, 6))) * time.Second).UnixMilli()
	}
	_ = e.Repo.UpdateVideoEvent(ctx, updated)
}
func (e *Engine) updateVideoAlarmMedia(ctx context.Context, event model.VideoAlarmEvent) error {
	// DeviceID may be the platform device for external events or a camera for
	// legacy events. Match the exact attached event, including recovered alarms
	// and fused confirmation slots, and recheck it inside the mutation fence.
	const pageSize = 250
	for offset := 0; ctx.Err() == nil; offset += pageSize {
		alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: event.TenantID, Limit: pageSize, Offset: offset})
		if err != nil {
			return err
		}
		for _, listed := range alarms {
			matched := false
			for _, slot := range []string{"videoEvent", "latestVideoEvent", "videoConfirmation"} {
				matched = matched || sameVideoEvent(listed.Details[slot], event)
			}
			if !matched {
				continue
			}
			_, _, err = e.mutateAlarm(ctx, listed.TenantID, listed.ID, func(alarm *model.Alarm) (bool, error) {
				changed := false
				for _, slot := range []string{"videoEvent", "latestVideoEvent", "videoConfirmation"} {
					if sameVideoEvent(alarm.Details[slot], event) {
						alarm.Details[slot] = event
						changed = true
					}
				}
				return changed, nil
			})
			if err != nil {
				return err
			}
		}
		if len(alarms) < pageSize {
			return nil
		}
	}
	return ctx.Err()
}

// retryPendingVideoMediaOnce retries media transfers of video alarms; it runs
// as a cluster singleton job.
func (e *Engine) retryPendingVideoMediaOnce(ctx context.Context) error {
	items, err := e.Repo.ListPendingVideoEvents(ctx, 100)
	if err != nil {
		return err
	}
	for _, item := range items {
		e.processVideoMedia(ctx, item)
	}
	return nil
}
func (e *Engine) transferVideoURL(ctx context.Context, v model.VideoAlarmEvent, rawURL, kind string) (string, error) {
	u, err := parseVideoMediaURL(rawURL)
	if err != nil {
		return "", err
	}
	if err = e.videoMediaURLAllowed(ctx, v, u); err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("too many redirects")
		}
		return e.videoMediaURLAllowed(ctx, v, req.URL)
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", errors.New("无法创建媒体下载请求")
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("媒体下载失败，请检查网络、证书及允许名单")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("媒体下载返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxVideoMediaBytes+1))
	if err != nil {
		return "", errors.New("读取媒体内容失败")
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
		return "", errors.New("媒体归档保存失败")
	}
	return stored, nil
}
func safeSegment(v string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(v)
}
