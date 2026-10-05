package capacity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
)

// ModuleConfig carries what agents need for business-module streams.
type ModuleConfig struct {
	AIMaxRuns         int      `json:"aiMaxRuns,omitempty"`
	AITimeout         Duration `json:"aiTimeout,omitempty"`
	KnowledgeWorkflow string   `json:"knowledgeWorkflow,omitempty"`
	KnowledgeBytes    int      `json:"knowledgeBytes,omitempty"`
	VideoCameras      []string `json:"videoCameras,omitempty"`
	VideoTimeout      Duration `json:"videoTimeout,omitempty"`
	WebURL            string   `json:"webUrl,omitempty"`
	ExportTimeout     Duration `json:"exportTimeout,omitempty"`
	OpenAPIKey        string   `json:"openApiKey,omitempty"`
	RealtimeSubs      int      `json:"realtimeSubscribers,omitempty"`
}

// moduleState is prepared once per run on an agent.
type moduleState struct {
	alarms    []string
	aiRuns    atomic.Int64
	knowledge atomic.Int64
	realtime  *realtimeSubscribers
}

// ModuleStreamNames lists every module stream an agent can run.
var ModuleStreamNames = []string{"ai", "knowledge", "video", "export_raw", "export_replay", "export_inspection", "openapi"}

func jsonCall(ctx context.Context, c *http.Client, method, u, token string, body any) (int, []byte, error) {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	hdr := map[string]string{}
	if run, ok := ctx.Value(capacityRunContextKey{}).(string); ok {
		hdr["X-Capacity-Run-ID"] = run
	}
	if token != "" {
		hdr["Authorization"] = "Bearer " + token
	}
	return doHTTP(ctx, c, method, u, b, hdr)
}

func (w *Worker) prepareModules(ctx context.Context, r *workerRun) {
	cfg := r.req.Config
	st := &moduleState{}
	r.modules = st
	if cfg.Modules.AIMaxRuns > 0 {
		// Manual analysis needs alarms; the test tenant's active alarms are
		// the targets (the device load with alarmFraction creates them).
		status, body, err := jsonCall(ctx, r.http.query, http.MethodGet, cfg.API+"/api/v1/alarms?page=1&pageSize=100&status=ACTIVE", cfg.OperatorToken, nil)
		if err == nil && status == 200 {
			var v struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			}
			_ = json.Unmarshal(body, &v)
			for _, a := range v.Items {
				st.alarms = append(st.alarms, a.ID)
			}
		}
	}
	if n := cfg.Modules.RealtimeSubs; n > 0 && cfg.MQTT != "" {
		st.realtime = connectRealtime(ctx, r.http.query, cfg, n, r.req.RunID)
	} else if n > 0 {
		st.realtime = &realtimeSubscribers{rec: &streamRecorder{}, failures: map[string]int{"no_mqtt_url": n}}
	}
}

// poll repeats fn until it reports done or the deadline passes.
func poll(ctx context.Context, every time.Duration, fn func() (done bool, ok bool, code string)) (bool, string) {
	for {
		done, ok, code := fn()
		if done {
			return ok, code
		}
		select {
		case <-ctx.Done():
			return false, "timeout"
		case <-time.After(every):
		}
	}
}

type capacityRunContextKey struct{}

