package protocolruntime

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
)

const (
	listenerMaxBuffer    = protocolworker.MaxFrameBytes
	listenerMaxState     = 64 << 10
	listenerMaxSessions  = 128
	listenerIdle         = 2 * time.Minute
	listenerFrameTimeout = 30 * time.Second
)

type listenerCall func(context.Context, string, model.ProtocolRelease, protocolworker.Request) (protocolworker.Response, error)

// Listeners owns generic TCP/UDP sockets. Protocol packages own framing,
// session state and wire encoding; the host owns tenant/product identity and
// only acknowledges an incoming frame after the ingest callback succeeds.
type Listeners struct {
	registerDevice     func(context.Context, model.DeviceAccessProfile, string, string) (model.ManagedDevice, error)
	coordinator        *Coordinator
	connectionMu       sync.Mutex
	connectionCounts   map[string]int
	connectionReporter func(context.Context, string, string, string, bool, int64) error
	repo               ports.Repository
	root               string
	ingest             IngestFunc
	log                *slog.Logger
	call               listenerCall
	workers            chan struct{}
	mu                 sync.Mutex
	hosts              map[string]*protocolListener
	failures           map[string]string
	registerMu         sync.Mutex
	once               sync.Once
}

type protocolListener struct {
	lastAcceptedAt int64
	owner          *Listeners
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	profile        model.DeviceAccessProfile
	tcp            net.Listener
	udp            net.PacketConn
	sessions       map[string]*listenerSession
}

type listenerSession struct {
	host         *protocolListener
	remote       string
	conn         net.Conn
	addr         net.Addr
	packets      chan []byte
	done         chan struct{}
	once         sync.Once
	mu           sync.Mutex
	closed       bool
	deviceID     string
	state        json.RawMessage
	release      model.ProtocolRelease
	partial      bool
	frameStarted time.Time
	lastSeen     time.Time
	pending      map[string]chan commandResult
}

type commandResult struct {
	value map[string]any
	err   error
}

func NewListeners(repo ports.Repository, root string, ingest IngestFunc, log *slog.Logger) *Listeners {
	return &Listeners{repo: repo, root: root, ingest: ingest, log: log, call: protocolworker.Call, workers: make(chan struct{}, 32), hosts: make(map[string]*protocolListener), failures: make(map[string]string)}
}

func (r *Listeners) SetCoordinator(c *Coordinator) { r.coordinator = c }

func (r *Listeners) Start(ctx context.Context) {
	r.once.Do(func() {
		go func() {
			r.reconcile(ctx)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			defer r.stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r.reconcile(ctx)
				}
			}
		}()
	})
}

func listenerKey(tenant, profile string) string { return tenant + "\x00" + profile }

// Status reports the sockets in this process, without overwriting profile
// configuration when an operator edits or disables it concurrently.
func (r *Listeners) Status(tenant, profile string) (string, string, int64) {
	key := listenerKey(tenant, profile)
	r.mu.Lock()
	h, failure := r.hosts[key], r.failures[key]
	r.mu.Unlock()
	if h == nil {
		if failure != "" {
			return "ERROR", failure, 0
		}
		return "PENDING", "", 0
	}
	p := h.snapshot()
	if _, err := r.release(h.ctx, p); err != nil {
		return "ERROR", err.Error(), 0
	}
	h.mu.Lock()
	last := h.lastAcceptedAt
	h.mu.Unlock()
	return "LISTENING", "", last
}

