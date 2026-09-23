package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"    /* 执行当前语句并推进处理流程。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"fmt"      /* 执行当前语句并推进处理流程。 */
	"io"       /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"net/url"  /* 执行当前语句并推进处理流程。 */
	"path"     /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const maxVideoMediaBytes = 256 << 20 /* 声明 maxVideoMediaBytes。 */

func (e *Engine) archiveVideoMedia(ctx context.Context, v model.VideoAlarmEvent) (model.VideoAlarmEvent, error) { /* 定义 archiveVideoMedia 函数。 */
	var err error                                                                                    /* 声明 err。 */
	if strings.HasPrefix(v.SnapshotURL, "http://") || strings.HasPrefix(v.SnapshotURL, "https://") { /* 判断条件并选择处理分支。 */
		v.SnapshotURL, err = e.transferVideoURL(ctx, v, v.SnapshotURL, "snapshot") /* 更新 err 的值。 */
		if err != nil {                                                            /* 判断条件并选择处理分支。 */
			return v, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasPrefix(v.VideoClipURL, "http://") || strings.HasPrefix(v.VideoClipURL, "https://") { /* 判断条件并选择处理分支。 */
		v.VideoClipURL, err = e.transferVideoURL(ctx, v, v.VideoClipURL, "clip") /* 更新 err 的值。 */
		if err != nil {                                                          /* 判断条件并选择处理分支。 */
			return v, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func isExternalMedia(value string) bool { /* 定义 isExternalMedia 函数。 */
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) validateVideoMediaURLs(v model.VideoAlarmEvent) error { /* 定义 validateVideoMediaURLs 函数。 */
	for _, rawURL := range []string{v.SnapshotURL, v.VideoClipURL} { /* 循环处理当前数据。 */
		if !isExternalMedia(rawURL) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		u, err := url.Parse(rawURL) /* 更新 err 的值。 */
		if err != nil {             /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if !e.videoHostAllowed(u.Hostname()) { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("video media host %q is not allowlisted", u.Hostname()) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) processVideoMedia(ctx context.Context, original model.VideoAlarmEvent) { /* 定义 processVideoMedia 函数。 */
	updated, err := e.archiveVideoMedia(ctx, original) /* 更新 err 的值。 */
	if updated.Raw == nil {                            /* 判断条件并选择处理分支。 */
		updated.Raw = map[string]any{} /* 更新 updated.Raw 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		updated.Raw["mediaTransferStatus"] = "FAILED"   /* 执行当前语句并推进处理流程。 */
		updated.Raw["mediaTransferError"] = err.Error() /* 执行当前语句并推进处理流程。 */
		if e.Metrics != nil {                           /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("video_media_transfer_failed_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		updated.Raw["mediaTransferStatus"] = "STORED" /* 执行当前语句并推进处理流程。 */
		delete(updated.Raw, "mediaTransferError")     /* 执行当前语句并推进处理流程。 */
		if e.Metrics != nil {                         /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("video_media_transfer_success_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	_ = e.Repo.UpdateVideoEvent(ctx, updated) /* 更新 _ 的值。 */
	if err == nil {                           /* 判断条件并选择处理分支。 */
		e.updateVideoAlarmMedia(ctx, updated) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) updateVideoAlarmMedia(ctx context.Context, event model.VideoAlarmEvent) { /* 定义 updateVideoAlarmMedia 函数。 */
	alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: event.TenantID, Status: "ACTIVE", Limit: 1000}) /* 更新 err 的值。 */
	if err != nil {                                                                                                   /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, alarm := range alarms { /* 循环处理当前数据。 */
		if alarm.Source != "video" || alarm.DeviceID != event.CameraID || alarm.RuleID != "video:"+event.AlarmType { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if alarm.Details == nil { /* 判断条件并选择处理分支。 */
			alarm.Details = map[string]any{} /* 更新 alarm.Details 的值。 */
		} /* 结束当前表达式或代码块。 */
		alarm.Details["videoEvent"] = event /* 执行当前语句并推进处理流程。 */
		_ = e.Repo.UpdateAlarm(ctx, alarm)  /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) retryPendingVideoMedia(ctx context.Context) { /* 定义 retryPendingVideoMedia 函数。 */
	ticker := time.NewTicker(30 * time.Second) /* 更新 ticker 的值。 */
	defer ticker.Stop()                        /* 安排函数结束时执行清理。 */
	for {                                      /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case <-ticker.C: /* 处理当前分支。 */
			items, err := e.Repo.ListPendingVideoEvents(ctx, 100) /* 更新 err 的值。 */
			if err != nil {                                       /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			for _, item := range items { /* 循环处理当前数据。 */
				e.processVideoMedia(ctx, item) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) transferVideoURL(ctx context.Context, v model.VideoAlarmEvent, rawURL, kind string) (string, error) { /* 定义 transferVideoURL 函数。 */
	u, err := url.Parse(rawURL) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !e.videoHostAllowed(u.Hostname()) { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("video media host %q is not allowlisted", u.Hostname()) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { /* 更新 client 的值。 */
		if len(via) > 3 { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("too many redirects") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if !e.videoHostAllowed(req.URL.Hostname()) { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("redirect host %q is not allowlisted", req.URL.Hostname()) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return nil /* 返回当前处理结果。 */
	}} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := client.Do(req) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("download video media: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()       /* 安排函数结束时执行清理。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("download video media: %s", resp.Status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxVideoMediaBytes+1)) /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) > maxVideoMediaBytes { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("video media exceeds %d bytes", maxVideoMediaBytes) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ext := path.Ext(u.Path)                               /* 更新 ext 的值。 */
	if len(ext) > 10 || strings.ContainsAny(ext, "/\\") { /* 判断条件并选择处理分支。 */
		ext = "" /* 更新 ext 的值。 */
	} /* 结束当前表达式或代码块。 */
	bucket := "video-alarm"                                                                                                                                                             /* 更新 bucket 的值。 */
	key := fmt.Sprintf("%s/%s/%s/%s-%s%s", safeSegment(v.TenantID), time.UnixMilli(v.EventTime).UTC().Format("2006/01/02"), safeSegment(v.EventID), kind, safeSegment(v.CameraID), ext) /* 更新 key 的值。 */
	stored, err := e.Archive.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), resp.Header.Get("Content-Type"))                                                      /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return stored, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) videoHostAllowed(host string) bool { /* 定义 videoHostAllowed 函数。 */
	for _, allowed := range e.VideoMediaAllowedHosts { /* 循环处理当前数据。 */
		if strings.EqualFold(strings.TrimSpace(allowed), host) { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func safeSegment(v string) string { /* 定义 safeSegment 函数。 */
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(v) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
