package edgeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/fieldprotocol"
	"iot-platform/internal/model"
	"iot-platform/internal/protocolruntime"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Options struct {
	DiscoveryInterfaces                    []string
	DiscoveryProbeAddress                  string
	AllowAutoRegister                      bool
	AllowCommands                          bool
	CredentialFile                         string
	AllowGoWorkers                         bool
	AllowedListenAddresses                 []string
	URL, TenantID, NodeID, Secret, DataDir string
	AllowedCIDRs                           []string
	AllowedSerialPorts                     []string
	AllowInsecureHTTP                      bool
}
type Agent struct {
	validatedWorkers    map[string]bool
	syncMu              sync.Mutex
	listeners           *protocolruntime.Listeners
	collector           *fieldprotocol.Collector
	options             Options
	client              *http.Client
	queue               *Queue
	log                 *slog.Logger
	mu                  sync.Mutex
	config              model.EdgeConfiguration
	repo                *memory.Repository
	cancel              context.CancelFunc
	lastError           string
	authenticatedConfig bool
	errors              map[string]string
}

func New(options Options, log *slog.Logger) (*Agent, error) {
	u, err := url.Parse(options.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("edge platform URL must be an HTTP(S) origin")
	}
	if u.Scheme != "https" && !(options.AllowInsecureHTTP && u.Scheme == "http") {
		return nil, errors.New("edge transport requires HTTPS; HTTP is only allowed explicitly for isolated tests")
	}
	for _, id := range []string{options.TenantID, options.NodeID} {
		if id == "" || strings.ContainsAny(id, "/\\?#\x00") || len(id) > 128 {
			return nil, errors.New("invalid edge identity")
		}
	}
	if options.Secret == "" || options.DataDir == "" || len(options.AllowedCIDRs) == 0 {
		return nil, errors.New("edge credential, data directory and allowed networks are required")
	}
	queue, err := OpenQueue(filepath.Join(options.DataDir, "outbox"), 64<<20, 10000)
	if err != nil {
		return nil, err
	}
	options.URL = strings.TrimRight(options.URL, "/")
	if log == nil {
		log = slog.Default()
	}
	credentials, err := fieldprotocol.LoadCredentials(options.CredentialFile)
	if err != nil {
		_ = queue.Close()
		return nil, err
	}
	return &Agent{collector: &fieldprotocol.Collector{AllowedCIDRs: options.AllowedCIDRs, Credentials: credentials}, options: options, queue: queue, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, log: log}, nil
}
func (a *Agent) Close() error {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()
	return a.queue.Close()
}
func (a *Agent) request(ctx context.Context, method, suffix string, body, output any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	address := a.options.URL + "/api/v1/edge/" + url.PathEscape(a.options.TenantID) + "/" + url.PathEscape(a.options.NodeID) + suffix
	r, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	r.Header.Set("X-Edge-Secret", a.options.Secret)
	r.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &HTTPError{Status: response.StatusCode}
	}
	if output == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 8<<20))
		return err
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(output)
}

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("edge platform returned HTTP %d", e.Status) }
func (a *Agent) record(operation string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.errors == nil {
		a.errors = map[string]string{}
	}
	if err == nil {
		delete(a.errors, operation)
	} else {
		message := err.Error()
		if len(message) > 512 {
			message = message[:512]
		}
		a.errors[operation] = message
		a.log.Warn("edge operation failed", "operation", operation, "error", message)
	}
	messages := []string{}
	for key, message := range a.errors {
		messages = append(messages, key+": "+message)
	}
	sort.Strings(messages)
	a.lastError = strings.Join(messages, "; ")
	if len(a.lastError) > 512 {
		a.lastError = a.lastError[:512]
	}

}
func (a *Agent) apply(ctx context.Context, cfg model.EdgeConfiguration) error {
	if cfg.TenantID != a.options.TenantID || cfg.NodeID != a.options.NodeID || len(cfg.Tasks) > 256 || cfg.ExpiresAt <= time.Now().UnixMilli() {
		return errors.New("invalid or expired edge configuration")
	}
	for _, task := range cfg.Tasks {
		if task.Profile.TenantID != cfg.TenantID || task.Profile.EdgeNodeID != cfg.NodeID || !task.Profile.Enabled || !(supportsRead(task.Release.Transport) || (task.Profile.Mode == "listener" && workerRelease(task.Release))) {
			return errors.New("unsupported or foreign edge task")
		}
	}
	for _, product := range cfg.Products {
		if product.TenantID != cfg.TenantID {
			return errors.New("foreign product")
		}
	}
	for _, device := range cfg.Devices {
		if device.TenantID != cfg.TenantID {
			return errors.New("foreign device")
		}
	}
	for _, task := range cfg.Tasks {
		if task.Release.TenantID != cfg.TenantID {
			return errors.New("foreign protocol")
		}
	}
	for i := range cfg.Tasks {
		if workerRelease(cfg.Tasks[i].Release) {
			if err := a.prepareWorker(ctx, &cfg.Tasks[i]); err != nil {
				return err
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.config.Revision == cfg.Revision && a.repo != nil {
		a.config.ExpiresAt = cfg.ExpiresAt
		return nil
	}
	repo := a.repo
	fresh := repo == nil
	if fresh {
		repo = memory.NewRepository()
	}
	for _, product := range cfg.Products {
		if product.TenantID != cfg.TenantID {
			return errors.New("foreign product")
		}
		if err := repo.SaveProduct(ctx, product); err != nil {
			return err
		}
	}
	for _, device := range cfg.Devices {
		if device.TenantID != cfg.TenantID {
			return errors.New("foreign device")
		}
		// Public node configuration intentionally omits credentials. Assign a
		// unique local inventory key so multiple public devices do not collide;
		// the empty secret still cannot authenticate any device.
		device.AccessKey = model.ProtocolDeviceAccessKey(device.TenantID, device.ID)
		if err := repo.SaveManagedDevice(ctx, device); err != nil {
			return err
		}
	}
	deviceIDs := map[string]bool{}
	for _, device := range cfg.Devices {
		deviceIDs[device.ID] = true
	}
	previousDevices, err := repo.ListManagedDevices(ctx, cfg.TenantID)
	if err != nil {
		return err
	}
	for _, device := range previousDevices {
		if !deviceIDs[device.ID] {
			device.Status = "DISABLED"
			if err := repo.SaveManagedDevice(ctx, device); err != nil {
				return err
			}
		}
	}
	releases := map[string]bool{}
	for _, task := range cfg.Tasks {
		release := task.Release
		if release.TenantID != cfg.TenantID {
			return errors.New("foreign protocol")
		}
		releaseKey := release.ProtocolID + "\x00" + release.Version
		if _, err := repo.GetProtocolRelease(ctx, release.TenantID, release.ProtocolID, release.Version); err != nil && !releases[releaseKey] {
			if err := repo.CreateProtocolRelease(ctx, release); err != nil {
				return err
			}
			releases[releaseKey] = true
		}
		p := task.Profile
		p.EdgeNodeID = ""
		if err := repo.SaveDeviceAccessProfile(ctx, p); err != nil {
			return err
		}
		if err := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: p.TenantID, ProductID: p.ProductID, ProtocolID: release.ProtocolID, Version: release.Version}); err != nil {
			return err
		}
	}
	// Keep existing listener sessions across protocol binding changes. The
	// runtime switches versions at complete-frame and pending-command boundaries.
	wanted := map[string]bool{}
	for _, task := range cfg.Tasks {
		wanted[task.Profile.ID] = true
	}
	profiles, err := repo.ListDeviceAccessProfiles(ctx, cfg.TenantID)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if !wanted[p.ID] {
			p.Enabled = false
			if err := repo.SaveDeviceAccessProfile(ctx, p); err != nil {
				return err
			}
		}
	}
	a.config, a.repo = cfg, repo
	if fresh {
		runtimeCtx, cancel := context.WithCancel(ctx)
		a.cancel = cancel
		ingest := func(ctx context.Context, raw model.RawMessage) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			raw.CollectorID = a.options.NodeID
			return a.queue.Put(raw)
		}
		runtime := protocolruntime.New(repo, ingest, a.log, a.options.AllowedCIDRs...)
		runtime.SetSerialPorts(a.options.AllowedSerialPorts)
		runtime.SetCollector(a.collector.Read)
		runtime.Start(runtimeCtx)
		a.listeners = protocolruntime.NewListeners(repo, a.options.DataDir, ingest, a.log)
		a.listeners.SetDeviceRegistrar(a.registerProtocolDevice)
		a.listeners.Start(runtimeCtx)
	}
	return nil
}
func (a *Agent) sync(ctx context.Context) error {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	var cfg model.EdgeConfiguration
	if err := a.request(ctx, "GET", "/config", nil, &cfg); err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
			a.mu.Lock()
			if a.cancel != nil {
				a.cancel()
			}
			a.repo = nil
			a.authenticatedConfig = false
			a.mu.Unlock()
		}
		return err
	}
	if err := a.apply(ctx, cfg); err != nil {
		return err
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := atomicFile(filepath.Join(a.options.DataDir, "configuration.json"), b); err != nil {
		return err
	}
	a.mu.Lock()
	a.authenticatedConfig = true
	a.mu.Unlock()
	return nil
}
func (a *Agent) heartbeat(ctx context.Context) error {
	a.mu.Lock()
	h := model.EdgeHeartbeat{Version: Version, ConfigRevision: a.config.Revision, QueueDepth: a.queue.Depth(), RejectedDepth: a.queue.Rejected(), LastError: a.lastError, Capabilities: []string{"MODBUS_TCP", "MODBUS_RTU", "OPC_UA", "SNMP", "BACNET", "ONVIF_READ", "HTTPS_OUTBOX", "READ_DIAGNOSTIC"}}
	for _, address := range a.options.DiscoveryInterfaces {
		if strings.TrimSpace(address) != "" {
			h.Capabilities = append(h.Capabilities, "ONVIF_DISCOVERY")
			break
		}
	}
	if a.options.AllowGoWorkers {
		h.Capabilities = append(h.Capabilities, "GO_PROTOCOL_V2")
		if a.options.AllowAutoRegister {
			h.Capabilities = append(h.Capabilities, "PROTOCOL_AUTO_REGISTER")
		}
	}
	if a.options.AllowCommands && a.options.AllowGoWorkers {
		h.Capabilities = append(h.Capabilities, "PROTOCOL_COMMANDS")
	}
	repo, listeners := a.repo, a.listeners
	a.mu.Unlock()
	if repo != nil {
		profiles, err := repo.ListDeviceAccessProfiles(ctx, a.options.TenantID)
		if err != nil {
			return err
		}
		for i := range profiles {
			profiles[i].EdgeNodeID = a.options.NodeID
			if profiles[i].Mode == "listener" && listeners != nil {
				status, message, last := listeners.Status(profiles[i].TenantID, profiles[i].ID)
				profiles[i].RuntimeStatus, profiles[i].LastError = status, message
				if status == "LISTENING" {
					profiles[i].LastSuccessAt = max(profiles[i].LastSuccessAt, last)
				}
				if status == "ERROR" {
					profiles[i].LastErrorAt = time.Now().UnixMilli()
				}
			}
		}
		h.Profiles = profiles
	}
	return a.request(ctx, "POST", "/heartbeat", h, nil)
}
func (a *Agent) flush(ctx context.Context) error {
	raw, ok, err := a.queue.Next()
	if err != nil || !ok {
		return err
	}
	var receipt struct {
		MessageID string `json:"messageId"`
	}
	if err = a.request(ctx, "POST", "/raw", raw, &receipt); err != nil {
		var rejected *HTTPError
		if errors.As(err, &rejected) && (rejected.Status == 400 || rejected.Status == 403 || rejected.Status == 404 || rejected.Status == 409 || rejected.Status == 413 || rejected.Status == 422) {
			if retainErr := a.queue.Reject(raw); retainErr != nil {
				return retainErr
			}
		}
		return err
	}
	if receipt.MessageID != raw.MessageID {
		return errors.New("platform did not confirm expected raw message id")
	}
	return a.queue.Ack(raw)
}
func (a *Agent) Run(ctx context.Context) error {
	// A previously authenticated configuration may collect offline until its
	// bounded expiry; disk data contains no node credential or device secrets.
	if b, err := os.ReadFile(filepath.Join(a.options.DataDir, "configuration.json")); err == nil {
		var cfg model.EdgeConfiguration
		if json.Unmarshal(b, &cfg) == nil {
			a.record("configuration", a.apply(ctx, cfg))
		}
	}
	initialSync := a.sync(ctx)
	a.record("configuration", initialSync)
	initialHeartbeat := a.heartbeat(ctx)
	a.record("heartbeat", initialHeartbeat)
	ready := false
	if initialSync == nil && initialHeartbeat == nil {
		err := a.writeReady()
		a.record("readiness", err)
		ready = err == nil
	}
	jobsCtx, jobsCancel := context.WithCancel(ctx)
	defer jobsCancel()
	jobsDone := make(chan struct{})
	go func() { defer close(jobsDone); a.readJobs(jobsCtx) }()
	defer func() { jobsCancel(); <-jobsDone }()
	commandDone := make(chan struct{})
	go func() { defer close(commandDone); a.commandJobs(jobsCtx) }()
	defer func() { jobsCancel(); <-commandDone }()
	syncTick := time.NewTicker(5 * time.Second)
	defer syncTick.Stop()
	heartTick := time.NewTicker(10 * time.Second)
	defer heartTick.Stop()
	sendTimer := time.NewTimer(0)
	defer sendTimer.Stop()
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-syncTick.C:
			a.record("configuration", a.sync(ctx))
			a.mu.Lock()
			if a.config.ExpiresAt <= time.Now().UnixMilli() && a.cancel != nil {
				a.cancel()
				a.repo = nil
			}
			a.mu.Unlock()
		case <-heartTick.C:
			heartbeatErr := a.heartbeat(ctx)
			a.record("heartbeat", heartbeatErr)
			a.mu.Lock()
			configured := a.authenticatedConfig && a.repo != nil && a.config.ExpiresAt > time.Now().UnixMilli()
			a.mu.Unlock()
			if !ready && heartbeatErr == nil && configured {
				err := a.writeReady()
				a.record("readiness", err)
				ready = err == nil
			}
		case <-sendTimer.C:
			err := a.flush(ctx)
			a.record("upload", err)
			if err != nil {
				sendTimer.Reset(backoff)
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			} else {
				backoff = time.Second
				sendTimer.Reset(100 * time.Millisecond)
			}
		}
	}
}

// Pending returns only the count; credentials and raw payloads are not exposed.
func (a *Agent) Pending() int { return a.queue.Depth() }

func (a *Agent) RetryRejected() error { return a.queue.RetryRejected() }
