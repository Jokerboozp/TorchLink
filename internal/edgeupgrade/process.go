package edgeupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"iot-platform/internal/edgeagent"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type childProcess struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func (c *childProcess) stop() {
	select {
	case <-c.done:
		return
	default:
	}
	_ = c.command.Process.Signal(os.Interrupt)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-c.done:
		return
	case <-timer.C:
		_ = c.command.Process.Kill()
		<-c.done
	}
}
func (l *Launcher) start(ctx context.Context, path, version string) (*childProcess, string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", errors.New("program binary is missing or not a regular file")
	}
	nonce := uuid.NewString()
	ready := filepath.Join(l.options.DataDir, "ready-"+nonce+".json")
	defer os.Remove(ready)
	command := exec.Command(path, "--env-file", l.options.AgentEnvFile)
	command.Dir = filepath.Dir(l.options.AgentEnvFile)
	command.Stdout = l.options.Output
	command.Stderr = l.options.Output
	// The local agent env file owns credentials. Remove inherited node settings
	// so another terminal session cannot redirect the managed process identity.
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "IOT_EDGE_") {
			command.Env = append(command.Env, item)
		}
	}
	command.Env = append(command.Env, "IOT_EDGE_READY_FILE="+ready, "IOT_EDGE_READY_NONCE="+nonce)
	if err = command.Start(); err != nil {
		return nil, "", errors.New("start program process failed")
	}
	child := &childProcess{command: command, done: make(chan struct{})}
	go func() { child.err = command.Wait(); close(child.done) }()
	timeout := time.NewTimer(l.options.ReadyTimeout)
	defer timeout.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			child.stop()
			return nil, "", ctx.Err()
		case <-child.done:
			return nil, "", fmt.Errorf("program exited before authenticated readiness: %v", child.err)
		case <-timeout.C:
			child.stop()
			return nil, "", errors.New("program did not confirm configuration and platform heartbeat before readiness deadline")
		case <-tick.C:
			data, err := readFile(ready, 4096)
			if err != nil {
				continue
			}
			var state edgeagent.ReadyState
			if json.Unmarshal(data, &state) == nil && state.Nonce == nonce && state.PID == command.Process.Pid && state.TenantID == l.options.TenantID && state.NodeID == l.options.NodeID && state.Version != "" && (version == "" || state.Version == version) {
				return child, state.Version, nil
			}
		}
	}
}
