package parser

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// WorkerModeServe marks an artifact whose Worker was verified at publication to
// answer consecutive requests from one process. Such Workers stay resident and
// are reused; other artifacts still start one process per call.
const WorkerModeServe = "serve"

// WorkerModeEnv tells a Worker to serve newline-delimited requests until stdin
// closes, echoing each request's requestId in its one-line JSON response.
const WorkerModeEnv = "IOT_PROTOCOL_WORKER_MODE"

var (
	residentPerArtifact = 4
	residentIdleTimeout = 2 * time.Minute
	residentReapEvery   = 30 * time.Second
)

var residentWorkers = &residentPool{groups: map[string]*residentGroup{}}

type residentPool struct {
	mu      sync.Mutex
	groups  map[string]*residentGroup
	reaping bool
}

// residentGroup holds the processes of one artifact. Processes are never shared
// between artifacts, so tenants and release versions stay separated.
type residentGroup struct {
	slots chan struct{}
	mu    sync.Mutex
	idle  []*residentProcess
}

type residentProcess struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	responses chan residentLine
	stderr    *stderrTail
	exited    chan struct{}
	closing   chan struct{}
	closeOnce sync.Once
	lastUsed  time.Time
	nextID    atomic.Uint64
}

type residentLine struct {
	data []byte
	err  error
}

func (p *residentPool) group(key string) *residentGroup {
	p.mu.Lock()
	defer p.mu.Unlock()
	g := p.groups[key]
	if g == nil {
		g = &residentGroup{slots: make(chan struct{}, residentPerArtifact)}
		p.groups[key] = g
	}
	if !p.reaping {
		p.reaping = true
		go p.reap()
	}
	return g
}

// invoke runs one request on an idle process of the artifact, starting one when
// none is idle. A process that fails, times out or answers out of contract is
// killed and replaced on a later call.
func (p *residentPool) invoke(ctx context.Context, key, path string, timeout time.Duration, input []byte) ([]byte, error) {
	g := p.group(key)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case g.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("external parser timed out after %s waiting for a worker", timeout)
	}
	defer func() { <-g.slots }()

	proc := g.take()
	if proc == nil {
		var err error
		if proc, err = startResident(path); err != nil {
			return nil, err
		}
	}
	output, err := proc.call(ctx, input)
	if err != nil {
		proc.kill()
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("external parser timed out after %s", timeout)
		}
		return nil, err
	}
	g.put(proc)
	return output, nil
}

func (g *residentGroup) take() *residentProcess {
	g.mu.Lock()
	defer g.mu.Unlock()
	for len(g.idle) > 0 {
		proc := g.idle[len(g.idle)-1]
		g.idle = g.idle[:len(g.idle)-1]
		select {
		case <-proc.exited:
			continue
		default:
			return proc
		}
	}
	return nil
}

func (g *residentGroup) put(proc *residentProcess) {
	proc.lastUsed = time.Now()
	g.mu.Lock()
	g.idle = append(g.idle, proc)
	g.mu.Unlock()
}

func (p *residentPool) reap() {
	ticker := time.NewTicker(residentReapEvery)
	defer ticker.Stop()
	for range ticker.C {
		p.mu.Lock()
		groups := make([]*residentGroup, 0, len(p.groups))
		for _, g := range p.groups {
			groups = append(groups, g)
		}
		p.mu.Unlock()
		for _, g := range groups {
			g.mu.Lock()
			kept := g.idle[:0]
			for _, proc := range g.idle {
				if time.Since(proc.lastUsed) >= residentIdleTimeout {
					proc.stop()
				} else {
					kept = append(kept, proc)
				}
			}
			g.idle = kept
			g.mu.Unlock()
		}
	}
}

func startResident(path string) (*residentProcess, error) {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.Env = append(externalEnvironment(), WorkerModeEnv+"="+WorkerModeServe)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	proc := &residentProcess{cmd: cmd, stdin: stdin, responses: make(chan residentLine, 1), stderr: &stderrTail{}, exited: make(chan struct{}), closing: make(chan struct{})}
	cmd.Stderr = proc.stderr
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("start external parser: %w", err)
	}
	go proc.readResponses(stdout)
	return proc, nil
}

