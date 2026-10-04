// Package protocolrunner compiles and executes uploaded Go protocol code in
// a separate process — in deployments a separate container without network,
// secrets or platform data — and serves it to the platform over a Unix
// socket. Workers are cached by content hash, so the runner never needs the
// platform's data directory: the platform sends a binary the first time the
// runner does not know its hash.
package protocolrunner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"iot-platform/internal/parser"
	"iot-platform/internal/protocolbuild"
)

const (
	maxWorker      = 64 << 20
	maxBuildSource = 160 << 20
	maxInvokeInput = 4 << 20
)

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// InvokeRequest is one protocol operation for a cached worker.
type InvokeRequest struct {
	SHA256     string          `json:"sha256"`
	WorkerMode string          `json:"workerMode,omitempty"`
	TimeoutMs  int64           `json:"timeoutMs"`
	Input      json.RawMessage `json:"input"`
}

// BuildRequest compiles uploaded sources for a platform.
type BuildRequest struct {
	Files    map[string][]byte `json:"files"`
	Entry    string            `json:"entry"`
	Platform string            `json:"platform"`
}

type buildResponse struct {
	Worker []byte `json:"worker,omitempty"`
	Log    string `json:"log"`
	Error  string `json:"error,omitempty"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Unknown bool   `json:"unknownArtifact,omitempty"`
}

// Server is the runner side.
type Server struct {
	// Dir holds the worker cache and build workspace.
	Dir string
	Log *slog.Logger
}

func (s *Server) workerPath(sha string) string { return filepath.Join(s.Dir, "workers", sha) }

// Handler serves /health, /build, /artifacts/{sha256} and /invoke.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("PUT /artifacts/{sha}", s.putArtifact)
	mux.HandleFunc("POST /invoke", s.invoke)
	mux.HandleFunc("POST /build", s.build)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) putArtifact(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	if !hashPattern.MatchString(sha) {
		writeJSON(w, 400, errorResponse{Error: "invalid sha256"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxWorker+1))
	if err != nil || len(data) == 0 || len(data) > maxWorker {
		writeJSON(w, 400, errorResponse{Error: "worker must be 1 byte to 64 MiB"})
		return
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != sha {
		writeJSON(w, 400, errorResponse{Error: "worker hash mismatch"})
		return
	}
	dir := filepath.Join(s.Dir, "workers")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	tmp, err := os.CreateTemp(dir, ".upload-")
	if err == nil {
		_, err = tmp.Write(data)
		err = errors.Join(err, tmp.Chmod(0o700), tmp.Close())
		if err == nil {
			err = os.Rename(tmp.Name(), s.workerPath(sha))
		}
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) invoke(w http.ResponseWriter, r *http.Request) {
	var in InvokeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxInvokeInput)).Decode(&in); err != nil || !hashPattern.MatchString(in.SHA256) {
		writeJSON(w, 400, errorResponse{Error: "invalid invoke request"})
		return
	}
	if _, err := os.Stat(s.workerPath(in.SHA256)); err != nil {
		writeJSON(w, 404, errorResponse{Error: "unknown artifact", Unknown: true})
		return
	}
	artifact := map[string]any{"path": filepath.Join("workers", in.SHA256), "sha256": in.SHA256, "runtime": "go-protocol-v2"}
	if in.WorkerMode != "" {
		artifact["workerMode"] = in.WorkerMode
	}
	config := map[string]any{"artifact": artifact, "timeoutMs": in.TimeoutMs}
	var request any = in.Input
	output, err := parser.ExternalParser{Root: s.Dir, Local: true}.Invoke(r.Context(), config, request)
	if err != nil {
		writeJSON(w, 422, errorResponse{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(output)
}

func (s *Server) build(w http.ResponseWriter, r *http.Request) {
	var in BuildRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBuildSource)).Decode(&in); err != nil {
		writeJSON(w, 400, buildResponse{Error: "invalid build request"})
		return
	}
	worker, log, err := protocolbuild.BuildLocal(r.Context(), s.Dir, in.Files, in.Entry, in.Platform)
	if err != nil {
		writeJSON(w, 422, buildResponse{Log: log, Error: err.Error()})
		return
	}
	writeJSON(w, 200, buildResponse{Worker: worker, Log: log})
}

// Serve listens on the Unix socket until ctx ends.
func (s *Server) Serve(ctx context.Context, socket string) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	// The platform process shares the user id; nobody else may connect.
	if err = os.Chmod(socket, 0o600); err != nil {
		listener.Close()
		return err
	}
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if s.Log != nil {
		s.Log.Info("protocol runner listening", "socket", socket)
	}
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Client is the platform side; it is installed into the parser and the
// builder so every protocol execution goes through the runner.
type Client struct {
	http *http.Client
}

func NewClient(socket string) *Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, MaxIdleConnsPerHost: 64}
	return &Client{http: &http.Client{Transport: transport, Timeout: 5 * time.Minute}}
}

func (c *Client) post(ctx context.Context, path, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://runner"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("protocol runner unavailable: %w", err)
	}
	return resp, nil
}

// Invoke implements parser.Executor. The worker binary is uploaded only when
// the runner does not have its hash yet.
func (c *Client) Invoke(ctx context.Context, worker func() ([]byte, error), sha, workerMode string, timeout time.Duration, input []byte) ([]byte, error) {
	body, err := json.Marshal(InvokeRequest{SHA256: sha, WorkerMode: workerMode, TimeoutMs: timeout.Milliseconds(), Input: input})
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := c.post(ctx, "/invoke", "application/json", body)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode == http.StatusOK {
			return data, nil
		}
		var failure errorResponse
		_ = json.Unmarshal(data, &failure)
		if resp.StatusCode == http.StatusNotFound && failure.Unknown && attempt == 0 {
			binary, err := worker()
			if err != nil {
				return nil, err
			}
			if err = c.upload(ctx, sha, binary); err != nil {
				return nil, err
			}
			continue
		}
		if failure.Error == "" {
			failure.Error = resp.Status
		}
		return nil, errors.New(failure.Error)
	}
	return nil, errors.New("protocol runner did not accept the worker")
}

func (c *Client) upload(ctx context.Context, sha string, binary []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://runner/artifacts/"+sha, bytes.NewReader(binary))
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("protocol runner unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		var failure errorResponse
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&failure)
		return fmt.Errorf("upload worker to protocol runner: %s", failure.Error)
	}
	return nil
}

// Build implements the remote builder.
func (c *Client) Build(ctx context.Context, files map[string][]byte, entry, platform string) ([]byte, string, error) {
	body, err := json.Marshal(BuildRequest{Files: files, Entry: entry, Platform: platform})
	if err != nil {
		return nil, "", err
	}
	resp, err := c.post(ctx, "/build", "application/json", body)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	var out buildResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, maxWorker*2)).Decode(&out); err != nil {
		return nil, "", fmt.Errorf("read protocol runner build result: %w", err)
	}
	if out.Error != "" {
		return nil, out.Log, errors.New(out.Error)
	}
	return out.Worker, out.Log, nil
}

// Health checks the runner.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://runner/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("protocol runner health: %s", resp.Status)
	}
	return nil
}
