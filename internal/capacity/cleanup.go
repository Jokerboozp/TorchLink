package capacity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/model"
)

// Test data belongs to dedicated test products (model.IsCapacityFixture).
// Deleting one run removes its record and the devices no other run uses;
// cleaning all removes every ended run and every dedicated test product. The
// platform verifies ownership and performs the deletion; the controller only
// decides the scope and removes its own files.

// CleanupPreview is shown before the operator confirms a cleanup.
type CleanupPreview struct {
	RunID         string                         `json:"runId,omitempty"`
	Runs          int                            `json:"runs"`
	Products      []model.CapacityFixtureProduct `json:"products"`
	Devices       int64                          `json:"devices"`
	SharedDevices int                            `json:"sharedDevices"`
	RawMessages   int64                          `json:"rawMessages"`
	Warnings      []string                       `json:"warnings"`
}

// CleanupJob is persisted so progress and failures survive leaving the page.
// Delegated credentials never enter the results directory or a response.
type CleanupJob struct {
	ID          string                      `json:"id"`
	Scope       string                      `json:"scope"` // run, all
	RunID       string                      `json:"runId,omitempty"`
	Tenant      string                      `json:"-"`
	Environment string                      `json:"environment"`
	Status      string                      `json:"status"` // RUNNING, SUCCEEDED, PARTIAL, FAILED
	Phase       string                      `json:"phase"`
	Processed   int                         `json:"processed"`
	Total       int                         `json:"total"`
	Counts      model.CapacityCleanupCounts `json:"counts"`
	Error       string                      `json:"error,omitempty"`
	Warnings    []string                    `json:"warnings,omitempty"`
	UpdatedAt   int64                       `json:"updatedAt"`
}

type cleanupRequest struct {
	Environment   string `json:"environment"`
	Tenant        string `json:"tenant"`
	OperatorToken string `json:"operatorToken"`
}

type cleanupContext struct {
	Environment string `json:"environment"`
	APIHash     string `json:"apiHash"`
	PlanFile    string `json:"planFile"`
}

// runScope is one ended run of the tenant and the devices only it uses.
type runScope struct {
	id            string
	plan          *Plan
	inv           *Inventory
	environment   string
	dir           string
	devices       []string
	exclusive     []string
	sharedProduct bool
	source        cleanupContext
}

// cleanupTimeout bounds one background cleanup; the delegated operator token
// is valid for an hour.
const cleanupTimeout = 30 * time.Minute

func apiHash(api string) string {
	h := sha256.Sum256([]byte(strings.TrimRight(api, "/")))
	return hex.EncodeToString(h[:])
}

func safeCleanupPath(root string, parts ...string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(append([]string{base}, parts...)...)
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid cleanup path")
	}
	current := base
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		st, e := os.Lstat(current)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("cleanup paths must not contain symbolic links")
		}
	}
	return p, nil
}

func fixtureIDs(dir string, p *Plan, id string) ([]string, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.RunID != id || m.Tenant != p.Fixtures.Tenant || m.Product != p.Fixtures.Product {
		return nil, errors.New("fixture manifest does not match the run")
	}
	if len(m.Devices) > 0 {
		return m.Devices, nil
	}
	if p.Fixtures.DeviceCount < 0 {
		return nil, errors.New("invalid fixture device count")
	}
	prefix := p.Fixtures.DevicePrefix
	if !p.Fixtures.ReuseDevices {
		prefix += "-" + id[len(id)-6:]
	}
	ids := make([]string, p.Fixtures.DeviceCount)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%06d", prefix, i)
	}
	return ids, nil
}

