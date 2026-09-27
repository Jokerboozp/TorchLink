package capacity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// FaultCommand is one allowlisted failure on an agent host. Commands are
// argv lists executed without a shell; the controller can only name them.
type FaultCommand struct {
	Inject  []string `yaml:"inject" json:"-"`
	Recover []string `yaml:"recover" json:"-"`
	Timeout Duration `yaml:"timeout" json:"-"`
}

// FaultAllowlist maps action names to commands (capacity-test agent -fault-allow).
type FaultAllowlist map[string]FaultCommand

// LoadFaultAllowlist reads the agent-local allowlist. The file must not be
// writable by group or others: it decides which commands the agent runs.
func LoadFaultAllowlist(path string) (FaultAllowlist, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%s must not be writable by group or others (chmod 600)", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Actions FaultAllowlist `yaml:"actions"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err = dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("fault allowlist: %w", err)
	}
	for name, c := range doc.Actions {
		if !identifier.MatchString(name) || len(c.Inject) == 0 || c.Inject[0] == "" || len(c.Recover) == 0 || c.Recover[0] == "" {
			return nil, fmt.Errorf("fault action %q needs a name and non-empty inject and recover argv", name)
		}
		if c.Timeout <= 0 {
			c.Timeout = Duration(time.Minute)
			doc.Actions[name] = c
		}
	}
	return doc.Actions, nil
}

