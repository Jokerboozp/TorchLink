package capacity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// AgentConfig is what an agent needs to reach the target. Secrets travel only
// on the authenticated control channel and stay in agent memory.
type AgentConfig struct {
	API            string             `json:"api"`
	MQTT           string             `json:"mqtt,omitempty"`
	TCP            string             `json:"tcp,omitempty"`
	OperatorToken  string             `json:"operatorToken,omitempty"`
	Tenant         string             `json:"tenant"`
	Product        string             `json:"product"`
	RequestTimeout Duration           `json:"requestTimeout"`
	ReceiptWait    Duration           `json:"receiptWait"`
	ReceiptRetries int                `json:"receiptRetries"`
	MaxInflight    int                `json:"maxInflight"`
	Fields         int                `json:"fields"`
	MessageBytes   int                `json:"messageBytes"`
	AlarmFraction  float64            `json:"alarmFraction"`
	Seed           int64              `json:"seed"`
	QueryMix       map[string]float64 `json:"queryMix,omitempty"`
	Modules        ModuleConfig       `json:"modules,omitempty"`
}

type DeviceCredential struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Secret string `json:"secret"`
}

type PrepareRequest struct {
	RunID          string             `json:"runId"`
	Generation     int64              `json:"generation"`
	Agent          string             `json:"agent"`
	AgentIndex     int                `json:"agentIndex"`
	Lease          Duration           `json:"lease"`
	Config         AgentConfig        `json:"config"`
	HTTPDevices    []DeviceCredential `json:"httpDevices,omitempty"`
	MQTTDevices    []DeviceCredential `json:"mqttDevices,omitempty"`
	TCPConnections int                `json:"tcpConnections,omitempty"`
}

type PrepareResult struct {
	RealtimeSubscribers int               `json:"realtimeSubscribers"`
	MQTTConnected       int               `json:"mqttConnected"`
	MQTTFailed          int               `json:"mqttFailed"`
	Failures            map[string]uint64 `json:"failures,omitempty"`
	HTTPDevices         int               `json:"httpDevices"`
	TCPDevices          int               `json:"tcpDevices"`
}

// PhaseAssignment gives this agent its share of one load step. StartAt is in
// the agent's own clock (Unix ms) so all agents begin together.
type PhaseAssignment struct {
	RunID      string             `json:"runId"`
	Generation int64              `json:"generation"`
	PhaseID    string             `json:"phaseId"`
	PhaseIndex int                `json:"phaseIndex"`
	StartAt    int64              `json:"startAt"`
	Warmup     Duration           `json:"warmup"`
	Measure    Duration           `json:"measure"`
	Rates      map[string]float64 `json:"rates"`
}

type RunRef struct {
	RunID      string `json:"runId"`
	Generation int64  `json:"generation"`
	Hard       bool   `json:"hard,omitempty"`
}

type AgentStatus struct {
	Agent      string `json:"agent"`
	RunID      string `json:"runId,omitempty"`
	Generation int64  `json:"generation,omitempty"`
	LeaseValid bool   `json:"leaseValid"`
	PhaseID    string `json:"phaseId,omitempty"`
	Running    bool   `json:"running"`
	AgentTime  int64  `json:"agentTime"` // Unix microseconds
	HeapBytes  uint64 `json:"heapBytes"`
	Goroutines int    `json:"goroutines"`
	// Faults lists the allowlisted fault actions (names only).
	Faults []string `json:"faults,omitempty"`
}

type AgentPhaseResult struct {
	Agent        string                  `json:"agent"`
	PhaseID      string                  `json:"phaseId"`
	Done         bool                    `json:"done"`
	Interrupted  bool                    `json:"interrupted"`
	LeaseExpired bool                    `json:"leaseExpired"`
	Reason       string                  `json:"reason,omitempty"`
	Streams      map[string]*StreamStats `json:"streams"`
	Ledger       LedgerInfo              `json:"ledger"`
	HeapBytes    uint64                  `json:"heapBytes"`
	Goroutines   int                     `json:"goroutines"`
}

