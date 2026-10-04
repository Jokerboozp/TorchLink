// Package gbtest simulates a GB/T 28181 device for tests and lab runs: it
// registers with digest authentication, sends keepalives, answers catalog and
// device information queries (GBK-encoded, as real devices do) and accepts
// live INVITEs.
package gbtest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"golang.org/x/text/encoding/simplifiedchinese"

	"iot-platform/internal/video/gb28181"
)

// Channel is one catalog entry the device reports.
type Channel struct {
	ID, Name string
}

// Invite is a live request the device received.
type Invite struct {
	ChannelID string
	CallID    string
	Offer     gb28181.Answer // the platform's SDP, parsed
	Subject   string
}

// Config describes the simulated device.
type Config struct {
	ID, Password, Domain string
	// Server is the platform SIP address (host:port); Host the local address
	// to listen on (default 127.0.0.1).
	Server   string
	Host     string
	Channels []Channel
	// Answer decides the INVITE reply: status code and the SSRC to put in the
	// answer (empty echoes the offer). Nil accepts every INVITE.
	Answer func(Invite) (int, string)
	// OnAck runs when the platform confirms a call; a lab device starts
	// sending media here.
	OnAck func(Invite)
}

// Device is a running simulator.
type Device struct {
	cfg     Config
	ua      *sipgo.UserAgent
	srv     *sipgo.Server
	client  *sipgo.Client
	dialogs *sipgo.DialogServerCache
	conn    net.PacketConn
	cseq    atomic.Uint32

	mu      sync.Mutex
	invites []Invite
	active  map[string]*sipgo.DialogServerSession
	calls   map[string]Invite
	byes    int
	queries map[string]int
}

// Start listens on a UDP port of 127.0.0.1 and serves platform requests.
func Start(ctx context.Context, cfg Config) (*Device, error) {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	conn, err := net.ListenPacket("udp", net.JoinHostPort(cfg.Host, "0"))
	if err != nil {
		return nil, err
	}
	laddr := conn.LocalAddr().(*net.UDPAddr)
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("gbtest"), sipgo.WithUserAgentHostname(cfg.Domain))
	if err != nil {
		return nil, err
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, err
	}
	// Requests leave from the listening socket, as a real device's do.
	client, err := sipgo.NewClient(ua, sipgo.WithClientConnectionAddr(laddr.String()), sipgo.WithClientHostname(cfg.Host), sipgo.WithClientPort(laddr.Port))
	if err != nil {
		return nil, err
	}
	d := &Device{cfg: cfg, ua: ua, srv: srv, client: client, conn: conn, active: map[string]*sipgo.DialogServerSession{}, calls: map[string]Invite{}, queries: map[string]int{}}
	d.dialogs = sipgo.NewDialogServerCache(client, sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: cfg.ID, Host: cfg.Host, Port: laddr.Port}})
	srv.OnMessage(d.onMessage)
	srv.OnInvite(d.onInvite)
	srv.OnAck(d.onAck)
	srv.OnBye(d.onBye)
	go func() { _ = srv.ServeUDP(conn) }()
	// Requests may only leave once the listener is registered for reuse.
	for i := 0; i < 200; i++ {
		if c, err := ua.TransportLayer().GetConnection("udp", laddr.String()); err == nil && c != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	go func() {
		<-ctx.Done()
		d.Close()
	}()
	return d, nil
}

// Addr is the device's SIP address.
func (d *Device) Addr() string { return d.conn.LocalAddr().String() }

func (d *Device) Close() {
	_ = d.conn.Close()
	_ = d.ua.Close()
}

func (d *Device) request(method sip.RequestMethod) *sip.Request {
	host, portText, _ := net.SplitHostPort(d.cfg.Server)
	port, _ := strconv.Atoi(portText)
	req := sip.NewRequest(method, sip.Uri{Scheme: "sip", User: "34020000002000000001", Host: host, Port: port})
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: d.cfg.ID, Host: d.cfg.Domain}, Params: sip.NewParams()}
	from.Params.Add("tag", sip.GenerateTagN(10))
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: d.cfg.ID, Host: d.cfg.Domain}})
	req.AppendHeader(&sip.CSeqHeader{SeqNo: d.cseq.Add(1), MethodName: method})
	req.SetTransport("UDP")
	return req
}

// Register registers (expires > 0) or unregisters (0) the device and
// returns the final status code.
func (d *Device) Register(ctx context.Context, expires int) (int, error) {
	req := d.request(sip.REGISTER)
	local := d.conn.LocalAddr().(*net.UDPAddr)
	req.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: d.cfg.ID, Host: d.cfg.Host, Port: local.Port}})
	e := sip.ExpiresHeader(expires)
	req.AppendHeader(&e)
	res, err := d.client.Do(ctx, req)
	if err != nil {
		return 0, err
	}
	if res.StatusCode == 401 {
		req.RemoveHeader("Via")
		res, err = d.client.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{Username: d.cfg.ID, Password: d.cfg.Password})
		if err != nil {
			return 0, err
		}
	}
	return res.StatusCode, nil
}

func (d *Device) message(ctx context.Context, body []byte) (int, error) {
	req := d.request(sip.MESSAGE)
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.SetBody(body)
	res, err := d.client.Do(ctx, req)
	if err != nil {
		return 0, err
	}
	return res.StatusCode, nil
}

