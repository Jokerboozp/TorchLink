package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// The browser asks the capacity controller to delete one run or all test
// data; the controller calls back cleanup-fixtures / cleanup-data with the
// operator's delegated token. Ownership is always verified here, from stored
// products and devices, never from a list chosen by the browser.
const (
	capacityCleanupPermission  = "DELETE /api/v1/ops/capacity/runs/:id"
	capacityCleanupPreviewPath = "/api/v1/ops/capacity/runs/:id/cleanup"
	capacityCleanupAllPath     = "/api/v1/ops/capacity/cleanup"
	capacityCleanupStatusPath  = capacityCleanupAllPath + "/status"
	capacityCleanupDataPath    = "/api/v1/ops/capacity/cleanup-data"
	capacityFixturePath        = "/api/v1/ops/capacity/cleanup-fixtures"
)

// Preview, status and the controller callbacks are covered by the cleanup
// permission and are not listed separately in the permission catalog.
func capacityCleanupRoute(path string) bool {
	return slices.Contains([]string{capacityCleanupPreviewPath, capacityCleanupAllPath, capacityCleanupStatusPath, capacityCleanupDataPath, capacityFixturePath}, path)
}

// SetCapacityMQTT lets cleanup clear retained state topics of removed devices.
func (s *Server) SetCapacityMQTT(cleaner ports.CapacityRetainedCleaner) { s.capacityMQTT = cleaner }

// capacityRequestRunID reads the run that a capacity Agent tags its module
// requests with, so the resulting tasks can be removed with that run.
func capacityRequestRunID(r *http.Request) string {
	run := r.Header.Get("X-Capacity-Run-ID")
	if capacityRunID.MatchString(run) {
		return run
	}
	return ""
}

func capacityJobContext(r *http.Request) context.Context {
	return ports.WithCapacityRunID(r.Context(), capacityRequestRunID(r))
}

func (s *Server) capacityOperatorToken(r *http.Request) (string, error) {
	return s.reissueToken(claims(r), time.Hour)
}

func capacityFullScope(w http.ResponseWriter, r *http.Request) bool {
	if limited(r.Context()) {
		problem(w, 403, "清理容量测试需要全租户设备范围")
		return false
	}
	return true
}

// capacityOperatorBody carries the tenant and a delegated operator token to
// the controller; the token never reaches the browser.
func (s *Server) capacityOperatorBody(w http.ResponseWriter, r *http.Request, environment string) ([]byte, bool) {
	token, err := s.capacityOperatorToken(r)
	if err != nil {
		problem(w, 500, "无法签发清理操作凭据")
		return nil, false
	}
	body, _ := json.Marshal(map[string]string{"tenant": claims(r).TenantID, "environment": environment, "operatorToken": token})
	return body, true
}

