package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode"

	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/video/gb28181"
)

// GB28181 access: devices register to the platform's SIP server; playing a
// channel opens an RTP receiver on the media server and INVITEs the device
// to send there. The resulting stream is managed like a pulled RTSP source.

// gbTarget is one channel of a registered device.
type gbTarget struct {
	Tenant    string
	DeviceID  string
	ChannelID string
}

// A device counts as online while its registration is valid and it sent a
// keepalive (default interval 60s) within this window.
const gbKeepaliveTimeout = 3 * time.Minute

var (
	errGBUnavailable = errors.New("GB28181 接入未启用或配置无效")
	errGBMissing     = errors.New("国标设备不存在")
	errGBDisabled    = errors.New("国标设备已停用")
	errGBOffline     = errors.New("国标设备不在线，请检查设备的 SIP 服务器配置与网络")
	errGBNoMediaIP   = errors.New("未配置 GB28181 媒体接收地址（IOT_GB28181_MEDIA_IP）")
)

type gbCall struct {
	call     *gb28181.Call
	deviceID string
	ssrc     string
}

type catalogBuffer struct {
	sn    int
	order []string
	items map[string]model.GBChannel
}

// gbGateway implements gb28181.Handler on top of the video store and runs
// live calls for the manager.
type gbGateway struct {
	cfg    config.GB28181Config
	store  ports.VideoStore
	seal   sealer
	guard  targetGuard
	media  mediaServer
	log    *slog.Logger
	now    func() time.Time
	sip    *gb28181.Server
	onLost func(app, stream string)

	mu       sync.Mutex
	calls    map[string]gbCall
	pending  map[string]bool
	catalogs map[string]*catalogBuffer
	seq      int
	running  bool
	startErr string
}

func gbSealID(deviceID string) string { return "gb28181/" + deviceID }

func gbOnline(d model.GBDevice, now time.Time) bool {
	st := d.State
	ms := now.UnixMilli()
	return d.Enabled && st.RemoteAddr != "" && st.ExpiresAt > ms && ms-st.LastSeenAt <= gbKeepaliveTimeout.Milliseconds()
}

func gbDevice(d model.GBDevice) gb28181.Device {
	transport := d.State.Transport
	if transport == "" {
		transport = "UDP"
	}
	return gb28181.Device{ID: d.DeviceID, Addr: d.State.RemoteAddr, Transport: transport}
}

func sameHost(a, b string) bool {
	ha, _, errA := net.SplitHostPort(a)
	hb, _, errB := net.SplitHostPort(b)
	return errA == nil && errB == nil && ha == hb
}

func (g *gbGateway) device(ctx context.Context, id string) (model.GBDevice, bool) {
	d, err := g.store.GetGBDevice(ctx, id)
	return d, err == nil
}

func (g *gbGateway) password(d model.GBDevice) (string, error) {
	if d.Password == nil {
		return "", errors.New("未设置注册密码")
	}
	sealed := *d.Password
	sealed.TenantID, sealed.CameraID = d.TenantID, gbSealID(d.DeviceID)
	c, err := g.seal.open(sealed)
	return c.Password, err
}

func (g *gbGateway) saveState(ctx context.Context, d model.GBDevice) {
	if err := g.store.SaveGBDeviceState(ctx, d.DeviceID, d.State); err != nil {
		g.log.Warn("save gb28181 device state failed", "device", d.DeviceID, "error", err)
	}
}

// ---- gb28181.Handler ----

func (g *gbGateway) AllowSource(addr netip.Addr) bool { return g.guard.allowedAddr(addr) }

func (g *gbGateway) Password(ctx context.Context, id string) (string, bool) {
	d, ok := g.device(ctx, id)
	if !ok || !d.Enabled {
		return "", false
	}
	password, err := g.password(d)
	if err != nil || password == "" {
		g.log.Warn("gb28181 device password unavailable", "device", id)
		return "", false
	}
	return password, true
}