// Keepalive sends a keepalive and returns the platform's status code.
func (d *Device) Keepalive(ctx context.Context) (int, error) {
	return d.message(ctx, []byte(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n<Notify>\r\n<CmdType>Keepalive</CmdType>\r\n<SN>1</SN>\r\n<DeviceID>%s</DeviceID>\r\n<Status>OK</Status>\r\n</Notify>\r\n", d.cfg.ID)))
}

func gbk(s string) string {
	out, err := simplifiedchinese.GBK.NewEncoder().String(s)
	if err != nil {
		return s
	}
	return out
}

func (d *Device) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	body := string(req.Body())
	sn := between(body, "<SN>", "</SN>")
	cmd := between(body, "<CmdType>", "</CmdType>")
	d.mu.Lock()
	d.queries[cmd]++
	d.mu.Unlock()
	go func() {
		ctx := context.Background()
		switch cmd {
		case "Catalog":
			// One message per channel, like devices with large catalogs.
			for _, ch := range d.cfg.Channels {
				item := fmt.Sprintf("<Item>\r\n<DeviceID>%s</DeviceID>\r\n<Name>%s</Name>\r\n<Manufacturer>Sim</Manufacturer>\r\n<Status>ON</Status>\r\n<ParentID>%s</ParentID>\r\n</Item>\r\n", ch.ID, gbk(ch.Name), d.cfg.ID)
				_, _ = d.message(ctx, []byte(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n<Response>\r\n<CmdType>Catalog</CmdType>\r\n<SN>%s</SN>\r\n<DeviceID>%s</DeviceID>\r\n<SumNum>%d</SumNum>\r\n<DeviceList Num=\"1\">\r\n%s</DeviceList>\r\n</Response>\r\n", sn, d.cfg.ID, len(d.cfg.Channels), item)))
			}
		case "DeviceInfo":
			_, _ = d.message(ctx, []byte(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n<Response>\r\n<CmdType>DeviceInfo</CmdType>\r\n<SN>%s</SN>\r\n<DeviceID>%s</DeviceID>\r\n<Result>OK</Result>\r\n<DeviceName>%s</DeviceName>\r\n<Manufacturer>Sim</Manufacturer>\r\n<Model>SIM-1</Model>\r\n<Firmware>V1.0</Firmware>\r\n<Channel>%d</Channel>\r\n</Response>\r\n", sn, d.cfg.ID, gbk("模拟设备"), len(d.cfg.Channels))))
		}
	}()
}

func between(s, open, close string) string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, close)
	return strings.TrimSpace(v)
}

func (d *Device) onInvite(req *sip.Request, tx sip.ServerTransaction) {
	inv := Invite{ChannelID: req.Recipient.User, CallID: req.CallID().Value(), Offer: gb28181.ParseAnswer(req.Body())}
	if h := req.GetHeader("Subject"); h != nil {
		inv.Subject = h.Value()
	}
	d.mu.Lock()
	d.invites = append(d.invites, inv)
	d.mu.Unlock()
	code, ssrc := 200, ""
	if d.cfg.Answer != nil {
		code, ssrc = d.cfg.Answer(inv)
	}
	dialog, err := d.dialogs.ReadInvite(req, tx)
	if err != nil {
		return
	}
	if code != 200 {
		_ = dialog.Respond(code, "Rejected", nil)
		return
	}
	if ssrc == "" {
		ssrc = inv.Offer.SSRC
	}
	answer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 %s\r\ns=Play\r\nc=IN IP4 %s\r\nt=0 0\r\nm=video 15060 RTP/AVP 96\r\na=sendonly\r\na=rtpmap:96 PS/90000\r\ny=%s\r\n", d.cfg.ID, d.cfg.Host, d.cfg.Host, ssrc)
	// Record the call first: the ACK can arrive before RespondSDP returns.
	d.mu.Lock()
	d.active[inv.CallID] = dialog
	d.calls[inv.CallID] = inv
	d.mu.Unlock()
	if err := dialog.RespondSDP([]byte(answer)); err != nil {
		d.mu.Lock()
		delete(d.active, inv.CallID)
		delete(d.calls, inv.CallID)
		d.mu.Unlock()
	}
}

func (d *Device) onAck(req *sip.Request, tx sip.ServerTransaction) {
	if err := d.dialogs.ReadAck(req, tx); err != nil {
		return
	}
	d.mu.Lock()
	inv, ok := d.calls[req.CallID().Value()]
	d.mu.Unlock()
	if ok && d.cfg.OnAck != nil {
		d.cfg.OnAck(inv)
	}
}

func (d *Device) onBye(req *sip.Request, tx sip.ServerTransaction) {
	id := req.CallID().Value()
	if _, err := d.dialogs.MatchDialogRequest(req); err != nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Call Does Not Exist", nil))
		return
	}
	// Record the BYE before answering it: the platform carries on as soon
	// as the 200 OK arrives, and tests check the device right after that.
	d.mu.Lock()
	delete(d.active, id)
	delete(d.calls, id)
	d.byes++
	d.mu.Unlock()
	if err := d.dialogs.ReadBye(req, tx); err != nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Call Does Not Exist", nil))
	}
}

// Invites returns the INVITEs received so far.
func (d *Device) Invites() []Invite {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Invite(nil), d.invites...)
}

// Byes counts BYEs received from the platform.
func (d *Device) Byes() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.byes
}

// Active counts established calls.
func (d *Device) Active() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.active)
}

// Queries counts platform queries by command.
func (d *Device) Queries(cmd string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.queries[cmd]
}

// HangUp ends every call from the device side (BYE), like a device that
// stops streaming.
func (d *Device) HangUp(ctx context.Context) error {
	d.mu.Lock()
	dialogs := make([]*sipgo.DialogServerSession, 0, len(d.active))
	for id, dialog := range d.active {
		dialogs = append(dialogs, dialog)
		delete(d.active, id)
		delete(d.calls, id)
	}
	d.mu.Unlock()
	var errs []error
	for _, dialog := range dialogs {
		errs = append(errs, dialog.Bye(ctx))
	}
	return errors.Join(errs...)
}
