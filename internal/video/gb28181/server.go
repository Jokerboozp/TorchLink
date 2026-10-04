package gb28181

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// Config is the platform's SIP server identity.
type Config struct {
	// ServerID is the platform's 20-digit SIP ID; Domain its 10-digit realm.
	ServerID string
	Domain   string
	// Listen is the UDP and TCP listen address, such as ":5060".
	Listen string
	// SIPHost and SIPPort are advertised to devices (Via, Contact). An empty
	// host uses the local address that routes to each device.
	SIPHost string
	SIPPort int
	// NonceKey signs registration challenges.
	NonceKey []byte
	Log      *slog.Logger
}

// Device is a registered device as a request target.
type Device struct {
	ID        string
	Addr      string // ip:port the device's requests come from
	Transport string // UDP or TCP
}

// Registration is an accepted REGISTER.
type Registration struct {
	Device
	Expires   time.Duration
	UserAgent string
}

// Handler connects the signalling to platform state. Every method must be
// safe for concurrent use.
type Handler interface {
	// AllowSource limits which networks may talk to the SIP server at all.
	AllowSource(addr netip.Addr) bool
	// Password returns the device's registration password; false for devices
	// that are unknown or disabled.
	Password(ctx context.Context, deviceID string) (string, bool)
	Registered(ctx context.Context, r Registration)
	Unregistered(ctx context.Context, deviceID string)
	// Known reports whether a request comes from a device that is currently
	// registered from the same IP address.
	Known(ctx context.Context, d Device) bool
	Keepalive(ctx context.Context, d Device)
	Catalog(ctx context.Context, deviceID string, sn, sumNum int, items []CatalogItem)
	DeviceInfo(ctx context.Context, deviceID string, info DeviceInfo)
	// CallEnded is called when a device ends a live call with BYE.
	CallEnded(callID string)
}

// InviteError is a final non-2xx answer to INVITE.
type InviteError struct {
	Status int
	Reason string
}

func (e *InviteError) Error() string {
	return fmt.Sprintf("设备拒绝点播：%d %s", e.Status, e.Reason)
}

var deviceIDPattern = regexp.MustCompile(`^[0-9]{20}$`)

// ValidID reports whether s is a 20-digit GB/T 28181 code.
func ValidID(s string) bool { return deviceIDPattern.MatchString(s) }

// Server is the platform's SIP endpoint for GB/T 28181 devices.
type Server struct {
	cfg    Config
	h      Handler
	log    *slog.Logger
	ua     *sipgo.UserAgent
	srv    *sipgo.Server
	client *sipgo.Client
	dialog sipgo.DialogUA
	nonce  nonceIssuer
	sn     atomic.Int64

	mu    sync.Mutex
	calls map[string]*Call
	udp   net.PacketConn
	tcp   net.Listener
	hosts map[string]string
}

// Call is one live INVITE dialog.
type Call struct {
	ID     string
	Answer Answer
	dialog *sipgo.DialogClientSession
	server *Server
}

// New creates a server; Start binds the sockets.
func New(cfg Config, h Handler) (*Server, error) {
	if !ValidID(cfg.ServerID) {
		return nil, errors.New("GB28181 server ID must be 20 digits")
	}
	if len(cfg.NonceKey) < 16 {
		return nil, errors.New("GB28181 nonce key is too short")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent(cfg.ServerID), sipgo.WithUserAgentHostname(cfg.Domain))
	if err != nil {
		return nil, err
	}
	srv, err := sipgo.NewServer(ua, sipgo.WithServerLogger(cfg.Log))
	if err != nil {
		return nil, err
	}
	opts := []sipgo.ClientOption{sipgo.WithClientLogger(cfg.Log)}
	if cfg.SIPHost != "" {
		opts = append(opts, sipgo.WithClientHostname(cfg.SIPHost), sipgo.WithClientPort(cfg.SIPPort))
	}
	client, err := sipgo.NewClient(ua, opts...)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, h: h, log: cfg.Log, ua: ua, srv: srv, client: client, nonce: nonceIssuer{key: cfg.NonceKey, now: time.Now}, calls: map[string]*Call{}, hosts: map[string]string{}}
	s.dialog = sipgo.DialogUA{Client: client, ContactHDR: sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: cfg.ServerID, Host: cfg.SIPHost, Port: cfg.SIPPort}}, RewriteContact: true}
	srv.OnRegister(s.onRegister)
	srv.OnMessage(s.onMessage)
	srv.OnBye(s.onBye)
	srv.OnAck(func(*sip.Request, sip.ServerTransaction) {})
	srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) { s.respond(req, tx, 200, "OK") })
	srv.OnNotify(s.onNotify)
	srv.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) { s.respond(req, tx, 488, "Not Acceptable Here") })
	s.sn.Store(time.Now().Unix() % 100000)
	return s, nil
}

