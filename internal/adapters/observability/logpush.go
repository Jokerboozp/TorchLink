package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LokiPush ships a process's own structured logs to Loki. It is meant for
// processes started outside Docker (local source debugging); containers are
// collected by the log collector instead, so the two paths never duplicate.
// Delivery is best effort: a full queue drops entries instead of blocking.
type LokiPush struct {
	url     string
	tenant  string
	labels  map[string]string
	queue   chan pushedLine
	client  *http.Client
	dropped atomic.Int64
	done    chan struct{}
	once    sync.Once
}

type pushedLine struct {
	ts   int64
	line string
}

var levelPattern = regexp.MustCompile(`"level":"([A-Za-z]+)"`)

func NewLokiPush(baseURL, tenant, service string) *LokiPush {
	host, _ := os.Hostname()
	p := &LokiPush{
		url:    strings.TrimRight(baseURL, "/") + "/loki/api/v1/push",
		tenant: tenant,
		labels: map[string]string{"service_name": service, "host": host, "source": "push"},
		queue:  make(chan pushedLine, 20000),
		client: &http.Client{Timeout: 10 * time.Second},
		done:   make(chan struct{}),
	}
	go p.run()
	return p
}

// Write receives exactly one JSON log line per call from slog.JSONHandler.
func (p *LokiPush) Write(b []byte) (int, error) {
	line := strings.TrimRight(string(b), "\n")
	select {
	case p.queue <- pushedLine{ts: time.Now().UnixNano(), line: line}:
	default:
		p.dropped.Add(1)
	}
	return len(b), nil
}

func (p *LokiPush) Dropped() int64 { return p.dropped.Load() }

// Close flushes queued lines until ctx expires.
func (p *LokiPush) Close(ctx context.Context) {
	p.once.Do(func() { close(p.queue) })
	select {
	case <-p.done:
	case <-ctx.Done():
	}
}

func (p *LokiPush) run() {
	defer close(p.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	batch := make([]pushedLine, 0, 500)
	flush := func() {
		if len(batch) > 0 {
			p.send(batch)
			batch = batch[:0]
		}
	}
	for {
		select {
		case line, ok := <-p.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, line)
			if len(batch) >= 500 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (p *LokiPush) send(batch []pushedLine) {
	streams := map[string][][2]string{}
	for _, item := range batch {
		level := "unknown"
		if m := levelPattern.FindStringSubmatch(item.line); len(m) == 2 {
			level = normalizeLevel(m[1])
		}
		streams[level] = append(streams[level], [2]string{strconv.FormatInt(item.ts, 10), item.line})
	}
	payload := map[string]any{"streams": []any{}}
	for level, values := range streams {
		labels := map[string]string{"level": level}
		for k, v := range p.labels {
			labels[k] = v
		}
		payload["streams"] = append(payload["streams"].([]any), map[string]any{"stream": labels, "values": values})
	}
	body, _ := json.Marshal(payload)
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodPost, p.url, bytes.NewReader(body))
		if err != nil {
			break
		}
		req.Header.Set("Content-Type", "application/json")
		if p.tenant != "" {
			req.Header.Set("X-Scope-OrgID", p.tenant)
		}
		resp, err := p.client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return
			}
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				break
			}
		}
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if p.dropped.Add(int64(len(batch))) == int64(len(batch)) {
		// Report the first loss on stderr only; logging through slog would loop.
		fmt.Fprintln(os.Stderr, "log push to Loki failed; entries remain on stdout")
	}
}

func normalizeLevel(level string) string {
	switch strings.ToLower(level) {
	case "debug", "trace":
		return "debug"
	case "info", "information":
		return "info"
	case "warn", "warning":
		return "warn"
	case "error", "err":
		return "error"
	case "fatal", "panic", "critical":
		return "fatal"
	}
	return "unknown"
}

// TeeHandler writes each record to the primary handler (stdout) and to a
// second handler that feeds Loki.
type TeeHandler struct{ primary, secondary slog.Handler }

func NewTeeHandler(primary, secondary slog.Handler) *TeeHandler {
	return &TeeHandler{primary: primary, secondary: secondary}
}

func (t *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.primary.Enabled(ctx, level) || t.secondary.Enabled(ctx, level)
}

func (t *TeeHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	if t.primary.Enabled(ctx, r.Level) {
		err = t.primary.Handle(ctx, r.Clone())
	}
	if t.secondary.Enabled(ctx, r.Level) {
		_ = t.secondary.Handle(ctx, r)
	}
	return err
}

func (t *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TeeHandler{primary: t.primary.WithAttrs(attrs), secondary: t.secondary.WithAttrs(attrs)}
}

func (t *TeeHandler) WithGroup(name string) slog.Handler {
	return &TeeHandler{primary: t.primary.WithGroup(name), secondary: t.secondary.WithGroup(name)}
}
