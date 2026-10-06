// Package video implements the optional camera live module: per-camera live
// configuration, connection tests, play sessions and the lifecycle of media
// tasks on an independent media server (ZLMediaKit). Basic camera metadata
// and alarm linkage never depend on this package being deployed or healthy.
package video

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/video/gb28181"
)

// Module states reported to the UI.
const (
	StateNotDeployed   = "not_deployed"
	StateMisconfigured = "misconfigured"
	StateDisabled      = "disabled"
	StateEnabled       = "enabled"
	StateDegraded      = "degraded"
)

// Errors with a stable meaning for HTTP mapping.
var (
	ErrNotDeployed    = errors.New("直播模块未部署")
	ErrModuleDisabled = errors.New("直播功能未启用")
	ErrMediaDown      = errors.New("媒体服务暂不可用，请稍后重试")
	ErrNotConfigured  = errors.New("该摄像头未配置或未启用直播")
	ErrForbidden      = errors.New("无权观看该摄像头直播")
	ErrSessionGone    = errors.New("播放会话已结束")
)

// Viewer is the authenticated user asking to watch.
type Viewer struct {
	Username       string
	Managed        bool
	SessionVersion int64
	ExpiresAt      time.Time
}

// CameraLookup reads basic camera metadata without request scoping.
type CameraLookup func(ctx context.Context, tenant, camera string) (model.VideoCameraMapping, error)

// Authorizer re-checks, at any time, that a viewer may still watch a camera:
// the account is enabled, its session is valid, it holds the watch permission
// and the camera's device is in its device scope.
type Authorizer func(ctx context.Context, tenant string, viewer Viewer, camera model.VideoCameraMapping) error

type Service struct {
	cfg       config.VideoConfig
	store     ports.VideoStore
	cameras   CameraLookup
	authorize Authorizer
	media     mediaServer
	failover  *failoverMedia
	mgr       *manager
	gb        *gbGateway
	guard     targetGuard
	seal      sealer
	log       *slog.Logger

	healthMu     sync.Mutex
	healthy      bool
	healthErr    string
	healthAt     time.Time
	keepaliveAt  time.Time
	testMu       sync.Mutex
	testResults  map[string]testMemo
	now          func() time.Time
	started      bool
	transcodeCap bool
}

type testMemo struct {
	fingerprint string
	result      model.CameraLiveTest
}

// New builds the service. It is always safe to call; when the media server is
// not deployed every live operation reports ErrNotDeployed.
func New(cfg config.VideoConfig, store ports.VideoStore, cameras CameraLookup, authorize Authorizer, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{cfg: cfg, store: store, cameras: cameras, authorize: authorize, log: log, testResults: map[string]testMemo{}, now: time.Now}
	s.guard = targetGuard{networks: cfg.AllowedCIDRs, denied: cfg.DeniedCIDRs, ports: cfg.AllowedPorts}
	s.seal = sealer{key: cfg.CredentialKey, keyID: cfg.CredentialKeyID}
	if cfg.Deployed() && cfg.Problem() == nil && store != nil {
		s.media = newZLM(cfg.MediaAPIURL, cfg.MediaSecret)
		if len(cfg.Standbys) > 0 {
			members := []mediaMember{{id: cfg.MediaServerID, mediaIP: cfg.GB28181.MediaIP, server: s.media}}
			for _, standby := range cfg.Standbys {
				members = append(members, mediaMember{id: standby.ID, mediaIP: standby.MediaIP, server: newZLM(standby.URL, cfg.MediaSecret)})
			}
			s.failover = newFailoverMedia(members)
			s.failover.onSwitch = s.mediaSwitched
			s.media = s.failover
		}
		s.transcodeCap = cfg.Transcode
		s.mgr = newManager(managerConfig{HookSecret: cfg.HookSecret, LeaseTTL: cfg.LeaseTTL, IdleGrace: cfg.IdleGrace, StartTimeout: cfg.StartTimeout, MaxSessions: cfg.MaxSessions, MaxSources: cfg.MaxSourceStreams, MaxTranscodes: cfg.MaxTranscodes}, s.media, store, log)
		s.newGB()
	}
	return s
}

// newGB prepares the GB28181 SIP server. A configuration problem only
// disables GB28181 access; RTSP and ONVIF cameras are unaffected.
func (s *Service) newGB() {
	c := s.cfg.GB28181
	if !c.Enabled || c.Problem() != nil {
		return
	}
	g := &gbGateway{cfg: c, store: s.store, seal: s.seal, guard: s.guard, media: s.media, failover: s.failover, log: s.log, now: func() time.Time { return s.now() }, calls: map[string]gbCall{}, pending: map[string]bool{}, catalogs: map[string]*catalogBuffer{}}
	mac := hmac.New(sha256.New, []byte(s.cfg.HookSecret))
	mac.Write([]byte("gb28181-nonce-key"))
	srv, err := gb28181.New(gb28181.Config{ServerID: c.ServerID, Domain: c.Domain, Listen: c.Listen, SIPHost: c.SIPHost, SIPPort: c.SIPPort, NonceKey: mac.Sum(nil), Log: s.log}, g)
	if err != nil {
		s.log.Warn("GB28181 server not created", "error", err)
		return
	}
	g.sip = srv
	g.onLost = func(app, stream string) { s.mgr.markLost(app, stream, "国标设备已停止发送视频") }
	s.gb, s.mgr.gb = g, g
}

func (s *Service) active() bool { return s.mgr != nil }

// Start runs background maintenance. Failures only degrade the module.
func (s *Service) Start(ctx context.Context) {
	if !s.active() || s.started {
		return
	}
	s.started = true
	if s.gb != nil {
		err := s.gb.sip.Start(ctx)
		s.gb.mu.Lock()
		s.gb.running = err == nil
		if err != nil {
			s.gb.startErr = "SIP 端口监听失败，请检查端口占用：" + err.Error()
			s.log.Warn("GB28181 SIP server failed to start", "error", err)
		}
		s.gb.mu.Unlock()
	}
	if err := s.mgr.restore(ctx); err != nil {
		s.log.Warn("restore video sessions failed", "error", err)
	}
	s.checkHealth(ctx)
	go s.loop(ctx)
}

func (s *Service) loop(ctx context.Context) {
	reap := time.NewTicker(5 * time.Second)
	sweep := time.NewTicker(15 * time.Second)
	reconcile := time.NewTicker(30 * time.Second)
	prune := time.NewTicker(time.Hour)
	defer reap.Stop()
	defer sweep.Stop()
	defer reconcile.Stop()
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reap.C:
			s.mgr.reap()
		case <-sweep.C:
			s.checkHealth(ctx)
			s.sweep(ctx)
		case <-reconcile.C:
			s.reconcile(ctx)
		case <-prune.C:
			_ = s.store.PruneVideoPlaySessions(ctx, s.now().Add(-24*time.Hour).UnixMilli())
		}
	}
}

