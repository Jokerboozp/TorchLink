package capacity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/model"
)

type CleanupPreview struct {
	Resources     int      `json:"resources"`
	RunID         string   `json:"runId"`
	Devices       int      `json:"devices"`
	SharedDevices int      `json:"sharedDevices"`
	RawMessages   int64    `json:"rawMessages"`
	Warnings      []string `json:"warnings"`
}
type CleanupResult struct {
	CleanupPreview
	Deleted bool                        `json:"deleted"`
	Counts  model.CapacityCleanupCounts `json:"counts"`
}
type cleanupContext struct {
	Environment string `json:"environment"`
	APIHash     string `json:"apiHash"`
	PlanFile    string `json:"planFile"`
}
type cleanupScope struct {
	resources       []model.CapacityCleanupResource
	preview         CleanupPreview
	plan            *Plan
	inv             *Inventory
	dir             string
	devices, remove []string
	source          cleanupContext
	sharedProduct   bool
}

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

func (s *Service) cleanupScope(id, tenant string) (cleanupScope, error) {
	var out cleanupScope
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
	if !slices.Contains([]string{StatusFinished, StatusFailed, StatusCancelled}, st.Status) {
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
	var src cleanupContext
	if b, e := os.ReadFile(filepath.Join(dir, "cleanup-context.json")); e == nil {
		if err = json.Unmarshal(b, &src); err != nil {
			return out, err
		}
	} else {
		var env struct {
			Inventory string `json:"inventory"`
		}
		b, e := os.ReadFile(filepath.Join(dir, "environment.json"))
		if e != nil {
			return out, e
		}
		if err = json.Unmarshal(b, &env); err != nil {
			return out, err
		}
		for _, registered := range s.environments() {
			inv, _, e := s.resolve(registered.Name)
			if e == nil && inv.Name == env.Inventory {
				src.Environment = registered.Name
				break
			}
		}
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
	shared := map[string]bool{}
	entries, err := os.ReadDir(s.opt.ResultsDir)
	if err != nil {
		return out, err
	}
	sharedProduct := false
	for _, entry := range entries {
		if entry.Name() == id || !runIDPattern.MatchString(entry.Name()) {
			continue
		}
		other, err := safeCleanupPath(s.opt.ResultsDir, entry.Name())
		if err != nil {
			return out, err
		}
		op, err := LoadPlan(filepath.Join(other, "plan.sanitized.yaml"))
		if err != nil {
			return out, errors.New("其他运行记录不完整，无法确认共享设备范围")
		}
		if op.Fixtures.Tenant != tenant || op.Fixtures.Product != p.Fixtures.Product {
			continue
		}
		sharedProduct = true
		ids, err := fixtureIDs(other, op, entry.Name())
		if err != nil {
			return out, err
		}
		for _, d := range ids {
			shared[d] = true
		}
	}
	var remove []string
	for _, d := range devices {
		if !identifier.MatchString(d) {
			return out, errors.New("invalid fixture device ID")
		}
		if !shared[d] {
			remove = append(remove, d)
		}
	}
	// Shared devices keep their credentials for other runs; only this run's
	// ledgered messages are removed from them.
	preview := CleanupPreview{RunID: id, Devices: len(remove), SharedDevices: len(devices) - len(remove), Warnings: []string{"系统审计日志、监控历史和消息队列留存记录保留"}}
	out = cleanupScope{preview: preview, plan: p, inv: inv, dir: dir, devices: devices, remove: remove, source: src, sharedProduct: sharedProduct}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, e error) error {
		if e == nil && d.Type()&os.ModeSymlink != 0 {
			return errors.New("run artifacts contain a symbolic link")
		}
		return e
	})
	if err != nil {
		return out, err
	}
	seenResources := map[string]bool{}
	err = out.ledgerBatches(func(ids, _ []string) error { out.preview.RawMessages += int64(len(ids)); return nil }, func(r model.CapacityCleanupResource) error {
		key := r.Kind + ":" + r.ID
		if !seenResources[key] {
			out.resources = append(out.resources, r)
			seenResources[key] = true
		}
		return nil
	})
	out.preview.Resources = len(out.resources)
	if p.Modules.Backup.Enabled {
		out.preview.Warnings = append(out.preview.Warnings, "包含平台全量数据的备份与独立恢复目标属于共享制品，保留")
	}
	if p.Load.IngressShare["tcp"] > 0 {
		out.preview.Warnings = append(out.preview.Warnings, "TCP 场景仅记录协议 ACK，无法定位其平台原文与设备；这部分数据保留")
	}
	if len(out.resources) == 0 && (p.Modules.AI.Enabled || p.Modules.Knowledge.Enabled || p.Modules.Exports.Enabled) {
		out.preview.Warnings = append(out.preview.Warnings, "旧运行未记录业务任务归属，无法确认归属的 AI、知识及巡检任务保留")
	}
	return out, err
}

