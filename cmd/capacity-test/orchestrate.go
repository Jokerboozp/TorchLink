package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"iot-platform/internal/capacity"
)

const subcommandUsage = `capacity-test one-click orchestration (docs/DEVELOPMENT.md#容量验证):

  capacity-test plan validate --plan <plan.yaml> [--inventory <inventory.yaml>]
  capacity-test run --plan <plan.yaml> [--inventory <file>] [--secrets <file>] [--results capacity-results]
  capacity-test status --run <runId> [--results capacity-results]
  capacity-test stop --run <runId> [--force] [--results capacity-results]
  capacity-test report --run <runId> [--secrets <file>] [--results capacity-results]
  capacity-test agent --listen :7070 --token-ref <name> [--secrets <file>] [--name <agent>]

Legacy single-mode usage (-mode ...) is unchanged; run with -h for its flags.
`

// subcommand runs the orchestration commands. It returns false for legacy
// "-mode" invocations so the original flag parsing keeps working.
func subcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	var err error
	switch args[0] {
	case "plan":
		if len(args) < 2 || args[1] != "validate" {
			err = errors.New("usage: capacity-test plan validate --plan <file>")
		} else {
			err = planValidate(args[2:])
		}
	case "run":
		err = runCmd(args[1:])
	case "status":
		err = statusCmd(args[1:])
	case "stop":
		err = stopCmd(args[1:])
	case "report":
		err = reportCmd(args[1:])
	case "agent":
		err = agentCmd(args[1:])
	case "help", "--help":
		fmt.Print(subcommandUsage)
	default:
		return false
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	return true
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, subcommandUsage) }
	return fs
}

func planValidate(args []string) error {
	fs := newFlags("plan validate")
	planPath := fs.String("plan", "", "plan file")
	invPath := fs.String("inventory", "", "inventory file (default: target.inventoryRef)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := capacity.LoadPlan(*planPath)
	if err != nil {
		return err
	}
	if err = p.Validate(); err != nil {
		return fmt.Errorf("plan is invalid:\n%w", err)
	}
	inv := *invPath
	if inv == "" {
		inv = capacity.ResolveRef(*planPath, p.Target.InventoryRef)
	}
	if inv != "" {
		if _, err = capacity.LoadInventory(inv); err != nil {
			return err
		}
	}
	_, hash, _ := p.Sanitized()
	fmt.Printf("plan %s is valid (preset %s, suite %s, hash %s)\n", p.Name, p.Preset, p.Suite, hash[:12])
	fmt.Printf("per-device allowance caps the mixed rate at %.0f msg/s; budget caps it at %.0f msg/s\n", p.MaxRateForDevices(), p.Budget.MaximumMessagesPerSecond)
	if inv == "" {
		fmt.Println("no inventory checked (set target.inventoryRef or --inventory)")
	}
	return nil
}

func sourceCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short=12", "HEAD").Output()
	if err != nil {
		return ""
	}
	commit := strings.TrimSpace(string(out))
	if st, err := exec.Command("git", "status", "--porcelain", "--untracked-files=no").Output(); err == nil && len(strings.TrimSpace(string(st))) > 0 {
		commit += "-dirty"
	}
	return commit
}