func (r *Listeners) reconcile(ctx context.Context) {
	profiles, err := r.repo.ListDeviceAccessProfiles(ctx, "")
	if err != nil {
		r.warn("list protocol listeners", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	wanted := make(map[string]bool)
	for _, p := range profiles {
		if !p.Enabled || p.EdgeNodeID != "" || !strings.EqualFold(p.Mode, "listener") {
			continue
		}
		executionCtx := ctx
		if r.coordinator != nil {
			var owned bool
			executionCtx, owned = r.coordinator.Claim(ctx, p)
			if !owned {
				continue
			}
		}
		p.Network = strings.ToLower(strings.TrimSpace(p.Network))
		key := listenerKey(p.TenantID, p.ID)
		wanted[key] = true
		if current := r.hosts[key]; current != nil {
			current.mu.Lock()
			old := current.profile
			unchanged := current.ctx.Err() == nil && old.Host == p.Host && old.Port == p.Port && old.Network == p.Network && old.ProductID == p.ProductID && old.DeviceID == p.DeviceID
			if unchanged {
				current.profile = p
			}
			current.mu.Unlock()
			if unchanged {
				continue
			}
			current.stop()
			delete(r.hosts, key)
		}
		if p.TenantID == "" || p.ProductID == "" || p.Port < 1 || p.Port > 65535 || (p.Network != "tcp" && p.Network != "udp") {
			r.warn("invalid protocol listener profile", fmt.Errorf("profile %s", p.ID))
			continue
		}
		if _, err := r.release(ctx, p); err != nil {
			r.failures[key] = err.Error()
			r.warn("protocol listener binding", err)
			continue
		}
		hostCtx, cancel := context.WithCancel(executionCtx)
		h := &protocolListener{owner: r, ctx: hostCtx, cancel: cancel, profile: p, sessions: make(map[string]*listenerSession)}
		address := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
		if p.Network == "tcp" {
			h.tcp, err = net.Listen("tcp", address)
		} else {
			h.udp, err = net.ListenPacket("udp", address)
		}
		if err != nil {
			cancel()
			r.failures[key] = err.Error()
			r.warn("open protocol listener", fmt.Errorf("profile %s: %w", p.ID, err))
			continue
		}
		context.AfterFunc(hostCtx, h.stop)
		r.hosts[key] = h
		delete(r.failures, key)
		if h.tcp != nil {
			go h.accept()
		} else {
			go h.receiveUDP()
		}
	}
	for key, h := range r.hosts {
		if !wanted[key] {
			h.stop()
			delete(r.hosts, key)
		}
	}
	for key := range r.failures {
		if !wanted[key] {
			delete(r.failures, key)
		}
	}
}

func (r *Listeners) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, h := range r.hosts {
		h.stop()
		delete(r.hosts, key)
	}
}

func (r *Listeners) warn(message string, err error) {
	if r.log != nil {
		r.log.Warn(message, "error", err)
	}
}

func (r *Listeners) release(ctx context.Context, p model.DeviceAccessProfile) (model.ProtocolRelease, error) {
	binding, err := r.repo.GetProductProtocolBinding(ctx, p.TenantID, p.ProductID)
	if err != nil {
		return model.ProtocolRelease{}, fmt.Errorf("product binding: %w", err)
	}
	release, err := r.repo.GetProtocolRelease(ctx, p.TenantID, binding.ProtocolID, binding.Version)
	if err != nil {
		return release, err
	}
	if release.Status != "PUBLISHED" {
		return release, errors.New("listener protocol release is not published")
	}
	if release.ParserType != parser.GoProtocolParserName || release.Artifact["runtime"] != protocolworker.Runtime || !protocolworker.HasCapability(release, "ingress") {
		return release, errors.New("listener requires a complete Go protocol package")
	}
	if !strings.EqualFold(release.Transport, p.Network) && release.Transport != "TCP_UDP" {
		return release, errors.New("listener network does not match bound protocol transport")
	}
	return release, nil
}

func (h *protocolListener) snapshot() model.DeviceAccessProfile {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.profile
}

func (h *protocolListener) stop() {
	h.cancel()
	if h.tcp != nil {
		_ = h.tcp.Close()
	}
	if h.udp != nil {
		_ = h.udp.Close()
	}
	h.mu.Lock()
	sessions := make([]*listenerSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	for _, s := range sessions {
		s.close()
	}
}

func (h *protocolListener) newSession(remote string, conn net.Conn, addr net.Addr) *listenerSession {
	return &listenerSession{host: h, remote: remote, conn: conn, addr: addr, done: make(chan struct{}), lastSeen: time.Now(), pending: make(map[string]chan commandResult)}
}

func (h *protocolListener) accept() {
	for {
		conn, err := h.tcp.Accept()
		if err != nil {
			if h.ctx.Err() == nil {
				h.owner.warn("accept protocol connection", err)
			}
			return
		}
		h.mu.Lock()
		if len(h.sessions) >= listenerMaxSessions || h.ctx.Err() != nil {
			h.mu.Unlock()
			_ = conn.Close()
			continue
		}
		s := h.newSession(conn.RemoteAddr().String(), conn, nil)
		h.sessions[s.remote] = s
		h.mu.Unlock()
		go s.receiveTCP()
	}
}

func (s *listenerSession) close() {
	s.once.Do(func() {
		close(s.done)
		if s.conn != nil {
			_ = s.conn.Close()
		}
		s.mu.Lock()
		s.closed = true
		deviceID := s.deviceID
		for id, ch := range s.pending {
			ch <- commandResult{err: errors.New("device session closed")}
			delete(s.pending, id)
		}
		s.mu.Unlock()
		s.host.mu.Lock()
		if s.host.sessions[s.remote] == s {
			delete(s.host.sessions, s.remote)
		}
		s.host.mu.Unlock()
		if deviceID != "" {
			s.host.owner.reportConnection(s.host.snapshot(), deviceID, false)
		}
	})
}

func (s *listenerSession) receiveTCP() {
	defer s.close()
	buffer := make([]byte, 0, 16384)
	chunk := make([]byte, 16384)
	for {
		s.mu.Lock()
		deadline := time.Now().Add(listenerIdle)
		if s.partial {
			deadline = s.frameStarted.Add(listenerFrameTimeout)
		}
		s.mu.Unlock()
		_ = s.conn.SetReadDeadline(deadline)
		n, err := s.conn.Read(chunk[:min(len(chunk), listenerMaxBuffer-len(buffer))])
		if n > 0 {
			if len(buffer)+n > listenerMaxBuffer {
				return
			}
			buffer = append(buffer, chunk[:n]...)
			for len(buffer) > 0 {
				consumed, needMore, frameErr := s.frame(buffer, false)
				if frameErr != nil {
					s.host.owner.warn("protocol TCP frame rejected", frameErr)
					return
				}
				if needMore {
					break
				}
				buffer = buffer[consumed:]
			}
		}
		if err != nil {
			return
		}
	}
}

func (h *protocolListener) receiveUDP() {
	buffer := make([]byte, 65536)
	for {
		_ = h.udp.SetReadDeadline(time.Now().Add(time.Second))
		n, addr, err := h.udp.ReadFrom(buffer)
		if err != nil {
			if h.ctx.Err() != nil {
				return
			}
			if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
				h.owner.warn("receive protocol datagram", err)
				return
			}
			h.expireUDP()
			continue
		}
		if n == 0 {
			continue
		}
		h.mu.Lock()
		s := h.sessions[addr.String()]
		if s == nil && len(h.sessions) < listenerMaxSessions {
			s = h.newSession(addr.String(), nil, addr)
			s.packets = make(chan []byte, 16)
			h.sessions[s.remote] = s
			go s.processUDP()
		}
		h.mu.Unlock()
		if s != nil {
			select {
			case s.packets <- append([]byte(nil), buffer[:n]...):
			default:
			}
		}
		h.expireUDP()
	}
}

func (h *protocolListener) expireUDP() {
	h.mu.Lock()
	sessions := make([]*listenerSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		expired := time.Since(s.lastSeen) > listenerIdle && len(s.pending) == 0
		s.mu.Unlock()
		if expired {
			s.close()
		}
	}
}

func (s *listenerSession) processUDP() {
	defer s.close()
	for {
		select {
		case <-s.done:
			return
		case <-s.host.ctx.Done():
			return
		case packet := <-s.packets:
			if _, _, err := s.frame(packet, true); err != nil {
				s.host.owner.warn("protocol UDP frame rejected", err)
			}
		}
	}
}

// selectRelease is called under the session lock. A partially received frame
// and outstanding commands keep their original wire semantics until complete.
func (s *listenerSession) selectRelease(ctx context.Context, p model.DeviceAccessProfile) error {
	if s.release.Version != "" && (s.partial || len(s.pending) > 0) {
		return nil
	}
	release, err := s.host.owner.release(ctx, p)
	if err != nil {
		return err
	}
	if s.release.ProtocolID != release.ProtocolID || s.release.Version != release.Version {
		s.state = nil
	}
	s.release = release
	return nil
}

func (s *listenerSession) invoke(ctx context.Context, p model.DeviceAccessProfile, request protocolworker.Request) (protocolworker.Response, error) {
	r := s.host.owner
	timeout := time.Duration(p.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case r.workers <- struct{}{}:
	case <-ctx.Done():
		return protocolworker.Response{}, ctx.Err()
	}
	defer func() { <-r.workers }()
	request.Version = 2
	request.State = append(json.RawMessage(nil), s.state...)
	if request.Now == 0 {
		request.Now = time.Now().UnixMilli()
	}
	response, err := r.call(ctx, r.root, s.release, request)
	if err != nil {
		return response, err
	}
	if response.Error != "" {
		return response, errors.New(response.Error)
	}
	if len(response.State) > listenerMaxState || (len(response.State) > 0 && !json.Valid(response.State)) {
		return response, errors.New("protocol state is invalid or too large")
	}
	if len(response.Reply) > listenerMaxBuffer*2 {
		return response, errors.New("protocol reply is too large")
	}
	return response, nil
}

func (s *listenerSession) frame(data []byte, datagram bool) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.host.ctx.Err() != nil {
		return 0, false, errors.New("listener stopped")
	}
	p := s.host.snapshot()
	if err := s.selectRelease(s.host.ctx, p); err != nil {
		return 0, false, err
	}
	receivedAt := time.Now().UnixMilli()
	response, err := s.invoke(s.host.ctx, p, protocolworker.Request{Operation: "ingress", Data: hex.EncodeToString(data), Now: receivedAt})
	if err != nil {
		return 0, false, err
	}
	if response.NeedMore {
		if datagram || response.Consumed != 0 || len(data) >= listenerMaxBuffer {
			return 0, false, errors.New("invalid incomplete protocol frame")
		}
		if !s.partial {
			s.frameStarted = time.Now()
		}
		s.partial = true
		return 0, true, nil
	}
	if response.Consumed <= 0 || response.Consumed > len(data) || (datagram && response.Consumed != len(data)) {
		return 0, false, errors.New("invalid protocol consumed length")
	}
	reply, err := hex.DecodeString(response.Reply)
	if err != nil {
		return 0, false, errors.New("invalid protocol reply hex")
	}
	deviceID := strings.TrimSpace(response.DeviceID)
	if deviceID == "" {
		deviceID = s.deviceID
	}
	if deviceID == "" {
		deviceID = p.DeviceID
	}
	if s.deviceID != "" && deviceID != s.deviceID {
		return 0, false, errors.New("protocol session device identity changed")
	}
	device, err := s.host.owner.device(s.host.ctx, p, deviceID, response.DeviceName)
	if err != nil {
		return 0, false, err
	}
	payload, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(data[:response.Consumed])))
	raw := model.RawMessage{Source: "go-protocol-" + p.Network + "-listener", TenantID: p.TenantID, ProductID: p.ProductID, DeviceID: device.ID, DeviceName: device.Name, Protocol: s.release.ProtocolID, Transport: strings.ToUpper(p.Network), PayloadFormat: "hex", Payload: payload, RemoteAddress: s.remote, ProtocolID: s.release.ProtocolID, ProtocolVersion: s.release.Version, PointTableVersion: s.release.PointTableVersion, CollectorID: p.CollectorID, Metadata: map[string]any{"profileId": p.ID}}
	// Decode runs in a fresh process, possibly asynchronously or during replay.
	// Preserve the state from before this frame instead of consulting a live session.
	if len(s.state) > 0 {
		raw.Metadata["protocolState"] = append(json.RawMessage(nil), s.state...)
	}
	raw.ReceivedAt = receivedAt
	raw.Normalize(time.Now())
	if c := s.host.owner.coordinator; c != nil {
		if err := c.Validate(s.host.ctx, s.host.snapshot()); err != nil {
			return 0, false, err
		}
	}
	if err := s.host.owner.ingest(s.host.ctx, raw); err != nil {
		return 0, false, fmt.Errorf("ingest protocol frame: %w", err)
	}
	s.partial = false
	if s.deviceID == "" {
		s.host.owner.reportConnection(p, device.ID, true)
	}
	s.deviceID = device.ID
	s.lastSeen = time.Now()
	s.host.mu.Lock()
	s.host.lastAcceptedAt = max(s.host.lastAcceptedAt, s.lastSeen.UnixMilli())
	s.host.mu.Unlock()
	s.state = append(json.RawMessage(nil), response.State...)
	if len(reply) > 0 {
		if err := s.write(reply); err != nil {
			return 0, false, err
		}
	}
	if ch := s.pending[response.CorrelationID]; response.CorrelationID != "" && ch != nil {
		delete(s.pending, response.CorrelationID)
		ch <- commandResult{value: map[string]any{"status": "acknowledged", "correlationId": response.CorrelationID, "rawMessageId": raw.MessageID, "protocolId": s.release.ProtocolID, "protocolVersion": s.release.Version}}
	}
	return response.Consumed, false, nil
}

