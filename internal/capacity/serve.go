package capacity

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ServeOptions configure the controller service (capacity-test serve). The
// environments are the trusted inventories in InventoryDir; callers pick one
// by name and can never supply addresses, secrets or fault commands.
type ServeOptions struct {
	InventoryDir string
	ResultsDir   string
	SecretsPath  string
	Token        string
	FaultAllow   FaultAllowlist
	// Self, when set, replaces InventoryDir with the single environment
	// "self": the platform the capacity module is deployed with.
	Self         *SelfEnvironment
	SourceCommit string
	Log          io.Writer
	// Test seams passed to Run.
	NewStore func(ctx context.Context, pgDSN, chURL string) (Store, error)
	NewAgent func(t AgentTarget, token, workDir string) Agent
}

// Service runs at most one capacity run at a time.
type Service struct {
	opt    ServeOptions
	mu     sync.Mutex
	active string
	done   chan struct{}
	cancel context.CancelFunc
	last   error
}

func NewService(opt ServeOptions) *Service {
	if opt.Log == nil {
		opt.Log = io.Discard
	}
	return &Service{opt: opt}
}

var runIDPattern = regexp.MustCompile(`^cap-[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)

// Environment describes a trusted inventory without addresses or secrets.
type Environment struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Agents   int    `json:"agents"`
	Metrics  int    `json:"metricsTargets"`
	MQTT     bool   `json:"mqtt"`
	TCP      bool   `json:"tcp"`
	Web      bool   `json:"web"`
	Observer bool   `json:"clickhouseObserver"`
	Error    string `json:"error,omitempty"`
}

func (s *Service) environments() []Environment {
	if s.opt.Self != nil {
		e := Environment{Name: SelfEnvironmentName, Title: "本平台"}
		if inv, err := s.opt.Self.Inventory(); err != nil {
			e.Error = clip(err.Error(), 200)
		} else {
			e.Agents, e.Metrics = len(inv.Agents), len(inv.Metrics)
			e.MQTT, e.Web, e.Observer = inv.MQTT != "", inv.Web != "", inv.Observers.ClickHouseSecretRef != ""
		}
		return []Environment{e}
	}
	entries, _ := os.ReadDir(s.opt.InventoryDir)
	var out []Environment
	for _, e := range entries {
		name, ok := envName(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		env := Environment{Name: name}
		inv, err := LoadInventory(filepath.Join(s.opt.InventoryDir, e.Name()))
		if err != nil {
			env.Error = "清单无效：" + clip(err.Error(), 200)
		} else {
			env.Title, env.Agents, env.Metrics = inv.Name, len(inv.Agents), len(inv.Metrics)
			env.MQTT, env.TCP, env.Web, env.Observer = inv.MQTT != "", inv.TCP != "", inv.Web != "", inv.Observers.ClickHouseSecretRef != ""
		}
		out = append(out, env)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func envName(file string) (string, bool) {
	ext := filepath.Ext(file)
	if ext != ".yaml" && ext != ".yml" {
		return "", false
	}
	name := strings.TrimSuffix(file, ext)
	return name, identifier.MatchString(name) && !strings.HasPrefix(name, ".")
}

// resolve returns the inventory of an environment and, for inventory
// directories, its file path.
func (s *Service) resolve(name string) (*Inventory, string, error) {
	if s.opt.Self != nil {
		if name != SelfEnvironmentName && name != "" {
			return nil, "", errors.New("unknown environment")
		}
		inv, err := s.opt.Self.Inventory()
		return inv, "", err
	}
	path, err := s.inventoryPath(name)
	if err != nil {
		return nil, "", err
	}
	inv, err := LoadInventory(path)
	if err != nil {
		return nil, path, fmt.Errorf("环境清单无效：%w", err)
	}
	return inv, path, nil
}

func (s *Service) inventoryPath(name string) (string, error) {
	if !identifier.MatchString(name) || strings.HasPrefix(name, ".") {
		return "", errors.New("unknown environment")
	}
	for _, ext := range []string{".yaml", ".yml"} {
		p := filepath.Join(s.opt.InventoryDir, name+ext)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", errors.New("unknown environment")
}

// PlanCheck is the validation answer shown before starting a run.
type PlanCheck struct {
	Valid       bool      `json:"valid"`
	Errors      []string  `json:"errors"`
	Name        string    `json:"name,omitempty"`
	Preset      string    `json:"preset,omitempty"`
	Suite       string    `json:"suite,omitempty"`
	DeviceCount int       `json:"deviceCount,omitempty"`
	Rates       []float64 `json:"rates,omitempty"`
	Modules     []string  `json:"modules"`
	Faults      int       `json:"faults"`
	MaxWall     string    `json:"maximumWallTime,omitempty"`
}

type planRequest struct {
	Environment string `json:"environment"`
	Plan        string `json:"plan"`
	// Set by the platform proxy, never by browsers: the caller's tenant and a
	// token issued for the caller, used as the run's operator identity.
	Tenant        string `json:"tenant,omitempty"`
	OperatorToken string `json:"operatorToken,omitempty"`
}

func (s *Service) check(req planRequest) (PlanCheck, *Plan, *Inventory, string) {
	out := PlanCheck{Errors: []string{}, Modules: []string{}}
	inv, invPath, err := s.resolve(req.Environment)
	if err != nil {
		if strings.HasPrefix(err.Error(), "环境清单无效") {
			out.Errors = append(out.Errors, err.Error())
		} else {
			out.Errors = append(out.Errors, "请选择已登记的测试环境")
		}
	}
	if len(req.Plan) > 256<<10 {
		out.Errors = append(out.Errors, "计划超过 256 KiB")
		return out, nil, nil, ""
	}
	p, perr := ParsePlan([]byte(req.Plan))
	if perr != nil {
		out.Errors = append(out.Errors, perr.Error())
		return out, nil, nil, ""
	}
	if req.Tenant != "" {
		p.Fixtures.Tenant = req.Tenant
	}
	if verr := p.Validate(); verr != nil {
		out.Errors = append(out.Errors, strings.Split(verr.Error(), "\n")...)
	}
	if inv != nil {
		for _, f := range p.Faults.Actions {
			if !inventoryHasAgent(inv, f.Agent) {
				out.Errors = append(out.Errors, fmt.Sprintf("故障动作引用的 Agent %s 不在环境 %s 中", f.Agent, req.Environment))
			}
		}
	}
	out.Name, out.Preset, out.Suite, out.DeviceCount, out.Rates = p.Name, p.Preset, p.Suite, p.Fixtures.DeviceCount, p.Search.Rates
	out.Modules = sortedKeys(p.ModuleStreams())
	if p.Modules.Backup.Enabled {
		out.Modules = append(out.Modules, "backup")
	}
	if p.Modules.Realtime.Enabled {
		out.Modules = append(out.Modules, "realtime")
	}
	out.Faults = len(p.Faults.Actions)
	out.MaxWall = p.Budget.MaximumWallTime.D().String()
	out.Valid = len(out.Errors) == 0
	return out, p, inv, invPath
}

func inventoryHasAgent(inv *Inventory, name string) bool {
	for _, a := range inv.Agents {
		if a.Name == name {
			return true
		}
	}
	return false
}

var ErrRunActive = errors.New("a capacity run is already active")

// Start saves the plan under the results directory and starts it; it returns
// once the run has an ID (or failed before creating evidence).
func (s *Service) Start(req planRequest) (string, PlanCheck, error) {
	chk, _, inv, invPath := s.check(req)
	if !chk.Valid {
		return "", chk, errors.New("plan is invalid")
	}
	s.mu.Lock()
	if s.active != "" {
		s.mu.Unlock()
		return "", chk, ErrRunActive
	}
	s.active = "starting"
	s.mu.Unlock()
	sum := sha256.Sum256([]byte(req.Plan))
	planDir := filepath.Join(s.opt.ResultsDir, ".plans")
	planPath := filepath.Join(planDir, time.Now().UTC().Format("20060102T150405Z")+"-"+hex.EncodeToString(sum[:4])+".yaml")
	err := os.MkdirAll(planDir, 0o750)
	if err == nil {
		err = os.WriteFile(planPath, []byte(req.Plan), 0o640)
	}
	if err != nil {
		s.finish("", err)
		return "", chk, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan string, 1)
	done := make(chan struct{})
	s.mu.Lock()
	s.cancel, s.done = cancel, done
	s.mu.Unlock()
	opt := RunOptions{PlanPath: planPath, InventoryPath: invPath, SecretsPath: s.opt.SecretsPath, ResultsDir: s.opt.ResultsDir, SourceCommit: s.opt.SourceCommit, Log: s.opt.Log, FaultAllow: s.opt.FaultAllow, NewStore: s.opt.NewStore, NewAgent: s.opt.NewAgent,
		Tenant: req.Tenant, OperatorToken: req.OperatorToken,
		OnStart: func(id string) {
			s.mu.Lock()
			s.active = id
			s.mu.Unlock()
			started <- id
		}}
	if s.opt.Self != nil {
		opt.Inventory, opt.ExtraSecrets = inv, s.opt.Self.secrets()
	}
	errc := make(chan error, 1)
	go func() {
		defer close(done)
		defer cancel()
		id, err := Run(ctx, opt)
		s.finish(id, err)
		errc <- err
	}()
	select {
	case id := <-started:
		return id, chk, nil
	case err := <-errc:
		if err == nil {
			err = errors.New("run ended before it started")
		}
		return "", chk, err
	}
}

func (s *Service) finish(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active, s.last = "", err
	if err != nil {
		fmt.Fprintf(s.opt.Log, "capacity run %s ended with error: %v\n", id, err)
	}
}

// Shutdown stops an active run (soft) and waits for it to write its report.
func (s *Service) Shutdown(timeout time.Duration) {
	s.mu.Lock()
	id, done := s.active, s.done
	s.mu.Unlock()
	if id == "" || done == nil {
		return
	}
	if runIDPattern.MatchString(id) {
		_ = RequestStop(s.opt.ResultsDir, id, false)
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// RunInfo is one row of the run list.
type RunInfo struct {
	RunID          string       `json:"runId"`
	Status         string       `json:"status"`
	Message        string       `json:"message,omitempty"`
	StartedAt      int64        `json:"startedAt"`
	UpdatedAt      int64        `json:"updatedAt"`
	PhaseID        string       `json:"phaseId,omitempty"`
	TargetRate     float64      `json:"targetRate,omitempty"`
	MeasureFrom    int64        `json:"measureFrom,omitempty"`
	MeasureTo      int64        `json:"measureTo,omitempty"`
	Completed      []PhaseBrief `json:"completed"`
	Active         bool         `json:"active"`
	Plan           string       `json:"plan,omitempty"`
	Preset         string       `json:"preset,omitempty"`
	Verdict        string       `json:"verdict,omitempty"`
	VerdictReason  string       `json:"verdictReason,omitempty"`
	Classification string       `json:"classification,omitempty"`
	LowerBound     *float64     `json:"lowerPassedBound,omitempty"`
	UpperBound     *float64     `json:"upperFailedBound,omitempty"`
	Recommended    *float64     `json:"recommendedOperatingValue,omitempty"`
	Conclusion     string       `json:"conclusion,omitempty"`
	Reports        []string     `json:"reports,omitempty"`
}

func (s *Service) runInfo(id string) (RunInfo, error) {
	st, err := ReadState(s.opt.ResultsDir, id)
	if err != nil {
		return RunInfo{}, err
	}
	s.mu.Lock()
	active := s.active == id
	s.mu.Unlock()
	info := RunInfo{RunID: id, Status: st.Status, Message: st.Message, StartedAt: st.StartedAt, UpdatedAt: st.UpdatedAt, PhaseID: st.PhaseID, TargetRate: st.TargetRate, MeasureFrom: st.MeasureFrom, MeasureTo: st.MeasureTo, Completed: st.Completed, Active: active}
	if info.Completed == nil {
		info.Completed = []PhaseBrief{}
	}
	dir := filepath.Join(s.opt.ResultsDir, id)
	if p, err := LoadPlan(filepath.Join(dir, "plan.sanitized.yaml")); err == nil {
		info.Plan, info.Preset = p.Name, p.Preset
	}
	var sum Summary
	if b, err := os.ReadFile(filepath.Join(dir, "summary.json")); err == nil && json.Unmarshal(b, &sum) == nil {
		info.Verdict, info.VerdictReason = sum.Verdict, sum.VerdictReason
		r := sum.Capacity["mixedBusinessMessagesPerSecond"]
		info.Classification, info.LowerBound, info.UpperBound, info.Recommended = r.Classification, r.LowerPassedBound, r.UpperFailedBound, r.RecommendedOperatingValue
	}
	for _, f := range reportFiles {
		if _, err := os.Stat(filepath.Join(dir, f.file)); err == nil {
			info.Reports = append(info.Reports, f.format)
		}
	}
	if len(info.Reports) > 0 {
		info.Reports = append(info.Reports, "zip")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "report.md")); err == nil {
		// The first paragraph after the title is the plan §9.3 conclusion.
		if _, rest, ok := strings.Cut(string(b), "## 1. 本次结论\n\n"); ok {
			info.Conclusion, _, _ = strings.Cut(rest, "\n")
		}
	}
	return info, nil
}

func (s *Service) runs(limit int) []RunInfo {
	entries, _ := os.ReadDir(s.opt.ResultsDir)
	var ids []string
	for _, e := range entries {
		if e.IsDir() && runIDPattern.MatchString(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	out := []RunInfo{}
	for _, id := range ids {
		if len(out) >= limit {
			break
		}
		if info, err := s.runInfo(id); err == nil {
			out = append(out, info)
		}
	}
	return out
}

var reportFiles = []struct{ format, file, contentType string }{
	{"html", "report.html", "text/html; charset=utf-8"},
	{"markdown", "report.md", "text/markdown; charset=utf-8"},
	{"json", "summary.json", "application/json"},
	{"csv", "phases.csv", "text/csv; charset=utf-8"},
}

func (s *Service) writeReport(w http.ResponseWriter, id, format string) {
	dir := filepath.Join(s.opt.ResultsDir, id)
	if format == "zip" {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.zip"`)
		zw := zip.NewWriter(w)
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			if rel == StopFile {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			zf, err := zw.Create(id + "/" + filepath.ToSlash(rel))
			if err == nil {
				_, _ = io.Copy(zf, f)
			}
			return nil
		})
		_ = zw.Close()
		return
	}
	for _, f := range reportFiles {
		if f.format != format {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, f.file))
		if err != nil {
			serveError(w, http.StatusNotFound, "report_missing", "报告尚未生成")
			return
		}
		w.Header().Set("Content-Type", f.contentType)
		w.Header().Set("Content-Disposition", `attachment; filename="`+id+"-"+f.file+`"`)
		_, _ = w.Write(b)
		return
	}
	serveError(w, http.StatusBadRequest, "bad_format", "format must be html, markdown, json, csv or zip")
}