func (g *gbGateway) Registered(ctx context.Context, r gb28181.Registration) {
	d, ok := g.device(ctx, r.ID)
	if !ok {
		return
	}
	now := g.now()
	st := &d.State
	st.Transport, st.RemoteAddr, st.UserAgent = r.Transport, r.Addr, truncate(r.UserAgent, 128)
	st.RegisteredAt, st.LastSeenAt, st.ExpiresAt = now.UnixMilli(), now.UnixMilli(), now.Add(r.Expires).UnixMilli()
	g.saveState(ctx, d)
	g.log.Info("gb28181 device registered", "device", d.DeviceID, "tenant", d.TenantID, "transport", r.Transport)
	go func() {
		// Devices expect the REGISTER answer before the first query.
		time.Sleep(500 * time.Millisecond)
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := g.refresh(c, d.DeviceID); err != nil {
			g.log.Info("gb28181 device query failed", "device", d.DeviceID, "error", err)
		}
	}()
}

func (g *gbGateway) Unregistered(ctx context.Context, id string) {
	d, ok := g.device(ctx, id)
	if !ok {
		return
	}
	d.State.ExpiresAt, d.State.RemoteAddr = 0, ""
	g.saveState(ctx, d)
	g.endDevice(id, "device unregistered")
}

func (g *gbGateway) Known(ctx context.Context, dev gb28181.Device) bool {
	d, ok := g.device(ctx, dev.ID)
	return ok && d.Enabled && d.State.ExpiresAt > g.now().UnixMilli() && sameHost(d.State.RemoteAddr, dev.Addr)
}

func (g *gbGateway) Keepalive(ctx context.Context, dev gb28181.Device) {
	d, ok := g.device(ctx, dev.ID)
	if !ok {
		return
	}
	d.State.LastSeenAt, d.State.RemoteAddr, d.State.Transport = g.now().UnixMilli(), dev.Addr, dev.Transport
	g.saveState(ctx, d)
}

func (g *gbGateway) Catalog(ctx context.Context, id string, sn, sumNum int, items []gb28181.CatalogItem) {
	g.mu.Lock()
	buf := g.catalogs[id]
	if buf == nil || buf.sn != sn {
		buf = &catalogBuffer{sn: sn, items: map[string]model.GBChannel{}}
		g.catalogs[id] = buf
	}
	for _, item := range items {
		if !gb28181.ValidID(item.DeviceID) || len(buf.order) >= 2000 {
			continue
		}
		if _, seen := buf.items[item.DeviceID]; !seen {
			buf.order = append(buf.order, item.DeviceID)
		}
		buf.items[item.DeviceID] = model.GBChannel{ChannelID: item.DeviceID, Name: cleanText(item.Name, 64), Manufacturer: cleanText(item.Manufacturer, 64), Model: cleanText(item.Model, 64), Status: cleanText(item.Status, 16), ParentID: cleanText(item.ParentID, 32)}
	}
	channels := make([]model.GBChannel, 0, len(buf.order))
	for _, cid := range buf.order {
		channels = append(channels, buf.items[cid])
	}
	if sumNum <= len(buf.order) {
		delete(g.catalogs, id)
	}
	g.mu.Unlock()
	d, ok := g.device(ctx, id)
	if !ok {
		return
	}
	d.State.Channels, d.State.CatalogAt = channels, g.now().UnixMilli()
	g.saveState(ctx, d)
}

func (g *gbGateway) DeviceInfo(ctx context.Context, id string, info gb28181.DeviceInfo) {
	d, ok := g.device(ctx, id)
	if !ok {
		return
	}
	d.State.Manufacturer, d.State.Model, d.State.Firmware = cleanText(info.Manufacturer, 64), cleanText(info.Model, 64), cleanText(info.Firmware, 64)
	g.saveState(ctx, d)
}

