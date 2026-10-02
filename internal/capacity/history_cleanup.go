package capacity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
)

type HistoryCleanupItem struct {
	ProductID   string `json:"productId,omitempty"`
	RunID       string `json:"runId,omitempty"`
	Title       string `json:"title"`
	Eligible    bool   `json:"eligible"`
	Devices     int64  `json:"devices"`
	RawMessages int64  `json:"rawMessages"`
	Reason      string `json:"reason,omitempty"`
}

type HistoryCleanupPreview struct {
	Token       string               `json:"token"`
	Runs        int                  `json:"runs"`
	Products    int                  `json:"products"`
	Devices     int64                `json:"devices"`
	RawMessages int64                `json:"rawMessages"`
	Items       []HistoryCleanupItem `json:"items"`
	Warnings    []string             `json:"warnings"`
}

// Jobs contain counts and scope metadata only. Delegated credentials never
// enter the results directory, reports or browser response.
type HistoryCleanupJob struct {
	ID          string                      `json:"id"`
	Tenant      string                      `json:"-"`
	Environment string                      `json:"environment"`
	Status      string                      `json:"status"`
	Phase       string                      `json:"phase"`
	Processed   int                         `json:"processed"`
	Total       int                         `json:"total"`
	Counts      model.CapacityCleanupCounts `json:"counts"`
	Error       string                      `json:"error,omitempty"`
	Warnings    []string                    `json:"warnings,omitempty"`
	UpdatedAt   int64                       `json:"updatedAt"`
}

type historyRequest struct {
	Environment   string `json:"environment"`
	Tenant        string `json:"tenant"`
	OperatorToken string `json:"operatorToken"`
	PreviewToken  string `json:"previewToken,omitempty"`
}

type historyScope struct {
	preview  HistoryCleanupPreview
	inv      *Inventory
	runs     []string
	products []model.CapacityFixtureProduct
}

func (s *Service) historyCall(ctx context.Context, inv *Inventory, operator, method, path string, body any, out any) error {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	status, response, err := doHTTP(ctx, newHTTPClient(10*time.Minute, 1), method, strings.TrimRight(inv.API, "/")+path, data, map[string]string{"Authorization": "Bearer " + operator, "X-Capacity-Service-Token": s.opt.Token})
	if err != nil {
		return errors.New("历史测试数据请求失败，已保留清理记录，可重试")
	}
	if status != http.StatusOK {
		return fmt.Errorf("历史测试数据请求返回 HTTP %d，范围可能已变化或仍在处理，请重新预览", status)
	}
	if out != nil {
		return json.Unmarshal(response, out)
	}
	return nil
}

