package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/netguard"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/capacity"
)

// The capacity page drives the capacity controller service (capacity-test
// serve) through these routes. The browser only names a trusted environment
// and sends plan text; addresses, secrets and fault commands stay on the
// controller host. Access follows the ops tenant boundary (opsCapacity menu).

var capacityRunID = regexp.MustCompile(`^cap-[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)

func (s *Server) capacityEnvironments(w http.ResponseWriter, r *http.Request) {
	s.proxyCapacity(w, r, http.MethodGet, "/v1/environments", nil, 30*time.Second)
}

func (s *Server) capacityValidate(w http.ResponseWriter, r *http.Request) {
	body, ok := s.capacityPlanBody(w, r, false)
	if ok {
		s.proxyCapacity(w, r, http.MethodPost, "/v1/plans/validate", body, 30*time.Second)
	}
}

func (s *Server) capacityRuns(w http.ResponseWriter, r *http.Request) {
	query := url.Values{}
	for _, key := range []string{"page", "pageSize"} {
		if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil && v > 0 {
			query.Set(key, strconv.Itoa(v))
		}
	}
	resp, ok := s.callCapacity(w, r, http.MethodGet, "/v1/runs", query, nil, 30*time.Second)
	if !ok {
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	writeRaw(w, capacityStatus(resp.StatusCode), data)
}

func (s *Server) capacityStart(w http.ResponseWriter, r *http.Request) {
	body, ok := s.capacityPlanBody(w, r, true)
	if !ok {
		return
	}
	var req struct {
		Environment string `json:"environment"`
	}
	_ = json.Unmarshal(body, &req)
	status, data, ok := s.callCapacityJSON(w, r, http.MethodPost, "/v1/runs", body, 2*time.Minute)
	if !ok {
		return
	}
	if status == http.StatusAccepted {
		var started struct {
			RunID string `json:"runId"`
		}
		_ = json.Unmarshal(data, &started)
		s.audit(r, "capacity.run.start", "capacity_run", started.RunID, map[string]any{"environment": req.Environment})
	}
	writeRaw(w, status, data)
}

func (s *Server) capacityRun(w http.ResponseWriter, r *http.Request) {
	if id, ok := capacityID(w, r); ok {
		s.proxyCapacity(w, r, http.MethodGet, "/v1/runs/"+id, nil, 30*time.Second)
	}
}

func (s *Server) capacityStop(w http.ResponseWriter, r *http.Request) {
	id, ok := capacityID(w, r)
	if !ok {
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req)
	body, _ := json.Marshal(req)
	status, data, ok := s.callCapacityJSON(w, r, http.MethodPost, "/v1/runs/"+id+"/stop", body, 30*time.Second)
	if !ok {
		return
	}
	if status == http.StatusAccepted {
		s.audit(r, "capacity.run.stop", "capacity_run", id, map[string]any{"force": req.Force})
	}
	writeRaw(w, status, data)
}

func (s *Server) capacityReport(w http.ResponseWriter, r *http.Request) {
	id, ok := capacityID(w, r)
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	switch format {
	case "", "html", "markdown", "json", "csv", "zip":
	default:
		problem(w, http.StatusUnprocessableEntity, "format must be html, markdown, json, csv or zip")
		return
	}
	resp, ok := s.callCapacity(w, r, http.MethodGet, "/v1/runs/"+id+"/report", url.Values{"format": {firstNonEmptyString(format, "html")}}, nil, 10*time.Minute)
	if !ok {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		writeRaw(w, capacityStatus(resp.StatusCode), data)
		return
	}
	for _, h := range []string{"Content-Type", "Content-Disposition"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	s.audit(r, "capacity.report.download", "capacity_run", id, map[string]any{"format": format})
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

func capacityID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !capacityRunID.MatchString(id) {
		problem(w, http.StatusNotFound, "capacity run not found")
		return "", false
	}
	return id, true
}

// capacityPlanBody forwards the browser's environment and plan text and adds
// what only the platform may decide: the caller's tenant (fixtures.tenant is
// always overridden) and, when starting, a token issued for the caller so the
// run acts with exactly that user's permissions and device scope. A managed
// user's token dies with a session change (password, permissions, disable).
func (s *Server) capacityPlanBody(w http.ResponseWriter, r *http.Request, start bool) ([]byte, bool) {
	var req struct {
		Environment   string `json:"environment"`
		Plan          string `json:"plan"`
		Tenant        string `json:"tenant"`
		OperatorToken string `json:"operatorToken,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 300<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Plan) == "" {
		problem(w, http.StatusUnprocessableEntity, "请求需要 environment 与 plan（YAML 文本，最大 256 KiB）")
		return nil, false
	}
	c := claims(r)
	req.Tenant, req.OperatorToken = c.TenantID, ""
	if start {
		// The credential covers the whole budget (the controller rejects
		// plans longer than capacity.MaxWallTime).
		ttl := capacity.MaxOperatorTokenTTL
		if p, err := capacity.ParsePlan([]byte(req.Plan)); err == nil && p.Budget.MaximumWallTime.D() > 0 {
			ttl = capacity.OperatorTokenTTL(p)
		}
		token, err := s.reissueToken(c, ttl)
		if err != nil {
			s.fail(w, r, err, "无法为容量测试签发操作凭据")
			return nil, false
		}
		req.OperatorToken = token
	}
	b, _ := json.Marshal(req)
	return b, true
}