func serveError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "code": code})
}

func serveJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Handler serves the controller API; every request needs the service token.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if s.opt.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.opt.Token)) != 1 {
				serveError(w, http.StatusUnauthorized, "unauthorized", "invalid service token")
				return
			}
			h(w, r)
		}
	}
	decode := func(w http.ResponseWriter, r *http.Request) (planRequest, bool) {
		var req planRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
			serveError(w, http.StatusBadRequest, "bad_request", "body must be {environment, plan}")
			return req, false
		}
		return req, true
	}
	runID := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := r.PathValue("id")
		if !runIDPattern.MatchString(id) {
			serveError(w, http.StatusNotFound, "not_found", "run not found")
			return "", false
		}
		if _, err := os.Stat(filepath.Join(s.opt.ResultsDir, id, "state.json")); err != nil {
			serveError(w, http.StatusNotFound, "not_found", "run not found")
			return "", false
		}
		return id, true
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { serveJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /v1/environments", auth(func(w http.ResponseWriter, _ *http.Request) {
		serveJSON(w, 200, map[string]any{"items": s.environments()})
	}))
	mux.HandleFunc("POST /v1/plans/validate", auth(func(w http.ResponseWriter, r *http.Request) {
		if req, ok := decode(w, r); ok {
			chk, _, _, _ := s.check(req)
			serveJSON(w, 200, chk)
		}
	}))
	mux.HandleFunc("GET /v1/runs", auth(func(w http.ResponseWriter, _ *http.Request) {
		serveJSON(w, 200, map[string]any{"items": s.runs(50)})
	}))
	mux.HandleFunc("POST /v1/runs", auth(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decode(w, r)
		if !ok {
			return
		}
		id, chk, err := s.Start(req)
		switch {
		case errors.Is(err, ErrRunActive):
			serveError(w, http.StatusConflict, "run_active", "已有容量测试在运行，请等待结束或先停止")
		case err != nil && !chk.Valid:
			serveJSON(w, http.StatusUnprocessableEntity, chk)
		case err != nil:
			serveError(w, http.StatusInternalServerError, "start_failed", clip(err.Error(), 400))
		default:
			serveJSON(w, http.StatusAccepted, map[string]string{"runId": id})
		}
	}))
	mux.HandleFunc("GET /v1/runs/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := runID(w, r); ok {
			info, err := s.runInfo(id)
			if err != nil {
				serveError(w, http.StatusNotFound, "not_found", "run not found")
				return
			}
			serveJSON(w, 200, info)
		}
	}))
	mux.HandleFunc("POST /v1/runs/{id}/stop", auth(func(w http.ResponseWriter, r *http.Request) {
		id, ok := runID(w, r)
		if !ok {
			return
		}
		var body struct {
			Force bool `json:"force"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body)
		if err := RequestStop(s.opt.ResultsDir, id, body.Force); err != nil {
			serveError(w, http.StatusInternalServerError, "stop_failed", clip(err.Error(), 200))
			return
		}
		serveJSON(w, http.StatusAccepted, map[string]any{"runId": id, "force": body.Force})
	}))
	mux.HandleFunc("GET /v1/runs/{id}/report", auth(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := runID(w, r); ok {
			s.writeReport(w, id, firstNonEmpty(r.URL.Query().Get("format"), "html"))
		}
	}))
	return mux
}