// Agent is implemented in-process by *Worker and remotely by *RemoteAgent.
type Agent interface {
	Name() string
	Status(ctx context.Context) (AgentStatus, error)
	Prepare(ctx context.Context, req PrepareRequest) (PrepareResult, error)
	StartPhase(ctx context.Context, a PhaseAssignment) error
	Heartbeat(ctx context.Context, ref RunRef) (AgentStatus, error)
	PhaseResult(ctx context.Context, ref RunRef, phaseID string) (AgentPhaseResult, error)
	FetchLedger(ctx context.Context, ref RunRef, phaseID string, w io.Writer) error
	Stop(ctx context.Context, ref RunRef) error
	Release(ctx context.Context, ref RunRef) error
	Fault(ctx context.Context, req FaultRequest) (FaultResult, error)
}

var (
	ErrAgentBusy       = errors.New("agent is leased to another run")
	ErrStaleGeneration = errors.New("stale run or generation")
	ErrNoPhase         = errors.New("phase not found")
	ErrPhaseRunning    = errors.New("phase still running")
)

// Worker is the load-generating core. It holds at most one leased run; when the
// controller stops renewing the lease, the worker stops sending by itself.
type Worker struct {
	name string
	dir  string
	mu   sync.Mutex
	run  *workerRun
	// now is replaceable in tests.
	now    func() time.Time
	faults *faultState
}

type workerRun struct {
	req        PrepareRequest
	leaseUntil time.Time
	cancel     context.CancelFunc
	http       *httpClients
	mqtt       []*mqttPublisher
	tcp        []*tcpDevice
	phases     map[string]*workerPhase
	current    *workerPhase
	modules    *moduleState
}

// httpClients separates device load from management queries so a saturated
// ingest pool cannot starve query measurements (and vice versa).
type httpClients struct {
	load, query *http.Client
}

func newHTTPClients(timeout time.Duration, conns int) *httpClients {
	return &httpClients{load: newHTTPClient(timeout, conns), query: newHTTPClient(timeout, max(conns/4, 16))}
}

func (c *httpClients) close() {
	c.load.CloseIdleConnections()
	c.query.CloseIdleConnections()
}

type workerPhase struct {
	a            PhaseAssignment
	cancel       context.CancelFunc
	hardCancel   context.CancelFunc
	done         chan struct{}
	result       AgentPhaseResult
	ledgerPath   string
	leaseExpired atomic.Bool
}

func NewWorker(name, dir string) *Worker {
	return &Worker{name: name, dir: dir, now: time.Now}
}

func (w *Worker) Name() string { return w.name }

func (w *Worker) Status(context.Context) (AgentStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.statusLocked(), nil
}

func (w *Worker) statusLocked() AgentStatus {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s := AgentStatus{Agent: w.name, AgentTime: w.now().UnixMicro(), HeapBytes: mem.HeapInuse, Goroutines: runtime.NumGoroutine(), Faults: w.faultNames()}
	if r := w.run; r != nil {
		s.RunID, s.Generation, s.LeaseValid = r.req.RunID, r.req.Generation, w.now().Before(r.leaseUntil)
		if r.current != nil {
			s.PhaseID = r.current.a.PhaseID
			select {
			case <-r.current.done:
			default:
				s.Running = true
			}
		}
	}
	return s
}