func (w *Worker) sendModule(ctx context.Context, r *workerRun, stream string, k uint64) (result sendResult) {
	ctx = context.WithValue(ctx, capacityRunContextKey{}, r.req.RunID)
	var resourceKind, resourceID string
	defer func() { result.resourceKind, result.resourceID = resourceKind, resourceID }()
	cfg := r.req.Config
	mc := cfg.Modules
	st := r.modules
	c := r.http.query
	switch stream {
	case "ai":
		if mc.AIMaxRuns > 0 && st.aiRuns.Add(1) > int64(mc.AIMaxRuns) {
			return sendResult{code: "budget_exhausted", attempts: 1}
		}
		if len(st.alarms) == 0 {
			return sendResult{code: "no_active_alarm", attempts: 1}
		}
		alarm := st.alarms[k%uint64(len(st.alarms))]
		rctx, cancel := context.WithTimeout(ctx, max(mc.AITimeout.D(), time.Minute))
		defer cancel()
		status, body, err := jsonCall(rctx, c, http.MethodPost, cfg.API+"/api/v1/ai/alarm-analysis/"+url.PathEscape(alarm)+"/run", cfg.OperatorToken, nil)
		if err != nil || (status != 202 && status != 200) {
			return sendResult{code: codeOf(status, err), attempts: 1}
		}
		var job struct {
			JobID string `json:"jobId"`
		}
		_ = json.Unmarshal(body, &job)
		resourceKind, resourceID = "alarm-analysis", job.JobID
		ok, code := poll(rctx, 500*time.Millisecond, func() (bool, bool, string) {
			s, b, e := jsonCall(rctx, c, http.MethodGet, cfg.API+"/api/v1/ai/alarm-analysis/"+url.PathEscape(alarm)+"/progress/"+url.PathEscape(job.JobID), cfg.OperatorToken, nil)
			if e != nil || s != 200 {
				return false, false, ""
			}
			var v struct{ Status string }
			_ = json.Unmarshal(b, &v)
			switch v.Status {
			case "succeeded":
				return true, true, "succeeded"
			case "failed":
				return true, false, "failed"
			}
			return false, false, ""
		})
		return sendResult{ok: ok, code: code, attempts: 1}
	case "knowledge":
		n := st.knowledge.Add(1)
		doc := knowledgeDocument(r.req.RunID, n, mc.KnowledgeBytes)
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", fmt.Sprintf("capacity-%s-%d-%d.md", r.req.RunID, r.req.AgentIndex, n))
		_, _ = fw.Write(doc)
		_ = mw.WriteField("workflowId", mc.KnowledgeWorkflow)
		_ = mw.WriteField("category", "capacity-test")
		_ = mw.Close()
		rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		req, _ := http.NewRequestWithContext(rctx, http.MethodPost, cfg.API+"/api/v1/knowledge/documents", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Capacity-Run-ID", r.req.RunID)
		req.Header.Set("Authorization", "Bearer "+cfg.OperatorToken)
		resp, err := c.Do(req)
		if err != nil {
			return sendResult{code: ShortError(err), attempts: 1}
		}
		var docResult struct {
			ID string `json:"id"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&docResult)
		resourceKind, resourceID = "knowledge", docResult.ID
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
			return sendResult{code: strconv.Itoa(resp.StatusCode), bytes: len(doc), attempts: 1}
		}
		if decodeErr != nil || strings.TrimSpace(docResult.ID) == "" {
			return sendResult{code: "invalid_upload_response", bytes: len(doc), attempts: 1}
		}
		if resp.StatusCode == http.StatusCreated {
			// In-memory implementations complete indexing synchronously.
			return sendResult{ok: true, code: "201", bytes: len(doc), attempts: 1}
		}
		// A durable upload is only accepted at 202. Measure the whole operation,
		// including extraction, embedding API calls and committed indexing.
		ok, code := poll(rctx, 500*time.Millisecond, func() (bool, bool, string) {
			status, body, err := jsonCall(rctx, c, http.MethodGet, cfg.API+"/api/v1/knowledge/documents/"+url.PathEscape(docResult.ID), cfg.OperatorToken, nil)
			if err != nil || status >= http.StatusInternalServerError {
				return false, false, ""
			}
			if status != http.StatusOK {
				return true, false, "index_status_" + strconv.Itoa(status)
			}
			var detail struct {
				Document struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"document"`
			}
			if json.Unmarshal(body, &detail) != nil || detail.Document.ID != docResult.ID {
				return true, false, "invalid_index_response"
			}
			switch strings.ToUpper(strings.TrimSpace(detail.Document.Status)) {
			case "INDEXED":
				return true, true, "indexed"
			case "INDEX_FAILED":
				return true, false, "index_failed"
			case "DELETING":
				return true, false, "deleting"
			}
			return false, false, ""
		})
		return sendResult{ok: ok, code: code, bytes: len(doc), attempts: 1}
	case "video":
		camera := mc.VideoCameras[k%uint64(len(mc.VideoCameras))]
		rctx, cancel := context.WithTimeout(ctx, max(mc.VideoTimeout.D(), 10*time.Second))
		defer cancel()
		status, body, err := jsonCall(rctx, c, http.MethodPost, cfg.API+"/api/v1/video/cameras/"+url.PathEscape(camera)+"/play-sessions", cfg.OperatorToken, map[string]string{"stream": "sub", "protocol": "hls"})
		if err != nil || status != 201 {
			return sendResult{code: codeOf(status, err), attempts: 1}
		}
		var grant struct {
			SessionID string `json:"sessionId"`
			HLSURL    string `json:"hlsUrl"`
		}
		_ = json.Unmarshal(body, &grant)
		defer func() {
			stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			_, _, _ = jsonCall(stopCtx, c, http.MethodDelete, cfg.API+"/api/v1/video/play-sessions/"+url.PathEscape(grant.SessionID), cfg.OperatorToken, nil)
			stop()
		}()
		if mc.WebURL == "" || grant.HLSURL == "" {
			return sendResult{ok: true, code: "session_only", attempts: 1}
		}
		// The playlist appears once the media server receives the stream;
		// its first segment approximates the first frame.
		playlist := strings.TrimRight(mc.WebURL, "/") + grant.HLSURL
		var segment string
		ok, code := poll(rctx, 300*time.Millisecond, func() (bool, bool, string) {
			s, b, e := doHTTP(rctx, c, http.MethodGet, playlist, nil, nil)
			if e != nil || s != 200 {
				return false, false, ""
			}
			sc := bufio.NewScanner(bytes.NewReader(b))
			for sc.Scan() {
				if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
					segment = line
					return true, true, "playlist"
				}
			}
			return false, false, ""
		})
		if !ok {
			return sendResult{code: "no_playlist_" + code, attempts: 1}
		}
		ref, err := url.Parse(segment)
		base, _ := url.Parse(playlist)
		if err == nil && base != nil {
			if s, b, e := doHTTP(rctx, c, http.MethodGet, base.ResolveReference(ref).String(), nil, nil); e == nil && s == 200 && len(b) > 0 {
				return sendResult{ok: true, code: "first_segment", bytes: len(b), attempts: 1}
			}
		}
		return sendResult{code: "no_segment", attempts: 1}
	case "export_raw":
		rctx, cancel := context.WithTimeout(ctx, max(mc.ExportTimeout.D(), time.Minute))
		defer cancel()
		status, body, err := jsonCall(rctx, c, http.MethodGet, cfg.API+"/api/v1/raw-messages?page=1&pageSize=50", cfg.OperatorToken, nil)
		if err != nil || status != 200 {
			return sendResult{code: codeOf(status, err), attempts: 1}
		}
		var list struct {
			Items []struct {
				MessageID string `json:"messageId"`
			} `json:"items"`
		}
		_ = json.Unmarshal(body, &list)
		ids := make([]string, 0, len(list.Items))
		for _, it := range list.Items {
			ids = append(ids, it.MessageID)
		}
		if len(ids) == 0 {
			return sendResult{code: "no_raw_messages", attempts: 1}
		}
		status, body, err = jsonCall(rctx, c, http.MethodPost, cfg.API+"/api/v1/raw-messages/download", cfg.OperatorToken, map[string]any{"messageIds": ids})
		return sendResult{ok: err == nil && status == 200 && len(body) > 0, code: codeOf(status, err), bytes: len(body), attempts: 1}
	case "export_replay":
		rctx, cancel := context.WithTimeout(ctx, max(mc.ExportTimeout.D(), time.Minute))
		defer cancel()
		now := time.Now()
		status, body, err := jsonCall(rctx, c, http.MethodPost, cfg.API+"/api/v1/raw-messages/replay", cfg.OperatorToken, map[string]any{"mode": "DRY_RUN", "start": now.Add(-10 * time.Minute).UnixMilli(), "end": now.UnixMilli(), "ratePerSecond": 1000})
		if err != nil || status != 202 {
			return sendResult{code: codeOf(status, err), attempts: 1}
		}
		var task struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(body, &task)
		resourceKind, resourceID = "replay", task.ID
		ok, code := poll(rctx, time.Second, func() (bool, bool, string) {
			s, b, e := jsonCall(rctx, c, http.MethodGet, cfg.API+"/api/v1/replays/"+url.PathEscape(task.ID), cfg.OperatorToken, nil)
			if e != nil || s != 200 {
				return false, false, ""
			}
			var v struct{ Status string }
			_ = json.Unmarshal(b, &v)
			switch v.Status {
			case "COMPLETED":
				return true, true, "completed"
			case "FAILED", "INTERRUPTED":
				return true, false, strings.ToLower(v.Status)
			}
			return false, false, ""
		})
		return sendResult{ok: ok, code: code, attempts: 1}
	case "export_inspection":
		rctx, cancel := context.WithTimeout(ctx, max(mc.ExportTimeout.D(), 3*time.Minute))
		defer cancel()
		status, body, err := jsonCall(rctx, c, http.MethodPost, cfg.API+"/api/v1/ai/health-inspection/run", cfg.OperatorToken, nil)
		if err != nil || (status != 202 && status != 200) {
			return sendResult{code: codeOf(status, err), attempts: 1}
		}
		var job struct {
			JobID string `json:"jobId"`
		}
		_ = json.Unmarshal(body, &job)
		resourceKind, resourceID = "inspection", job.JobID
		ok, code := poll(rctx, time.Second, func() (bool, bool, string) {
			s, b, e := jsonCall(rctx, c, http.MethodGet, cfg.API+"/api/v1/ai/health-inspection/progress/"+url.PathEscape(job.JobID), cfg.OperatorToken, nil)
			if e != nil || s != 200 {
				return false, false, ""
			}
			var v struct{ Status string }
			_ = json.Unmarshal(b, &v)
			switch v.Status {
			case "succeeded":
				return true, true, ""
			case "failed":
				return true, false, "inspection_failed"
			}
			return false, false, ""
		})
		if !ok {
			return sendResult{code: code, attempts: 1}
		}
		status, pdf, err := jsonCall(rctx, c, http.MethodPost, cfg.API+"/api/v1/ai/health-inspection/pdf?jobId="+url.QueryEscape(job.JobID), cfg.OperatorToken, nil)
		return sendResult{ok: err == nil && status == 200 && len(pdf) > 0, code: codeOf(status, err), bytes: len(pdf), attempts: 1}
	case "openapi":
		rctx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout.D())
		defer cancel()
		status, body, err := doHTTP(rctx, c, http.MethodGet, cfg.API+"/api/open/v1/devices?page=1&pageSize=20", nil, map[string]string{"X-API-Key": mc.OpenAPIKey})
		return sendResult{ok: err == nil && status/100 == 2, code: codeOf(status, err), bytes: len(body), attempts: 1}
	}
	return sendResult{code: "unknown_stream", attempts: 1}
}

