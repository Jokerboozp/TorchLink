package gbmetadata

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	ServerID     string   `json:"serverId"`
	Realm        string   `json:"realm"`
	Listen       string   `json:"listen"`
	AllowedCIDRs []string `json:"allowedCidrs"`
	// Device passwords are local to this standalone process and never uploaded.
	Devices map[string]string `json:"devices"`
}
type transactionResponse struct {
	data    []byte
	expires time.Time
}
type nonceState struct {
	device, peer string
	expires      time.Time
}
type sender func([]byte) error
type session struct {
	peer, network      string
	send               sender
	expires, lastQuery time.Time
	catalog            DeviceCatalog
}
type pendingQuery struct {
	device, kind, callID string
	sn                   int
	deadline, nextRetry  time.Time
	wire                 []byte
	attempts             int
	acked                bool
	channels             map[string]Channel
	total                int
}
type Registrar struct {
	cfg       Config
	mu        sync.Mutex
	sessions  map[string]*session
	nonces    map[string]nonceState
	queries   map[int]*pendingQuery
	responses map[string]transactionResponse
	nextSN    int
}

func New(cfg Config) (*Registrar, error) {
	if !deviceIDPattern.MatchString(cfg.ServerID) || !regexp.MustCompile(`^[0-9]{10}$`).MatchString(cfg.Realm) || len(cfg.Devices) == 0 || len(cfg.Devices) > 256 || len(cfg.AllowedCIDRs) == 0 {
		return nil, errors.New("valid server identity, local device credentials and allowed networks are required")
	}
	host, port, err := net.SplitHostPort(cfg.Listen)
	ip := net.ParseIP(host)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || ip.IsUnspecified() || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return nil, errors.New("SIP listen address requires a concrete local IP and port 1 to 65535")
	}
	for _, cidr := range cfg.AllowedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, err
		}
	}
	for id, password := range cfg.Devices {
		if !deviceIDPattern.MatchString(id) || len(password) < 8 || len(password) > 256 {
			return nil, errors.New("device IDs must be 20 digits and passwords 8 to 256 bytes")
		}
	}
	return &Registrar{cfg: cfg, sessions: map[string]*session{}, nonces: map[string]nonceState{}, queries: map[int]*pendingQuery{}, responses: map[string]transactionResponse{}, nextSN: 1}, nil
}
func randomToken() string {
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (r *Registrar) allowed(peer string) bool {
	host, _, err := net.SplitHostPort(peer)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, value := range r.cfg.AllowedCIDRs {
		_, network, _ := net.ParseCIDR(value)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Handle is invoked by the bounded transport worker pool. Source and send are
// supplied by the socket, never by Contact/Via addresses advertised by a peer.
func (r *Registrar) Handle(data []byte, peer, network string, send sender) {
	if len(data) > 64<<10 || !r.allowed(peer) {
		return
	}
	message, err := sip.ParseMessage(data)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if response, ok := message.(*sip.Response); ok {
		r.response(response, peer, network)
		return
	}
	req, ok := message.(*sip.Request)
	if !ok {
		return
	}
	req.SetSource(peer)
	req.SetTransport(network)
	sum := sha256.Sum256(data)
	key := peer + network + hex.EncodeToString(sum[:])
	now := time.Now()
	if previous, ok := r.responses[key]; ok && now.Before(previous.expires) {
		_ = send(previous.data)
		return
	}
	for key, value := range r.responses {
		if now.After(value.expires) {
			delete(r.responses, key)
		}
	}
	if len(r.responses) >= 2048 {
		_ = send([]byte(sip.NewResponseFromRequest(req, 503, "Transaction Capacity", nil).String()))
		return
	}
	respond := func(data []byte) error {
		r.responses[key] = transactionResponse{append([]byte(nil), data...), now.Add(32 * time.Second)}
		return send(data)
	}
	reply := func(status int, reason string) {
		res := sip.NewResponseFromRequest(req, status, reason, nil)
		_ = respond([]byte(res.String()))
	}
	if req.From() == nil || req.To() == nil || req.CSeq() == nil || req.CallID() == nil || req.Via() == nil || req.CSeq().MethodName != req.Method {
		reply(400, "Bad Request")
		return
	}
	id := req.From().Address.User
	if _, exists := r.cfg.Devices[id]; !exists || !deviceIDPattern.MatchString(id) {
		reply(403, "Forbidden")
		return
	}
	switch req.Method {
	case sip.REGISTER:
		r.register(req, id, peer, network, send, respond)
	case sip.MESSAGE:
		s, ok := r.sessions[id]
		if !ok || s.peer != peer || s.network != network || time.Now().After(s.expires) {
			reply(403, "Registration Required")
			return
		}
		m, err := parseXML(req.Body())
		if err != nil || m.DeviceID != id {
			reply(400, "Invalid Metadata")
			return
		}
		if m.CmdType == "Keepalive" && m.XMLName.Local == "Notify" {
			if m.Status != "OK" {
				reply(400, "Invalid Keepalive")
				return
			}
			s.catalog.LastSeenAt = time.Now().UnixMilli()
			s.catalog.Registered = true
			reply(200, "OK")
			return
		}
		if err := r.metadata(id, m); err != nil {
			s.catalog.LastError = "metadata response rejected: " + err.Error()
			reply(400, "Unmatched Metadata")
			return
		}
		reply(200, "OK")
	default:
		reply(405, "Method Not Allowed")
	}
}
func (r *Registrar) register(req *sip.Request, id, peer, network string, send, respond sender) {
	reply := func(status int, reason string) {
		_ = respond([]byte(sip.NewResponseFromRequest(req, status, reason, nil).String()))
	}
	if req.To().Address.User != id {
		reply(403, "Identity Mismatch")
		return
	}
	header := req.GetHeader("Authorization")
	if header == nil {
		now := time.Now()
		for nonce, state := range r.nonces {
			if now.After(state.expires) {
				delete(r.nonces, nonce)
			}
		}
		if len(r.nonces) >= 1024 {
			reply(503, "Authentication Capacity")
			return
		}
		nonce := randomToken()
		r.nonces[nonce] = nonceState{id, peer, now.Add(30 * time.Second)}
		challenge := digest.Challenge{Realm: r.cfg.Realm, Nonce: nonce, Algorithm: "MD5", QOP: []string{"auth"}}
		res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
		res.AppendHeader(sip.NewHeader("WWW-Authenticate", challenge.String()))
		_ = respond([]byte(res.String()))
		return
	}
	credential, err := digest.ParseCredentials(header.Value())
	if err != nil {
		reply(403, "Forbidden")
		return
	}
	state, ok := r.nonces[credential.Nonce]
	delete(r.nonces, credential.Nonce)
	if !ok || state.device != id || state.peer != peer || time.Now().After(state.expires) || credential.Username != id || credential.Realm != r.cfg.Realm || credential.URI != req.Recipient.String() || credential.QOP != "auth" || credential.Nc != 1 || len(credential.Cnonce) == 0 || len(credential.Cnonce) > 128 || (credential.Algorithm != "" && credential.Algorithm != "MD5") {
		reply(403, "Forbidden")
		return
	}
	expected, err := digest.Digest(&digest.Challenge{Realm: r.cfg.Realm, Nonce: credential.Nonce, Algorithm: "MD5", QOP: []string{"auth"}}, digest.Options{Method: "REGISTER", URI: credential.URI, Username: id, Password: r.cfg.Devices[id], Count: 1, Cnonce: credential.Cnonce})
	if err != nil || subtle.ConstantTimeCompare([]byte(expected.Response), []byte(credential.Response)) != 1 {
		reply(403, "Forbidden")
		return
	}
	expires := 3600
	if h := req.GetHeader("Expires"); h != nil {
		expires, err = strconv.Atoi(h.Value())
		if err != nil || expires < 0 {
			reply(400, "Invalid Expires")
			return
		}
	}
	if expires == 0 {
		delete(r.sessions, id)
		for sn, q := range r.queries {
			if q.device == id {
				delete(r.queries, sn)
			}
		}
		reply(200, "OK")
		return
	}
	expires = min(3600, max(60, expires))
	previous := r.sessions[id]
	s := &session{peer: peer, network: network, send: send, expires: time.Now().Add(time.Duration(expires) * time.Second), catalog: DeviceCatalog{DeviceID: id, Channels: []Channel{}}}
	if previous != nil {
		s.catalog = previous.catalog
	}
	s.catalog.Registered = true
	s.catalog.LastSeenAt = time.Now().UnixMilli()
	r.sessions[id] = s
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	res.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	_ = respond([]byte(res.String()))
}
func (r *Registrar) metadata(id string, m Message) error {
	q, ok := r.queries[m.SN]
	if !ok || q.device != id || q.kind != m.CmdType || m.XMLName.Local != "Response" || time.Now().After(q.deadline) {
		return errors.New("unmatched metadata")
	}
	s := r.sessions[id]
	if s == nil {
		return errors.New("unregistered")
	}
	switch m.CmdType {
	case "DeviceInfo":
		if m.Result != "OK" {
			return errors.New("device information query failed")
		}
		if len(m.DeviceName) > 256 || len(m.Manufacturer) > 128 || len(m.Model) > 128 || len(m.Firmware) > 128 {
			return errors.New("metadata text too long")
		}
		s.catalog.Name, s.catalog.Manufacturer, s.catalog.Model, s.catalog.Firmware = m.DeviceName, m.Manufacturer, m.Model, m.Firmware
		delete(r.queries, m.SN)
	case "Catalog":
		if q.total >= 0 && q.total != m.SumNum {
			return errors.New("catalog total changed")
		}
		q.total = m.SumNum
		for _, c := range m.DeviceList.Items {
			if old, exists := q.channels[c.DeviceID]; exists {
				if old != c {
					return errors.New("conflicting catalog channel")
				}
				continue
			}
			if len(q.channels) >= q.total {
				return errors.New("catalog count overflow")
			}
			q.channels[c.DeviceID] = c
		}
		if len(q.channels) > q.total {
			return errors.New("catalog count overflow")
		}
		if len(q.channels) == q.total {
			count := len(q.channels)
			for device, other := range r.sessions {
				if device != id {
					count += len(other.catalog.Channels)
				}
			}
			if count > 1000 {
				return errors.New("node catalog exceeds 1000 channels")
			}
			s.catalog.Channels = []Channel{}
			for _, c := range q.channels {
				s.catalog.Channels = append(s.catalog.Channels, c)
			}
			sort.Slice(s.catalog.Channels, func(i, j int) bool { return s.catalog.Channels[i].DeviceID < s.catalog.Channels[j].DeviceID })
			s.catalog.CatalogAt = time.Now().UnixMilli()
			s.catalog.LastError = ""
			delete(r.queries, m.SN)
		}
	default:
		return errors.New("unsupported metadata")
	}
	return nil
}
func (r *Registrar) response(response *sip.Response, peer, network string) {
	if response.CallID() == nil || response.CSeq() == nil || response.CSeq().MethodName != sip.MESSAGE {
		return
	}
	for _, q := range r.queries {
		if q.callID != response.CallID().Value() {
			continue
		}
		s := r.sessions[q.device]
		if s == nil || s.peer != peer || s.network != network || response.CSeq().SeqNo != uint32(q.sn) {
			return
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			q.acked = true
		} else if response.StatusCode >= 300 {
			s.catalog.LastError = "SIP metadata query rejected"
			delete(r.queries, q.sn)
		}
		return
	}
}
func (r *Registrar) query(s *session, kind string, now time.Time) {
	sn := r.nextSN
	r.nextSN++
	if r.nextSN > 999999999 {
		r.nextSN = 1
	}
	callID := randomToken()
	body := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?><Query><CmdType>%s</CmdType><SN>%d</SN><DeviceID>%s</DeviceID></Query>", kind, sn, s.catalog.DeviceID)
	wire := fmt.Sprintf("MESSAGE sip:%s@%s SIP/2.0\r\nVia: SIP/2.0/%s %s;branch=z9hG4bK%s;rport\r\nFrom: <sip:%s@%s>;tag=%s\r\nTo: <sip:%s@%s>\r\nCall-ID: %s\r\nCSeq: %d MESSAGE\r\nMax-Forwards: 70\r\nContent-Type: Application/MANSCDP+xml\r\nContent-Length: %d\r\n\r\n%s", s.catalog.DeviceID, r.cfg.Realm, strings.ToUpper(s.network), r.cfg.Listen, randomToken(), r.cfg.ServerID, r.cfg.Realm, randomToken(), s.catalog.DeviceID, r.cfg.Realm, callID, sn, len([]byte(body)), body)
	r.queries[sn] = &pendingQuery{device: s.catalog.DeviceID, kind: kind, callID: callID, sn: sn, deadline: now.Add(10 * time.Second), nextRetry: now.Add(time.Second), attempts: 1, wire: []byte(wire), channels: map[string]Channel{}, total: -1}
	if err := s.send([]byte(wire)); err != nil {
		s.catalog.LastError = "send metadata query failed"
	}
}
func (r *Registrar) Tick(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if now.After(s.expires) || now.UnixMilli()-s.catalog.LastSeenAt > 180000 {
			s.catalog.Registered = false
			continue
		}
		if s.lastQuery.IsZero() || now.Sub(s.lastQuery) >= time.Minute {
			s.lastQuery = now
			r.query(s, "Catalog", now)
			r.query(s, "DeviceInfo", now)
		}
	}
	for sn, q := range r.queries {
		s := r.sessions[q.device]
		if s == nil || !s.catalog.Registered {
			delete(r.queries, sn)
			continue
		}
		if now.After(q.deadline) {
			s.catalog.LastError = "metadata response incomplete or timed out"
			delete(r.queries, sn)
			continue
		}
		if !q.acked && q.attempts < 4 && !now.Before(q.nextRetry) {
			_ = s.send(q.wire)
			q.attempts++
			q.nextRetry = now.Add(time.Duration(q.attempts) * time.Second)
		}
	}
}
func (r *Registrar) Snapshot() []DeviceCatalog {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []DeviceCatalog{}
	for _, s := range r.sessions {
		v := s.catalog
		v.Channels = append([]Channel{}, v.Channels...)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out
}
func (r *Registrar) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.Tick(now)
		}
	}
}