// capacityStatus reports whether the capacity module is deployed; the page
// hides itself when it is not.
func (s *Server) capacityModuleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Ops
	enabled := cfg.CapacityURL != "" && cfg.CapacityToken != ""
	out := map[string]any{"enabled": enabled, "reachable": false}
	if enabled {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.CapacityURL, "/")+"/health", nil); err == nil {
			if resp, err := (&http.Client{Transport: netguard.Direct()}).Do(req); err == nil {
				resp.Body.Close()
				out["reachable"] = resp.StatusCode == http.StatusOK
			}
		}
	}
	write(w, http.StatusOK, out)
}

func (s *Server) proxyCapacity(w http.ResponseWriter, r *http.Request, method, path string, body []byte, timeout time.Duration) {
	if status, data, ok := s.callCapacityJSON(w, r, method, path, body, timeout); ok {
		writeRaw(w, status, data)
	}
}

func (s *Server) callCapacityJSON(w http.ResponseWriter, r *http.Request, method, path string, body []byte, timeout time.Duration) (int, []byte, bool) {
	resp, ok := s.callCapacity(w, r, method, path, nil, body, timeout)
	if !ok {
		return 0, nil, false
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return capacityStatus(resp.StatusCode), data, true
}

// capacityStatus keeps client-meaningful statuses and maps the rest to 502;
// an upstream 401 means a platform misconfiguration, not the user's session.
func capacityStatus(code int) int {
	switch code {
	case 200, 202, 400, 404, 409, 422:
		return code
	}
	return http.StatusBadGateway
}

func (s *Server) callCapacity(w http.ResponseWriter, r *http.Request, method, path string, query url.Values, body []byte, timeout time.Duration) (*http.Response, bool) {
	cfg := s.cfg.Ops
	if cfg.CapacityURL == "" || cfg.CapacityToken == "" {
		opsProblem(w, http.StatusServiceUnavailable, "CAPACITY_NOT_CONFIGURED", "容量测试控制服务未配置：在控制机运行 capacity-test serve，并设置 IOT_OPS_CAPACITY_URL 与 IOT_OPS_CAPACITY_TOKEN 后重启平台，详见 docs/DEVELOPMENT.md", nil)
		return nil, false
	}
	base, err := url.Parse(cfg.CapacityURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		opsProblem(w, http.StatusServiceUnavailable, "CAPACITY_NOT_CONFIGURED", "IOT_OPS_CAPACITY_URL 无效", nil)
		return nil, false
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = query.Encode()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, base.String(), rd)
	if err != nil {
		opsProblem(w, http.StatusBadGateway, "CAPACITY_UNAVAILABLE", "无法创建容量测试服务请求", nil)
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+cfg.CapacityToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: timeout, Transport: netguard.Direct()}).Do(req)
	if err != nil {
		if r.Context().Err() == nil {
			opsProblem(w, http.StatusBadGateway, "CAPACITY_UNAVAILABLE", "容量测试控制服务不可用", nil)
		}
		return nil, false
	}
	return resp, true
}

func writeRaw(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