func codeOf(status int, err error) string {
	if err != nil {
		return ShortError(err)
	}
	return strconv.Itoa(status)
}

// knowledgeDocument builds a deterministic Markdown document of about n bytes.
func knowledgeDocument(runID string, seq int64, n int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# 容量测试知识文档 %s-%d\n\n", runID, seq)
	line := "消防设备巡检记录：检查烟感、温感与手动报警按钮，记录压力、电压与信号强度，确认联动与恢复流程。\n"
	for b.Len() < n {
		b.WriteString(line)
	}
	return []byte(b.String())
}

// realtimeSubscribers are MQTT sessions subscribed like browser pages to the
// tenant's alarm and device-state pushes.
type realtimeSubscribers struct {
	mu       sync.Mutex
	clients  []mqtt.Client
	failures map[string]int
	rec      *streamRecorder
	measure  atomic.Bool
}

func connectRealtime(ctx context.Context, c *http.Client, cfg AgentConfig, n int, runID string) *realtimeSubscribers {
	rs := &realtimeSubscribers{rec: &streamRecorder{}, failures: map[string]int{}}
	for i := 0; i < n; i++ {
		status, body, err := jsonCall(ctx, c, http.MethodPost, cfg.API+"/api/v1/mqtt/token", cfg.OperatorToken, nil)
		var tok struct {
			Username      string   `json:"username"`
			Token         string   `json:"token"`
			Subscriptions []string `json:"subscriptions"`
		}
		if err != nil || status != 200 || json.Unmarshal(body, &tok) != nil {
			rs.failures["token_"+codeOf(status, err)]++
			continue
		}
		filters := map[string]byte{}
		for _, s := range tok.Subscriptions {
			if strings.Contains(s, "/iot/alarm/") || strings.Contains(s, "/iot/device/state/") {
				filters[s] = 0
			}
		}
		if len(filters) == 0 {
			rs.failures["no_alarm_or_state_scope"]++
			continue
		}
		o := mqtt.NewClientOptions().AddBroker(cfg.MQTT).SetClientID(fmt.Sprintf("cap-ui-%s-%d", runID[max(0, len(runID)-6):], i)).SetUsername(tok.Username).SetPassword(tok.Token).SetAutoReconnect(true).SetCleanSession(true).SetConnectTimeout(cfg.RequestTimeout.D())
		client := mqtt.NewClient(o)
		if t := client.Connect(); !t.WaitTimeout(cfg.RequestTimeout.D()) {
			rs.failures["connect_timeout"]++
			continue
		} else if err := t.Error(); err != nil {
			rs.failures[realtimeConnectCode(err)]++
			continue
		}
		t := client.SubscribeMultiple(filters, rs.receive)
		if !t.WaitTimeout(cfg.RequestTimeout.D()) || t.Error() != nil || subscribeDenied(t.(*mqtt.SubscribeToken).Result()) {
			client.Disconnect(100)
			rs.failures["subscribe_denied"]++
			continue
		}
		rs.mu.Lock()
		rs.clients = append(rs.clients, client)
		rs.mu.Unlock()
	}
	return rs
}