func (g *gbGateway) CallEnded(callID string) {
	g.mu.Lock()
	key := ""
	for k, c := range g.calls {
		if c.call.ID == callID {
			key = k
			delete(g.calls, k)
			break
		}
	}
	g.mu.Unlock()
	if key == "" {
		return
	}
	app, stream, _ := strings.Cut(key, "/")
	g.log.Info("gb28181 device ended the stream", "task", key)
	if g.onLost != nil {
		g.onLost(app, stream)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = g.media.CloseRTPServer(ctx, app, stream)
}

// cleanText keeps device-supplied text printable and bounded.
func cleanText(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	return truncate(s, limit)
}

// refresh asks a registered device for its information and channel list.
func (g *gbGateway) refresh(ctx context.Context, id string) error {
	d, ok := g.device(ctx, id)
	if !ok {
		return errGBMissing
	}
	if !gbOnline(d, g.now()) {
		return errGBOffline
	}
	g.mu.Lock()
	delete(g.catalogs, id)
	g.mu.Unlock()
	if err := g.sip.Query(ctx, gbDevice(d), "DeviceInfo"); err != nil {
		g.log.Info("gb28181 DeviceInfo query failed", "device", id, "error", err)
	}
	return g.sip.Query(ctx, gbDevice(d), "Catalog")
}

// ---- live calls for the manager ----

func (g *gbGateway) nextSSRCLocked() string {
	used := map[string]bool{}
	for _, c := range g.calls {
		used[c.ssrc] = true
	}
	prefix := "0" + g.cfg.Domain[3:8]
	for range 10000 {
		g.seq = (g.seq + 1) % 10000
		ssrc := fmt.Sprintf("%s%04d", prefix, g.seq)
		if !used[ssrc] {
			return ssrc
		}
	}
	return prefix + "0000"
}

// start opens a receiver for app/stream and asks the device to send the
// channel there. The caller waits for media and calls stop on failure.
func (g *gbGateway) start(ctx context.Context, app, stream string, target gbTarget) error {
	key := app + "/" + stream
	g.stop(app, stream) // a call left over from a lost stream
	d, err := g.store.GetGBDevice(ctx, target.DeviceID)
	if err != nil || d.TenantID != target.Tenant {
		return errGBMissing
	}
	if !d.Enabled {
		return errGBDisabled
	}
	if !gbOnline(d, g.now()) {
		return errGBOffline
	}
	if g.cfg.MediaIP == "" {
		return errGBNoMediaIP
	}
	g.mu.Lock()
	ssrc := g.nextSSRCLocked()
	g.pending[key] = true
	g.mu.Unlock()
	done := false
	defer func() {
		if !done {
			g.mu.Lock()
			delete(g.pending, key)
			g.mu.Unlock()
		}
	}()
	tcp := d.StreamTransport == "TCP"
	port, err := g.media.OpenRTPServer(ctx, app, stream, tcp, ssrc)
	var apiErr *errMediaAPI
	if errors.As(err, &apiErr) && strings.Contains(apiErr.Msg, "already exists") {
		_ = g.media.CloseRTPServer(ctx, app, stream)
		port, err = g.media.OpenRTPServer(ctx, app, stream, tcp, ssrc)
	}
	if err != nil {
		return fmt.Errorf("媒体服务未能打开 RTP 接收端口：%w", err)
	}
	inviteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	call, err := g.sip.Invite(inviteCtx, gbDevice(d), target.ChannelID, gb28181.Offer{ServerID: g.cfg.ServerID, MediaIP: g.cfg.MediaIP, Port: port, SSRC: ssrc, TCP: tcp})
	cancel()
	if err != nil {
		_ = g.media.CloseRTPServer(context.Background(), app, stream)
		return err
	}
	// Some devices pick their own SSRC; follow the answer so the receiver's
	// filter matches what actually arrives.
	if answered := call.Answer.SSRC; gb28181.ValidSSRC(answered) && strings.TrimLeft(answered, "0") != strings.TrimLeft(ssrc, "0") {
		if err := g.media.UpdateRTPServerSSRC(ctx, app, stream, answered); err != nil {
			g.log.Warn("update gb28181 receiver ssrc failed", "task", key, "error", redactText(err.Error()))
		}
		ssrc = answered
	}
	if err := call.Ack(ctx); err != nil {
		byeCtx, byeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = call.Bye(byeCtx)
		byeCancel()
		_ = g.media.CloseRTPServer(context.Background(), app, stream)
		return fmt.Errorf("点播确认发送失败：%w", err)
	}
	g.mu.Lock()
	g.calls[key] = gbCall{call: call, deviceID: d.DeviceID, ssrc: ssrc}
	delete(g.pending, key)
	g.mu.Unlock()
	done = true
	return nil
}

// stop ends the call (BYE) and closes the receiver; it is idempotent.
func (g *gbGateway) stop(app, stream string) {
	key := app + "/" + stream
	g.mu.Lock()
	c, ok := g.calls[key]
	delete(g.calls, key)
	g.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ok {
		if err := c.call.Bye(ctx); err != nil {
			g.log.Debug("gb28181 bye failed", "task", key, "error", err)
		}
	}
	_ = g.media.CloseRTPServer(ctx, app, stream)
}

// owns reports whether a live call backs app/stream.
func (g *gbGateway) owns(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.calls[key]
	return ok
}

// expects reports whether media may be published to app/stream: a call is
// active or being set up.
func (g *gbGateway) expects(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.calls[key]
	return ok || g.pending[key]
}

// endDevice ends every call of a device; the manager restarts them on the
// next heartbeat if the device comes back.
func (g *gbGateway) endDevice(id, reason string) {
	g.mu.Lock()
	keys := []string{}
	for k, c := range g.calls {
		if c.deviceID == id {
			keys = append(keys, k)
		}
	}
	g.mu.Unlock()
	for _, k := range keys {
		app, stream, _ := strings.Cut(k, "/")
		g.stop(app, stream)
		g.log.Info("gb28181 stream ended", "task", k, "reason", reason)
		if g.onLost != nil {
			g.onLost(app, stream)
		}
	}
}

// ---- device management ----

// GBDeviceInput is the administrator's device form. Password is write-only;
// empty keeps the stored one.
type GBDeviceInput struct {
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	StreamTransport string `json:"streamTransport"`
	Password        string `json:"password"`
}

// GBStatus describes the SIP server for administrators configuring devices.
type GBStatus struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	ServerID string `json:"serverId,omitempty"`
	Domain   string `json:"domain,omitempty"`
	SIPPort  int    `json:"sipPort,omitempty"`
	SIPHost  string `json:"sipHost,omitempty"`
	MediaIP  string `json:"mediaIp,omitempty"`
	Message  string `json:"message"`
}

