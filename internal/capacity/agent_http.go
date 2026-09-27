package capacity

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AgentHandler exposes a Worker on the control channel. Every request needs the
// shared agent token; requests carry runId and generation so an old controller
// cannot drive a newer run.
func AgentHandler(w *Worker, token string) http.Handler {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(rw http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				agentError(rw, http.StatusUnauthorized, "unauthorized", "invalid agent token")
				return
			}
			h(rw, r)
		}
	}
	decode := func(r *http.Request, v any) error {
		return json.NewDecoder(io.LimitReader(r.Body, 256<<20)).Decode(v)
	}
	ref := func(r *http.Request) RunRef {
		g, _ := strconv.ParseInt(r.URL.Query().Get("generation"), 10, 64)
		return RunRef{RunID: r.URL.Query().Get("runId"), Generation: g}
	}
	mux.HandleFunc("GET /v1/status", auth(func(rw http.ResponseWriter, r *http.Request) {
		s, _ := w.Status(r.Context())
		writeAgentJSON(rw, s)
	}))
	mux.HandleFunc("POST /v1/prepare", auth(func(rw http.ResponseWriter, r *http.Request) {
		var req PrepareRequest
		if decode(r, &req) != nil {
			agentError(rw, 400, "bad_request", "invalid prepare request")
			return
		}
		res, err := w.Prepare(r.Context(), req)
		if err != nil {
			writeAgentErr(rw, err)
			return
		}
		writeAgentJSON(rw, res)
	}))
	mux.HandleFunc("POST /v1/phase", auth(func(rw http.ResponseWriter, r *http.Request) {
		var a PhaseAssignment
		if decode(r, &a) != nil {
			agentError(rw, 400, "bad_request", "invalid phase assignment")
			return
		}
		if err := w.StartPhase(r.Context(), a); err != nil {
			writeAgentErr(rw, err)
			return
		}
		writeAgentJSON(rw, map[string]string{"status": "started"})
	}))
	mux.HandleFunc("POST /v1/heartbeat", auth(func(rw http.ResponseWriter, r *http.Request) {
		var rr RunRef
		if decode(r, &rr) != nil {
			agentError(rw, 400, "bad_request", "invalid heartbeat")
			return
		}
		s, err := w.Heartbeat(r.Context(), rr)
		if err != nil {
			writeAgentErr(rw, err)
			return
		}
		writeAgentJSON(rw, s)
	}))
	mux.HandleFunc("GET /v1/phase-result", auth(func(rw http.ResponseWriter, r *http.Request) {
		res, err := w.PhaseResult(r.Context(), ref(r), r.URL.Query().Get("phaseId"))
		if err != nil {
			writeAgentErr(rw, err)
			return
		}
		writeAgentJSON(rw, res)
	}))
	mux.HandleFunc("GET /v1/ledger", auth(func(rw http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		if err := w.FetchLedger(r.Context(), ref(r), r.URL.Query().Get("phaseId"), &buf); err != nil {
			writeAgentErr(rw, err)
			return
		}
		rw.Header().Set("Content-Type", "application/gzip")
		_, _ = rw.Write(buf.Bytes())
	}))
	for _, path := range []string{"stop", "release"} {
		path := path
		mux.HandleFunc("POST /v1/"+path, auth(func(rw http.ResponseWriter, r *http.Request) {
			var rr RunRef
			if decode(r, &rr) != nil {
				agentError(rw, 400, "bad_request", "invalid request")
				return
			}
			var err error
			if path == "stop" {
				err = w.Stop(r.Context(), rr)
			} else {
				err = w.Release(r.Context(), rr)
			}
			if err != nil {
				writeAgentErr(rw, err)
				return
			}
			writeAgentJSON(rw, map[string]string{"status": path})
		}))
	}
	return mux
}

func writeAgentJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(v)
}

func agentError(rw http.ResponseWriter, status int, code, msg string) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(map[string]string{"errorCode": code, "error": msg})
}

var agentErrorCodes = []struct {
	err    error
	status int
	code   string
}{
	{ErrAgentBusy, http.StatusConflict, "busy"},
	{ErrStaleGeneration, http.StatusConflict, "stale"},
	{ErrNoPhase, http.StatusNotFound, "no_phase"},
	{ErrPhaseRunning, http.StatusConflict, "running"},
}

func writeAgentErr(rw http.ResponseWriter, err error) {
	for _, c := range agentErrorCodes {
		if errors.Is(err, c.err) {
			agentError(rw, c.status, c.code, err.Error())
			return
		}
	}
	agentError(rw, http.StatusInternalServerError, "internal", err.Error())
}

// RemoteAgent is the controller-side client of AgentHandler.
type RemoteAgent struct {
	name, base, token string
	client            *http.Client
}

func NewRemoteAgent(name, base, token string) *RemoteAgent {
	return &RemoteAgent{name: name, base: strings.TrimRight(base, "/"), token: token, client: &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{Proxy: nil}}}
}

func (a *RemoteAgent) Name() string { return a.name }

func (a *RemoteAgent) call(ctx context.Context, method, path string, in, out any, raw io.Writer) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("agent %s unreachable: %w", a.name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e struct {
			Code  string `json:"errorCode"`
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&e)
		for _, c := range agentErrorCodes {
			if c.code == e.Code {
				return c.err
			}
		}
		return fmt.Errorf("agent %s: %d %s", a.name, resp.StatusCode, e.Error)
	}
	if raw != nil {
		_, err = io.Copy(raw, resp.Body)
		return err
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func refQuery(ref RunRef, phaseID string) string {
	v := url.Values{"runId": {ref.RunID}, "generation": {strconv.FormatInt(ref.Generation, 10)}, "phaseId": {phaseID}}
	return "?" + v.Encode()
}

func (a *RemoteAgent) Status(ctx context.Context) (s AgentStatus, err error) {
	err = a.call(ctx, http.MethodGet, "/v1/status", nil, &s, nil)
	return
}
func (a *RemoteAgent) Prepare(ctx context.Context, req PrepareRequest) (r PrepareResult, err error) {
	err = a.call(ctx, http.MethodPost, "/v1/prepare", req, &r, nil)
	return
}
func (a *RemoteAgent) StartPhase(ctx context.Context, p PhaseAssignment) error {
	return a.call(ctx, http.MethodPost, "/v1/phase", p, nil, nil)
}
func (a *RemoteAgent) Heartbeat(ctx context.Context, ref RunRef) (s AgentStatus, err error) {
	err = a.call(ctx, http.MethodPost, "/v1/heartbeat", ref, &s, nil)
	return
}
func (a *RemoteAgent) PhaseResult(ctx context.Context, ref RunRef, phaseID string) (r AgentPhaseResult, err error) {
	err = a.call(ctx, http.MethodGet, "/v1/phase-result"+refQuery(ref, phaseID), nil, &r, nil)
	return
}
func (a *RemoteAgent) FetchLedger(ctx context.Context, ref RunRef, phaseID string, w io.Writer) error {
	return a.call(ctx, http.MethodGet, "/v1/ledger"+refQuery(ref, phaseID), nil, nil, w)
}
func (a *RemoteAgent) Stop(ctx context.Context, ref RunRef) error {
	return a.call(ctx, http.MethodPost, "/v1/stop", ref, nil, nil)
}
func (a *RemoteAgent) Release(ctx context.Context, ref RunRef) error {
	return a.call(ctx, http.MethodPost, "/v1/release", ref, nil, nil)
}
