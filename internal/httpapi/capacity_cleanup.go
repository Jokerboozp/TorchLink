package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/ports"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Preview and the controller callback are covered by the cleanup permission
// and are not listed separately in the permission catalog.
const (
	capacityCleanupPermission  = "DELETE /api/v1/ops/capacity/runs/:id"
	capacityCleanupPreviewPath = "/api/v1/ops/capacity/runs/:id/cleanup"
	capacityCleanupDataPath    = "/api/v1/ops/capacity/cleanup-data"
)

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

func (s *Server) capacityCleanupPreview(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, 403, "清理容量测试需要全租户设备范围")
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

func (s *Server) capacityCleanup(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, 403, "清理容量测试需要全租户设备范围")
		return
	}
	id, ok := capacityID(w, r)
	if !ok {
		return
	}
	c := claims(r)
	token, err := s.capacityOperatorToken(r)
	if err != nil {
		problem(w, 500, "无法签发清理操作凭据")
		return
	}
	body, _ := json.Marshal(map[string]string{"tenant": c.TenantID, "operatorToken": token})
	// The controller checks the scope and cleans in the background (202).
	status, data, ok := s.callCapacityJSON(w, r, http.MethodDelete, "/v1/runs/"+id, body, 2*time.Minute)
	if !ok {
		return
	}
	s.audit(r, "capacity.run.cleanup", "capacity_run", id, map[string]any{"status": status})
	writeRaw(w, status, data)
}