// Start listens on UDP and TCP. It returns once both sockets are bound;
// serving stops when ctx ends.
func (s *Server) Start(ctx context.Context) error {
	udp, tcp, err := listenSIP(s.cfg.Listen)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.udp, s.tcp = udp, tcp
	if s.cfg.SIPPort == 0 {
		s.cfg.SIPPort = udp.LocalAddr().(*net.UDPAddr).Port
		s.dialog.ContactHDR.Address.Port = s.cfg.SIPPort
	}
	s.mu.Unlock()
	go func() { _ = s.srv.ServeUDP(udp) }()
	go func() { _ = s.srv.ServeTCP(tcp) }()
	go func() {
		<-ctx.Done()
		s.Close()
	}()
	return nil
}

// listenSIP binds UDP and TCP on one port. Port 0 (tests) takes the port
// the kernel picks for UDP and retries when that TCP port is already taken.
func listenSIP(addr string) (net.PacketConn, net.Listener, error) {
	host, port, _ := net.SplitHostPort(addr)
	attempts := 1
	if port == "0" {
		attempts = 20
	}
	var err error
	for range attempts {
		var udp net.PacketConn
		if udp, err = net.ListenPacket("udp", addr); err != nil {
			return nil, nil, fmt.Errorf("listen GB28181 UDP: %w", err)
		}
		tcpAddr := addr
		if port == "0" {
			tcpAddr = net.JoinHostPort(host, strconv.Itoa(udp.LocalAddr().(*net.UDPAddr).Port))
		}
		var tcp net.Listener
		if tcp, err = net.Listen("tcp", tcpAddr); err == nil {
			return udp, tcp, nil
		}
		_ = udp.Close()
	}
	return nil, nil, fmt.Errorf("listen GB28181 TCP: %w", err)
}

// LocalPort is the bound SIP port.
func (s *Server) LocalPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.udp == nil {
		return 0
	}
	return s.udp.LocalAddr().(*net.UDPAddr).Port
}

// Close stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	udp, tcp := s.udp, s.tcp
	s.udp, s.tcp = nil, nil
	s.mu.Unlock()
	if udp != nil {
		_ = udp.Close()
	}
	if tcp != nil {
		_ = tcp.Close()
	}
	_ = s.ua.Close()
}

func (s *Server) respond(req *sip.Request, tx sip.ServerTransaction, code int, reason string, headers ...sip.Header) {
	res := sip.NewResponseFromRequest(req, code, reason, nil)
	for _, h := range headers {
		res.AppendHeader(h)
	}
	if err := tx.Respond(res); err != nil {
		s.log.Debug("gb28181 respond failed", "error", err)
	}
}

func sourceAddr(req *sip.Request) (netip.AddrPort, bool) {
	ap, err := netip.ParseAddrPort(req.Source())
	if err != nil {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), true
}

func transportOf(req *sip.Request) string {
	if strings.EqualFold(req.Transport(), "TCP") {
		return "TCP"
	}
	return "UDP"
}