func (s *Service) gbStatus() *GBStatus {
	c := s.cfg.GB28181
	st := &GBStatus{Enabled: c.Enabled, ServerID: c.ServerID, Domain: c.Domain, SIPPort: c.SIPPort, SIPHost: c.SIPHost, MediaIP: c.MediaIP}
	switch {
	case !c.Enabled:
		st.Message = "GB28181 接入未启用（IOT_GB28181_ENABLED=false）。"
	case c.Problem() != nil:
		st.Message = "GB28181 配置无效，请检查 IOT_GB28181_* 配置。"
	case s.gb == nil:
		st.Message = "直播模块未部署，GB28181 信令服务未启动。"
	default:
		s.gb.mu.Lock()
		st.Running, st.Message = s.gb.running, s.gb.startErr
		s.gb.mu.Unlock()
		if st.Running {
			st.Message = "SIP 信令服务运行中。"
			if c.MediaIP == "" {
				st.Message += "未配置媒体接收地址，设备可注册但无法播放。"
			}
		}
	}
	return st
}

func (s *Service) gbReady() error {
	if s.store == nil || len(s.cfg.CredentialKey) != 32 {
		return ErrNotDeployed
	}
	return nil
}

func (s *Service) presentGB(d model.GBDevice) model.GBDevice {
	d.HasPassword = d.Password != nil
	d.Password = nil
	d.Online = gbOnline(d, s.now())
	return d
}