func (r *Listeners) device(ctx context.Context, p model.DeviceAccessProfile, id, name string) (model.ManagedDevice, error) {
	if !protocolworker.ValidDeviceID(id) {
		return model.ManagedDevice{}, errors.New("protocol returned invalid device id")
	}
	if p.DeviceID != "" && p.DeviceID != id {
		return model.ManagedDevice{}, errors.New("protocol device does not match access profile")
	}
	r.registerMu.Lock()
	defer r.registerMu.Unlock()
	device, err := r.repo.GetManagedDevice(ctx, p.TenantID, id)
	if err == nil {
		if device.TenantID != p.TenantID || device.ProductID != p.ProductID || strings.EqualFold(device.Status, "DISABLED") {
			return device, errors.New("protocol device is disabled or belongs to another product")
		}
		return device, nil
	}
	if !p.AutoRegister {
		return device, fmt.Errorf("protocol device is not registered: %w", err)
	}
	if r.registerDevice != nil {
		registered, err := r.registerDevice(ctx, p, id, name)
		if err != nil {
			return device, err
		}
		if registered.ID != id || model.RegisteredProtocolDevice(registered, p) != nil {
			return device, model.ErrProtocolRegistration
		}
		name = registered.Name
	}
	device, _, err = r.repo.RegisterProtocolDevice(ctx, p, id, name)
	return device, err

}

