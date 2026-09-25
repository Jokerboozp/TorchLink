// Package observability adapts Prometheus, Loki, Grafana and Alertmanager
// HTTP APIs to the platform's ops ports. Every call goes to a fixed,
// configured base URL; callers can never choose the upstream host or path.
package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iot-platform/internal/ports"
)

const maxResponseBytes = 64 << 20

type client struct {
	base    string
	timeout time.Duration
	http    *http.Client
	headers map[string]string
	user    string
	pass    string
}

func newClient(base string, timeout time.Duration) *client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &client{base: strings.TrimRight(base, "/"), timeout: timeout, http: &http.Client{Transport: http.DefaultTransport}, headers: map[string]string{}}
}

func (c *client) configured() bool { return c != nil && c.base != "" }

type request struct {
	method      string
	path        string
	query       url.Values
	body        any
	rawBody     []byte
	contentType string
	timeout     time.Duration
	accept      string
}

// do performs a request and returns the response body for 2xx responses.
// Non-2xx responses become *ports.OpsUpstreamError with the component's own
// message; network failures map to ErrOpsUnavailable / ErrOpsTimeout.
func (c *client) do(ctx context.Context, req request) ([]byte, int, error) {
	if !c.configured() {
		return nil, 0, ports.ErrOpsNotConfigured
	}
	timeout := req.timeout
	if timeout <= 0 {
		timeout = c.timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	target := c.base + req.path
	if len(req.query) > 0 {
		target += "?" + req.query.Encode()
	}
	var body io.Reader
	contentType := req.contentType
	switch {
	case req.rawBody != nil:
		body = bytes.NewReader(req.rawBody)
	case req.body != nil:
		encoded, err := json.Marshal(req.body)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(encoded)
		if contentType == "" {
			contentType = "application/json"
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.method, target, body)
	if err != nil {
		return nil, 0, err
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	accept := req.accept
	if accept == "" {
		accept = "application/json"
	}
	httpReq.Header.Set("Accept", accept)
	for key, value := range c.headers {
		httpReq.Header.Set(key, value)
	}
	if c.user != "" {
		httpReq.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, 0, networkError(ctx, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, resp.StatusCode, networkError(ctx, err)
	}
	if len(data) > maxResponseBytes {
		return nil, resp.StatusCode, &ports.OpsUpstreamError{Status: http.StatusBadGateway, Kind: "too_large", Message: "组件返回的数据过大，请缩小时间范围或增加筛选条件"}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return data, resp.StatusCode, upstreamError(resp.StatusCode, data)
	}
	return data, resp.StatusCode, nil
}

func (c *client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	data, _, err := c.do(ctx, request{method: http.MethodGet, path: path, query: query})
	if err != nil {
		return err
	}
	return decodeJSON(data, out)
}

func decodeJSON(data []byte, out any) error {
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &ports.OpsUpstreamError{Status: http.StatusBadGateway, Kind: "bad_response", Message: "组件返回了无法识别的响应"}
	}
	return nil
}

func networkError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	var netErr net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return ports.ErrOpsTimeout
	}
	return ports.ErrOpsUnavailable
}

// upstreamError extracts the component's message from the common error
// envelopes: Prometheus/Loki {"error"}, Grafana {"message"}, Alertmanager
// plain text. Messages are truncated and never include request URLs.
func upstreamError(status int, data []byte) error {
	message := strings.TrimSpace(string(data))
	kind := ""
	var envelope struct {
		Error     string `json:"error"`
		ErrorType string `json:"errorType"`
		Message   string `json:"message"`
		Status    string `json:"status"`
	}
	if json.Unmarshal(data, &envelope) == nil {
		switch {
		case envelope.Error != "":
			message, kind = envelope.Error, envelope.ErrorType
		case envelope.Message != "":
			message, kind = envelope.Message, envelope.Status
		}
	}
	if len([]rune(message)) > 600 {
		message = string([]rune(message)[:600]) + "…"
	}
	if message == "" {
		message = fmt.Sprintf("组件返回 HTTP %d", status)
	}
	switch status {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ports.ErrOpsNotFound, message)
	case http.StatusPreconditionFailed, http.StatusConflict:
		return fmt.Errorf("%w: %s", ports.ErrOpsConflict, message)
	}
	return &ports.OpsUpstreamError{Status: status, Kind: kind, Message: message}
}

func unixSeconds(t time.Time) string {
	return fmt.Sprintf("%.3f", float64(t.UnixNano())/1e9)
}

// describe renders an adapter error as a short Chinese message for status
// displays. It never includes addresses or credentials.
func describe(err error) string {
	var upstream *ports.OpsUpstreamError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ports.ErrOpsNotConfigured):
		return "未配置"
	case errors.Is(err, ports.ErrOpsTimeout):
		return "连接超时"
	case errors.Is(err, ports.ErrOpsUnavailable):
		return "无法连接"
	case errors.As(err, &upstream):
		if upstream.Status == http.StatusUnauthorized || upstream.Status == http.StatusForbidden {
			return "认证失败，请检查平台配置的组件凭据"
		}
		return upstream.Message
	}
	return "请求失败"
}