func (s *Server) onRegister(req *sip.Request, tx sip.ServerTransaction) {
	src, ok := sourceAddr(req)
	if !ok || !s.h.AllowSource(src.Addr()) {
		s.respond(req, tx, 403, "Forbidden")
		return
	}
	from := req.From()
	if from == nil || !ValidID(from.Address.User) {
		s.respond(req, tx, 400, "Bad Request")
		return
	}
	id := from.Address.User
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	auth := req.GetHeader("Authorization")
	if auth == nil {
		s.challenge(req, tx, false)
		return
	}
	p := parseDigest(auth.Value())
	if !s.nonce.valid(p["nonce"], 5*time.Minute) {
		s.challenge(req, tx, true)
		return
	}
	password, known := s.h.Password(ctx, id)
	if !known || p["username"] != id || !verifyDigest(p, string(sip.REGISTER), password) {
		s.log.Info("gb28181 registration rejected", "device", id, "source", src.Addr().String())
		s.respond(req, tx, 403, "Forbidden")
		return
	}
	expires := expiresOf(req)
	if expires == 0 {
		s.h.Unregistered(ctx, id)
		s.respond(req, tx, 200, "OK")
		return
	}
	expires = min(max(expires, 60*time.Second), 24*time.Hour)
	agent := ""
	if h := req.GetHeader("User-Agent"); h != nil {
		agent = h.Value()
	}
	s.h.Registered(ctx, Registration{Device: Device{ID: id, Addr: src.String(), Transport: transportOf(req)}, Expires: expires, UserAgent: agent})
	e := sip.ExpiresHeader(uint32(expires / time.Second))
	headers := []sip.Header{&e, sip.NewHeader("Date", time.Now().Format("2006-01-02T15:04:05.000"))}
	if c := req.Contact(); c != nil {
		headers = append(headers, sip.NewHeader("Contact", c.Value()))
	}
	s.respond(req, tx, 200, "OK", headers...)
}

func (s *Server) challenge(req *sip.Request, tx sip.ServerTransaction, stale bool) {
	value := fmt.Sprintf(`Digest realm="%s",nonce="%s",algorithm=MD5`, s.cfg.Domain, s.nonce.issue())
	if stale {
		value += ",stale=TRUE"
	}
	s.respond(req, tx, 401, "Unauthorized", sip.NewHeader("WWW-Authenticate", value))
}

func expiresOf(req *sip.Request) time.Duration {
	if c := req.Contact(); c != nil {
		if v, ok := c.Params.Get("expires"); ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				return time.Duration(n) * time.Second
			}
		}
	}
	if h := req.GetHeader("Expires"); h != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(h.Value())); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return time.Hour
}

func (s *Server) requestDevice(req *sip.Request) (Device, bool) {
	src, ok := sourceAddr(req)
	from := req.From()
	if !ok || from == nil || !ValidID(from.Address.User) || !s.h.AllowSource(src.Addr()) {
		return Device{}, false
	}
	return Device{ID: from.Address.User, Addr: src.String(), Transport: transportOf(req)}, true
}

func (s *Server) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	d, ok := s.requestDevice(req)
	if !ok {
		s.respond(req, tx, 403, "Forbidden")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Only devices registered from the same address are listened to; an
	// unregistered device gets 403 so that it registers again.
	if !s.h.Known(ctx, d) {
		s.respond(req, tx, 403, "Forbidden")
		return
	}
	m, err := parseMessage(req.Body())
	if err != nil {
		s.respond(req, tx, 400, "Bad Request")
		return
	}
	s.respond(req, tx, 200, "OK")
	switch m.CmdType {
	case "Keepalive":
		s.h.Keepalive(ctx, d)
	case "Catalog":
		if m.XMLName.Local == "Response" {
			s.h.Catalog(ctx, d.ID, m.SN, m.SumNum, m.DeviceList.Items)
		}
	case "DeviceInfo":
		if m.XMLName.Local == "Response" {
			s.h.DeviceInfo(ctx, d.ID, DeviceInfo{DeviceName: m.DeviceName, Manufacturer: m.Manufacturer, Model: m.Model, Firmware: m.Firmware, Channels: m.Channel})
		}
	}
}

// onNotify accepts catalog notifications of subscriptions the platform did
// not make; they are acknowledged and ignored.
func (s *Server) onNotify(req *sip.Request, tx sip.ServerTransaction) {
	if _, ok := s.requestDevice(req); !ok {
		s.respond(req, tx, 403, "Forbidden")
		return
	}
	s.respond(req, tx, 200, "OK")
}

func (s *Server) onBye(req *sip.Request, tx sip.ServerTransaction) {
	id := ""
	if c := req.CallID(); c != nil {
		id = c.Value()
	}
	s.mu.Lock()
	call := s.calls[id]
	delete(s.calls, id)
	s.mu.Unlock()
	if call == nil {
		s.respond(req, tx, 481, "Call/Transaction Does Not Exist")
		return
	}
	_ = call.dialog.ReadBye(req, tx)
	s.h.CallEnded(id)
}

