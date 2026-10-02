package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const capacityHistoryPath = "/api/v1/ops/capacity/cleanup/history"
const capacityHistoryStatusPath = capacityHistoryPath + "/status"
const capacityFixturePath = "/api/v1/ops/capacity/cleanup-fixtures"

func capacityCleanupRoute(path string) bool {
	return path == capacityCleanupPreviewPath || path == capacityCleanupDataPath || path == capacityHistoryPath || path == capacityHistoryStatusPath || path == capacityFixturePath
}

func (s *Server) capacityOperatorToken(r *http.Request) (string, error) {
	c := claims(r)
	if c.TokenUse == "user" {
		return s.auth.IssueUser(c.Username, c.TenantID, c.SessionVersion, time.Hour)
	}
	return s.auth.Issue(c.Username, c.TenantID, c.Role, nil, time.Hour)
}

func (s *Server) capacityHistory(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, 403, "清理容量测试需要全租户设备范围")
		return
	}
	var body struct {
		Environment  string `json:"environment"`
		PreviewToken string `json:"previewToken"`
	}
	path := "/v1/cleanup/history/preview"
	if r.Method == http.MethodPost {
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body) != nil || body.PreviewToken == "" {
			problem(w, 400, "请先预览历史测试数据")
			return
		}
		path = "/v1/cleanup/history"
	} else {
		body.Environment = r.URL.Query().Get("environment")
	}
	token, err := s.capacityOperatorToken(r)
	if err != nil {
		problem(w, 500, "无法签发清理操作凭据")
		return
	}
	data, _ := json.Marshal(map[string]string{"environment": body.Environment, "previewToken": body.PreviewToken, "tenant": claims(r).TenantID, "operatorToken": token})
	status, response, ok := s.callCapacityJSON(w, r, http.MethodPost, path, data, 2*time.Minute)
	if !ok {
		return
	}
	if r.Method == http.MethodPost && status == http.StatusAccepted {
		s.audit(r, "capacity.history.cleanup", "capacity_history", body.Environment, map[string]any{"status": status})
	}
	writeRaw(w, status, response)
}

func (s *Server) capacityHistoryStatus(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, 403, "清理容量测试需要全租户设备范围")
		return
	}
	query := url.Values{"tenant": {claims(r).TenantID}, "environment": {r.URL.Query().Get("environment")}}
	resp, ok := s.callCapacity(w, r, http.MethodGet, "/v1/cleanup/history/status", query, nil, 30*time.Second)
	if !ok {
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	writeRaw(w, capacityStatus(resp.StatusCode), data)
}

func (s *Server) capacityControllerScope(w http.ResponseWriter, r *http.Request) bool {
	got := r.Header.Get("X-Capacity-Service-Token")
	if s.cfg.Ops.CapacityToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Ops.CapacityToken)) != 1 || limited(r.Context()) {
		problem(w, 403, "capacity controller and full tenant device scope required")
		return false
	}
	return true
}

// This internal callback discovers fixtures from repository evidence. Neither
// a browser nor a controller can invent a device list for historical cleanup.
func (s *Server) capacityFixtures(w http.ResponseWriter, r *http.Request) {
	if !s.capacityControllerScope(w, r) {
		return
	}
	lister, ok := s.unscopedRepo().(ports.CapacityFixtureLister)
	if !ok {
		problem(w, 501, "当前存储不支持历史测试数据识别")
		return
	}
	tenant := claims(r).TenantID
	if r.Method == http.MethodPost {
		var body struct {
			Product     string `json:"product"`
			Fingerprint string `json:"fingerprint"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body) != nil || body.Product == "" || body.Fingerprint == "" {
			problem(w, 400, "缺少历史测试范围指纹")
			return
		}
		if err := lister.PrepareCapacityFixture(r.Context(), tenant, body.Product, body.Fingerprint); err != nil {
			capacityDataError(w, err)
			return
		}
		write(w, 200, map[string]bool{"prepared": true})
		return
	}
	product := r.URL.Query().Get("product")
	if product != "" {
		_, err := s.unscopedRepo().GetProduct(r.Context(), tenant, product)
		if errors.Is(err, model.ErrNotFound) {
			write(w, 200, map[string]any{"devices": []string{}, "nextAfter": ""})
			return
		}
		if err != nil {
			capacityDataError(w, err)
			return
		}
		devices, err := lister.ListCapacityFixtureDevices(r.Context(), tenant, product, r.URL.Query().Get("after"), 1000)
		if err != nil {
			capacityDataError(w, err)
			return
		}
		next := ""
		if len(devices) == 1000 {
			next = devices[len(devices)-1]
		}
		if devices == nil {
			devices = []string{}
		}
		write(w, 200, map[string]any{"devices": devices, "nextAfter": next})
		return
	}
	products, err := lister.ListCapacityFixtureProducts(r.Context(), tenant, r.URL.Query().Get("after"), 100)
	if err != nil {
		capacityDataError(w, err)
		return
	}
	next := ""
	if len(products) == 100 {
		next = products[len(products)-1].ProductID
	}
	if products == nil {
		products = []model.CapacityFixtureProduct{}
	}
	write(w, 200, map[string]any{"products": products, "nextAfter": next})
}