func (s *Service) historyScope(ctx context.Context, req historyRequest) (historyScope, error) {
	out := historyScope{preview: HistoryCleanupPreview{Items: []HistoryCleanupItem{}, Warnings: []string{"备份、监控历史及无法确认测试归属的数据保留；混合业务报告和消息队列可能只能部分清理"}}}
	if !identifier.MatchString(req.Tenant) || req.OperatorToken == "" {
		return out, errors.New("缺少历史清理操作身份")
	}
	inv, _, err := s.resolve(req.Environment)
	if err != nil {
		return out, err
	}
	out.inv = inv
	entries, err := os.ReadDir(s.opt.ResultsDir)
	if os.IsNotExist(err) {
		entries = nil
	} else if err != nil {
		return out, err
	}
	var runFingerprints []string
	runScopes := map[string]cleanupScope{}
	protectedProducts := map[string]bool{}
	unknownRunOwnership := false
	for _, entry := range entries {
		if !entry.IsDir() || !runIDPattern.MatchString(entry.Name()) {
			continue
		}
		id := entry.Name()
		dir, e := safeCleanupPath(s.opt.ResultsDir, id)
		if e != nil {
			return out, e
		}
		p, e := LoadPlan(filepath.Join(dir, "plan.sanitized.yaml"))
		if e != nil {
			out.preview.Warnings = append(out.preview.Warnings, "部分旧运行记录不完整，已保留")
			unknownRunOwnership = true
			continue
		}
		if p.Fixtures.Tenant != req.Tenant {
			continue
		}
		if _, e := os.Stat(filepath.Join(dir, "manifest.json")); e != nil {
			out.preview.Items = append(out.preview.Items, HistoryCleanupItem{RunID: id, Title: id, Reason: "测试设备清单缺失，关联数据保留"})
			protectedProducts[p.Fixtures.Product] = true
			continue
		}
		scope, e := s.cleanupScope(id, req.Tenant)
		if e != nil {
			out.preview.Items = append(out.preview.Items, HistoryCleanupItem{RunID: id, Title: id, Reason: "运行尚未结束、被其他运行共享或记录不完整"})
			protectedProducts[p.Fixtures.Product] = true
			continue
		}
		if apiHash(scope.inv.API) != apiHash(inv.API) {
			continue
		}
		out.runs = append(out.runs, id)
		runScopes[id] = scope
		out.preview.Runs++
		out.preview.Items = append(out.preview.Items, HistoryCleanupItem{RunID: id, Title: id, Eligible: true, Devices: int64(scope.preview.Devices), RawMessages: scope.preview.RawMessages})
		// The manifest binds exact device ownership even if a file is edited
		// without changing the device count. Closed ledger file metadata binds
		// the remaining evidence without reading multi-gigabyte payloads.
		h := sha256.New()
		for _, name := range []string{"plan.sanitized.yaml", "manifest.json", "state.json", "cleanup-context.json"} {
			b, e := os.ReadFile(filepath.Join(dir, name))
			if e != nil && !os.IsNotExist(e) {
				return out, e
			}
			h.Write([]byte(name))
			h.Write(b)
		}
		e = filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("运行制品包含链接，拒绝清理")
			}
			if !d.IsDir() {
				st, e := d.Info()
				if e != nil {
					return e
				}
				fmt.Fprintf(h, "%s:%d:%d", path, st.Size(), st.ModTime().UnixNano())
			}
			return nil
		})
		if e != nil {
			return out, e
		}
		runFingerprints = append(runFingerprints, id+":"+hex.EncodeToString(h.Sum(nil)))
	}
	for after := ""; ; {
		var page struct {
			Products  []model.CapacityFixtureProduct `json:"products"`
			NextAfter string                         `json:"nextAfter"`
		}
		path := "/api/v1/ops/capacity/cleanup-fixtures?after=" + url.QueryEscape(after)
		if err = s.historyCall(ctx, inv, req.OperatorToken, http.MethodGet, path, nil, &page); err != nil {
			return out, err
		}
		for _, p := range page.Products {
			if unknownRunOwnership || protectedProducts[p.ProductID] {
				p.BlockedReason = "仍有未结束或记录不完整的测试运行，产品及设备保留"
			}
			eligible := p.BlockedReason == ""
			out.preview.Items = append(out.preview.Items, HistoryCleanupItem{ProductID: p.ProductID, Title: p.Name, Eligible: eligible, Devices: p.DeviceCount, RawMessages: p.RawMessages, Reason: p.BlockedReason})
			if eligible {
				out.products = append(out.products, p)
				out.preview.Products++
				out.preview.Devices += p.DeviceCount
				out.preview.RawMessages += p.RawMessages
			}
		}
		if page.NextAfter == "" {
			break
		}
		if page.NextAfter <= after {
			return out, errors.New("历史数据分页游标没有前进")
		}
		after = page.NextAfter
	}
	// If a retained run has incomplete device membership, even another closed
	// run cannot prove that its devices are exclusive. Preserve that product's
	// runs together, including runs using an existing non-fixture product.
	out.runs = slices.DeleteFunc(out.runs, func(id string) bool {
		return unknownRunOwnership || protectedProducts[runScopes[id].plan.Fixtures.Product]
	})
	out.preview.Runs = len(out.runs)
	for i := range out.preview.Items {
		item := &out.preview.Items[i]
		if run, ok := runScopes[item.RunID]; ok && (unknownRunOwnership || protectedProducts[run.plan.Fixtures.Product]) {
			item.Eligible = false
			item.Reason = "仍有未结束或记录不完整的测试运行，关联数据保留"
		}
	}
	runFingerprints = slices.DeleteFunc(runFingerprints, func(v string) bool {
		id, _, _ := strings.Cut(v, ":")
		return !slices.Contains(out.runs, id)
	})
	sort.Strings(out.runs)
	sort.Strings(runFingerprints)
	sort.Slice(out.products, func(i, j int) bool { return out.products[i].ProductID < out.products[j].ProductID })
	// A run may use an existing product instead of an auto-created fixture.
	// Count its exclusive devices separately, without double-counting products
	// or devices still shared by a run that this request will preserve.
	productIDs := map[string]bool{}
	for _, p := range out.products {
		productIDs[p.ProductID] = true
	}
	devices := map[string]map[string]bool{}
	for _, id := range out.runs {
		run := runScopes[id]
		product := run.plan.Fixtures.Product
		if productIDs[product] {
			continue
		}
		out.preview.RawMessages += run.preview.RawMessages
		if devices[product] == nil {
			devices[product] = map[string]bool{}
		}
		for _, device := range run.devices {
			devices[product][device] = true
		}
	}
	for _, entry := range entries {
		if !runIDPattern.MatchString(entry.Name()) || slices.Contains(out.runs, entry.Name()) {
			continue
		}
		dir, e := safeCleanupPath(s.opt.ResultsDir, entry.Name())
		if e != nil {
			return out, e
		}
		p, e := LoadPlan(filepath.Join(dir, "plan.sanitized.yaml"))
		if e != nil || p.Fixtures.Tenant != req.Tenant || devices[p.Fixtures.Product] == nil {
			continue
		}
		shared, e := fixtureIDs(dir, p, entry.Name())
		if e != nil {
			return out, e
		}
		for _, device := range shared {
			delete(devices[p.Fixtures.Product], device)
		}
	}
	for _, ids := range devices {
		out.preview.Devices += int64(len(ids))
	}
	// Products and run fingerprints bind the approved scope.
	b, _ := json.Marshal(struct {
		Tenant, API, Environment string
		Runs                     []string
		Products                 []model.CapacityFixtureProduct
	}{req.Tenant, apiHash(inv.API), req.Environment, runFingerprints, out.products})
	sum := sha256.Sum256(b)
	out.preview.Token = hex.EncodeToString(sum[:])
	return out, nil
}