// runEnvironment returns the registered environment a run was started in.
func (s *Service) runEnvironment(dir string) (cleanupContext, error) {
	var src cleanupContext
	if b, err := os.ReadFile(filepath.Join(dir, "cleanup-context.json")); err == nil {
		err = json.Unmarshal(b, &src)
		return src, err
	}
	var env struct {
		Inventory string `json:"inventory"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "environment.json"))
	if err != nil {
		return src, err
	}
	if err = json.Unmarshal(b, &env); err != nil {
		return src, err
	}
	for _, registered := range s.environments() {
		if inv, _, e := s.resolve(registered.Name); e == nil && inv.Name == env.Inventory {
			src.Environment = registered.Name
			break
		}
	}
	return src, nil
}

// tenantRuns lists the run IDs whose plan targets the tenant.
func (s *Service) tenantRuns(tenant string) ([]string, error) {
	entries, err := os.ReadDir(s.opt.ResultsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() || !runIDPattern.MatchString(entry.Name()) {
			continue
		}
		p, err := LoadPlan(filepath.Join(s.opt.ResultsDir, entry.Name(), "plan.sanitized.yaml"))
		if err == nil && p.Fixtures.Tenant == tenant {
			ids = append(ids, entry.Name())
		}
	}
	return ids, nil
}

func (s *Service) runScope(id, tenant string) (runScope, error) {
	var out runScope
	if !runIDPattern.MatchString(id) {
		return out, os.ErrNotExist
	}
	dir, err := safeCleanupPath(s.opt.ResultsDir, id)
	if err != nil {
		return out, err
	}
	st, err := ReadState(s.opt.ResultsDir, id)
	if err != nil {
		return out, err
	}
	if st, _ = InterruptedState(st, time.Now()); !st.Ended() {
		return out, ErrRunActive
	}
	p, err := LoadPlan(filepath.Join(dir, "plan.sanitized.yaml"))
	if err != nil {
		return out, err
	}
	if p.Fixtures.Tenant != tenant {
		return out, os.ErrNotExist
	}
	if !identifier.MatchString(p.Fixtures.Tenant) || !identifier.MatchString(p.Fixtures.Product) || !identifier.MatchString(p.Fixtures.DevicePrefix) {
		return out, errors.New("invalid fixture identifiers")
	}
	src, err := s.runEnvironment(dir)
	if err != nil {
		return out, err
	}
	inv, _, err := s.resolve(src.Environment)
	if err != nil {
		return out, err
	}
	if src.APIHash != "" && src.APIHash != apiHash(inv.API) {
		return out, errors.New("测试环境的 API 地址已变化，不能清理旧运行")
	}
	if src.PlanFile != "" && (filepath.Base(src.PlanFile) != src.PlanFile || strings.ContainsAny(src.PlanFile, `/\\`) || !strings.HasSuffix(src.PlanFile, ".yaml")) {
		return out, errors.New("invalid source plan filename")
	}
	devices, err := fixtureIDs(dir, p, id)
	if err != nil {
		return out, err
	}
	others, err := s.tenantRuns(tenant)
	if err != nil {
		return out, err
	}
	shared := map[string]bool{}
	sharedProduct := false
	for _, other := range others {
		if other == id {
			continue
		}
		otherDir := filepath.Join(s.opt.ResultsDir, other)
		op, err := LoadPlan(filepath.Join(otherDir, "plan.sanitized.yaml"))
		if err != nil || op.Fixtures.Product != p.Fixtures.Product {
			continue
		}
		sharedProduct = true
		ids, err := fixtureIDs(otherDir, op, other)
		if err != nil {
			return out, err
		}
		for _, d := range ids {
			shared[d] = true
		}
	}
	var exclusive []string
	for _, d := range devices {
		if !identifier.MatchString(d) {
			return out, errors.New("invalid fixture device ID")
		}
		if !shared[d] {
			exclusive = append(exclusive, d)
		}
	}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, e error) error {
		if e == nil && d.Type()&os.ModeSymlink != 0 {
			return errors.New("run artifacts contain a symbolic link")
		}
		return e
	})
	return runScope{id: id, plan: p, inv: inv, environment: src.Environment, dir: dir, devices: devices, exclusive: exclusive, sharedProduct: sharedProduct, source: src}, err
}

// RunCleanupPreview summarizes what deleting one run removes.
func (s *Service) RunCleanupPreview(id, tenant string) (CleanupPreview, error) {
	scope, err := s.runScope(id, tenant)
	if err != nil {
		return CleanupPreview{}, err
	}
	preview := CleanupPreview{RunID: id, Runs: 1, Products: []model.CapacityFixtureProduct{}, Devices: int64(len(scope.exclusive)), SharedDevices: len(scope.devices) - len(scope.exclusive), Warnings: []string{"监控历史和平台全量备份保留"}}
	if preview.SharedDevices > 0 {
		preview.Warnings = append(preview.Warnings, "其他运行仍在使用的设备及其数据保留，可在“清理全部测试数据”中一并删除")
	}
	return preview, nil
}

// platform calls the delegated platform API as the operator.
func (s *Service) platform(ctx context.Context, inv *Inventory, operator, method, path string, body, out any) error {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	status, resp, err := doHTTP(ctx, newHTTPClient(10*time.Minute, 1), method, strings.TrimRight(inv.API, "/")+path, data, map[string]string{"Authorization": "Bearer " + operator, "X-Capacity-Service-Token": s.opt.Token})
	if err != nil {
		return errors.New("测试数据清理请求失败；已保留记录，可重试")
	}
	if status != http.StatusOK {
		reason := "清理服务暂时不可用，请稍后重试"
		var response struct {
			Detail string `json:"detail"`
		}
		switch {
		case status == http.StatusConflict && json.Unmarshal(resp, &response) == nil && response.Detail != "":
			reason = clip(response.Detail, 240)
		case status == http.StatusForbidden || status == http.StatusUnauthorized:
			reason = "清理权限或登录状态已变化，请重新登录后重试"
		case status == http.StatusNotImplemented:
			reason = "当前存储不支持容量测试数据清理"
		}
		return fmt.Errorf("测试数据清理未完成，已保留记录：%s", reason)
	}
	if out != nil {
		return json.Unmarshal(resp, out)
	}
	return nil
}

func (s *Service) cleanData(ctx context.Context, inv *Inventory, operator string, job *CleanupJob, q model.CapacityCleanupBatch) error {
	var counts model.CapacityCleanupCounts
	if err := s.platform(ctx, inv, operator, http.MethodPost, "/api/v1/ops/capacity/cleanup-data", q, &counts); err != nil {
		return err
	}
	job.Counts.Add(counts)
	return s.saveJob(job)
}

func (s *Service) fixtureProducts(ctx context.Context, inv *Inventory, operator string) ([]model.CapacityFixtureProduct, error) {
	var page struct {
		Products []model.CapacityFixtureProduct `json:"products"`
	}
	err := s.platform(ctx, inv, operator, http.MethodGet, "/api/v1/ops/capacity/cleanup-fixtures", nil, &page)
	return page.Products, err
}

// AllCleanupPreview counts every ended run and dedicated test product.
func (s *Service) AllCleanupPreview(ctx context.Context, req cleanupRequest) (CleanupPreview, error) {
	inv, runs, err := s.allScope(req)
	if err != nil {
		return CleanupPreview{}, err
	}
	products, err := s.fixtureProducts(ctx, inv, req.OperatorToken)
	if err != nil {
		return CleanupPreview{}, err
	}
	preview := CleanupPreview{Runs: len(runs), Products: products, Warnings: []string{"监控历史和平台全量备份保留；被网关、摄像头或接入配置引用的测试设备会阻止清理"}}
	for _, p := range products {
		preview.Devices += p.DeviceCount
		preview.RawMessages += p.RawMessages
	}
	return preview, nil
}

// allScope returns the environment and its ended runs; an unfinished run of
// the tenant blocks cleaning everything.
func (s *Service) allScope(req cleanupRequest) (*Inventory, []runScope, error) {
	if !identifier.MatchString(req.Tenant) || req.OperatorToken == "" {
		return nil, nil, errors.New("缺少清理操作身份")
	}
	inv, _, err := s.resolve(req.Environment)
	if err != nil {
		return nil, nil, err
	}
	ids, err := s.tenantRuns(req.Tenant)
	if err != nil {
		return nil, nil, err
	}
	var runs []runScope
	for _, id := range ids {
		dir := filepath.Join(s.opt.ResultsDir, id)
		src, err := s.runEnvironment(dir)
		if err != nil || src.Environment != req.Environment {
			continue
		}
		scope, err := s.runScope(id, req.Tenant)
		if errors.Is(err, ErrRunActive) {
			return nil, nil, errors.New("有测试运行尚未结束，请等待结束或停止后再清理")
		}
		if err != nil {
			return nil, nil, fmt.Errorf("运行 %s 的记录无法核对：%w", id, err)
		}
		runs = append(runs, scope)
	}
	return inv, runs, nil
}

func newCleanupID() (string, error) {
	var random [3]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "cap-" + time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(random[:]), nil
}

func (s *Service) jobPath(tenant, environment string) (string, error) {
	sum := sha256.Sum256([]byte(tenant + "\x00" + environment))
	return safeCleanupPath(s.opt.ResultsDir, ".cleanup", hex.EncodeToString(sum[:])+".json")
}

func (s *Service) saveJob(job *CleanupJob) error {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	return s.saveJobLocked(job)
}

func (s *Service) saveJobLocked(job *CleanupJob) error {
	path, err := s.jobPath(job.Tenant, job.Environment)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	job.UpdatedAt = time.Now().UnixMilli()
	return writeJSONAtomic(path, job)
}

// CleanupStatus returns the latest cleanup of the tenant and environment. A
// RUNNING job without a live worker was interrupted by a restart.
func (s *Service) CleanupStatus(tenant, environment string) (*CleanupJob, error) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	path, err := s.jobPath(tenant, environment)
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
	var job CleanupJob
	if err = json.Unmarshal(b, &job); err != nil {
		return nil, err
	}
	job.Tenant = tenant
	s.mu.Lock()
	live := s.cleaning == job.ID
	s.mu.Unlock()
	if job.Status == "RUNNING" && !live {
		job.Status, job.Phase, job.Error = "FAILED", "清理进程已中断", "清理进程已重启或中断，已保留结果，请重试"
		if err = s.saveJobLocked(&job); err != nil {
			return nil, err
		}
	}
	return &job, nil
}

func (s *Service) beginCleanup(job *CleanupJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != "" {
		return ErrRunActive
	}
	s.active, s.cleaning = "cleaning", job.ID
	if job.RunID != "" {
		s.cleaningRun = job.RunID
		delete(s.cleanupErr, job.RunID)
	}
	return nil
}

func (s *Service) endCleanup(job *CleanupJob, failure error) {
	for _, warning := range job.Counts.Warnings {
		if !slices.Contains(job.Warnings, warning) {
			job.Warnings = append(job.Warnings, warning)
		}
	}
	switch {
	case failure != nil:
		job.Status, job.Phase, job.Error = "FAILED", "清理未完成", clip(failure.Error(), 400)
	case len(job.Warnings) > 0:
		job.Status, job.Phase = "PARTIAL", "已完成可清理部分"
	default:
		job.Status, job.Phase = "SUCCEEDED", "清理完成"
	}
	if err := s.saveJob(job); err != nil {
		failure = errors.Join(failure, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active, s.cleaning, s.cleaningRun = "", "", ""
	if failure != nil {
		if job.RunID != "" {
			s.cleanupErr[job.RunID] = clip(failure.Error(), 400)
		}
		fmt.Fprintf(s.opt.Log, "capacity cleanup %s failed: %v\n", job.ID, failure)
		return
	}
	fmt.Fprintf(s.opt.Log, "capacity cleanup %s done: %+v\n", job.ID, job.Counts)
}

func (s *Service) startJob(job *CleanupJob, work func(context.Context) error) (*CleanupJob, error) {
	if err := s.beginCleanup(job); err != nil {
		return nil, err
	}
	if err := s.saveJob(job); err != nil {
		s.mu.Lock()
		s.active, s.cleaning, s.cleaningRun = "", "", ""
		s.mu.Unlock()
		return nil, err
	}
	response := *job
	// The worker owns the job; the caller receives a copy, so serialization
	// cannot race with progress updates.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		s.endCleanup(job, work(ctx))
	}()
	return &response, nil
}

// StartRunCleanup deletes one ended run in the background: its exclusive
// devices with all their data, its module tasks and its files.
func (s *Service) StartRunCleanup(id string, req cleanupRequest) (*CleanupJob, error) {
	scope, err := s.runScope(id, req.Tenant)
	if err != nil {
		return nil, err
	}
	jobID, err := newCleanupID()
	if err != nil {
		return nil, err
	}
	job := &CleanupJob{ID: jobID, Scope: "run", RunID: id, Tenant: req.Tenant, Environment: scope.environment, Status: "RUNNING", Phase: "清理测试运行 " + id, Total: 1}
	return s.startJob(job, func(ctx context.Context) error {
		agents, err := s.idleAgents(ctx, scope)
		if err != nil {
			return err
		}
		for i := 0; i < len(scope.exclusive); i += 500 {
			if err = s.cleanData(ctx, scope.inv, req.OperatorToken, job, model.CapacityCleanupBatch{RunID: id, Product: scope.plan.Fixtures.Product, Devices: scope.exclusive[i:min(i+500, len(scope.exclusive))]}); err != nil {
				return err
			}
		}
		final := model.CapacityCleanupBatch{RunID: id, Product: scope.plan.Fixtures.Product, RemoveProduct: !scope.sharedProduct && scope.plan.Fixtures.AutoProvision}
		if err = s.cleanData(ctx, scope.inv, req.OperatorToken, job, final); err != nil {
			return err
		}
		if err = s.removeRunFiles(ctx, scope, agents, scope.exclusive); err != nil {
			return err
		}
		job.Processed = 1
		return nil
	})
}

// StartAllCleanup removes every ended run and dedicated test product of the
// environment in the background.
func (s *Service) StartAllCleanup(ctx context.Context, req cleanupRequest) (*CleanupJob, error) {
	inv, runs, err := s.allScope(req)
	if err != nil {
		return nil, err
	}
	products, err := s.fixtureProducts(ctx, inv, req.OperatorToken)
	if err != nil {
		return nil, err
	}
	jobID, err := newCleanupID()
	if err != nil {
		return nil, err
	}
	job := &CleanupJob{ID: jobID, Scope: "all", Tenant: req.Tenant, Environment: req.Environment, Status: "RUNNING", Phase: "准备清理", Total: len(runs) + len(products)}
	return s.startJob(job, func(ctx context.Context) error {
		agents := map[string][]*RemoteAgent{}
		for _, scope := range runs {
			list, err := s.idleAgents(ctx, scope)
			if err != nil {
				return err
			}
			agents[scope.id] = list
		}
		for _, p := range products {
			job.Phase = "清理测试产品 " + p.Name
			for {
				var page struct {
					Devices []string `json:"devices"`
				}
				if err := s.platform(ctx, inv, req.OperatorToken, http.MethodGet, "/api/v1/ops/capacity/cleanup-fixtures?product="+url.QueryEscape(p.ProductID), nil, &page); err != nil {
					return err
				}
				if len(page.Devices) == 0 {
					break
				}
				before := job.Counts.Devices
				if err := s.cleanData(ctx, inv, req.OperatorToken, job, model.CapacityCleanupBatch{Product: p.ProductID, Devices: page.Devices}); err != nil {
					return err
				}
				if job.Counts.Devices == before {
					return errors.New("测试设备清理没有进展，已保留记录，请重试")
				}
			}
			if err := s.cleanData(ctx, inv, req.OperatorToken, job, model.CapacityCleanupBatch{Product: p.ProductID, RemoveProduct: true}); err != nil {
				return err
			}
			job.Processed++
		}
		job.Phase = "清理测试任务与运行记录"
		if err := s.cleanData(ctx, inv, req.OperatorToken, job, model.CapacityCleanupBatch{AllRuns: true}); err != nil {
			return err
		}
		for _, scope := range runs {
			if err := s.removeRunFiles(ctx, scope, agents[scope.id], scope.devices); err != nil {
				return err
			}
			job.Processed++
			if err := s.saveJob(job); err != nil {
				return err
			}
		}
		return nil
	})
}

// idleAgents refuses cleanup while a remote Agent still holds a run lease.
func (s *Service) idleAgents(ctx context.Context, scope runScope) ([]*RemoteAgent, error) {
	var out []*RemoteAgent
	var token string
	for _, target := range scope.inv.Agents {
		if target.URL == "" {
			continue
		}
		if token == "" {
			secrets, err := LoadSecrets(s.opt.SecretsPath)
			if err == nil {
				token, err = secrets.Get(scope.plan.Credentials.AgentSecretRef)
			}
			if err != nil {
				return nil, err
			}
		}
		agent := NewRemoteAgent(target.Name, target.URL, token)
		status, err := agent.Status(ctx)
		if err != nil {
			return nil, err
		}
		if status.LeaseValid || status.Running {
			return nil, fmt.Errorf("Agent %s 仍在使用中，拒绝清理", target.Name)
		}
		out = append(out, agent)
	}
	return out, nil
}

// removeRunFiles deletes agent work directories, removed devices from the
// private credential cache, the source plan and finally the run record.
func (s *Service) removeRunFiles(ctx context.Context, scope runScope, agents []*RemoteAgent, removed []string) error {
	for _, a := range agents {
		if err := a.Cleanup(ctx, scope.id); err != nil {
			return fmt.Errorf("远程 Agent %s 清理失败；保留运行记录，可重试：%w", a.Name(), err)
		}
	}
	cache, err := safeCleanupPath(s.opt.ResultsDir, ".work", "fixtures", fmt.Sprintf("%s-%s-%s.json", scope.plan.Fixtures.Tenant, scope.plan.Fixtures.Product, scope.plan.Fixtures.DevicePrefix))
	if err != nil {
		return err
	}
	if b, e := os.ReadFile(cache); e == nil {
		var have []DeviceCredential
		if err = json.Unmarshal(b, &have); err != nil {
			return err
		}
		have = slices.DeleteFunc(have, func(d DeviceCredential) bool { return slices.Contains(removed, d.ID) })
		if len(have) == 0 {
			err = os.Remove(cache)
		} else {
			b, _ = json.Marshal(have)
			err = writePrivateAtomic(cache, b)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	for _, a := range scope.inv.Agents {
		if a.URL == "" {
			p, err := safeCleanupPath(s.opt.ResultsDir, ".work", "agent-"+a.Name, scope.id)
			if err != nil {
				return err
			}
			if err = os.RemoveAll(p); err != nil {
				return err
			}
		}
	}
	if scope.source.PlanFile != "" {
		p, err := safeCleanupPath(s.opt.ResultsDir, ".plans", scope.source.PlanFile)
		if err != nil {
			return err
		}
		if err = os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.RemoveAll(scope.dir)
}

func writePrivateAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".cleanup-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (w *Worker) Cleanup(ctx context.Context, id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !runIDPattern.MatchString(id) {
		return errors.New("invalid run ID")
	}
	p, err := safeCleanupPath(w.dir, id)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if w.run != nil {
		status := w.statusLocked()
		if w.run.req.RunID != id || status.LeaseValid || status.Running {
			return ErrAgentBusy
		}
		w.releaseLocked()
	}
	return os.RemoveAll(p)
}