func (w *Worker) Prepare(ctx context.Context, req PrepareRequest) (PrepareResult, error) {
	if req.Lease <= 0 {
		req.Lease = Duration(20 * time.Second)
	}
	w.mu.Lock()
	if r := w.run; r != nil {
		sameRun := r.req.RunID == req.RunID && req.Generation >= r.req.Generation
		if !sameRun && w.now().Before(r.leaseUntil) {
			w.mu.Unlock()
			return PrepareResult{}, ErrAgentBusy
		}
		w.releaseLocked()
	}
	runCtx, cancel := context.WithCancel(context.Background())
	run := &workerRun{req: req, leaseUntil: w.now().Add(req.Lease.D()), cancel: cancel, phases: map[string]*workerPhase{}}
	w.run = run
	w.mu.Unlock()
	go w.watchLease(runCtx, run)

	cfg := req.Config
	conns := max(cfg.MaxInflight, 16)
	run.http = newHTTPClients(cfg.RequestTimeout.D(), conns)
	res := PrepareResult{HTTPDevices: len(req.HTTPDevices), Failures: map[string]uint64{}}
	if len(req.MQTTDevices) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 64)
		pubs := make([]*mqttPublisher, len(req.MQTTDevices))
		for i, d := range req.MQTTDevices {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, d DeviceCredential) {
				defer wg.Done()
				defer func() { <-sem }()
				p, fail := connectMQTT(ctx, run.http.query, cfg, d, req.RunID)
				mu.Lock()
				defer mu.Unlock()
				if p == nil {
					res.Failures[fail]++
					return
				}
				pubs[i] = p
			}(i, d)
		}
		wg.Wait()
		for _, p := range pubs {
			if p != nil {
				run.mqtt = append(run.mqtt, p)
			}
		}
		res.MQTTConnected, res.MQTTFailed = len(run.mqtt), len(req.MQTTDevices)-len(run.mqtt)
	}
	for i := 0; i < req.TCPConnections; i++ {
		d := &tcpDevice{source: [6]byte{0x90, byte(0x10 + req.AgentIndex%200)}}
		d.source[2], d.source[3], d.source[4], d.source[5] = byte(i>>24), byte(i>>16), byte(i>>8), byte(i)
		run.tcp = append(run.tcp, d)
	}
	res.TCPDevices = len(run.tcp)
	w.prepareModules(ctx, run)
	if run.modules.realtime != nil {
		res.RealtimeSubscribers = len(run.modules.realtime.clients)
		for code, n := range run.modules.realtime.failures {
			res.Failures["realtime_"+code] += uint64(n)
		}
	}
	// Opening many connections can outlast the lease; the lease counts from
	// the end of preparation, when the controller starts heartbeats.
	w.mu.Lock()
	if w.run == run {
		run.leaseUntil = w.now().Add(req.Lease.D())
	}
	w.mu.Unlock()
	return res, nil
}

func (w *Worker) watchLease(ctx context.Context, run *workerRun) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		w.mu.Lock()
		if w.run != run {
			w.mu.Unlock()
			return
		}
		if !w.now().Before(run.leaseUntil) {
			// The controller is gone: stop sending. Results stay
			// collectable; the next Prepare or Release closes connections.
			if ph := run.current; ph != nil {
				ph.leaseExpired.Store(true)
				ph.cancel()
			}
			// Injected failures are undone without the controller.
			go w.recoverAllFaults()
		}
		w.mu.Unlock()
	}
}

func (w *Worker) runFor(ref RunRef) (*workerRun, error) {
	r := w.run
	if r == nil || r.req.RunID != ref.RunID || ref.Generation < r.req.Generation {
		return nil, ErrStaleGeneration
	}
	return r, nil
}

func (w *Worker) Heartbeat(_ context.Context, ref RunRef) (AgentStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	r, err := w.runFor(ref)
	if err != nil {
		return AgentStatus{}, err
	}
	if !w.now().Before(r.leaseUntil) {
		// An expired lease is not silently revived; the run must be prepared again.
		return w.statusLocked(), ErrStaleGeneration
	}
	r.leaseUntil = w.now().Add(r.req.Lease.D())
	return w.statusLocked(), nil
}