func (s *Service) historyJobPath(tenant, environment string) (string, error) {
	sum := sha256.Sum256([]byte(tenant + "\x00" + environment))
	return safeCleanupPath(s.opt.ResultsDir, ".cleanup-history", hex.EncodeToString(sum[:])+".json")
}

func (s *Service) saveHistoryJob(job *HistoryCleanupJob) error {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	return s.saveHistoryJobLocked(job)
}

func (s *Service) saveHistoryJobLocked(job *HistoryCleanupJob) error {
	path, err := s.historyJobPath(job.Tenant, job.Environment)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	job.UpdatedAt = time.Now().UnixMilli()
	return writeJSONAtomic(path, job)
}

func (s *Service) historyJob(tenant, environment string) (*HistoryCleanupJob, error) {
	// Serialize the file read and interruption check with worker persistence.
	// Otherwise a stale RUNNING read could overwrite a just-completed result.
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	path, err := s.historyJobPath(tenant, environment)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var job HistoryCleanupJob
	if err = json.Unmarshal(b, &job); err != nil {
		return nil, err
	}
	job.Tenant = tenant
	s.mu.Lock()
	live := s.cleaning == "history:"+job.ID
	s.mu.Unlock()
	if job.Status == "RUNNING" && !live {
		job.Status = "FAILED"
		job.Phase = "清理进程已中断"
		job.Error = "清理进程已重启或中断，已保留结果，请重新预览后重试"
		if err = s.saveHistoryJobLocked(&job); err != nil {
			return nil, err
		}
	}
	return &job, nil
}

func (s *Service) startHistoryCleanup(ctx context.Context, req historyRequest) (*HistoryCleanupJob, error) {
	var random [3]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	id := "cap-" + time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(random[:])
	if err := s.beginCleanup("history:" + id); err != nil {
		return nil, err
	}
	release := true
	defer func() {
		if release {
			s.endCleanup("history:"+id, model.CapacityCleanupCounts{}, nil)
		}
	}()
	scope, err := s.historyScope(ctx, req)
	if err != nil {
		return nil, err
	}
	if req.PreviewToken == "" || req.PreviewToken != scope.preview.Token {
		return nil, errors.New("历史测试数据范围已变化，请重新预览后清理")
	}
	job := &HistoryCleanupJob{ID: id, Tenant: req.Tenant, Environment: req.Environment, Status: "RUNNING", Phase: "准备清理", Total: len(scope.runs) + len(scope.products), Warnings: []string{}}
	for _, item := range scope.preview.Items {
		if !item.Eligible {
			job.Warnings = append(job.Warnings, item.Title+"："+item.Reason)
		}
	}
	if err = s.saveHistoryJob(job); err != nil {
		return nil, err
	}
	release = false
	// Return a value copy. The worker owns its mutable state, so no browser
	// serialization can race with count/progress updates.
	response := *job
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		s.runHistoryCleanup(ctx, req, scope, job)
	}()
	return &response, nil
}