// write is serialized by the session lock with ingress and command encoding.
func (s *listenerSession) write(data []byte) error {
	if s.conn != nil {
		_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		for len(data) > 0 {
			n, err := s.conn.Write(data)
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			data = data[n:]
		}
		return nil
	}
	n, err := s.host.udp.WriteTo(data, s.addr)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

// Command dispatches through the package that owns the online session. A
// nonempty correlationId waits for matching, successfully ingested ingress;
// packages without correlation support return an explicit sent status.
func (r *Listeners) Command(ctx context.Context, tenant, profileID, deviceID string, command map[string]any) (map[string]any, error) {
	r.mu.Lock()
	h := r.hosts[listenerKey(tenant, profileID)]
	r.mu.Unlock()
	if h == nil {
		return nil, errors.New("protocol listener is not running")
	}
	h.mu.Lock()
	sessions := make([]*listenerSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		if s.closed || s.deviceID != deviceID || deviceID == "" {
			s.mu.Unlock()
			continue
		}
		if s.partial {
			s.mu.Unlock()
			return nil, errors.New("device is receiving an incomplete frame; retry command")
		}
		p := h.snapshot()
		if _, err := r.device(ctx, p, deviceID, ""); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if err := s.selectRelease(ctx, p); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if len(s.pending) >= 16 {
			s.mu.Unlock()
			return nil, errors.New("too many pending device commands")
		}
		response, err := s.invoke(ctx, p, protocolworker.Request{Operation: "encode", Command: command})
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		wire, err := hex.DecodeString(response.Reply)
		if err != nil || len(wire) == 0 || (s.conn == nil && len(wire) > 65507) {
			s.mu.Unlock()
			return nil, errors.New("protocol command bytes are empty or invalid")
		}
		id := response.CorrelationID
		if len(id) > 256 {
			s.mu.Unlock()
			return nil, errors.New("protocol command correlation id is too long")
		}
		if _, exists := s.pending[id]; id != "" && exists {
			s.mu.Unlock()
			return nil, errors.New("duplicate protocol command correlation id")
		}
		ch := make(chan commandResult, 1)
		if id != "" {
			s.pending[id] = ch
		}
		if err := s.write(wire); err != nil {
			delete(s.pending, id)
			s.mu.Unlock()
			s.close()
			return nil, err
		}
		s.state = append(json.RawMessage(nil), response.State...)
		version, protocolID := s.release.Version, s.release.ProtocolID
		s.mu.Unlock()
		if id == "" {
			return map[string]any{"status": "sent", "protocolId": protocolID, "protocolVersion": version}, nil
		}
		timeout := time.Duration(p.TimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		if timeout > 30*time.Second {
			timeout = 30 * time.Second
		}
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		defer func() {
			s.mu.Lock()
			if s.pending[id] == ch {
				delete(s.pending, id)
			}
			s.mu.Unlock()
		}()
		select {
		case result := <-ch:
			return result.value, result.err
		case <-waitCtx.Done():
			return nil, fmt.Errorf("wait protocol command reply: %w", waitCtx.Err())
		}
	}
	return nil, errors.New("device has no online protocol session")
}

// SetDeviceRegistrar is configured before Start. Edge nodes must obtain real
// platform registration before acknowledging an unknown protocol identity.
func (r *Listeners) SetDeviceRegistrar(register func(context.Context, model.DeviceAccessProfile, string, string) (model.ManagedDevice, error)) {
	r.registerDevice = register
}