func (w *Worker) StartPhase(_ context.Context, a PhaseAssignment) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	r, err := w.runFor(RunRef{RunID: a.RunID, Generation: a.Generation})
	if err != nil {
		return err
	}
	if !w.now().Before(r.leaseUntil) {
		return ErrStaleGeneration
	}
	if r.current != nil {
		select {
		case <-r.current.done:
		default:
			return ErrPhaseRunning
		}
	}
	if _, exists := r.phases[a.PhaseID]; exists {
		return fmt.Errorf("phase %s already assigned", a.PhaseID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hardCtx, hardCancel := context.WithCancel(context.Background())
	ph := &workerPhase{a: a, cancel: cancel, hardCancel: hardCancel, done: make(chan struct{}), ledgerPath: filepath.Join(w.dir, a.RunID, a.PhaseID+".jsonl.gz")}
	r.phases[a.PhaseID], r.current = ph, ph
	go w.execute(ctx, hardCtx, r, ph)
	return nil
}

func (w *Worker) PhaseResult(_ context.Context, ref RunRef, phaseID string) (AgentPhaseResult, error) {
	w.mu.Lock()
	r, err := w.runFor(ref)
	var ph *workerPhase
	if err == nil {
		ph = r.phases[phaseID]
	}
	w.mu.Unlock()
	if err != nil {
		return AgentPhaseResult{}, err
	}
	if ph == nil {
		return AgentPhaseResult{}, ErrNoPhase
	}
	select {
	case <-ph.done:
		return ph.result, nil
	default:
		return AgentPhaseResult{Agent: w.name, PhaseID: phaseID}, ErrPhaseRunning
	}
}

func (w *Worker) FetchLedger(_ context.Context, ref RunRef, phaseID string, out io.Writer) error {
	w.mu.Lock()
	r, err := w.runFor(ref)
	var ph *workerPhase
	if err == nil {
		ph = r.phases[phaseID]
	}
	w.mu.Unlock()
	if err != nil {
		return err
	}
	if ph == nil {
		return ErrNoPhase
	}
	select {
	case <-ph.done:
	default:
		return ErrPhaseRunning
	}
	f, err := os.Open(ph.ledgerPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(out, f)
	return err
}

// Stop ends new sends of the current phase; a hard stop also aborts in-flight
// requests. Results stay collectable.
func (w *Worker) Stop(_ context.Context, ref RunRef) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	r, err := w.runFor(ref)
	if err != nil {
		return err
	}
	if ph := r.current; ph != nil {
		ph.cancel()
		if ref.Hard {
			ph.hardCancel()
		}
	}
	return nil
}

func (w *Worker) Release(_ context.Context, ref RunRef) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.runFor(ref); err != nil {
		return err
	}
	w.releaseLocked()
	return nil
}

func (w *Worker) releaseLocked() {
	r := w.run
	if r == nil {
		return
	}
	w.run = nil
	r.cancel()
	go w.recoverAllFaults()
	if ph := r.current; ph != nil {
		ph.cancel()
		ph.hardCancel()
	}
	go func() {
		for _, ph := range r.phases {
			<-ph.done
		}
		for _, p := range r.mqtt {
			p.client.Disconnect(250)
		}
		for _, d := range r.tcp {
			d.mu.Lock()
			if d.conn != nil {
				d.conn.Close()
			}
			d.mu.Unlock()
		}
		if r.http != nil {
			r.http.close()
		}
		if r.modules != nil {
			r.modules.realtime.close()
		}
		_ = os.RemoveAll(filepath.Join(w.dir, r.req.RunID))
	}()
}