func (s *Service) runHistoryCleanup(ctx context.Context, req historyRequest, scope historyScope, job *HistoryCleanupJob) {
	var failure error
	defer func() {
		for _, warning := range job.Counts.Warnings {
			if !slices.Contains(job.Warnings, warning) {
				job.Warnings = append(job.Warnings, warning)
			}
		}
		if failure != nil {
			job.Status = "FAILED"
			job.Phase = "清理未完成"
			job.Error = clip(failure.Error(), 400)
		} else if len(job.Warnings) > 0 {
			job.Status = "PARTIAL"
			job.Phase = "已完成可清理部分"
		} else {
			job.Status = "SUCCEEDED"
			job.Phase = "清理完成"
		}
		if err := s.saveHistoryJob(job); err != nil {
			failure = errors.Join(failure, err)
		}
		s.endCleanup("history:"+job.ID, job.Counts, failure)
	}()
	// Disable every proven product against the preview fingerprint before
	// deleting any fixture. A changed product or device stops the job.
	for _, p := range scope.products {
		job.Phase = "停止测试设备接入"
		if failure = s.saveHistoryJob(job); failure != nil {
			return
		}
		if failure = s.historyCall(ctx, scope.inv, req.OperatorToken, http.MethodPost, "/api/v1/ops/capacity/cleanup-fixtures", map[string]string{"product": p.ProductID, "fingerprint": p.Fingerprint}, nil); failure != nil {
			return
		}
	}
	for _, id := range scope.runs {
		job.Phase = "清理测试运行 " + id
		if failure = s.saveHistoryJob(job); failure != nil {
			return
		}
		// Recalculate sharing after previous records have been removed.
		current, err := s.cleanupScope(id, req.Tenant)
		if err != nil {
			failure = err
			return
		}
		result, err := s.cleanup(ctx, id, req.Tenant, req.OperatorToken, current)
		job.Counts.Add(result.Counts)
		if err != nil {
			failure = err
			return
		}
		job.Processed++
		if failure = s.saveHistoryJob(job); failure != nil {
			return
		}
	}
	for _, p := range scope.products {
		job.Phase = "清理历史测试产品 " + p.Name
		if failure = s.saveHistoryJob(job); failure != nil {
			return
		}
		for {
			var page struct {
				Devices   []string `json:"devices"`
				NextAfter string   `json:"nextAfter"`
			}
			if failure = s.historyCall(ctx, scope.inv, req.OperatorToken, http.MethodGet, "/api/v1/ops/capacity/cleanup-fixtures?product="+url.QueryEscape(p.ProductID), nil, &page); failure != nil {
				return
			}
			if len(page.Devices) == 0 {
				break
			}
			q := model.CapacityCleanupBatch{RunID: job.ID, Product: p.ProductID, Devices: page.Devices, RemoveDevices: page.Devices, Historical: true}
			var n model.CapacityCleanupCounts
			if failure = s.historyCall(ctx, scope.inv, req.OperatorToken, http.MethodPost, "/api/v1/ops/capacity/cleanup-data", q, &n); failure != nil {
				return
			}
			job.Counts.Add(n)
			if failure = s.saveHistoryJob(job); failure != nil {
				return
			}
			if n.Devices == 0 {
				failure = errors.New("历史设备清理没有进展，已保留记录，请重新预览")
				return
			}
		}
		var n model.CapacityCleanupCounts
		q := model.CapacityCleanupBatch{RunID: job.ID, Product: p.ProductID, Historical: true, RemoveProduct: true}
		if failure = s.historyCall(ctx, scope.inv, req.OperatorToken, http.MethodPost, "/api/v1/ops/capacity/cleanup-data", q, &n); failure != nil {
			return
		}
		job.Counts.Add(n)
		job.Processed++
		if failure = s.saveHistoryJob(job); failure != nil {
			return
		}
	}
}

func (s *Service) registerHistoryCleanup(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	decode := func(w http.ResponseWriter, r *http.Request) (historyRequest, bool) {
		var req historyRequest
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req) != nil || req.Tenant == "" || req.OperatorToken == "" {
			serveError(w, 400, "bad_request", "缺少清理操作身份")
			return req, false
		}
		return req, true
	}
	mux.HandleFunc("POST /v1/cleanup/history/preview", auth(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decode(w, r)
		if !ok {
			return
		}
		scope, err := s.historyScope(r.Context(), req)
		if err != nil {
			s.cleanupError(w, err)
			return
		}
		job, err := s.historyJob(req.Tenant, req.Environment)
		if err != nil {
			s.cleanupError(w, err)
			return
		}
		serveJSON(w, 200, map[string]any{"preview": scope.preview, "job": job})
	}))
	mux.HandleFunc("POST /v1/cleanup/history", auth(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decode(w, r)
		if !ok {
			return
		}
		job, err := s.startHistoryCleanup(r.Context(), req)
		if err != nil {
			s.cleanupError(w, err)
			return
		}
		serveJSON(w, 202, map[string]any{"job": job})
	}))
	mux.HandleFunc("GET /v1/cleanup/history/status", auth(func(w http.ResponseWriter, r *http.Request) {
		tenant, environment := r.URL.Query().Get("tenant"), r.URL.Query().Get("environment")
		if !identifier.MatchString(tenant) {
			serveError(w, 400, "bad_request", "缺少清理租户")
			return
		}
		if _, _, err := s.resolve(environment); err != nil {
			s.cleanupError(w, err)
			return
		}
		job, err := s.historyJob(tenant, environment)
		if err != nil {
			s.cleanupError(w, err)
			return
		}
		serveJSON(w, 200, map[string]any{"job": job})
	}))
}