// advertisedHost is the address a device should use to reach the platform.
func (s *Server) advertisedHost(d Device) string {
	if s.cfg.SIPHost != "" {
		return s.cfg.SIPHost
	}
	host, _, _ := net.SplitHostPort(d.Addr)
	s.mu.Lock()
	cached, ok := s.hosts[host]
	s.mu.Unlock()
	if ok {
		return cached
	}
	// A connected UDP socket reveals the local address routing to the
	// device without sending anything.
	local := ""
	if conn, err := net.Dial("udp", net.JoinHostPort(host, "5060")); err == nil {
		local, _, _ = net.SplitHostPort(conn.LocalAddr().String())
		_ = conn.Close()
	}
	s.mu.Lock()
	s.hosts[host] = local
	s.mu.Unlock()
	return local
}

func (s *Server) newRequest(method sip.RequestMethod, d Device, user string) (*sip.Request, error) {
	host, portText, err := net.SplitHostPort(d.Addr)
	if err != nil {
		return nil, fmt.Errorf("设备地址无效")
	}
	port, _ := strconv.Atoi(portText)
	req := sip.NewRequest(method, sip.Uri{Scheme: "sip", User: user, Host: host, Port: port})
	req.SetTransport(d.Transport)
	req.SetDestination(d.Addr)
	via := &sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: d.Transport, Host: s.advertisedHost(d), Port: s.cfg.SIPPort, Params: sip.NewParams()}
	via.Params.Add("branch", sip.GenerateBranchN(16))
	via.Params.Add("rport", "")
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: s.cfg.ServerID, Host: s.cfg.Domain}, Params: sip.NewParams()}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(via)
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: user, Host: s.cfg.Domain}})
	return req, nil
}

// Query sends a MANSCDP query (Catalog, DeviceInfo). Responses arrive later
// as MESSAGE requests and are delivered to the Handler.
func (s *Server) Query(ctx context.Context, d Device, cmd string) error {
	req, err := s.newRequest(sip.MESSAGE, d, d.ID)
	if err != nil {
		return err
	}
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.SetBody(queryBody(cmd, int(s.sn.Add(1)%1000000), d.ID))
	res, err := s.client.Do(ctx, req)
	if err != nil {
		return fmt.Errorf("设备未应答查询：%w", err)
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("设备拒绝查询：%d %s", res.StatusCode, res.Reason)
	}
	return nil
}

// Invite asks the device to send a channel's real-time stream. The caller
// must Ack the returned call, and later Bye it.
func (s *Server) Invite(ctx context.Context, d Device, channelID string, offer Offer) (*Call, error) {
	req, err := s.newRequest(sip.INVITE, d, channelID)
	if err != nil {
		return nil, err
	}
	req.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: s.cfg.ServerID, Host: s.advertisedHost(d), Port: s.cfg.SIPPort}})
	req.AppendHeader(sip.NewHeader("Subject", fmt.Sprintf("%s:%s,%s:0", channelID, offer.SSRC, s.cfg.ServerID)))
	req.AppendHeader(sip.NewHeader("Content-Type", "APPLICATION/SDP"))
	req.SetBody(offer.SDP())
	dialog, err := s.dialog.WriteInvite(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("点播请求发送失败：%w", err)
	}
	if err := dialog.WaitAnswer(ctx, sipgo.AnswerOptions{}); err != nil {
		_ = dialog.Close()
		var resErr *sipgo.ErrDialogResponse
		if errors.As(err, &resErr) {
			return nil, &InviteError{Status: resErr.Res.StatusCode, Reason: resErr.Res.Reason}
		}
		return nil, fmt.Errorf("设备未应答点播：%w", err)
	}
	call := &Call{ID: req.CallID().Value(), Answer: ParseAnswer(dialog.InviteResponse.Body()), dialog: dialog, server: s}
	s.mu.Lock()
	s.calls[call.ID] = call
	s.mu.Unlock()
	return call, nil
}

// Ack confirms the call so the device starts sending.
func (c *Call) Ack(ctx context.Context) error { return c.dialog.Ack(ctx) }

// Bye ends the call; it is safe to call more than once.
func (c *Call) Bye(ctx context.Context) error {
	c.server.mu.Lock()
	_, active := c.server.calls[c.ID]
	delete(c.server.calls, c.ID)
	c.server.mu.Unlock()
	if !active {
		return nil
	}
	return c.dialog.Bye(ctx)
}