func (c cleanupScope) ledgerBatches(fn func([]string, []string) error, resourceFn func(model.CapacityCleanupResource) error) error {
	devices := make(map[string]bool, len(c.devices))
	for _, id := range c.devices {
		devices[id] = true
	}
	paths, err := filepath.Glob(filepath.Join(c.dir, "ledgers", "*", "*.jsonl.gz"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		var ids []string
		var batchDevices []string
		err = ReadLedger(path, func(h LedgerHeader, e LedgerEntry) error {
			if h.RunID != c.preview.RunID || h.Tenant != c.plan.Fixtures.Tenant || h.Product != c.plan.Fixtures.Product {
				return errors.New("ledger does not match cleanup scope")
			}
			if e.RawID == "" || e.Result == "not_sent" || e.Stream == "tcp" {
				if e.ResourceID != "" {
					if !slices.Contains([]string{"knowledge", "inspection", "alarm-analysis", "replay"}, e.ResourceKind) {
						return errors.New("unknown module resource in ledger")
					}
					if resourceFn != nil {
						return resourceFn(model.CapacityCleanupResource{Kind: e.ResourceKind, ID: e.ResourceID})
					}
				}
				return nil
			}
			if !devices[e.Device] {
				return errors.New("ledger device is outside cleanup scope")
			}
			ids = append(ids, e.RawID)
			if !slices.Contains(batchDevices, e.Device) {
				batchDevices = append(batchDevices, e.Device)
			}
			if len(ids) == 500 {
				if err := fn(ids, batchDevices); err != nil {
					return err
				}
				ids = nil
				batchDevices = nil
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			if err = fn(ids, batchDevices); err != nil {
				return err
			}
		}
	}
	return nil
}

// cleanupTimeout bounds one background cleanup; the delegated operator token
// is valid for an hour.
const cleanupTimeout = 30 * time.Minute

func (s *Service) beginCleanup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != "" {
		return ErrRunActive
	}
	s.active, s.cleaning = "cleaning", id
	delete(s.cleanupErr, id)
	return nil
}

func (s *Service) endCleanup(id string, n model.CapacityCleanupCounts, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active, s.cleaning = "", ""
	if err != nil {
		s.cleanupErr[id] = clip(err.Error(), 400)
		fmt.Fprintf(s.opt.Log, "capacity cleanup %s failed: %v\n", id, err)
		return
	}
	fmt.Fprintf(s.opt.Log, "capacity cleanup %s done: %+v\n", id, n)
}

// Cleanup removes one finished run's data and waits for the result.
func (s *Service) Cleanup(ctx context.Context, id, tenant, operator string) (CleanupResult, error) {
	if err := s.beginCleanup(id); err != nil {
		return CleanupResult{}, err
	}
	scope, err := s.cleanupScope(id, tenant)
	var result CleanupResult
	if err == nil {
		result, err = s.cleanup(ctx, id, tenant, operator, scope)
	}
	s.endCleanup(id, result.Counts, err)
	return result, err
}

// StartCleanup checks the scope, then cleans in the background so leaving
// the page or a proxy timeout cannot interrupt it halfway. Progress and the
// last failure are reported through the run list.
func (s *Service) StartCleanup(id, tenant, operator string) error {
	if err := s.beginCleanup(id); err != nil {
		return err
	}
	scope, err := s.cleanupScope(id, tenant)
	if err != nil {
		s.mu.Lock()
		s.active, s.cleaning = "", ""
		s.mu.Unlock()
		return err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		result, err := s.cleanup(ctx, id, tenant, operator, scope)
		s.endCleanup(id, result.Counts, err)
	}()
	return nil
}

func (s *Service) cleanup(ctx context.Context, id, tenant, operator string, scope cleanupScope) (CleanupResult, error) {
	var err error
	result := CleanupResult{CleanupPreview: scope.preview}
	var remoteAgents []*RemoteAgent
	var agentToken string
	for _, target := range scope.inv.Agents {
		if target.URL == "" {
			continue
		}
		if agentToken == "" {
			secrets, e := LoadSecrets(s.opt.SecretsPath)
			if e == nil {
				agentToken, e = secrets.Get(scope.plan.Credentials.AgentSecretRef)
			}
			if e != nil {
				return result, e
			}
		}
		agent := NewRemoteAgent(target.Name, target.URL, agentToken)
		status, e := agent.Status(ctx)
		if e != nil {
			return result, e
		}
		if status.LeaseValid || status.Running {
			return result, fmt.Errorf("Agent %s 仍在使用中，拒绝清理", target.Name)
		}
		remoteAgents = append(remoteAgents, agent)
	}
	client := newHTTPClient(10*time.Minute, 1)
	call := func(q model.CapacityCleanupBatch) error {
		b, _ := json.Marshal(q)
		status, resp, err := doHTTP(ctx, client, http.MethodPost, strings.TrimRight(scope.inv.API, "/")+"/api/v1/ops/capacity/cleanup-data", b, map[string]string{"Authorization": "Bearer " + operator, "X-Capacity-Service-Token": s.opt.Token})
		if err != nil {
			return errors.New("测试数据清理请求失败；已保留记录，可重试")
		}
		if status != 200 {
			return fmt.Errorf("测试数据清理返回 HTTP %d；已保留记录，可重试：%s", status, clip(string(resp), 240))
		}
		var counts model.CapacityCleanupCounts
		if err = json.Unmarshal(resp, &counts); err != nil {
			return err
		}
		result.Counts.Add(counts)
		return nil
	}
	base := model.CapacityCleanupBatch{RunID: id, Product: scope.plan.Fixtures.Product, Devices: scope.devices}
	for i := 0; i < len(scope.resources); i += 500 {
		q := base
		q.Devices = nil // Module resources are checked by their saved run ownership.
		q.Resources = scope.resources[i:min(i+500, len(scope.resources))]
		if err = call(q); err != nil {
			return result, err
		}
	}
	if err = scope.ledgerBatches(func(ids, devices []string) error { q := base; q.RawIDs = ids; q.Devices = devices; return call(q) }, nil); err != nil {
		return result, err
	}
	for i := 0; i < len(scope.remove); i += 1000 {
		q := base
		q.Devices = scope.remove[i:min(i+1000, len(scope.remove))]
		q.RemoveDevices = q.Devices
		if err = call(q); err != nil {
			return result, err
		}
	}
	base.Devices = nil
	if !scope.sharedProduct && scope.plan.Fixtures.AutoProvision {
		base.RemoveProduct = true
		base.RemoveRule = scope.plan.Fixtures.AlarmRuleID
	}
	if err = call(base); err != nil {
		return result, err
	}
	// A lost controller may not have released a remote agent. Its own control
	// endpoint checks for a live lease before deleting the run work directory.
	for _, a := range remoteAgents {
		if e := a.Cleanup(ctx, id); e != nil {
			return result, fmt.Errorf("远程 Agent %s 清理失败；保留运行记录，可重试：%w", a.Name(), e)
		}
	}
	// Update only this fixture's private cache. Shared credentials are kept so
	// unrelated retained runs can still be resumed.
	cache, err := safeCleanupPath(s.opt.ResultsDir, ".work", "fixtures", fmt.Sprintf("%s-%s-%s.json", tenant, scope.plan.Fixtures.Product, scope.plan.Fixtures.DevicePrefix))
	if err != nil {
		return result, err
	}
	if b, e := os.ReadFile(cache); e == nil {
		var have []DeviceCredential
		if err = json.Unmarshal(b, &have); err != nil {
			return result, err
		}
		removed := make(map[string]bool, len(scope.remove))
		for _, id := range scope.remove {
			removed[id] = true
		}
		have = slices.DeleteFunc(have, func(d DeviceCredential) bool { return removed[d.ID] })
		if len(have) == 0 {
			err = os.Remove(cache)
		} else {
			b, _ = json.Marshal(have)
			err = writePrivateAtomic(cache, b)
		}
		if err != nil {
			return result, err
		}
	} else if !os.IsNotExist(e) {
		return result, e
	}
	for _, a := range scope.inv.Agents {
		if a.URL == "" {
			p, e := safeCleanupPath(s.opt.ResultsDir, ".work", "agent-"+a.Name, id)
			if e != nil {
				return result, e
			}
			if e = os.RemoveAll(p); e != nil {
				return result, e
			}
		}
	}
	if scope.source.PlanFile != "" {
		p, e := safeCleanupPath(s.opt.ResultsDir, ".plans", scope.source.PlanFile)
		if e != nil {
			return result, e
		}
		if e = os.Remove(p); e != nil && !os.IsNotExist(e) {
			return result, e
		}
	}
	if err = os.RemoveAll(scope.dir); err != nil {
		return result, err
	}
	result.Deleted = true
	return result, nil
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