// execute runs one phase open loop: every stream follows its own fixed
// schedule from StartAt; requests that cannot start because MaxInflight is
// exhausted are recorded as not sent (generator shortfall), never delayed into
// a lower effective rate.
func (w *Worker) execute(ctx, hardCtx context.Context, r *workerRun, ph *workerPhase) {
	defer close(ph.done)
	a, cfg := ph.a, r.req.Config
	res := AgentPhaseResult{Agent: w.name, PhaseID: a.PhaseID, Streams: map[string]*StreamStats{}}
	ledger, err := NewLedgerWriter(ph.ledgerPath, LedgerHeader{RunID: a.RunID, Agent: w.name, Generation: a.Generation, PhaseID: a.PhaseID, Tenant: cfg.Tenant, Product: cfg.Product})
	if err != nil {
		res.Done, res.Interrupted, res.Reason = true, true, "ledger: "+err.Error()
		ph.result = res
		return
	}
	start := time.UnixMilli(a.StartAt)
	measureFrom := start.Add(a.Warmup.D())
	end := measureFrom.Add(a.Measure.D())
	sem := make(chan struct{}, max(cfg.MaxInflight, 1))
	var inflight sync.WaitGroup
	var streams sync.WaitGroup
	recs := map[string]*streamRecorder{}
	picker := newQueryPicker(cfg.QueryMix)
	short := a.RunID[max(0, len(a.RunID)-6):]
	for _, stream := range sortedKeys(a.Rates) {
		rate := a.Rates[stream]
		if rate <= 0 {
			continue
		}
		if (stream == "http" && len(r.req.HTTPDevices) == 0) || (stream == "mqtt" && len(r.mqtt) == 0) || (stream == "tcp" && len(r.tcp) == 0) || (stream == "query" && len(picker.names) == 0) {
			res.Reason = "no " + stream + " devices or queries prepared for a positive rate"
			res.Interrupted = true
			continue
		}
		rec := &streamRecorder{}
		recs[stream] = rec
		streams.Add(1)
		go func(stream string, rate float64, rec *streamRecorder) {
			defer streams.Done()
			interval := float64(time.Second) / rate
			for k := uint64(0); ; k++ {
				at := start.Add(time.Duration(float64(k) * interval))
				if !at.Before(end) {
					return
				}
				if d := at.Sub(w.now()); d > 0 {
					timer := time.NewTimer(d)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				} else if ctx.Err() != nil {
					return
				}
				measured := !at.Before(measureFrom)
				rec.scheduled(measured)
				entry := LedgerEntry{Seq: k, Stream: stream, Measured: measured, Scheduled: at.UnixMicro()}
				select {
				case sem <- struct{}{}:
				default:
					rec.notSent(measured)
					if isMessageStream(stream) {
						entry.Result = "not_sent"
						ledger.Write(entry)
					}
					continue
				}
				inflight.Add(1)
				go func(k uint64, at time.Time, entry LedgerEntry) {
					defer inflight.Done()
					defer func() { <-sem }()
					w.send(hardCtx, r, ph, stream, k, at, short, picker, rec, ledger, entry)
				}(k, at, entry)
			}
		}(stream, rate, rec)
	}
	// Realtime subscribers count pushes delivered during the measure window.
	var rt *realtimeSubscribers
	if r.modules != nil && r.modules.realtime != nil {
		rt = r.modules.realtime
		rt.rec = &streamRecorder{}
		go func() {
			if d := time.Until(measureFrom); d > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(d):
				}
			}
			rt.measure.Store(true)
		}()
	}
	streams.Wait()
	inflight.Wait()
	if rt != nil {
		rt.measure.Store(false)
		res.Streams["realtime"] = rt.rec.snapshot()
	}
	info, lerr := ledger.Close()
	res.Ledger = info
	for k, rec := range recs {
		res.Streams[k] = rec.snapshot()
	}
	if lerr != nil {
		res.Interrupted, res.Reason = true, "ledger: "+lerr.Error()
	}
	if ctx.Err() != nil && w.now().Before(end) {
		res.Interrupted = true
		if res.Reason == "" {
			res.Reason = "stopped before the phase ended"
		}
	}
	res.LeaseExpired = ph.leaseExpired.Load()
	if res.LeaseExpired {
		res.Reason = "controller lease expired; sending stopped"
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	res.HeapBytes, res.Goroutines, res.Done = mem.HeapInuse, runtime.NumGoroutine(), true
	ph.result = res
}