// Names lists the allowlisted actions (reported in agent status).
func (f FaultAllowlist) Names() []string {
	out := make([]string, 0, len(f))
	for n := range f {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

const (
	FaultInject  = "inject"
	FaultRecover = "recover"
)

// FaultRequest names an allowlisted action of the current run.
type FaultRequest struct {
	RunID      string `json:"runId"`
	Generation int64  `json:"generation"`
	Action     string `json:"action"`
	Op         string `json:"op"`
}

type FaultResult struct {
	OK         bool   `json:"ok"`
	StartedAt  int64  `json:"startedAt"` // agent clock, Unix ms
	FinishedAt int64  `json:"finishedAt"`
	ExitCode   int    `json:"exitCode"`
	Output     string `json:"output,omitempty"`
}

var ErrUnknownFault = errors.New("fault action is not in the agent allowlist")

type faultState struct {
	mu       sync.Mutex
	allow    FaultAllowlist
	injected map[string]bool
}

// SetFaults installs the allowlist; call before serving.
func (w *Worker) SetFaults(allow FaultAllowlist) {
	w.faults = &faultState{allow: allow, injected: map[string]bool{}}
}

func (w *Worker) faultNames() []string {
	if w.faults == nil {
		return nil
	}
	return w.faults.allow.Names()
}

// Fault runs an allowlisted inject or recover command for the leased run.
func (w *Worker) Fault(ctx context.Context, req FaultRequest) (FaultResult, error) {
	w.mu.Lock()
	r, err := w.runFor(RunRef{RunID: req.RunID, Generation: req.Generation})
	if err == nil && !w.now().Before(r.leaseUntil) {
		err = ErrStaleGeneration
	}
	w.mu.Unlock()
	if err != nil {
		return FaultResult{}, err
	}
	if w.faults == nil {
		return FaultResult{}, ErrUnknownFault
	}
	cmd, ok := w.faults.allow[req.Action]
	if !ok || (req.Op != FaultInject && req.Op != FaultRecover) {
		return FaultResult{}, ErrUnknownFault
	}
	res := w.runFault(ctx, cmd, req.Op)
	w.faults.mu.Lock()
	if req.Op == FaultInject {
		w.faults.injected[req.Action] = true
	} else if res.OK {
		delete(w.faults.injected, req.Action)
	}
	w.faults.mu.Unlock()
	return res, nil
}

func (w *Worker) runFault(ctx context.Context, c FaultCommand, op string) FaultResult {
	argv := c.Inject
	if op == FaultRecover {
		argv = c.Recover
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout.D())
	defer cancel()
	res := FaultResult{StartedAt: w.now().UnixMilli()}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	res.FinishedAt = w.now().UnixMilli()
	res.OK = err == nil
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	text := strings.TrimSpace(out.String())
	if err != nil && text == "" {
		text = ShortError(err)
	}
	if len(text) > 300 {
		text = "…" + text[len(text)-300:]
	}
	res.Output = text
	return res
}

// recoverAllFaults runs recover for every action still injected; releasing
// or losing the lease must never leave a failure behind.
func (w *Worker) recoverAllFaults() {
	if w.faults == nil {
		return
	}
	w.faults.mu.Lock()
	var names []string
	for n := range w.faults.injected {
		names = append(names, n)
	}
	w.faults.mu.Unlock()
	sort.Strings(names)
	for _, n := range names {
		res := w.runFault(context.Background(), w.faults.allow[n], FaultRecover)
		if res.OK {
			w.faults.mu.Lock()
			delete(w.faults.injected, n)
			w.faults.mu.Unlock()
		}
	}
}

// computeRecovery measures a resilience step from platform counters: the
// baseline before the first injection, throughput while faulted, and the time
// from the last recovery until throughput and backlog are back to baseline.
func computeRecovery(rounds []Round, faults []FaultEvent, measureFrom, measureTo int64) *RecoveryInfo {
	info := &RecoveryInfo{}
	if len(faults) == 0 {
		info.Detail = "没有执行故障动作"
		return info
	}
	first, last := faults[0].InjectedAt, int64(0)
	for _, f := range faults {
		first = min(first, f.InjectedAt)
		if f.RecoverAt == 0 {
			info.Detail = "故障 " + f.Action + " 未确认恢复"
			return info
		}
		last = max(last, f.RecoverAt)
	}
	type sample struct {
		at      int64
		rate    float64
		rateOK  bool
		backlog float64
		blOK    bool
	}
	backlog := BacklogSeries(rounds)
	var samples []sample
	for i := 1; i < len(rounds); i++ {
		s := sample{at: rounds[i].At, backlog: backlog[i].Value, blOK: backlog[i].Valid}
		if v, ok := CounterIncrease(rounds[i-1:i+1], "parse_success_total"); ok {
			if dt := float64(rounds[i].At-rounds[i-1].At) / 1000; dt > 0 {
				s.rate, s.rateOK = v/dt, true
			}
		}
		samples = append(samples, s)
	}
	mean := func(from, to int64) (float64, bool) {
		sum, n := 0.0, 0
		for _, s := range samples {
			if s.at > from && s.at <= to {
				n++
				if s.rateOK {
					sum += s.rate
				}
			}
		}
		if n == 0 {
			return 0, false
		}
		return sum / float64(n), true
	}
	baseline, ok := mean(measureFrom, first)
	if !ok || baseline <= 0 {
		info.Detail = "注入前没有有效的吞吐样本，无法建立基线（请加大 faults.actions.at）"
		return info
	}
	blMax := 0.0
	for _, s := range samples {
		if s.at > measureFrom && s.at <= first && s.blOK {
			blMax = math.Max(blMax, s.backlog)
		}
	}
	round1 := func(v float64) *float64 { v = math.Round(v*10) / 10; return &v }
	info.BaselinePerSec = round1(baseline)
	if during, ok := mean(first, last); ok {
		info.DuringPerSec = round1(during)
	}
	// Recovered once two consecutive samples have backlog back near the
	// baseline and, while load is still offered, throughput at 90% of it.
	limit := blMax*1.2 + 50
	streak := 0
	for _, s := range samples {
		if s.at <= last {
			continue
		}
		good := s.blOK && s.backlog <= limit && (s.at > measureTo || (s.rateOK && s.rate >= 0.9*baseline))
		if !good {
			streak = 0
			continue
		}
		if streak++; streak == 2 {
			info.RecoverySeconds = round1(math.Max(0, float64(s.at-last)/1000))
			info.Detail = fmt.Sprintf("基线 %.1f/s，故障期间 %s/s，恢复后 %.1f 秒回到基线（积压 ≤ %.0f）", baseline, fmtPtr(info.DuringPerSec, ""), *info.RecoverySeconds, limit)
			return info
		}
	}
	info.Detail = fmt.Sprintf("基线 %.1f/s，恢复后在观测窗口内未回到基线（积压上限 %.0f）", baseline, limit)
	return info
}