func realtimeConnectCode(err error) string {
	switch {
	case errors.Is(err, packets.ErrorRefusedBadUsernameOrPassword):
		return "connect_bad_credentials"
	case errors.Is(err, packets.ErrorRefusedNotAuthorised):
		return "connect_not_authorized"
	}
	return "connect_error"
}

// subscribeDenied reports a SUBACK failure code (0x80) for any filter.
func subscribeDenied(granted map[string]byte) bool {
	for _, q := range granted {
		if q == 0x80 {
			return true
		}
	}
	return false
}

// receive records push latency from the event time (alarm trigger or
// device report time, both from the device's timestamp) to delivery.
func (rs *realtimeSubscribers) receive(_ mqtt.Client, m mqtt.Message) {
	if !rs.measure.Load() {
		return
	}
	var v struct {
		LastTriggeredAt int64 `json:"lastTriggeredAt"`
		LastSeenAt      int64 `json:"lastSeenAt"`
	}
	if json.Unmarshal(m.Payload(), &v) != nil {
		return
	}
	at := v.LastTriggeredAt
	if at == 0 {
		at = v.LastSeenAt
	}
	if at == 0 {
		return
	}
	latency := float64(time.Now().UnixMilli() - at)
	rs.rec.done(true, true, "delivered", latency, 0, 1, len(m.Payload()))
}

func (rs *realtimeSubscribers) close() {
	if rs == nil {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, c := range rs.clients {
		c.Disconnect(100)
	}
}