// capacityCleanupData is called only by the configured capacity controller on
// behalf of the current operator; browsers never choose message or device IDs.
func (s *Server) capacityCleanupData(w http.ResponseWriter, r *http.Request) {
	if !s.capacityControllerScope(w, r) {
		return
	}
	var q model.CapacityCleanupBatch
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&q) != nil || !capacityRunID.MatchString(q.RunID) || q.Product == "" || len(q.RawIDs) > 500 || len(q.Devices) > 10000 || len(q.Resources) > 500 {
		problem(w, 400, "invalid capacity cleanup batch")
		return
	}
	tenant := claims(r).TenantID
	var fixtureProduct model.Product
	var finalFixture model.CapacityFixtureProduct
	if q.RemoveProduct && len(q.Devices) == 0 && len(q.RemoveDevices) == 0 {
		p, err := s.unscopedRepo().GetProduct(r.Context(), tenant, q.Product)
		if errors.Is(err, model.ErrNotFound) {
			write(w, 200, model.CapacityCleanupCounts{})
			return
		}
		if err != nil {
			capacityDataError(w, err)
			return
		}
		finalFixture, err = s.capacityFinalFixture(r.Context(), tenant, q.Product)
		if err != nil {
			capacityDataError(w, err)
			return
		}
		// The single-run cleanup also performs a final whole-product sweep.
		// Preparation locks and rechecks the empty canonical fixture first.
		if !q.Historical {
			if model.CapacityFixtureSource(p) != "capacity-test" {
				capacityDataError(w, model.ErrResourceInUse)
				return
			}
			lister := s.unscopedRepo().(ports.CapacityFixtureLister)
			if err = lister.PrepareCapacityFixture(r.Context(), tenant, q.Product, finalFixture.Fingerprint); err != nil {
				capacityDataError(w, err)
				return
			}
			q.Historical = true
		}
	}
	if q.Historical {
		var err error
		fixtureProduct, err = s.unscopedRepo().GetProduct(r.Context(), tenant, q.Product)
		if errors.Is(err, model.ErrNotFound) && len(q.Devices) == 0 && q.RemoveProduct {
			write(w, 200, model.CapacityCleanupCounts{})
			return
		}
		if err != nil || model.CapacityFixtureSource(fixtureProduct) == "" || fixtureProduct.Status != "DISABLED" {
			problem(w, 409, "历史产品不属于已停用的专用容量测试范围")
			return
		}
	}
	for _, id := range q.RemoveDevices {
		if !slices.Contains(q.Devices, id) {
			problem(w, 400, "cleanup device outside run scope")
			return
		}
	}
	for _, id := range q.Devices {
		d, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, id)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			problem(w, 500, "无法核对测试设备")
			return
		}
		owned := d.ProductID == q.Product && d.Name == "容量测试 "+id && d.RegistrationSource == "ONBOARDING" && d.GatewayID == ""
		if q.Historical {
			owned = model.CapacityFixtureDevice(fixtureProduct, d)
		}
		if !owned {
			problem(w, 409, "设备已被改为非测试用途，拒绝清理")
			return
		}
	}
	repo, ok := s.unscopedRepo().(ports.CapacityDataCleaner)
	if !ok {
		problem(w, 501, "当前存储不支持容量数据清理")
		return
	}
	if _, err := repo.CapacityMessageIDs(r.Context(), tenant, q); err != nil {
		capacityDataError(w, err)
		return
	}
	// Stop exclusive test devices from adding new ingress after the pending
	// check. Shared fixtures stay enabled and retain their current caches.
	for _, id := range q.RemoveDevices {
		d, err := s.unscopedRepo().GetManagedDevice(r.Context(), tenant, id)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			capacityDataError(w, err)
			return
		}
		if d.Status != "DISABLED" {
			d.Status = "DISABLED"
			if err = s.unscopedRepo().SaveManagedDevice(r.Context(), d); err != nil {
				capacityDataError(w, err)
				return
			}
		}
	}
	// Knowledge documents live outside the relational store; the adapter
	// removes the other resource kinds with the batch.
	knowledge := map[string]bool{}
	for _, resource := range q.Resources {
		if !slices.Contains([]string{"knowledge", "inspection", "alarm-analysis", "replay"}, resource.Kind) {
			problem(w, 400, "invalid cleanup resource")
			return
		}
		if resource.Kind == "knowledge" {
			knowledge[resource.ID] = true
		}
	}
	var documentsDeleted int64
	if len(knowledge) > 0 {
		docs, err := s.engine.Repo.ListKnowledgeDocs(r.Context(), tenant)
		if err != nil {
			capacityDataError(w, err)
			return
		}
		index, indexed := s.engine.KB.(ports.KnowledgeDocumentDeleter)
		objects, stored := s.engine.Archive.(ports.ObjectDeleter)
		for _, doc := range docs {
			if !knowledge[doc.ID] || doc.Category != "capacity-test" || doc.Metadata["capacityRunId"] != q.RunID {
				continue
			}
			if !indexed || !stored {
				problem(w, 501, "knowledge storage does not support cleanup")
				return
			}
			if err = index.DeleteKnowledgeDocument(r.Context(), tenant, doc.ID, doc.WorkflowID); err == nil {
				err = objects.DeleteObject(r.Context(), doc.ObjectBucket, doc.ObjectKey)
			}
			if err == nil {
				err = s.engine.Repo.DeleteResource(r.Context(), tenant, "knowledge", doc.ID)
			}
			if err != nil {
				capacityDataError(w, err)
				return
			}
			documentsDeleted++
		}
	}
	deleteObject := func(idx model.RawArchiveIndex) error {
		if idx.ObjectBucket == "postgres" || idx.ObjectBucket == "clickhouse" || idx.ObjectBucket == "" {
			return nil
		}
		deleter, ok := s.engine.Archive.(ports.CapacityRawObjectCleaner)
		if !ok {
			return model.ErrResourceInUse
		}
		// The first record of a segmented archive also has offset zero. The
		// adapter must prove the entire object is one owned record before delete.
		return deleter.DeleteCapacityRawObject(r.Context(), tenant, q, idx)
	}
	for _, id := range q.RawIDs {
		idx, err := s.engine.Repo.GetRawIndex(r.Context(), tenant, id)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			capacityDataError(w, err)
			return
		}
		if idx.ProductID != q.Product || !slices.Contains(q.Devices, idx.DeviceID) {
			problem(w, 409, "报文不属于本次测试设备")
			return
		}
		if err = deleteObject(idx); err != nil {
			capacityDataError(w, err)
			return
		}
	}
	// Older fixtures may contain unledgered per-message MinIO objects. Delete
	// them before dropping their indexes so retries still know every object key.
	if len(q.RemoveDevices) > 0 {
		for offset := 0; ; offset += 1000 {
			indexes, err := s.engine.Repo.ListRawIndexes(r.Context(), ports.RawFilter{TenantID: tenant, ProductID: q.Product, DeviceIDs: q.RemoveDevices, Limit: 1000, Offset: offset})
			if err != nil {
				capacityDataError(w, err)
				return
			}
			for _, idx := range indexes {
				if err = deleteObject(idx); err != nil {
					capacityDataError(w, err)
					return
				}
			}
			if len(indexes) < 1000 {
				break
			}
		}
	}
	runtimeCounts, err := s.cleanupCapacityRuntime(r.Context(), tenant, q)
	if err != nil {
		capacityDataError(w, err)
		return
	}
	if finalFixture.ProtocolID != "" {
		if _, err = s.capacityProtocolArtifactPath(tenant, finalFixture.ProtocolID); err != nil {
			q.KeepProtocol = true
			runtimeCounts.Warnings = append(runtimeCounts.Warnings, "测试协议制品无法安全定位，协议及制品已保留")
		}
	}
	n, err := repo.CleanupCapacityData(r.Context(), tenant, q)
	if err != nil {
		capacityDataError(w, err)
		return
	}
	// The relational transaction rechecks private ownership against concurrent
	// bindings. Files are removed only after it actually removed the protocol;
	// a newly shared protocol keeps every artifact intact.
	if finalFixture.ProtocolID != "" && n.Protocols > 0 {
		if err = s.removeCapacityProtocolArtifacts(tenant, finalFixture.ProtocolID); err != nil {
			runtimeCounts.Warnings = append(runtimeCounts.Warnings, "测试协议记录已清理，但制品清理失败，目录已保留")
		}
	}
	if n.Rules > 0 {
		s.engine.RulesChanged(tenant)
	}
	if n.Products > 0 || n.Protocols > 0 {
		s.engine.ProtocolsChanged(tenant)
	}
	n.Resources += documentsDeleted
	n.Inbox += runtimeCounts.Inbox
	n.InboxSkipped += runtimeCounts.InboxSkipped
	n.RetainedRequests += runtimeCounts.RetainedRequests
	n.QueueOffsetSpan += runtimeCounts.QueueOffsetSpan
	n.QueueSkippedPartitions += runtimeCounts.QueueSkippedPartitions
	n.Warnings = append(n.Warnings, runtimeCounts.Warnings...)
	if cache, ok := s.engine.RawStore.(interface {
		ForgetCapacityDevices(context.Context, string, []string) error
	}); ok {
		if err = cache.ForgetCapacityDevices(r.Context(), tenant, q.RemoveDevices); err != nil {
			capacityDataError(w, err)
			return
		}
	}
	if q.RemoveRule != "" {
		rules, err := s.engine.Repo.ListRules(r.Context(), tenant)
		var rule model.AlarmRule
		for _, item := range rules {
			if item.ID == q.RemoveRule {
				rule = item
				break
			}
		}
		if err == nil && rule.ProductID == q.Product && rule.AlarmType == "CAPACITY_TEST" && rule.Name == "容量测试告警 "+q.RemoveRule {
			if err = s.engine.DeleteRule(r.Context(), tenant, q.RemoveRule); err != nil {
				capacityDataError(w, err)
				return
			}
			n.Rules++
		} else if err != nil && !errors.Is(err, model.ErrNotFound) {
			capacityDataError(w, err)
			return
		}
	}
	if q.RemoveProduct && !q.Historical {
		p, err := s.engine.Repo.GetProduct(r.Context(), tenant, q.Product)
		if err == nil && p.ProtocolPackageID == onboarding.StandardPackageID && p.Name == "容量测试标准设备 "+p.ID && strings.HasPrefix(p.Description, "capacity-test 自动创建") {
			if err = s.engine.Repo.DeleteResource(r.Context(), tenant, "product", p.ID); err != nil {
				capacityDataError(w, err)
				return
			}
			n.Products++
		} else if err != nil && !errors.Is(err, model.ErrNotFound) {
			capacityDataError(w, err)
			return
		}
	}
	write(w, 200, n)
}

func capacityDataError(w http.ResponseWriter, err error) {
	if errors.Is(err, model.ErrResourceInUse) {
		problem(w, 409, "测试数据仍在处理、被引用或属于共享归档，已保留；请处理完成或解除引用后重试")
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		problem(w, 409, "清理超时，可保留记录后重试")
		return
	}
	problem(w, 500, "测试数据清理失败，可保留记录后重试")
}