func (w *Worker) send(ctx context.Context, r *workerRun, ph *workerPhase, stream string, k uint64, at time.Time, short string, picker queryPicker, rec *streamRecorder, ledger *LedgerWriter, e LedgerEntry) {
	cfg := r.req.Config
	a := ph.a
	timeout := cfg.RequestTimeout.D()
	dispatched := w.now()
	e.Dispatched = dispatched.UnixMicro()
	lateness := float64(dispatched.Sub(at).Microseconds()) / 1000
	alarm := mix(uint64(cfg.Seed), uint64(a.PhaseIndex), uint64(r.req.AgentIndex), k, 1) < cfg.AlarmFraction
	var res sendResult
	switch stream {
	case "http":
		d := r.req.HTTPDevices[k%uint64(len(r.req.HTTPDevices))]
		e.Device, e.ClientID = d.ID, fmt.Sprintf("%s-%d-%d-h%d", short, a.PhaseIndex, r.req.AgentIndex, k)
		body := reportBody(e.ClientID, at.UnixMilli(), cfg.Fields, cfg.MessageBytes, alarm, cfg.Seed, k)
		e.Hash, e.Bytes = payloadHash(body), len(body)
		rctx, cancel := context.WithTimeout(ctx, timeout)
		res = sendHTTPReport(rctx, r.http.load, cfg.API, cfg, d, body)
		cancel()
		if res.rawID == "" {
			res.rawID = StandardRawID(cfg.Tenant, cfg.Product, d.ID, "property", e.ClientID)
		}
	case "mqtt":
		p := r.mqtt[k%uint64(len(r.mqtt))]
		e.Device, e.ClientID = p.device, fmt.Sprintf("%s-%d-%d-m%d", short, a.PhaseIndex, r.req.AgentIndex, k)
		body := reportBody(e.ClientID, at.UnixMilli(), cfg.Fields, cfg.MessageBytes, alarm, cfg.Seed, k)
		e.Hash, e.Bytes = payloadHash(body), len(body)
		wait := cfg.ReceiptWait.D()
		rctx, cancel := context.WithTimeout(ctx, timeout+wait*time.Duration(cfg.ReceiptRetries+1))
		rr := p.receipts.Publish(rctx, p.client, p.topic, body, 1, wait, cfg.ReceiptRetries)
		cancel()
		res = sendResult{ok: rr.OK, code: rr.Code, bytes: int(rr.Bytes), attempts: rr.Attempts, rawID: rr.RawID}
		if res.rawID == "" {
			res.rawID = StandardRawID(cfg.Tenant, cfg.Product, p.device, "property", e.ClientID)
		}
	case "tcp":
		d := r.tcp[k%uint64(len(r.tcp))]
		e.Device = fmt.Sprintf("gb-%x", d.source)
		rctx, cancel := context.WithTimeout(ctx, timeout)
		res = sendGB26875(rctx, cfg.TCP, d, alarm, uint16(k), timeout)
		cancel()
		e.Bytes = res.bytes
	case "query":
		path := QueryEndpoints[picker.pick(mix(uint64(cfg.Seed), uint64(a.PhaseIndex), uint64(r.req.AgentIndex), k, 2))]
		rctx, cancel := context.WithTimeout(ctx, timeout)
		res = sendQuery(rctx, r.http.query, cfg.API, cfg.OperatorToken, path)
		cancel()
	default:
		res = w.sendModule(ctx, r, stream, k)
	}
	responded := w.now()
	rec.done(e.Measured, res.ok, res.code, float64(responded.Sub(dispatched).Microseconds())/1000, lateness, res.attempts, res.bytes)
	if !isMessageStream(stream) {
		if res.resourceID != "" {
			e.ResourceKind, e.ResourceID = res.resourceKind, res.resourceID
			e.Responded, e.Result, e.OK = responded.UnixMicro(), res.code, res.ok
			ledger.Write(e)
		}
		return
	}
	e.Alarm = alarm
	e.Responded, e.Attempts, e.Result, e.OK, e.RawID = responded.UnixMicro(), res.attempts, res.code, res.ok, res.rawID
	ledger.Write(e)
}

// isMessageStream reports device-message streams, which are ledgered and
// reconciled by raw message ID. Module resources are also ledgered for cleanup.
func isMessageStream(stream string) bool {
	return stream == "http" || stream == "mqtt" || stream == "tcp"
}