func (s *Service) reconcile(ctx context.Context) {
	if !s.mediaHealthy() {
		return
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := s.mgr.reconcile(c); err != nil {
		s.log.Warn("video reconcile failed", "error", redactText(err.Error()))
	}
}

func (s *Service) checkHealth(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	err := s.media.Health(c)
	cancel()
	s.healthMu.Lock()
	was := s.healthy
	s.healthy, s.healthAt = err == nil, s.now()
	s.healthErr = ""
	if err != nil {
		s.healthErr = err.Error()
	}
	s.healthMu.Unlock()
	if err == nil && !was {
		// Recovered (or first seen): reconcile to adopt or clean up.
		go s.reconcile(context.Background())
	}
}

// activeMediaID is the server ID whose hooks are accepted: the active media
// server. Hooks of a standby, such as its start-up event, are ignored.
func (s *Service) activeMediaID() string {
	if s.failover != nil {
		return s.failover.current().id
	}
	return s.cfg.MediaServerID
}

// mediaSwitched moves the module to another media server: streams of the
// previous server are lost, so tasks are marked for recovery and restarted
// on the new server; viewers reconnect as after a media server restart.
func (s *Service) mediaSwitched(from, to mediaMember) {
	s.log.Warn("media server failover", "from", from.id, "to", to.id)
	s.mgr.markAllLost()
	go s.reconcile(context.Background())
}

func (s *Service) mediaHealthy() bool {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	return s.healthy
}

// sweep re-validates every live session so permission changes, disabled
// users, removed device scope or disabled cameras stop playback within one
// sweep even if the browser keeps a WebRTC connection open.
func (s *Service) sweep(ctx context.Context) {
	enabled, _ := s.moduleEnabled(ctx)
	for _, sess := range s.mgr.activeSessions() {
		if !enabled {
			s.mgr.revoke(sess.ID, "module disabled")
			continue
		}
		if err := s.recheck(ctx, sess); err != nil {
			s.log.Info("video session revoked", "session", sess.ID, "reason", err.Error())
			s.mgr.revoke(sess.ID, "access revoked")
		}
	}
}

func (s *Service) recheck(ctx context.Context, sess model.VideoPlaySession) error {
	camera, err := s.cameras(ctx, sess.TenantID, sess.CameraID)
	if err != nil || !camera.Enabled {
		return ErrNotConfigured
	}
	cfg, err := s.store.GetCameraLiveConfig(ctx, sess.TenantID, sess.CameraID)
	if err != nil || !cfg.Enabled {
		return ErrNotConfigured
	}
	viewer := Viewer{Username: sess.Username, Managed: sess.ManagedUser, SessionVersion: sess.SessionVersion, ExpiresAt: time.UnixMilli(sess.MaxExpiresAt)}
	return s.authorize(ctx, sess.TenantID, viewer, camera)
}

func (s *Service) moduleEnabled(ctx context.Context) (bool, error) {
	if s.store == nil {
		return false, nil
	}
	state, err := s.store.GetVideoModuleState(ctx)
	return state.Enabled, err
}

// Status describes the module for the UI.
type Status struct {
	State        string `json:"state"`
	Deployed     bool   `json:"deployed"`
	Enabled      bool   `json:"enabled"`
	MediaHealthy bool   `json:"mediaHealthy"`
	// ActiveMediaServer names the media server in use when standbys exist.
	ActiveMediaServer  string           `json:"activeMediaServer,omitempty"`
	Message            string           `json:"message"`
	TranscodeAvailable bool             `json:"transcodeAvailable"`
	UpdatedBy          string           `json:"updatedBy,omitempty"`
	UpdatedAt          int64            `json:"updatedAt,omitempty"`
	Brands             []map[string]any `json:"brands,omitempty"`
	Profiles           []outputProfile  `json:"profiles,omitempty"`
	ActiveSessions     int              `json:"activeSessions"`
	ActiveSources      int              `json:"activeSources"`
	ActiveTranscodes   int              `json:"activeTranscodes"`
	MaxTranscodes      int              `json:"maxTranscodes,omitempty"`
	HeartbeatSeconds   int              `json:"heartbeatSeconds,omitempty"`
	GB28181            *GBStatus        `json:"gb28181,omitempty"`
}

func (s *Service) Status(ctx context.Context) Status {
	st := Status{State: StateNotDeployed, Message: "直播模块未部署；摄像头资料与设备关联不受影响。", Brands: BrandTemplates(), Profiles: OutputProfiles()}
	if !s.cfg.Deployed() {
		return st
	}
	st.Deployed = true
	st.GB28181 = s.gbStatus()
	if problem := s.cfg.Problem(); problem != nil || s.store == nil {
		st.State, st.Message = StateMisconfigured, "直播模块部署配置无效，请检查 IOT_VIDEO_* 配置。"
		return st
	}
	module, err := s.store.GetVideoModuleState(ctx)
	st.Enabled, st.UpdatedBy, st.UpdatedAt = module.Enabled, module.UpdatedBy, module.UpdatedAt
	st.MediaHealthy = s.mediaHealthy()
	if s.failover != nil {
		st.ActiveMediaServer = s.failover.current().id
	}
	st.TranscodeAvailable = s.transcodeCap
	st.MaxTranscodes = s.cfg.MaxTranscodes
	st.HeartbeatSeconds = max(5, int(s.cfg.LeaseTTL.Seconds()/3))
	st.ActiveSessions, st.ActiveSources, st.ActiveTranscodes = s.mgr.counts()
	switch {
	case err != nil:
		st.State, st.Message = StateDegraded, "读取直播开关失败"
	case !module.Enabled:
		st.State, st.Message = StateDisabled, "直播模块已部署但未启用。"
	case !st.MediaHealthy:
		st.State, st.Message = StateDegraded, "直播已启用，但媒体服务不可用；摄像头资料管理不受影响。"
	default:
		st.State, st.Message = StateEnabled, "直播已启用，媒体服务运行正常。"
	}
	return st
}

// SetModule switches the business switch. Turning it off revokes every
// session and removes every pull and transcode task; stored configuration is
// kept. The media container itself keeps running until the deployment stops it.
func (s *Service) SetModule(ctx context.Context, enabled bool, actor string) error {
	if !s.active() {
		return ErrNotDeployed
	}
	if err := s.store.SaveVideoModuleState(ctx, model.VideoModuleState{Enabled: enabled, UpdatedBy: actor, UpdatedAt: s.now().UnixMilli()}); err != nil {
		return err
	}
	if !enabled {
		s.mgr.stopAll("module disabled")
	}
	return nil
}

// LiveConfigInput is the administrator's form. Password is write-only: empty
// keeps the stored one, ClearPassword removes it.
type LiveConfigInput struct {
	Enabled          bool   `json:"enabled"`
	AccessMode       string `json:"accessMode"`
	BrandTemplate    string `json:"brandTemplate"`
	Host             string `json:"host"`
	RTSPPort         int    `json:"rtspPort"`
	ONVIFPort        int    `json:"onvifPort"`
	Channel          int    `json:"channel"`
	NVR              bool   `json:"nvr"`
	MainProfileToken string `json:"mainProfileToken"`
	SubProfileToken  string `json:"subProfileToken"`
	ManualURL        bool   `json:"manualUrl"`
	MainStreamURL    string `json:"mainStreamUrl"`
	SubStreamURL     string `json:"subStreamUrl"`
	DefaultStream    string `json:"defaultStream"`
	TranscodeMode    string `json:"transcodeMode"`
	TranscodeProfile string `json:"transcodeProfile"`
	SourceBFrames    bool   `json:"sourceBFrames"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	ClearPassword    bool   `json:"clearPassword"`
	Stream           string `json:"stream,omitempty"`
	GBDeviceID       string `json:"gbDeviceId"`
	GBChannelID      string `json:"gbChannelId"`
}

// ValidationError is a user-correctable input problem.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error { return ValidationError{fmt.Sprintf(format, args...)} }

// GetConfig returns the camera's live configuration without secrets.
func (s *Service) GetConfig(ctx context.Context, tenant, camera string) (model.CameraLiveConfig, error) {
	if s.store == nil {
		return model.CameraLiveConfig{}, ErrNotDeployed
	}
	cfg, err := s.store.GetCameraLiveConfig(ctx, tenant, camera)
	if errors.Is(err, model.ErrNotFound) {
		cfg = model.CameraLiveConfig{TenantID: tenant, CameraID: camera, AccessMode: "RTSP", BrandTemplate: "hikvision", Channel: 1, DefaultStream: "sub", TranscodeMode: transcodeOff, MediaNodeID: "default"}
	} else if err != nil {
		return cfg, err
	}
	if cred, err := s.store.GetCameraCredential(ctx, tenant, camera); err == nil {
		cfg.HasPassword = true
		if c, openErr := s.seal.open(cred); openErr == nil {
			cfg.Username = c.Username
			cfg.HasPassword = c.Password != ""
		}
	}
	return cfg, nil
}

// liveConfigured reports whether a configuration names a stream source.
func liveConfigured(cfg model.CameraLiveConfig) bool {
	if cfg.AccessMode == accessGB {
		return cfg.GBDeviceID != "" && cfg.GBChannelID != ""
	}
	return cfg.MainStreamURL != ""
}

const accessGB = "GB28181"

// LiveSummaries returns safe per-camera live state for list views.
func (s *Service) LiveSummaries(ctx context.Context, tenant string, cameras []string) map[string]map[string]any {
	out := map[string]map[string]any{}
	if s.store == nil || len(cameras) == 0 {
		return out
	}
	rows, err := s.store.ListCameraLiveConfigs(ctx, tenant, cameras)
	if err != nil {
		return out
	}
	for id, cfg := range rows {
		item := map[string]any{"configured": liveConfigured(cfg), "enabled": cfg.Enabled, "accessMode": cfg.AccessMode, "transcodeMode": cfg.TranscodeMode}
		if cfg.LastTest != nil {
			item["testStatus"], item["testedAt"] = cfg.LastTest.Status, cfg.LastTest.TestedAt
		}
		if cfg.LastPlayableAt > 0 {
			item["lastPlayableAt"] = cfg.LastPlayableAt
		}
		out[id] = item
	}
	return out
}

func (s *Service) credentials(ctx context.Context, tenant, camera string) (Credentials, error) {
	sealed, err := s.store.GetCameraCredential(ctx, tenant, camera)
	if errors.Is(err, model.ErrNotFound) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, err
	}
	return s.seal.open(sealed)
}

// resolveInput normalizes a form into a stored configuration and the
// credentials to use. ONVIF stream URIs are fetched from the device and
// validated here; browsers never supply the final ONVIF URI.
func (s *Service) resolveInput(ctx context.Context, tenant, camera string, in LiveConfigInput) (model.CameraLiveConfig, Credentials, error) {
	stored, err := s.credentials(ctx, tenant, camera)
	if err != nil {
		stored = Credentials{}
	}
	cred := Credentials{Username: strings.TrimSpace(in.Username), Password: in.Password}
	if in.ClearPassword {
		cred.Password = ""
	} else if cred.Password == "" {
		cred.Password = stored.Password
	}
	if len(cred.Username) > 128 || len(cred.Password) > 256 || strings.ContainsAny(cred.Username, "\r\n\x00") || strings.ContainsAny(cred.Password, "\r\n\x00") {
		return model.CameraLiveConfig{}, cred, invalid("用户名或密码长度或字符无效")
	}
	cfg := model.CameraLiveConfig{TenantID: tenant, CameraID: camera, Enabled: in.Enabled, AccessMode: strings.ToUpper(strings.TrimSpace(in.AccessMode)), BrandTemplate: strings.TrimSpace(in.BrandTemplate), Host: strings.TrimSpace(in.Host), RTSPPort: in.RTSPPort, ONVIFPort: in.ONVIFPort, Channel: in.Channel, NVR: in.NVR, DefaultStream: in.DefaultStream, TranscodeMode: in.TranscodeMode, TranscodeProfile: in.TranscodeProfile, SourceBFrames: in.SourceBFrames, MediaNodeID: "default", UpdatedAt: s.now().UnixMilli()}
	if _, ok := findBrand(cfg.BrandTemplate); !ok {
		cfg.BrandTemplate = "generic"
	}
	if cfg.Channel == 0 {
		cfg.Channel = 1
	}
	switch cfg.TranscodeMode {
	case "":
		cfg.TranscodeMode = transcodeOff
	case transcodeOff, transcodeAuto:
	case transcodeFixed:
		if p, ok := findProfile(cfg.TranscodeProfile); !ok || !p.Transcode {
			return cfg, cred, invalid("请选择有效的兼容输出规格")
		}
	default:
		return cfg, cred, invalid("转码策略无效")
	}
	if cfg.TranscodeProfile != "" {
		if _, ok := findProfile(cfg.TranscodeProfile); !ok {
			return cfg, cred, invalid("输出规格无效")
		}
	}
	switch cfg.AccessMode {
	case "ONVIF":
		if cfg.Host == "" {
			return cfg, cred, invalid("请填写设备地址")
		}
		if cfg.ONVIFPort == 0 {
			cfg.ONVIFPort = 80
		}
		if strings.TrimSpace(in.MainProfileToken) == "" {
			return cfg, cred, invalid("请先查询并选择主码流媒体配置")
		}
		cfg.MainProfileToken, cfg.SubProfileToken = strings.TrimSpace(in.MainProfileToken), strings.TrimSpace(in.SubProfileToken)
		client := s.guard.newONVIF(cfg.Host, cfg.ONVIFPort, cred, 8*time.Second)
		if cfg.MainStreamURL, err = client.StreamURI(ctx, cfg.MainProfileToken); err != nil {
			return cfg, cred, err
		}
		if cfg.SubProfileToken != "" {
			if cfg.SubStreamURL, err = client.StreamURI(ctx, cfg.SubProfileToken); err != nil {
				return cfg, cred, err
			}
		}
	case "RTSP":
		cfg.ManualURL = in.ManualURL || cfg.BrandTemplate == "generic"
		if cfg.ManualURL {
			if cfg.MainStreamURL, err = cleanStreamURL(in.MainStreamURL); err != nil {
				return cfg, cred, invalid("主码流地址：%v", err)
			}
			if strings.TrimSpace(in.SubStreamURL) != "" {
				if cfg.SubStreamURL, err = cleanStreamURL(in.SubStreamURL); err != nil {
					return cfg, cred, invalid("子码流地址：%v", err)
				}
			}
		} else {
			if cfg.MainStreamURL, err = buildTemplateURL(cfg.BrandTemplate, cfg.Host, cfg.RTSPPort, cfg.Channel, false); err != nil {
				return cfg, cred, invalid("%v", err)
			}
			cfg.SubStreamURL, _ = buildTemplateURL(cfg.BrandTemplate, cfg.Host, cfg.RTSPPort, cfg.Channel, true)
		}
	case accessGB:
		cfg.GBDeviceID, cfg.GBChannelID = strings.TrimSpace(in.GBDeviceID), strings.TrimSpace(in.GBChannelID)
		if cfg.GBChannelID == "" {
			cfg.GBChannelID = cfg.GBDeviceID
		}
		if !gb28181.ValidID(cfg.GBDeviceID) || !gb28181.ValidID(cfg.GBChannelID) {
			return cfg, cred, invalid("请选择国标设备和通道（20 位编号）")
		}
		device, err := s.store.GetGBDevice(ctx, cfg.GBDeviceID)
		if err != nil || device.TenantID != tenant {
			return cfg, cred, invalid("国标设备不存在，请先在“国标设备”中添加")
		}
		// The device authenticates at registration; cameras keep no account.
		cred = Credentials{}
		cfg.BrandTemplate, cfg.Host, cfg.RTSPPort, cfg.ONVIFPort, cfg.Channel, cfg.NVR = "", "", 0, 0, 0, false
		cfg.DefaultStream = "main"
		return cfg, cred, nil
	default:
		return cfg, cred, invalid("接入方式须为 ONVIF、RTSP 或 GB28181")
	}
	for _, raw := range []string{cfg.MainStreamURL, cfg.SubStreamURL} {
		if raw == "" {
			continue
		}
		if _, err := s.guard.pinnedRTSP(ctx, raw); err != nil {
			return cfg, cred, invalid("流地址未通过目标地址校验：%v", err)
		}
	}
	if cfg.DefaultStream != "main" && cfg.DefaultStream != "sub" {
		cfg.DefaultStream = "sub"
	}
	if cfg.SubStreamURL == "" {
		cfg.DefaultStream = "main"
	}
	return cfg, cred, nil
}

func cleanStreamURL(raw string) (string, error) {
	u, err := parseStreamURL(raw)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// SaveConfig stores the configuration and credentials. Existing sessions of
// the camera are ended because their source may have changed.
func (s *Service) SaveConfig(ctx context.Context, tenant, camera string, in LiveConfigInput) (model.CameraLiveConfig, error) {
	if s.store == nil {
		return model.CameraLiveConfig{}, ErrNotDeployed
	}
	cfg, cred, err := s.resolveInput(ctx, tenant, camera, in)
	if err != nil {
		return cfg, err
	}
	if previous, getErr := s.store.GetCameraLiveConfig(ctx, tenant, camera); getErr == nil {
		cfg.LastPlayableAt = previous.LastPlayableAt
		if fingerprint(previous, Credentials{}) != fingerprint(cfg, Credentials{}) {
			cfg.LastPlayableAt = 0
		}
	}
	if memo, ok := s.memo(tenant, camera); ok && memo.fingerprint == fingerprint(cfg, cred) {
		result := memo.result
		cfg.LastTest = &result
	}
	switch {
	case in.ClearPassword && cred.Username == "":
		if err := s.store.DeleteCameraCredential(ctx, tenant, camera); err != nil {
			return cfg, err
		}
	case cred.Username != "" || cred.Password != "":
		sealed, sealErr := s.seal.seal(tenant, camera, cred)
		if sealErr != nil {
			return cfg, sealErr
		}
		if err := s.store.SaveCameraCredential(ctx, sealed); err != nil {
			return cfg, err
		}
	default:
		if err := s.store.DeleteCameraCredential(ctx, tenant, camera); err != nil {
			return cfg, err
		}
	}
	if err := s.store.SaveCameraLiveConfig(ctx, cfg); err != nil {
		return cfg, err
	}
	if s.active() {
		s.mgr.stopCamera(tenant, camera, "live configuration changed")
	}
	cfg.Username, cfg.HasPassword = cred.Username, cred.Password != ""
	return cfg, nil
}

// ONVIFProfiles queries a device's media profiles for the form.
func (s *Service) ONVIFProfiles(ctx context.Context, tenant, camera string, in LiveConfigInput) ([]ONVIFProfile, error) {
	if !s.cfg.Deployed() {
		return nil, ErrNotDeployed
	}
	stored, _ := s.credentials(ctx, tenant, camera)
	cred := Credentials{Username: strings.TrimSpace(in.Username), Password: in.Password}
	if cred.Password == "" && !in.ClearPassword {
		cred.Password = stored.Password
	}
	if strings.TrimSpace(in.Host) == "" {
		return nil, invalid("请填写设备地址")
	}
	port := in.ONVIFPort
	if port == 0 {
		port = 80
	}
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return s.guard.newONVIF(strings.TrimSpace(in.Host), port, cred, 8*time.Second).Profiles(c)
}

// Test runs the connection test on the form values (or the stored
// configuration when in is nil). The result distinguishes network,
// authentication, stream and codec problems, and only reports PLAYABLE after
// the media server received ready video frames.
func (s *Service) Test(ctx context.Context, tenant, camera string, in *LiveConfigInput) (model.CameraLiveTest, error) {
	if !s.cfg.Deployed() {
		return model.CameraLiveTest{}, ErrNotDeployed
	}
	var cfg model.CameraLiveConfig
	var cred Credentials
	var err error
	variant := "main"
	if in == nil {
		stored, getErr := s.store.GetCameraLiveConfig(ctx, tenant, camera)
		if getErr != nil {
			return model.CameraLiveTest{}, ErrNotConfigured
		}
		cfg = stored
		if cred, err = s.credentials(ctx, tenant, camera); err != nil {
			return model.CameraLiveTest{}, invalid("%v", err)
		}
		variant = cfg.DefaultStream
	} else {
		if in.Stream == "sub" {
			variant = "sub"
		}
		cfg, cred, err = s.resolveInput(ctx, tenant, camera, *in)
		if err != nil {
			var pe *probeError
			if errors.As(err, &pe) {
				return s.finishTest(tenant, camera, cfg, cred, model.CameraLiveTest{Status: pe.Status, Message: pe.Detail, Stream: variant, TestedAt: s.now().UnixMilli()}, in == nil), nil
			}
			return model.CameraLiveTest{}, err
		}
	}
	if cfg.AccessMode == accessGB {
		return s.finishTest(tenant, camera, cfg, cred, s.testGB(ctx, tenant, cfg), in == nil), nil
	}
	raw := cfg.MainStreamURL
	if variant == "sub" && cfg.SubStreamURL != "" {
		raw = cfg.SubStreamURL
	} else {
		variant = "main"
	}
	result := model.CameraLiveTest{Stream: variant, TestedAt: s.now().UnixMilli()}
	pinned, err := s.guard.pinnedRTSP(ctx, raw)
	if err != nil {
		result.Status, result.Message = StatusTargetDenied, err.Error()
		return s.finishTest(tenant, camera, cfg, cred, result, in == nil), nil
	}
	sdp, err := s.guard.describe(ctx, pinned.String(), cred, 8*time.Second)
	if err != nil {
		var pe *probeError
		if errors.As(err, &pe) {
			result.Status, result.Message = pe.Status, pe.Detail
		} else {
			result.Status, result.Message = StatusProtocolError, "连接测试失败"
		}
		return s.finishTest(tenant, camera, cfg, cred, result, in == nil), nil
	}
	result.VideoCodec, result.H264Profile, result.AudioCodec = sdp.VideoCodec, sdp.H264Profile, sdp.AudioCodec
	if !s.active() || !s.mediaHealthy() {
		result.Status, result.Message = StatusMediaUnavailable, "摄像头 RTSP 应答正常，但媒体服务不可用，无法确认画面"
		return s.finishTest(tenant, camera, cfg, cred, result, in == nil), nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.cfg.StartTimeout+5*time.Second)
	info, err := s.mgr.probe(probeCtx, sourceSpec{URL: pinned.String(), Cred: cred}, 10*time.Second)
	cancel()
	if err != nil {
		result.Status, result.Message = StatusMediaFailed, "摄像头应答正常，但媒体服务未能取到画面："+truncate(redactText(err.Error()), 160)
		return s.finishTest(tenant, camera, cfg, cred, result, in == nil), nil
	}
	s.classify(&result, info, cfg)
	return s.finishTest(tenant, camera, cfg, cred, result, in == nil), nil
}

// classify reports playability from the media server's view of the stream.
func (s *Service) classify(result *model.CameraLiveTest, info mediaInfo, cfg model.CameraLiveConfig) {
	result.MediaVerified = true
	if v, ok := info.video(); ok {
		result.VideoCodec, result.Width, result.Height, result.FPS = v.Codec, v.Width, v.Height, v.FPS
	}
	if a, ok := info.audio(); ok {
		result.AudioCodec = a.Codec
	}
	transcodeOK := s.transcodeCap && cfg.TranscodeMode != transcodeOff
	switch result.VideoCodec {
	case "H264":
		result.Status, result.Message = StatusPlayable, "媒体服务已收到 H.264 画面，可直接播放"
		if cfg.SourceBFrames {
			result.Message += "；源含 B 帧，WebRTC 需转码，HLS 可直接播放"
			result.NeedTranscode = true
		}
	case "H265":
		result.NeedTranscode = true
		if transcodeOK {
			result.Status, result.Message = StatusTranscodeRequired, "源为 H.265：支持 H.265 的浏览器直接播放，其他浏览器将转码为 H.264"
		} else {
			result.Status, result.Message = StatusCodecIncompatible, "源为 H.265：仅支持 H.265 的浏览器可以播放；需要通用播放时请启用转码或改用 H.264 码流"
		}
	default:
		result.NeedTranscode = true
		if transcodeOK {
			result.Status, result.Message = StatusTranscodeRequired, "源视频编码 "+result.VideoCodec+" 需转码后播放"
		} else {
			result.Status, result.Message = StatusCodecIncompatible, "源视频编码 "+result.VideoCodec+" 浏览器无法直接播放"
		}
	}
}

// testGB checks a GB28181 channel: the device must be registered and online,
// accept the INVITE, and its media must reach the media server.
func (s *Service) testGB(ctx context.Context, tenant string, cfg model.CameraLiveConfig) model.CameraLiveTest {
	result := model.CameraLiveTest{Stream: "main", TestedAt: s.now().UnixMilli()}
	if s.gb == nil {
		result.Status, result.Message = StatusMediaUnavailable, errGBUnavailable.Error()
		return result
	}
	d, err := s.store.GetGBDevice(ctx, cfg.GBDeviceID)
	switch {
	case err != nil || d.TenantID != tenant:
		result.Status, result.Message = StatusTargetDenied, errGBMissing.Error()
		return result
	case !d.Enabled:
		result.Status, result.Message = StatusTargetDenied, errGBDisabled.Error()
		return result
	case !gbOnline(d, s.now()):
		result.Status, result.Message = StatusUnreachable, errGBOffline.Error()
		return result
	case !s.mediaHealthy():
		result.Status, result.Message = StatusMediaUnavailable, "国标设备在线，但媒体服务不可用，无法确认画面"
		return result
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.cfg.StartTimeout+15*time.Second)
	info, err := s.mgr.probe(probeCtx, sourceSpec{GB: &gbTarget{Tenant: tenant, DeviceID: cfg.GBDeviceID, ChannelID: cfg.GBChannelID}}, s.cfg.StartTimeout)
	cancel()
	var inviteErr *gb28181.InviteError
	switch {
	case errors.As(err, &inviteErr) && inviteErr.Status == 404:
		result.Status, result.Message = StatusStreamNotFound, "设备没有该通道："+inviteErr.Error()
	case errors.As(err, &inviteErr) && (inviteErr.Status == 401 || inviteErr.Status == 403):
		result.Status, result.Message = StatusAuthFailed, inviteErr.Error()
	case errors.As(err, &inviteErr):
		result.Status, result.Message = StatusProtocolError, inviteErr.Error()
	case errors.Is(err, errGBNoMediaIP):
		result.Status, result.Message = StatusMediaUnavailable, err.Error()
	case err != nil:
		result.Status, result.Message = StatusMediaFailed, "设备已应答点播，但媒体服务未收到画面："+truncate(redactText(err.Error()), 120)+"；请检查 IOT_GB28181_MEDIA_IP 以及 RTP 端口范围是否对设备开放"
	default:
		s.classify(&result, info, cfg)
	}
	return result
}

func (s *Service) finishTest(tenant, camera string, cfg model.CameraLiveConfig, cred Credentials, result model.CameraLiveTest, stored bool) model.CameraLiveTest {
	s.testMu.Lock()
	s.testResults[tenant+"\x00"+camera] = testMemo{fingerprint: fingerprint(cfg, cred), result: result}
	s.testMu.Unlock()
	if stored {
		if current, err := s.store.GetCameraLiveConfig(context.Background(), tenant, camera); err == nil {
			current.LastTest = &result
			_ = s.store.SaveCameraLiveConfig(context.Background(), current)
		}
	}
	return result
}

func (s *Service) memo(tenant, camera string) (testMemo, bool) {
	s.testMu.Lock()
	defer s.testMu.Unlock()
	m, ok := s.testResults[tenant+"\x00"+camera]
	return m, ok
}

// fingerprint identifies the connection-relevant part of a configuration.
func fingerprint(cfg model.CameraLiveConfig, cred Credentials) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%s\x00%s\x00%s", cfg.AccessMode, cfg.MainStreamURL, cfg.SubStreamURL, cfg.TranscodeMode, cfg.TranscodeProfile, cred.Username, cfg.SourceBFrames, cred.Password, cfg.GBDeviceID, cfg.GBChannelID)
	return hex.EncodeToString(h.Sum(nil))
}

// PlayRequest is the viewer's request. Browsers pick a variant and protocol
// only; stream IDs, source URLs and output profiles are decided server-side.
type PlayRequest struct {
	Stream   string     `json:"stream"`
	Protocol string     `json:"protocol"`
	Caps     clientCaps `json:"caps"`
}

// PlayGrant is returned to the browser. Token is the short-lived media
// credential for HLS; the WebRTC offer goes through the authenticated API.
type PlayGrant struct {
	SessionID        string   `json:"sessionId"`
	Token            string   `json:"token"`
	HLSURL           string   `json:"hlsUrl"`
	WHEPURL          string   `json:"whepUrl"`
	Protocol         string   `json:"protocol"`
	Stream           string   `json:"stream"`
	Streams          []string `json:"streams"`
	Profile          string   `json:"profile"`
	ProfileName      string   `json:"profileName"`
	Reason           string   `json:"reason,omitempty"`
	VideoCodec       string   `json:"videoCodec,omitempty"`
	AudioCodec       string   `json:"audioCodec,omitempty"`
	AudioNote        string   `json:"audioNote"`
	ExpiresAt        int64    `json:"expiresAt"`
	HeartbeatSeconds int      `json:"heartbeatSeconds"`
}

func (s *Service) ready(ctx context.Context) error {
	if !s.cfg.Deployed() {
		return ErrNotDeployed
	}
	if !s.active() {
		return ErrNotDeployed
	}
	enabled, err := s.moduleEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrModuleDisabled
	}
	if !s.mediaHealthy() {
		return ErrMediaDown
	}
	return nil
}

// CameraAvailable tells whether a camera can currently be offered for live view.
func (s *Service) CameraAvailable(ctx context.Context, tenant, camera string) bool {
	if s.ready(ctx) != nil {
		return false
	}
	cfg, err := s.store.GetCameraLiveConfig(ctx, tenant, camera)
	return err == nil && cfg.Enabled && liveConfigured(cfg)
}

// CreateSession authorizes the viewer, starts or joins the media tasks and
// issues a play session.
func (s *Service) CreateSession(ctx context.Context, tenant string, viewer Viewer, cameraID string, req PlayRequest) (PlayGrant, error) {
	if err := s.ready(ctx); err != nil {
		return PlayGrant{}, err
	}
	camera, err := s.cameras(ctx, tenant, cameraID)
	if err != nil || !camera.Enabled {
		return PlayGrant{}, ErrNotConfigured
	}
	if err := s.authorize(ctx, tenant, viewer, camera); err != nil {
		return PlayGrant{}, ErrForbidden
	}
	cfg, err := s.store.GetCameraLiveConfig(ctx, tenant, cameraID)
	if err != nil || !cfg.Enabled || !liveConfigured(cfg) {
		return PlayGrant{}, ErrNotConfigured
	}
	variant := req.Stream
	if variant != "main" && variant != "sub" {
		variant = cfg.DefaultStream
	}
	protocol := req.Protocol
	if protocol != "hls" {
		protocol = "webrtc"
	}
	spec, variant, err := s.source(ctx, tenant, cameraID, cfg, variant)
	if err != nil {
		return PlayGrant{}, err
	}
	cred := spec.Cred
	source, err := s.mgr.ensureSource(ctx, tenant, cameraID, variant, spec)
	if err != nil {
		return PlayGrant{}, scrubSecret(fmt.Errorf("取流失败：%w", err), cred.Password)
	}
	info, _ := s.mgr.sourceInfo(ctx, tenant, cameraID, variant)
	videoTrack, _ := info.video()
	audioTrack, _ := info.audio()
	profileID, reason, err := chooseProfile(cfg.TranscodeMode, cfg.TranscodeProfile, videoTrack.Codec, cfg.SourceBFrames, protocol, req.Caps, s.transcodeCap)
	if err != nil {
		if errors.Is(err, errNeedsTranscode) {
			return PlayGrant{}, invalid("%s；请管理员开启转码或改用兼容码流", reason)
		}
		return PlayGrant{}, invalid("%v", err)
	}
	app, stream := appSource, source.stream
	profile, _ := findProfile(profileID)
	if profile.Transcode {
		out, err := s.mgr.ensureTranscode(ctx, source, profile)
		if err != nil {
			return PlayGrant{}, scrubSecret(fmt.Errorf("转码失败：%w", err), cred.Password)
		}
		app, stream = appTranscode, out.stream
	}
	if protocol == "hls" {
		s.waitHLS(ctx, app, stream)
	}
	now := s.now()
	maxExpiry := now.Add(12 * time.Hour)
	if !viewer.ExpiresAt.IsZero() && viewer.ExpiresAt.Before(maxExpiry) {
		maxExpiry = viewer.ExpiresAt
	}
	token := randomToken(32)
	sess := &liveSession{VideoPlaySession: model.VideoPlaySession{ID: "vps_" + randomToken(12), TenantID: tenant, CameraID: cameraID, Username: viewer.Username, ManagedUser: viewer.Managed, SessionVersion: viewer.SessionVersion, Stream: variant, Profile: profileID, App: app, StreamKey: stream, TokenHash: hashToken(token), CreatedAt: now.UnixMilli(), ExpiresAt: min(now.Add(s.cfg.LeaseTTL).UnixMilli(), maxExpiry.UnixMilli()), MaxExpiresAt: maxExpiry.UnixMilli()}}
	if err := s.mgr.addSession(sess); err != nil {
		return PlayGrant{}, err
	}
	if err := s.store.SaveVideoPlaySession(ctx, sess.VideoPlaySession); err != nil {
		s.log.Warn("persist video session failed", "session", sess.ID, "error", err)
	}
	streams := []string{"main"}
	if cfg.SubStreamURL != "" && cfg.AccessMode != accessGB {
		streams = append(streams, "sub")
	}
	audioCodec := audioTrack.Codec
	return PlayGrant{
		SessionID: sess.ID, Token: token,
		HLSURL:   fmt.Sprintf("%s/%s/%s/%s/hls.m3u8", s.cfg.HLSPublicPath, token, app, stream),
		WHEPURL:  "/api/v1/video/play-sessions/" + sess.ID + "/whep",
		Protocol: protocol, Stream: variant, Streams: streams, Profile: profileID, ProfileName: profile.Name, Reason: reason,
		VideoCodec: videoTrack.Codec, AudioCodec: audioCodec, AudioNote: audioNote(audioCodec, profileID, protocol),
		ExpiresAt: sess.ExpiresAt, HeartbeatSeconds: max(5, int(s.cfg.LeaseTTL.Seconds()/3)),
	}, nil
}

// source resolves where a camera variant comes from. RTSP targets are
// validated again at use time, since DNS may have changed since saving.
func (s *Service) source(ctx context.Context, tenant, camera string, cfg model.CameraLiveConfig, variant string) (sourceSpec, string, error) {
	if cfg.AccessMode == accessGB {
		return sourceSpec{GB: &gbTarget{Tenant: tenant, DeviceID: cfg.GBDeviceID, ChannelID: cfg.GBChannelID}}, "main", nil
	}
	cred, err := s.credentials(ctx, tenant, camera)
	if err != nil {
		return sourceSpec{}, variant, invalid("摄像头凭据不可用：%v", err)
	}
	raw := cfg.MainStreamURL
	if variant == "sub" && cfg.SubStreamURL != "" {
		raw = cfg.SubStreamURL
	} else {
		variant = "main"
	}
	pinned, err := s.guard.pinnedRTSP(ctx, raw)
	if err != nil {
		return sourceSpec{}, variant, invalid("摄像头地址未通过目标地址校验")
	}
	return sourceSpec{URL: pinned.String(), Cred: cred}, variant, nil
}

// waitHLS waits briefly for the first HLS segment so the player's first
// playlist request succeeds; the player still retries if it is not ready.
func (s *Service) waitHLS(ctx context.Context, app, stream string) {
	deadline := s.now().Add(8 * time.Second)
	for s.now().Before(deadline) {
		if ready, err := s.media.HLSReady(ctx, app, stream); err == nil && ready {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (s *Service) ownSession(tenant string, viewer Viewer, id string) (*liveSession, error) {
	if !s.active() {
		return nil, ErrNotDeployed
	}
	sess, ok := s.mgr.session(id)
	if !ok || sess.TenantID != tenant || sess.Username != viewer.Username || sess.ManagedUser != viewer.Managed {
		return nil, ErrSessionGone
	}
	return sess, nil
}

// signalToken lets the media server's on_play hook recognize the API's own
// WHEP request for one session without exposing the browser's HLS token.
func (s *Service) signalToken(sessionID string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.HookSecret))
	mac.Write([]byte("whep\x00" + sessionID))
	return hex.EncodeToString(mac.Sum(nil))[:40]
}

// WHEP forwards the SDP offer of an authorized session.
func (s *Service) WHEP(ctx context.Context, tenant string, viewer Viewer, id, offer string) (string, error) {
	sess, err := s.ownSession(tenant, viewer, id)
	if err != nil {
		return "", err
	}
	if err := s.ready(ctx); err != nil {
		return "", err
	}
	if len(offer) == 0 || len(offer) > 64<<10 || !strings.HasPrefix(strings.TrimSpace(offer), "v=0") {
		return "", invalid("SDP offer 无效")
	}
	answer, del, err := s.media.WHEP(ctx, sess.App, sess.StreamKey, sess.ID+"."+s.signalToken(sess.ID), offer)
	if err != nil {
		return "", err
	}
	s.mgr.addWHEP(id, del)
	return answer, nil
}

// HeartbeatResult is returned on each lease renewal.
type HeartbeatResult struct {
	ExpiresAt  int64  `json:"expiresAt"`
	MediaState string `json:"mediaState"`
	Message    string `json:"message,omitempty"`
}

// Heartbeat re-validates the viewer and renews the lease. When the media task
// was lost (media server restart, camera reconnect give-up), it is restarted
// with bounded backoff and the player is told to reconnect.
func (s *Service) Heartbeat(ctx context.Context, tenant string, viewer Viewer, id string, rendering bool) (HeartbeatResult, error) {
	sess, err := s.ownSession(tenant, viewer, id)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if err := s.ready(ctx); err != nil {
		if errors.Is(err, ErrModuleDisabled) {
			s.mgr.revoke(id, "module disabled")
		}
		return HeartbeatResult{}, err
	}
	if err := s.recheck(ctx, sess.VideoPlaySession); err != nil {
		s.mgr.revoke(id, "access revoked")
		return HeartbeatResult{}, ErrForbidden
	}
	renewed, firstRender, err := s.mgr.renew(id, rendering)
	if err != nil {
		return HeartbeatResult{}, ErrSessionGone
	}
	if firstRender {
		if cfg, err := s.store.GetCameraLiveConfig(ctx, tenant, sess.CameraID); err == nil {
			cfg.LastPlayableAt = s.now().UnixMilli()
			_ = s.store.SaveCameraLiveConfig(ctx, cfg)
		}
	}
	out := HeartbeatResult{ExpiresAt: renewed.ExpiresAt, MediaState: "running"}
	state, lastErr := s.mgr.taskState(sess.App, sess.StreamKey)
	if state != taskRunning {
		if err := s.restart(ctx, sess.VideoPlaySession); err != nil {
			out.MediaState, out.Message = "reconnecting", redactText(err.Error())
		} else {
			out.MediaState = "restarted"
		}
		if lastErr != "" && out.Message == "" {
			out.Message = lastErr
		}
	}
	return out, nil
}

func (s *Service) restart(ctx context.Context, sess model.VideoPlaySession) error {
	cfg, err := s.store.GetCameraLiveConfig(ctx, sess.TenantID, sess.CameraID)
	if err != nil || !cfg.Enabled || !liveConfigured(cfg) {
		return ErrNotConfigured
	}
	spec, _, err := s.source(ctx, sess.TenantID, sess.CameraID, cfg, sess.Stream)
	if err != nil {
		return err
	}
	source, err := s.mgr.ensureSource(ctx, sess.TenantID, sess.CameraID, sess.Stream, spec)
	if err != nil {
		return scrubSecret(err, spec.Cred.Password)
	}
	if sess.App == appTranscode {
		profile, _ := findProfile(sess.Profile)
		if _, err := s.mgr.ensureTranscode(ctx, source, profile); err != nil {
			return err
		}
	}
	s.mgr.mu.Lock()
	if t := s.mgr.tasks[sess.App+"/"+sess.StreamKey]; t != nil {
		t.viewers[sess.ID] = true
		t.zeroSince = time.Time{}
	}
	s.mgr.mu.Unlock()
	return nil
}

// Stop ends a session on the viewer's request (closing the dialog).
func (s *Service) Stop(tenant string, viewer Viewer, id string) error {
	if _, err := s.ownSession(tenant, viewer, id); err != nil {
		return err
	}
	s.mgr.revoke(id, "stopped by viewer")
	return nil
}

// CameraChanged ends live playback of a camera after its metadata changed in
// a way that affects authorization (device link, disabled) or it was deleted.
func (s *Service) CameraChanged(tenant, camera, reason string) {
	if s.active() {
		s.mgr.stopCamera(tenant, camera, reason)
	}
}

// RevalidateTenant re-checks the tenant's sessions now, after users, roles or
// device scopes changed, instead of waiting for the next sweep.
func (s *Service) RevalidateTenant(ctx context.Context, tenant string) {
	if !s.active() {
		return
	}
	for _, sess := range s.mgr.activeSessions() {
		if sess.TenantID != tenant {
			continue
		}
		if err := s.recheck(ctx, sess); err != nil {
			s.mgr.revoke(sess.ID, "access revoked")
		}
	}
}

// ---- media server hooks ----

// VerifyHookSecret checks the shared secret carried in the hook URL.
func (s *Service) VerifyHookSecret(secret string) bool {
	return s.active() && secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(s.cfg.HookSecret)) == 1
}

// HookBody is the subset of ZLMediaKit hook fields the module reads.
type HookBody struct {
	MediaServerID string `json:"mediaServerId"`
	App           string `json:"app"`
	Stream        string `json:"stream"`
	Schema        string `json:"schema"`
	Params        string `json:"params"`
	IP            string `json:"ip"`
	ID            string `json:"id"`
}

// Hook handles one media server event and returns the JSON reply. Handlers are
// idempotent and never trust counts from hooks, so duplicated, delayed or
// reordered deliveries cannot corrupt viewer counts or task state.
func (s *Service) Hook(ctx context.Context, event string, body HookBody) map[string]any {
	ok := map[string]any{"code": 0, "msg": "success"}
	deny := func(msg string) map[string]any { return map[string]any{"code": -1, "msg": msg} }
	if body.MediaServerID != "" && body.MediaServerID != s.activeMediaID() {
		return deny("unknown media server")
	}
	params, _ := url.ParseQuery(body.Params)
	switch event {
	case "on_play":
		if s.playAllowed(ctx, body, params) {
			return ok
		}
		return deny("play not authorized")
	case "on_publish":
		// Only the module's own FFmpeg output (from loopback) and GB28181
		// streams the platform INVITEd may publish.
		allowed := body.App == appTranscode && loopback(body.IP) && s.mgr.validInternalToken(body.App, body.Stream, params.Get("vt"))
		if !allowed && s.gb != nil && (body.App == appSource || body.App == appTest) {
			allowed = s.gb.expects(body.App + "/" + body.Stream)
		}
		if allowed {
			return map[string]any{"code": 0, "msg": "success", "enable_hls": body.App != appTest, "enable_rtsp": true, "enable_rtmp": false, "enable_ts": false, "enable_fmp4": false, "enable_mp4": false, "enable_audio": true, "add_mute_audio": false, "auto_close": false}
		}
		return deny("publish not authorized")
	case "on_stream_none_reader":
		// The manager, not the reader count, decides when to release.
		go s.mgr.reap()
		return map[string]any{"code": 0, "close": false}
	case "on_stream_not_found":
		return map[string]any{"code": 0, "close": true}
	case "on_server_started":
		s.log.Info("media server started; marking media tasks for recovery")
		s.mgr.markAllLost()
		go s.reconcile(context.Background())
		return ok
	case "on_server_keepalive":
		s.healthMu.Lock()
		s.keepaliveAt = s.now()
		s.healthMu.Unlock()
		return ok
	case "on_rtp_server_timeout":
		// A GB28181 device stopped sending; reconcile marks the task lost.
		go s.reconcile(context.Background())
		return ok
	case "on_stream_changed":
		go s.reconcile(context.Background())
		return ok
	}
	return ok
}

func (s *Service) playAllowed(ctx context.Context, body HookBody, params url.Values) bool {
	if enabled, _ := s.moduleEnabled(ctx); !enabled {
		return false
	}
	vt := params.Get("vt")
	// FFmpeg reading a source inside the media container.
	if loopback(body.IP) && s.mgr.validInternalToken(body.App, body.Stream, vt) {
		return true
	}
	// The API's WHEP request: "<sessionId>.<signal>".
	if id, sig, found := strings.Cut(vt, "."); found {
		sess, ok := s.mgr.session(id)
		return ok && subtle.ConstantTimeCompare([]byte(sig), []byte(s.signalToken(id))) == 1 && sess.App == body.App && sess.StreamKey == body.Stream
	}
	// HLS through the authenticating reverse proxy.
	sess, ok := s.mgr.sessionByToken(vt)
	return ok && sess.App == body.App && sess.StreamKey == body.Stream
}

// MediaAuth authorizes one HLS request forwarded by the reverse proxy
// (nginx auth_request or the Vite dev middleware). The path carries the play
// token: <prefix>/<token>/<app>/<stream>/<file>.
func (s *Service) MediaAuth(ctx context.Context, uri string) bool {
	if !s.active() {
		return false
	}
	u, err := url.Parse(uri)
	if err != nil {
		return false
	}
	rest, ok := strings.CutPrefix(u.Path, s.cfg.HLSPublicPath+"/")
	if !ok {
		return false
	}
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) != 4 || strings.Contains(parts[3], "..") {
		return false
	}
	file := strings.ToLower(parts[3])
	if !strings.HasSuffix(file, ".m3u8") && !strings.HasSuffix(file, ".ts") && !strings.HasSuffix(file, ".m4s") && !strings.HasSuffix(file, ".mp4") {
		return false
	}
	if enabled, _ := s.moduleEnabled(ctx); !enabled {
		return false
	}
	sess, ok := s.mgr.sessionByToken(parts[0])
	return ok && sess.App == parts[1] && sess.StreamKey == parts[2]
}

func loopback(ip string) bool {
	addr, err := netip.ParseAddr(strings.Trim(ip, "[]"))
	if err != nil {
		parsed := net.ParseIP(ip)
		return parsed != nil && parsed.IsLoopback()
	}
	return addr.Unmap().IsLoopback()
}

// IsLimit reports resource-limit errors (HTTP 429).
func IsLimit(err error) bool {
	return errors.Is(err, errLimitSources) || errors.Is(err, errLimitTrans) || errors.Is(err, errLimitSession) || errors.Is(err, errBackoff)
}

// scrubbedError keeps the wrapped error for errors.Is while removing a secret
// from the message shown to users and written to logs.
type scrubbedError struct {
	msg string
	err error
}

func (e scrubbedError) Error() string { return e.msg }
func (e scrubbedError) Unwrap() error { return e.err }

func scrubSecret(err error, secret string) error {
	if err == nil || len(secret) < 3 || !strings.Contains(err.Error(), secret) {
		return err
	}
	return scrubbedError{msg: strings.ReplaceAll(err.Error(), secret, "***"), err: err}
}