// readResponses owns the process lifetime: it reads one line per request and
// waits for the process once stdout closes.
func (p *residentProcess) readResponses(stdout io.Reader) {
	defer close(p.exited)
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		line, err := readBoundedLine(reader, maxExternalOutput)
		select {
		case p.responses <- residentLine{data: line, err: err}:
		case <-p.closing:
			err = errors.New("worker closed")
		}
		if err != nil {
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
			return
		}
	}
}

func readBoundedLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(line)+len(chunk) > limit+1 {
			return nil, fmt.Errorf("external parser output exceeds %d bytes", limit)
		}
		line = append(line, chunk...)
		if err == nil {
			return bytes.TrimRight(line, "\r\n"), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}

func (p *residentProcess) call(ctx context.Context, input []byte) ([]byte, error) {
	id := strconv.FormatUint(p.nextID.Add(1), 10)
	p.stderr.Reset()
	if _, err := p.stdin.Write(withRequestID(input, id)); err != nil {
		return nil, p.failure(fmt.Errorf("write external parser request: %w", err))
	}
	select {
	case line := <-p.responses:
		if line.err != nil {
			return nil, p.failure(line.err)
		}
		var envelope struct {
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal(line.data, &envelope); err != nil || envelope.RequestID != id {
			// A stray or late line would otherwise be read as the next answer.
			return nil, errors.New("external parser response does not match the request")
		}
		return line.data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *residentProcess) failure(err error) error {
	if message := strings.TrimSpace(p.stderr.String()); message != "" {
		return fmt.Errorf("external parser failed: %s", message)
	}
	if errors.Is(err, io.EOF) {
		return errors.New("external parser exited without a response")
	}
	return fmt.Errorf("external parser failed: %w", err)
}

func (p *residentProcess) kill() {
	p.closeOnce.Do(func() { close(p.closing) })
	_ = p.cmd.Process.Kill()
}

// stop closes stdin so the Worker exits on its own, then kills it if it lingers.
func (p *residentProcess) stop() {
	_ = p.stdin.Close()
	go func() {
		select {
		case <-p.exited:
		case <-time.After(2 * time.Second):
			p.kill()
		}
	}()
}

// withRequestID adds the requestId field to a JSON object request line.
func withRequestID(input []byte, id string) []byte {
	input = bytes.TrimRight(input, "\r\n")
	field := `"requestId":"` + id + `"`
	out := make([]byte, 0, len(input)+len(field)+3)
	out = append(out, '{')
	out = append(out, field...)
	if body := bytes.TrimSpace(input[1:]); len(body) > 0 && body[0] != '}' {
		out = append(out, ',')
	}
	out = append(out, input[1:]...)
	return append(out, '\n')
}

// stderrTail keeps the start of a Worker's stderr for the current request and
// keeps draining the pipe once full so the Worker never blocks on logging.
type stderrTail struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *stderrTail) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if room := 16<<10 - s.buf.Len(); room > 0 {
		s.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (s *stderrTail) Reset() {
	s.mu.Lock()
	s.buf.Reset()
	s.mu.Unlock()
}

func (s *stderrTail) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// ProbeServe reports whether the artifact's Worker answers consecutive requests
// from one process exactly as it answers a fresh process. Publication records
// the result as the artifact's workerMode; it is never guessed at runtime.
func (p ExternalParser) ProbeServe(ctx context.Context, config map[string]any, request any) error {
	artifact, err := externalArtifact(config)
	if err != nil {
		return err
	}
	path, err := p.artifactPath(artifact)
	if err != nil {
		return err
	}
	single := map[string]any{}
	for key, value := range config {
		single[key] = value
	}
	plain := map[string]any{}
	for key, value := range artifact {
		plain[key] = value
	}
	delete(plain, "workerMode")
	single["artifact"] = plain
	want, err := p.Invoke(ctx, single, request)
	if err != nil {
		return err
	}
	input, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, externalTimeout(config))
	defer cancel()
	proc, err := startResident(path)
	if err != nil {
		return err
	}
	defer proc.kill()
	for attempt := 0; attempt < 2; attempt++ {
		got, callErr := proc.call(ctx, input)
		if callErr != nil {
			return callErr
		}
		if !sameResponse(want, got) {
			return errors.New("resident response differs from a fresh worker")
		}
	}
	return nil
}

func sameResponse(want, got []byte) bool {
	var a, b map[string]any
	if json.Unmarshal(want, &a) != nil || json.Unmarshal(got, &b) != nil {
		return false
	}
	delete(a, "requestId")
	delete(b, "requestId")
	return reflect.DeepEqual(a, b)
}