// ListGBDevices returns the tenant's GB28181 devices without secrets.
func (s *Service) ListGBDevices(ctx context.Context, tenant string) ([]model.GBDevice, error) {
	if err := s.gbReady(); err != nil {
		return nil, err
	}
	rows, err := s.store.ListGBDevices(ctx, tenant)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i] = s.presentGB(rows[i])
	}
	return rows, nil
}

// GetGBDevice returns one device of the tenant.
func (s *Service) GetGBDevice(ctx context.Context, tenant, id string) (model.GBDevice, error) {
	if err := s.gbReady(); err != nil {
		return model.GBDevice{}, err
	}
	d, err := s.store.GetGBDevice(ctx, id)
	if err != nil || d.TenantID != tenant {
		return model.GBDevice{}, model.ErrNotFound
	}
	return s.presentGB(d), nil
}

// SaveGBDevice creates or updates a device. Device IDs are global SIP
// identities: an ID of another tenant is refused without revealing more.
func (s *Service) SaveGBDevice(ctx context.Context, tenant, id string, in GBDeviceInput) (model.GBDevice, error) {
	if err := s.gbReady(); err != nil {
		return model.GBDevice{}, err
	}
	if !gb28181.ValidID(id) {
		return model.GBDevice{}, invalid("国标设备编号须为 20 位数字")
	}
	name := cleanText(in.Name, 64)
	if name == "" {
		name = id
	}
	transport := strings.ToUpper(strings.TrimSpace(in.StreamTransport))
	switch transport {
	case "":
		transport = "UDP"
	case "UDP", "TCP":
	default:
		return model.GBDevice{}, invalid("媒体传输方式须为 UDP 或 TCP")
	}
	if len(in.Password) > 64 || strings.ContainsAny(in.Password, "\r\n\x00\"") {
		return model.GBDevice{}, invalid("注册密码长度或字符无效")
	}
	now := s.now().UnixMilli()
	d := model.GBDevice{TenantID: tenant, DeviceID: id, CreatedAt: now}
	previous, err := s.store.GetGBDevice(ctx, id)
	switch {
	case err == nil && previous.TenantID != tenant:
		return model.GBDevice{}, model.ErrGBDeviceTaken
	case err == nil:
		d = previous
	case !errors.Is(err, model.ErrNotFound):
		return model.GBDevice{}, err
	}
	d.Name, d.Enabled, d.StreamTransport, d.UpdatedAt = name, in.Enabled, transport, now
	passwordChanged := in.Password != ""
	if passwordChanged {
		sealed, err := s.seal.seal(tenant, gbSealID(id), Credentials{Username: id, Password: in.Password})
		if err != nil {
			return model.GBDevice{}, err
		}
		d.Password = &sealed
	}
	if d.Password == nil {
		return model.GBDevice{}, invalid("请设置注册密码，与设备上配置的 SIP 密码一致")
	}
	if err := s.store.SaveGBDevice(ctx, d); err != nil {
		return model.GBDevice{}, err
	}
	if s.gb != nil && (!d.Enabled || passwordChanged || previous.StreamTransport != transport) {
		s.gb.endDevice(id, "device settings changed")
	}
	return s.presentGB(d), nil
}

// DeleteGBDevice removes a device; cameras using it lose live view until they
// are reconfigured.
func (s *Service) DeleteGBDevice(ctx context.Context, tenant, id string) error {
	if err := s.gbReady(); err != nil {
		return err
	}
	if err := s.store.DeleteGBDevice(ctx, tenant, id); err != nil {
		return err
	}
	if s.gb != nil {
		s.gb.endDevice(id, "device deleted")
	}
	return nil
}

// RefreshGBDevice re-queries the device information and catalog; results
// arrive asynchronously.
func (s *Service) RefreshGBDevice(ctx context.Context, tenant, id string) error {
	if s.gb == nil {
		return errGBUnavailable
	}
	d, err := s.store.GetGBDevice(ctx, id)
	if err != nil || d.TenantID != tenant {
		return model.ErrNotFound
	}
	if err := s.gb.refresh(ctx, id); err != nil {
		return invalid("%v", err)
	}
	return nil
}