func runCmd(args []string) error {
	fs := newFlags("run")
	opt := capacity.RunOptions{Log: os.Stdout}
	fs.StringVar(&opt.PlanPath, "plan", "", "plan file")
	fs.StringVar(&opt.InventoryPath, "inventory", "", "inventory file (default: target.inventoryRef)")
	fs.StringVar(&opt.SecretsPath, "secrets", "", "private secrets file (name: value, mode 0600)")
	fs.StringVar(&opt.ResultsDir, "results", "capacity-results", "results directory")
	fs.StringVar(&opt.WorkDir, "work-dir", "", "private work directory for fixture credentials (default <results>/.work)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opt.SourceCommit = sourceCommit()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	started := time.Now()
	go func() {
		<-sig
		fmt.Println("stopping: no new load, finishing in-flight requests, drain, verify, report (Ctrl-C again to skip the drain)")
		cancel()
		<-sig
		// The run ID is only returned when Run ends; the newest run directory
		// created after this command started is ours.
		if dir := newestRun(opt.ResultsDir, started); dir != "" {
			_ = os.WriteFile(filepath.Join(dir, capacity.StopFile), []byte("force\n"), 0o640)
		}
	}()
	runID, err := capacity.Run(ctx, opt)
	if runID != "" {
		dir := filepath.Join(opt.ResultsDir, runID)
		fmt.Printf("run %s finished; report: %s\n", runID, filepath.Join(dir, "report.html"))
		if b, rerr := os.ReadFile(filepath.Join(dir, "summary.json")); rerr == nil {
			var s capacity.Summary
			if json.Unmarshal(b, &s) == nil {
				fmt.Printf("execution=%s verdict=%s %s evidenceComplete=%v\n", s.ExecutionStatus, s.Verdict, s.VerdictReason, s.EvidenceComplete)
			}
		}
	}
	return err
}

func newestRun(results string, after time.Time) string {
	entries, err := os.ReadDir(results)
	if err != nil {
		return ""
	}
	best := ""
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && e.IsDir() && strings.HasPrefix(e.Name(), "cap-") && e.Name() > best && !info.ModTime().Before(after.Add(-time.Second)) {
			best = e.Name()
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join(results, best)
}

func statusCmd(args []string) error {
	fs := newFlags("status")
	run := fs.String("run", "", "run ID")
	results := fs.String("results", "capacity-results", "results directory")
	asJSON := fs.Bool("json", false, "print state.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := capacity.ReadState(*results, *run)
	if err != nil {
		return err
	}
	if *asJSON {
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("run %s  status %s  updated %s\n", st.RunID, st.Status, time.UnixMilli(st.UpdatedAt).Format(time.RFC3339))
	if st.Message != "" {
		fmt.Println("  ", st.Message)
	}
	if st.PhaseID != "" {
		fmt.Printf("  current %s target %.1f msg/s, measuring %s – %s\n", st.PhaseID, st.TargetRate, time.UnixMilli(st.MeasureFrom).Format("15:04:05"), time.UnixMilli(st.MeasureTo).Format("15:04:05"))
	}
	for _, a := range st.Agents {
		fmt.Printf("  agent %-12s lost=%v clock offset %.1fms ±%.1fms\n", a.Name, a.Lost, a.OffsetMS, a.UncertaintyMS)
	}
	for _, p := range st.Completed {
		fmt.Printf("  done %-24s %8.1f msg/s  %s %s\n", p.PhaseID, p.Rate, p.Verdict, p.StopReason)
	}
	return nil
}

func stopCmd(args []string) error {
	fs := newFlags("stop")
	run := fs.String("run", "", "run ID")
	results := fs.String("results", "capacity-results", "results directory")
	force := fs.Bool("force", false, "skip the drain after stopping load")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := capacity.RequestStop(*results, *run, *force); err != nil {
		return err
	}
	fmt.Println("stop requested; the controller stops new load, then drains, verifies and writes a partial report")
	return nil
}

func reportCmd(args []string) error {
	fs := newFlags("report")
	run := fs.String("run", "", "run ID")
	results := fs.String("results", "capacity-results", "results directory")
	secretsPath := fs.String("secrets", "", "secrets file, to verify no secret leaked into the report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var values []string
	if *secretsPath != "" {
		s, err := capacity.LoadSecrets(*secretsPath)
		if err != nil {
			return err
		}
		values = s.AllValues()
	}
	dir := filepath.Join(*results, *run)
	if err := capacity.GenerateReport(dir, values); err != nil {
		return err
	}
	fmt.Println("report regenerated from evidence:", filepath.Join(dir, "report.html"))
	return nil
}

func agentCmd(args []string) error {
	fs := newFlags("agent")
	listen := fs.String("listen", ":7070", "listen address for the controller channel")
	tokenRef := fs.String("token-ref", "", "secret reference of the shared agent token")
	secretsPath := fs.String("secrets", "", "private secrets file")
	name := fs.String("name", "", "agent name (default hostname)")
	workDir := fs.String("work-dir", "capacity-agent", "local ledger directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := capacity.LoadSecrets(*secretsPath)
	if err != nil {
		return err
	}
	token, err := s.Get(*tokenRef)
	if err != nil {
		return err
	}
	if *name == "" {
		*name, _ = os.Hostname()
	}
	w := capacity.NewWorker(*name, *workDir)
	srv := &http.Server{Addr: *listen, Handler: capacity.AgentHandler(w, token), ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf("capacity agent %s listening on %s (ledgers in %s)\n", *name, *listen, *workDir)
	return srv.ListenAndServe()
}
