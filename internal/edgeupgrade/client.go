package edgeupgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/edgeagent"
	"iot-platform/internal/model"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Options struct {
	URL, TenantID, NodeID, Secret                         string
	DataDir, BootstrapBinary, AgentEnvFile, CatalogPolicy string
	AllowHTTP                                             bool
	Output                                                io.Writer
	// Local controls only, chiefly useful for isolated deployment tests.
	PollInterval time.Duration
	ReadyTimeout time.Duration
}
type Launcher struct {
	options Options
	client  *http.Client
	lock    *os.File
	state   journal
	child   *childProcess
}
type journal struct {
	CurrentPath       string `json:"currentPath"`
	CurrentVersion    string `json:"currentVersion"`
	CurrentSHA256     string `json:"currentSha256"`
	PreviousPath      string `json:"previousPath"`
	PreviousVersion   string `json:"previousVersion"`
	PreviousSHA256    string `json:"previousSha256"`
	FailedGeneration  int64  `json:"failedGeneration"`
	AppliedGeneration int64  `json:"appliedGeneration"`
	LastError         string `json:"lastError,omitempty"`
	LastPhase         string `json:"lastPhase,omitempty"`
}

var ErrUnauthorized = errors.New("node program authentication was rejected")

func New(options Options) (*Launcher, error) {
	u, err := url.Parse(options.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(options.AllowHTTP && u.Scheme == "http")) {
		return nil, errors.New("program control requires HTTPS origin")
	}
	for _, id := range []string{options.TenantID, options.NodeID} {
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\?#\x00") {
			return nil, errors.New("invalid node identity")
		}
	}
	if options.Secret == "" || options.DataDir == "" || options.BootstrapBinary == "" || options.AgentEnvFile == "" {
		return nil, errors.New("node credential, program directory, bootstrap binary and private agent environment file are required")
	}
	options.URL = strings.TrimRight(options.URL, "/")
	options.DataDir, err = filepath.Abs(options.DataDir)
	if err != nil {
		return nil, err
	}
	options.BootstrapBinary, err = filepath.Abs(options.BootstrapBinary)
	if err != nil {
		return nil, err
	}
	options.AgentEnvFile, err = filepath.Abs(options.AgentEnvFile)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(options.AgentEnvFile)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("agent environment must be a local regular file")
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 15 * time.Second
	}
	if options.ReadyTimeout <= 0 {
		options.ReadyTimeout = 30 * time.Second
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if err = os.MkdirAll(options.DataDir, 0700); err != nil {
		return nil, err
	}
	lock, err := edgeagent.LockState(filepath.Join(options.DataDir, "launcher.lock"))
	if err != nil {
		return nil, errors.New("another launcher owns the program directory")
	}
	l := &Launcher{options: options, lock: lock, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	l.state.CurrentPath = options.BootstrapBinary
	if data, err := readFile(filepath.Join(options.DataDir, "program.json"), 8192); err == nil {
		if json.Unmarshal(data, &l.state) != nil {
			lock.Close()
			return nil, errors.New("invalid program journal")
		}
	} else if !os.IsNotExist(err) {
		lock.Close()
		return nil, err
	}
	for _, path := range []string{l.state.CurrentPath, l.state.PreviousPath} {
		if path == "" {
			continue
		}
		relative, err := filepath.Rel(options.DataDir, path)
		if path != options.BootstrapBinary && (err != nil || !filepath.IsLocal(relative)) {
			lock.Close()
			return nil, errors.New("program journal refers outside the local program directory")
		}
	}
	return l, nil
}
func readFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("local state exceeds limit")
	}
	return b, nil
}
func (l *Launcher) save() error {
	b, err := json.Marshal(l.state)
	if err != nil {
		return err
	}
	return edgeagent.WriteState(filepath.Join(l.options.DataDir, "program.json"), b)
}
func (l *Launcher) request(ctx context.Context, method string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, l.options.URL+"/api/v1/edge/"+url.PathEscape(l.options.TenantID)+"/"+url.PathEscape(l.options.NodeID)+"/program", bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("X-Edge-Secret", l.options.Secret)
	request.Header.Set("Content-Type", "application/json")
	response, err := l.client.Do(request)
	if err != nil {
		return errors.New("program control request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return ErrUnauthorized
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("program control HTTP %d", response.StatusCode)
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(result)
	}
	return nil
}
func (l *Launcher) report(ctx context.Context, phase, candidate string, generation int64, message string) error {
	if len(message) > 512 {
		message = message[:512]
	}
	return l.request(ctx, "POST", model.EdgeProgramStatus{Phase: phase, Version: l.state.CurrentVersion, CandidateVersion: candidate, Generation: generation, SHA256: l.state.CurrentSHA256, LastError: message}, nil)
}
func (l *Launcher) Close() error {
	if l.child != nil {
		l.child.stop()
	}
	l.client.CloseIdleConnections()
	if l.lock != nil {
		err := l.lock.Close()
		l.lock = nil
		return err
	}
	return nil
}
