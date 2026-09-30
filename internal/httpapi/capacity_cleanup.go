package httpapi

import (
	"context"
	"crypto/subtle"
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

const capacityCleanupPermission = "DELETE /api/v1/ops/capacity/runs/:id"
const capacityCleanupDataPath = "/api/v1/ops/capacity/cleanup-data"

type capacityJobContextKey struct{}

func capacityRequestRunID(r *http.Request) string {
	run := r.Header.Get("X-Capacity-Run-ID")
	if capacityRunID.MatchString(run) {
		return run
	}
	return ""
}
func capacityJobContext(r *http.Request) context.Context {
	return context.WithValue(r.Context(), capacityJobContextKey{}, capacityRequestRunID(r))
}
func capacityJobRun(ctx context.Context) string {
	run, _ := ctx.Value(capacityJobContextKey{}).(string)
	return run
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
	var token string
	var err error
	if c.TokenUse == "user" {
		token, err = s.auth.IssueUser(c.Username, c.TenantID, c.SessionVersion, time.Hour)
	} else {
		token, err = s.auth.Issue(c.Username, c.TenantID, c.Role, nil, time.Hour)
	}
	if err != nil {
		problem(w, 500, "无法签发清理操作凭据")
		return
	}
	body, _ := json.Marshal(map[string]string{"tenant": c.TenantID, "operatorToken": token})
	status, data, ok := s.callCapacityJSON(w, r, http.MethodDelete, "/v1/runs/"+id, body, 30*time.Minute)
	if !ok {
		return
	}
	s.audit(r, "capacity.run.cleanup", "capacity_run", id, map[string]any{"status": status})
	writeRaw(w, status, data)
}

// Only the configured capacity controller and an authorized current operator
// can call this callback. Browsers never choose message or fixture IDs.
func (s *Server) capacityCleanupData(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get("X-Capacity-Service-Token")
	if s.cfg.Ops.CapacityToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Ops.CapacityToken)) != 1 || limited(r.Context()) {
		problem(w, 403, "capacity controller and full tenant device scope required")
		return
	}
	var q model.CapacityCleanupBatch
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&q) != nil || !capacityRunID.MatchString(q.RunID) || q.Product == "" || len(q.RawIDs) > 500 || len(q.Devices) > 10000 || len(q.Resources) > 500 {
		problem(w, 400, "invalid capacity cleanup batch")
		return
	}
	tenant := claims(r).TenantID
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
		if d.ProductID != q.Product || d.Name != "容量测试 "+id || d.RegistrationSource != "ONBOARDING" || d.GatewayID != "" {
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
	var documentsDeleted int64
	for _, resource := range q.Resources {
		if !slices.Contains([]string{"knowledge", "inspection", "alarm-analysis", "replay"}, resource.Kind) {
			problem(w, 400, "invalid cleanup resource")
			return
		}
		if resource.Kind != "knowledge" {
			continue
		}
		docs, err := s.engine.Repo.ListKnowledgeDocs(r.Context(), tenant)
		if err != nil {
			capacityDataError(w, err)
			return
		}
		for _, doc := range docs {
			if doc.ID != resource.ID {
				continue
			}
			if doc.Category != "capacity-test" || doc.Metadata["capacityRunId"] != q.RunID {
				continue
			}
			index, indexed := s.engine.KB.(ports.KnowledgeDocumentDeleter)
			objects, stored := s.engine.Archive.(ports.ObjectDeleter)
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
		if idx.ObjectOffset != 0 {
			return model.ErrResourceInUse // Never remove a shared segmented archive.
		}
		deleter, ok := s.engine.Archive.(ports.ObjectDeleter)
		if !ok {
			return errors.New("raw object storage cannot delete objects")
		}
		return deleter.DeleteObject(r.Context(), idx.ObjectBucket, idx.ObjectKey)
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
	n, err := repo.CleanupCapacityData(r.Context(), tenant, q)
	if err != nil {
		capacityDataError(w, err)
		return
	}
	n.Resources += documentsDeleted
	if cache, ok := s.engine.RawStore.(interface {
		ForgetCapacityDevices(context.Context, string, []string) error
	}); ok {
		if err = cache.ForgetCapacityDevices(r.Context(), tenant, q.Devices); err != nil {
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
	if q.RemoveProduct {
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
		problem(w, 409, "测试数据仍在处理或设备仍被引用，请待处理完成或解除引用后重试")
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		problem(w, 409, "清理超时，可保留记录后重试")
		return
	}
	problem(w, 500, "测试数据清理失败，可保留记录后重试")
}