func (s *Server) capacityCleanupPreview(w http.ResponseWriter, r *http.Request) {
	if !capacityFullScope(w, r) {
		return
	}
	id, ok := capacityID(w, r)
	if !ok {
		return
	}
	resp, ok := s.callCapacity(w, r, http.MethodGet, "/v1/runs/"+id+"/cleanup", url.Values{"tenant": {claims(r).TenantID}}, nil, 2*time.Minute)
	if !ok {
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	writeRaw(w, capacityStatus(resp.StatusCode), data)
}

// capacityCleanup deletes one ended run; the controller works in the background (202).
func (s *Server) capacityCleanup(w http.ResponseWriter, r *http.Request) {
	if !capacityFullScope(w, r) {
		return
	}
	id, ok := capacityID(w, r)
	if !ok {
		return
	}
	body, ok := s.capacityOperatorBody(w, r, "")
	if !ok {
		return
	}
	status, data, ok := s.callCapacityJSON(w, r, http.MethodDelete, "/v1/runs/"+id, body, 2*time.Minute)
	if !ok {
		return
	}
	s.audit(r, "capacity.run.cleanup", "capacity_run", id, map[string]any{"status": status})
	writeRaw(w, status, data)
}

// capacityCleanupAll previews (GET) or starts (POST) cleaning every ended run
// and dedicated test product of an environment.
func (s *Server) capacityCleanupAll(w http.ResponseWriter, r *http.Request) {
	if !capacityFullScope(w, r) {
		return
	}
	environment := r.URL.Query().Get("environment")
	path := "/v1/cleanup/preview"
	if r.Method == http.MethodPost {
		var in struct {
			Environment string `json:"environment"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in) != nil {
			problem(w, 400, "请选择测试环境")
			return
		}
		environment, path = in.Environment, "/v1/cleanup"
	}
	body, ok := s.capacityOperatorBody(w, r, environment)
	if !ok {
		return
	}
	status, data, ok := s.callCapacityJSON(w, r, http.MethodPost, path, body, 2*time.Minute)
	if !ok {
		return
	}
	if r.Method == http.MethodPost && status == http.StatusAccepted {
		s.audit(r, "capacity.cleanup.all", "capacity_environment", environment, map[string]any{"status": status})
	}
	writeRaw(w, status, data)
}

func (s *Server) capacityCleanupStatus(w http.ResponseWriter, r *http.Request) {
	if !capacityFullScope(w, r) {
		return
	}
	query := url.Values{"tenant": {claims(r).TenantID}, "environment": {r.URL.Query().Get("environment")}}
	resp, ok := s.callCapacity(w, r, http.MethodGet, "/v1/cleanup/status", query, nil, 30*time.Second)
	if !ok {
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	writeRaw(w, capacityStatus(resp.StatusCode), data)
}

func (s *Server) capacityControllerScope(w http.ResponseWriter, r *http.Request) (ports.CapacityDataCleaner, bool) {
	got := r.Header.Get("X-Capacity-Service-Token")
	if s.cfg.Ops.CapacityToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Ops.CapacityToken)) != 1 || limited(r.Context()) {
		problem(w, 403, "capacity controller and full tenant device scope required")
		return nil, false
	}
	cleaner, ok := s.unscopedRepo().(ports.CapacityDataCleaner)
	if !ok {
		problem(w, 501, "当前存储不支持容量测试数据清理")
		return nil, false
	}
	return cleaner, true
}

// capacityFixtures lists dedicated test products, or one product's fixture
// devices page by page (?product=&after=).
func (s *Server) capacityFixtures(w http.ResponseWriter, r *http.Request) {
	cleaner, ok := s.capacityControllerScope(w, r)
	if !ok {
		return
	}
	tenant := claims(r).TenantID
	if product := r.URL.Query().Get("product"); product != "" {
		devices, err := cleaner.ListCapacityFixtureDevices(r.Context(), tenant, product, r.URL.Query().Get("after"), 500)
		if err != nil {
			capacityDataError(w, err)
			return
		}
		write(w, 200, map[string]any{"devices": devices})
		return
	}
	products, err := cleaner.ListCapacityFixtureProducts(r.Context(), tenant)
	if err != nil {
		capacityDataError(w, err)
		return
	}
	write(w, 200, map[string]any{"products": products})
}

// capacityCleanupData removes fixture devices with their data, module tasks
// and knowledge documents owned by a run (or every run), and the empty product.
func (s *Server) capacityCleanupData(w http.ResponseWriter, r *http.Request) {
	cleaner, ok := s.capacityControllerScope(w, r)
	if !ok {
		return
	}
	var q model.CapacityCleanupBatch
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&q) != nil || (q.RunID != "" && !capacityRunID.MatchString(q.RunID)) || len(q.Devices) > 1000 || ((len(q.Devices) > 0 || q.RemoveProduct) && q.Product == "") {
		problem(w, 400, "invalid capacity cleanup batch")
		return
	}
	tenant := claims(r).TenantID
	ctx := r.Context()
	// Stop new ingress from devices about to be removed. Ownership and
	// pending processing are rechecked by the storage transaction.
	for _, id := range q.Devices {
		d, err := s.unscopedRepo().GetManagedDevice(ctx, tenant, id)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		p, perr := s.unscopedRepo().GetProduct(ctx, tenant, q.Product)
		if err != nil || perr != nil || !model.CapacityFixtureDevice(p, d) {
			capacityDataError(w, errors.Join(err, perr, model.ErrResourceInUse))
			return
		}
		if d.Status != "DISABLED" {
			d.Status = "DISABLED"
			if err = s.unscopedRepo().SaveManagedDevice(ctx, d); err != nil {
				capacityDataError(w, err)
				return
			}
		}
	}
	// Disabled devices receive nothing new; their receipts still waiting in
	// the MQTT inbox would fail after the devices are removed.
	var inbox int64
	if s.capacityMQTT != nil && len(q.Devices) > 0 {
		var err error
		if inbox, err = s.capacityMQTT.DiscardCapacityInbox(ctx, tenant, q.Product, q.Devices); err != nil {
			capacityDataError(w, err)
			return
		}
	}
	n, err := cleaner.CleanupCapacityData(ctx, tenant, q)
	n.InboxMessages = inbox
	if err != nil {
		capacityDataError(w, err)
		return
	}
	if len(q.Devices) > 0 {
		if cache, ok := s.engine.RawStore.(interface {
			ForgetCapacityDevices(context.Context, string, []string) error
		}); ok {
			if err = cache.ForgetCapacityDevices(ctx, tenant, q.Devices); err != nil {
				capacityDataError(w, err)
				return
			}
		}
		if s.capacityMQTT != nil {
			if n.RetainedRequests, err = s.capacityMQTT.ClearCapacityRetained(ctx, tenant, q.Product, q.Devices); err != nil {
				capacityDataError(w, err)
				return
			}
		}
	}
	if q.RunID != "" || q.AllRuns {
		removed, err := s.removeCapacityKnowledge(ctx, tenant, q)
		n.Resources += removed
		if err != nil {
			capacityDataError(w, err)
			return
		}
	}
	if n.Rules > 0 {
		s.engine.RulesChanged(tenant)
	}
	if n.Products > 0 {
		s.engine.ProtocolsChanged(tenant)
	}
	write(w, 200, n)
}

// removeCapacityKnowledge deletes knowledge documents uploaded by capacity runs.
func (s *Server) removeCapacityKnowledge(ctx context.Context, tenant string, q model.CapacityCleanupBatch) (int64, error) {
	docs, err := s.engine.Repo.ListKnowledgeDocs(ctx, tenant)
	if err != nil {
		return 0, err
	}
	var removed int64
	for _, doc := range docs {
		run := doc.Metadata["capacityRunId"]
		if doc.Category != "capacity-test" || run == "" || (!q.AllRuns && run != q.RunID) {
			continue
		}
		index, indexed := s.engine.KB.(ports.KnowledgeDocumentDeleter)
		objects, stored := s.engine.Archive.(ports.ObjectDeleter)
		if !indexed || !stored {
			return removed, errors.New("knowledge storage does not support cleanup")
		}
		if err = index.DeleteKnowledgeDocument(ctx, tenant, doc.ID, doc.WorkflowID); err == nil {
			err = objects.DeleteObject(ctx, doc.ObjectBucket, doc.ObjectKey)
		}
		if err == nil {
			err = s.engine.Repo.DeleteResource(ctx, tenant, "knowledge", doc.ID)
		}
		if err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func capacityDataError(w http.ResponseWriter, err error) {
	if errors.Is(err, model.ErrResourceInUse) {
		problem(w, 409, "测试数据仍在处理、被引用或不属于专用测试产品，已保留；请处理完成或解除引用后重试")
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		problem(w, 409, "清理超时，可保留记录后重试")
		return
	}
	problem(w, 500, "测试数据清理失败，可保留记录后重试")
}
