package video

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Media apps. Only streams under these apps are managed (and reconciled).
const (
	appSource    = "src"
	appTranscode = "tc"
	appTest      = "probe"
)

// Task states.
const (
	taskStarting = "starting"
	taskRunning  = "running"
	taskFailed   = "failed"
	taskLost     = "lost"
)

var (
	errBackoff      = errors.New("摄像头连接失败，正在退避，请稍后重试")
	errLimitSources = errors.New("同时拉流的摄像头数量已达上限")
	errLimitTrans   = errors.New("转码任务数量已达上限，请稍后重试或改用直接播放")
	errLimitSession = errors.New("播放会话数量已达上限")
)

// sourceSpec describes how to obtain one camera stream: an RTSP pull (the URL
// is pinned to a validated IP and never contains credentials) or a GB28181
// channel the device is asked to send.
type sourceSpec struct {
	URL  string
	Cred Credentials
	GB   *gbTarget
}

// gbMedia runs GB28181 calls: a receiver on the media server plus the SIP
// dialog asking the device to send to it.
type gbMedia interface {
	start(ctx context.Context, app, stream string, target gbTarget) error
	stop(app, stream string)
	owns(key string) bool
}

// task is one media server object: a pull proxy (source) or an FFmpeg
// transcode (transcode). Real viewers are counted in viewers; a transcode
// keeps its source alive as an internal dependent, counted separately, so an
// FFmpeg reader alone can never keep a source alive after its viewers leave.
type task struct {
	app, stream  string
	kind         string
	tenant       string
	camera       string
	variant      string
	profile      string
	sourceKey    string
	ffmpegKey    string
	viewers      map[string]bool
	zeroSince    time.Time
	state        string
	lastErr      string
	failures     int
	backoffUntil time.Time
	info         mediaInfo
	// gb marks a source received from a GB28181 device instead of pulled.
	gb bool
	// seq orders the last state change against reconcile listings.
	seq uint64
}

func (t *task) key() string { return t.app + "/" + t.stream }

type flight struct {
	done chan struct{}
	err  error
}

type liveSession struct {
	model.VideoPlaySession
	whep      []string
	rendering bool
}

type managerConfig struct {
	HookSecret     string
	LeaseTTL       time.Duration
	IdleGrace      time.Duration
	StartTimeout   time.Duration
	MaxSessions    int
	MaxSources     int
	MaxTranscodes  int
	FailureBackoff time.Duration
	MaxBackoff     time.Duration
}

// manager owns play sessions and media tasks for a single media node. The
// media server is the source of truth for what is running; the manager keeps
// viewer counts, decides when to start and stop, and reconciles regularly.
type manager struct {
	cfg      managerConfig
	media    mediaServer
	store    ports.VideoStore
	log      *slog.Logger
	now      func() time.Time
	mu       sync.Mutex
	sessions map[string]*liveSession
	byToken  map[string]string
	tasks    map[string]*task
	flights  map[string]*flight
	probing  map[string]bool
	gb       gbMedia
	seq      uint64
}

func newManager(cfg managerConfig, media mediaServer, store ports.VideoStore, log *slog.Logger) *manager {
	if cfg.FailureBackoff <= 0 {
		cfg.FailureBackoff = 5 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 2 * time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	return &manager{cfg: cfg, media: media, store: store, log: log, now: time.Now, sessions: map[string]*liveSession{}, byToken: map[string]string{}, tasks: map[string]*task{}, flights: map[string]*flight{}, probing: map[string]bool{}}
}

// streamName derives a stable, unguessable stream ID. The HMAC covers the
// tenant, so streams of different tenants never collide even when camera IDs
// are equal, and knowing a camera ID does not reveal its stream.
func (m *manager) streamName(tenant, camera, variant string) string {
	mac := hmac.New(sha256.New, []byte(m.cfg.HookSecret))
	mac.Write([]byte("stream\x00" + tenant + "\x00" + camera + "\x00" + variant))
	return "c" + hex.EncodeToString(mac.Sum(nil))[:30]
}

// internalToken authorizes the media server's own FFmpeg reader and publisher
// for one stream. It is only accepted from loopback addresses.
func (m *manager) internalToken(app, stream string) string {
	mac := hmac.New(sha256.New, []byte(m.cfg.HookSecret))
	mac.Write([]byte("internal\x00" + app + "/" + stream))
	return hex.EncodeToString(mac.Sum(nil))[:40]
}

func (m *manager) validInternalToken(app, stream, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(m.internalToken(app, stream))) == 1
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// ensureSource starts (or joins) the pull proxy for one camera variant.
func (m *manager) ensureSource(ctx context.Context, tenant, camera, variant string, spec sourceSpec) (*task, error) {
	stream := m.streamName(tenant, camera, variant)
	return m.ensure(appSource+"/"+stream, func() *task {
		return &task{app: appSource, stream: stream, kind: "source", tenant: tenant, camera: camera, variant: variant, profile: profileDirect}
	}, func(c context.Context, t *task) error {
		m.mu.Lock()
		t.gb = spec.GB != nil
		m.mu.Unlock()
		if spec.GB != nil {
			if m.gb == nil {
				return errGBUnavailable
			}
			if err := m.gb.start(c, appSource, stream, *spec.GB); err != nil {
				return err
			}
			return m.waitReady(c, t)
		}
		if err := m.media.AddStreamProxy(c, appSource, stream, spec.URL, spec.Cred, proxyOptions{HLS: true, RTC: true, RetryCount: 3, Timeout: 10 * time.Second}); err != nil {
			return err
		}
		return m.waitReady(c, t)
	})
}

// ensureTranscode starts (or joins) an FFmpeg output of an existing source.
func (m *manager) ensureTranscode(ctx context.Context, source *task, profile outputProfile) (*task, error) {
	stream := source.stream + "_" + strings.ReplaceAll(profile.ID, "_", "")
	return m.ensure(appTranscode+"/"+stream, func() *task {
		return &task{app: appTranscode, stream: stream, kind: "transcode", tenant: source.tenant, camera: source.camera, variant: source.variant, profile: profile.ID, sourceKey: source.key()}
	}, func(c context.Context, t *task) error {
		src := fmt.Sprintf("rtsp://127.0.0.1:554/%s/%s?vt=%s", appSource, source.stream, m.internalToken(appSource, source.stream))
		dst := fmt.Sprintf("rtsp://127.0.0.1:554/%s/%s?vt=%s", appTranscode, stream, m.internalToken(appTranscode, stream))
		key, err := m.media.AddFFmpegSource(c, profile.CmdKey, src, dst, m.cfg.StartTimeout)
		if key != "" {
			m.mu.Lock()
			t.ffmpegKey = key
			m.mu.Unlock()
		}
		if err != nil {
			if key != "" {
				_ = m.media.DelFFmpegSource(context.Background(), key)
			}
			return fmt.Errorf("转码启动失败：%w", err)
		}
		// Verify the running command uses the expected encoder; ZLMediaKit
		// silently falls back to its default template when a key is missing.
		if sources, listErr := m.media.ListFFmpegSources(c); listErr == nil {
			for _, s := range sources {
				if s.Key == key && !strings.Contains(" "+s.Cmd+" ", " "+profile.Encoder+" ") {
					_ = m.media.DelFFmpegSource(context.Background(), key)
					return errors.New("媒体服务的转码模板缺失或编码器不符，已停止该转码任务")
				}
			}
		}
		return m.waitReady(c, t)
	})
}

func (m *manager) ensure(key string, create func() *task, start func(context.Context, *task) error) (*task, error) {
	m.mu.Lock()
	t := m.tasks[key]
	if t != nil && t.state == taskRunning {
		m.mu.Unlock()
		return t, nil
	}
	if f := m.flights[key]; f != nil {
		m.mu.Unlock()
		<-f.done
		if f.err != nil {
			return nil, f.err
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if joined := m.tasks[key]; joined != nil && joined.state == taskRunning {
			return joined, nil
		}
		return nil, errors.New("媒体任务已结束，请重试")
	}
	if t != nil && m.now().Before(t.backoffUntil) {
		err := fmt.Errorf("%w（%s）", errBackoff, t.lastErr)
		m.mu.Unlock()
		return nil, err
	}
	if t == nil {
		t = create()
		t.viewers = map[string]bool{}
		if err := m.checkLimitsLocked(t); err != nil {
			m.mu.Unlock()
			return nil, err
		}
		m.tasks[key] = t
	}
	m.setStateLocked(t, taskStarting, t.lastErr)
	t.zeroSince = m.now()
	f := &flight{done: make(chan struct{})}
	m.flights[key] = f
	m.mu.Unlock()

	// The start outlives any single request: other viewers may be waiting.
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.StartTimeout+10*time.Second)
	err := start(ctx, t)
	cancel()

	m.mu.Lock()
	delete(m.flights, key)
	if err != nil {
		m.setStateLocked(t, taskFailed, redactText(err.Error()))
		t.failures++
		backoff := m.cfg.FailureBackoff << min(t.failures-1, 6)
		t.backoffUntil = m.now().Add(min(backoff, m.cfg.MaxBackoff))
		f.err = err
		m.log.Warn("video task start failed", "task", key, "failures", t.failures, "error", t.lastErr)
	} else {
		m.setStateLocked(t, taskRunning, "")
		t.failures, t.backoffUntil = 0, time.Time{}
		t.zeroSince = m.now()
	}
	close(f.done)
	m.mu.Unlock()
	if err != nil {
		// Leave nothing half-started on the media server.
		m.stopTask(t)
		return nil, err
	}
	return t, nil
}

func (m *manager) checkLimitsLocked(t *task) error {
	sources, transcodes := 0, 0
	for _, other := range m.tasks {
		if other.state == taskFailed {
			continue
		}
		if other.kind == "source" {
			sources++
		} else {
			transcodes++
		}
	}
	if t.kind == "source" && m.cfg.MaxSources > 0 && sources >= m.cfg.MaxSources {
		return errLimitSources
	}
	if t.kind == "transcode" && transcodes >= m.cfg.MaxTranscodes {
		return errLimitTrans
	}
	return nil
}

// waitReady polls the media server until the stream has ready video tracks.
// Only this — not an API success response — means frames arrived.
func (m *manager) waitReady(ctx context.Context, t *task) error {
	deadline := m.now().Add(m.cfg.StartTimeout)
	for {
		info, online, err := m.media.MediaInfo(ctx, t.app, t.stream)
		if err == nil && online {
			if v, ok := info.video(); ok && v.Ready {
				m.mu.Lock()
				t.info = info
				m.mu.Unlock()
				return nil
			}
		}
		if m.now().After(deadline) {
			if err != nil {
				return err
			}
			return errors.New("媒体服务在限定时间内未收到可用的视频帧")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (m *manager) stopTask(t *task) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if t.kind == "transcode" {
		if t.ffmpegKey != "" {
			if err := m.media.DelFFmpegSource(ctx, t.ffmpegKey); err != nil {
				m.log.Warn("stop transcode failed", "task", t.key(), "error", redactText(err.Error()))
			}
		}
		_ = m.media.CloseStreams(ctx, t.app, t.stream)
		return
	}
	if t.gb && m.gb != nil {
		m.gb.stop(t.app, t.stream)
		_ = m.media.CloseStreams(ctx, t.app, t.stream)
		return
	}
	if err := m.media.DelStreamProxy(ctx, t.app, t.stream); err != nil {
		m.log.Warn("stop source failed", "task", t.key(), "error", redactText(err.Error()))
	}
	_ = m.media.CloseStreams(ctx, t.app, t.stream)
}

// addSession registers a viewer on its output task (and, through it, the source).
func (m *manager) addSession(s *liveSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.MaxSessions > 0 && len(m.sessions) >= m.cfg.MaxSessions {
		return errLimitSession
	}
	m.sessions[s.ID] = s
	m.byToken[s.TokenHash] = s.ID
	if t := m.tasks[s.App+"/"+s.StreamKey]; t != nil {
		t.viewers[s.ID] = true
		t.zeroSince = time.Time{}
	}
	return nil
}

func (m *manager) session(id string) (*liveSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.RevokedAt != 0 {
		return nil, false
	}
	copy := *s
	return &copy, true
}

// sessionByToken finds an active session for a media request.
func (m *manager) sessionByToken(token string) (*liveSession, bool) {
	if len(token) < 32 || len(token) > 128 {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byToken[hashToken(token)]
	if !ok {
		return nil, false
	}
	s := m.sessions[id]
	now := m.now().UnixMilli()
	if s == nil || s.RevokedAt != 0 || s.ExpiresAt <= now || s.MaxExpiresAt <= now {
		return nil, false
	}
	copy := *s
	return &copy, true
}

// renew extends a lease after the caller re-validated the viewer.
func (m *manager) renew(id string, rendering bool) (model.VideoPlaySession, bool, error) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok || s.RevokedAt != 0 {
		m.mu.Unlock()
		return model.VideoPlaySession{}, false, errors.New("播放会话已结束")
	}
	now := m.now()
	if s.MaxExpiresAt <= now.UnixMilli() {
		m.mu.Unlock()
		m.revoke(id, "session lifetime reached")
		return model.VideoPlaySession{}, false, errors.New("播放会话已到期，请重新打开")
	}
	s.ExpiresAt = min(now.Add(m.cfg.LeaseTTL).UnixMilli(), s.MaxExpiresAt)
	firstRender := rendering && !s.rendering
	s.rendering = s.rendering || rendering
	out := s.VideoPlaySession
	m.mu.Unlock()
	if err := m.store.SaveVideoPlaySession(context.Background(), out); err != nil {
		m.log.Warn("persist video session failed", "session", id, "error", err)
	}
	return out, firstRender, nil
}

func (m *manager) addWHEP(id, deleteQuery string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil && s.RevokedAt == 0 && deleteQuery != "" {
		s.whep = append(s.whep, deleteQuery)
	}
}

// revoke ends a session: it can no longer fetch HLS, its WebRTC peers are
// closed on the media server, and its viewer count is released. Revocation is
// idempotent.
func (m *manager) revoke(id, reason string) bool {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return false
	}
	delete(m.sessions, id)
	delete(m.byToken, s.TokenHash)
	for _, t := range m.tasks {
		if t.viewers[id] {
			delete(t.viewers, id)
			if len(t.viewers) == 0 {
				// The grace period starts when the last viewer leaves.
				t.zeroSince = m.now()
			}
		}
	}
	whep := s.whep
	s.RevokedAt, s.RevokeReason = m.now().UnixMilli(), reason
	record := s.VideoPlaySession
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, q := range whep {
		if err := m.media.DeleteWebRTC(ctx, q); err != nil {
			m.log.Warn("close webrtc peer failed", "session", id, "error", err)
		}
	}
	if err := m.store.SaveVideoPlaySession(ctx, record); err != nil {
		m.log.Warn("persist video session revoke failed", "session", id, "error", err)
	}
	return true
}

// revokeWhere revokes every session matching the predicate.
func (m *manager) revokeWhere(match func(model.VideoPlaySession) bool, reason string) int {
	m.mu.Lock()
	ids := []string{}
	for id, s := range m.sessions {
		if match(s.VideoPlaySession) {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.revoke(id, reason)
	}
	return len(ids)
}

func (m *manager) activeSessions() []model.VideoPlaySession {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.VideoPlaySession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.VideoPlaySession)
	}
	return out
}

// reap expires sessions whose lease lapsed (closed tab, crash, network loss)
// and stops tasks that have had no viewers for the grace period.
func (m *manager) reap() {
	now := m.now()
	expired := m.revokeWhere(func(s model.VideoPlaySession) bool {
		return s.ExpiresAt <= now.UnixMilli() || s.MaxExpiresAt <= now.UnixMilli()
	}, "lease expired")
	if expired > 0 {
		m.log.Info("video sessions expired", "count", expired)
	}
	m.mu.Lock()
	stop := []*task{}
	// Transcodes first, so their sources see the dependency released.
	for _, kind := range []string{"transcode", "source"} {
		for key, t := range m.tasks {
			if t.kind != kind || t.state == taskStarting {
				continue
			}
			users := len(t.viewers)
			if kind == "source" {
				for _, other := range m.tasks {
					if other.sourceKey == key && other.state != taskFailed {
						users++
					}
				}
			}
			if users > 0 {
				t.zeroSince = time.Time{}
				continue
			}
			if t.zeroSince.IsZero() {
				t.zeroSince = now
			}
			if now.Sub(t.zeroSince) >= m.cfg.IdleGrace && now.After(t.backoffUntil) {
				delete(m.tasks, key)
				if t.state != taskFailed {
					stop = append(stop, t)
				}
			}
		}
	}
	m.mu.Unlock()
	for _, t := range stop {
		m.log.Info("video task released", "task", t.key(), "kind", t.kind)
		m.stopTask(t)
	}
}

// reconcile compares the media server with the manager: objects the manager
// does not know (orphans after an API restart or crash) are removed, and
// tasks the media server lost (restart, give-up after retries) are marked so
// the next heartbeat restarts them.
func (m *manager) reconcile(ctx context.Context) error {
	// The listing is a snapshot: tasks that changed state after it was taken
	// (started, restarted, marked lost by a media restart) are left alone, so
	// a slow reconcile can never undo newer state.
	m.mu.Lock()
	listedSeq := m.seq
	m.mu.Unlock()
	proxies, err := m.media.ListStreamProxies(ctx)
	if err != nil {
		return err
	}
	sources, err := m.media.ListFFmpegSources(ctx)
	if err != nil {
		return err
	}
	var receivers []string
	if m.gb != nil {
		if receivers, err = m.media.ListRTPServers(ctx); err != nil {
			return err
		}
	}
	present := map[string]bool{}
	m.mu.Lock()
	orphanProxies := []string{}
	for _, key := range proxies {
		// key: __defaultVhost__/app/stream
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 || (parts[1] != appSource && parts[1] != appTest) {
			continue
		}
		k := parts[1] + "/" + parts[2]
		present[k] = true
		if _, ok := m.tasks[k]; !ok && m.flights[k] == nil && !m.probing[k] {
			orphanProxies = append(orphanProxies, k)
		}
	}
	// A GB28181 receiver only counts when this process holds its call; after
	// an API restart the dialog is gone, so the receiver is closed and the
	// next heartbeat INVITEs again.
	orphanReceivers := []string{}
	for _, k := range receivers {
		if !strings.HasPrefix(k, appSource+"/") && !strings.HasPrefix(k, appTest+"/") {
			continue
		}
		if m.flights[k] != nil || m.probing[k] {
			continue
		}
		if m.gb.owns(k) {
			present[k] = true
		} else {
			orphanReceivers = append(orphanReceivers, k)
		}
	}
	orphanFFmpeg := []string{}
	for _, s := range sources {
		u, parseErr := url.Parse(s.DstURL)
		if parseErr != nil {
			continue
		}
		k := strings.TrimPrefix(u.Path, "/")
		if !strings.HasPrefix(k, appTranscode+"/") {
			continue
		}
		present[k] = true
		if t, ok := m.tasks[k]; ok {
			if t.ffmpegKey == "" {
				t.ffmpegKey = s.Key
			}
		} else if m.flights[k] == nil {
			orphanFFmpeg = append(orphanFFmpeg, s.Key)
		}
	}
	for k, t := range m.tasks {
		if t.seq > listedSeq {
			continue
		}
		if t.state == taskRunning && !present[k] {
			m.setStateLocked(t, taskLost, "媒体服务上的任务已不存在")
		} else if t.state == taskLost && present[k] {
			m.setStateLocked(t, taskRunning, "")
		}
	}
	m.mu.Unlock()
	for _, k := range orphanProxies {
		app, stream, _ := strings.Cut(k, "/")
		m.log.Info("removing orphan media proxy", "task", k)
		_ = m.media.DelStreamProxy(ctx, app, stream)
	}
	for _, key := range orphanFFmpeg {
		m.log.Info("removing orphan transcode", "key", key)
		_ = m.media.DelFFmpegSource(ctx, key)
	}
	for _, k := range orphanReceivers {
		app, stream, _ := strings.Cut(k, "/")
		m.log.Info("removing orphan GB28181 receiver", "task", k)
		m.gb.stop(app, stream)
	}
	return nil
}

// markLost marks a task whose media ended (device BYE, receiver timeout) so
// the next heartbeat restarts it.
func (m *manager) markLost(app, stream, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.tasks[app+"/"+stream]; t != nil && t.state == taskRunning {
		m.setStateLocked(t, taskLost, reason)
	}
}

func (m *manager) setStateLocked(t *task, state, lastErr string) {
	m.seq++
	t.state, t.lastErr, t.seq = state, lastErr, m.seq
}

// markAllLost is used when the media server reports a restart.
func (m *manager) markAllLost() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.state == taskRunning {
			m.setStateLocked(t, taskLost, "媒体服务已重启")
		}
	}
}

// taskState reports the state of a session's output task.
func (m *manager) taskState(app, stream string) (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[app+"/"+stream]
	if t == nil {
		return taskLost, ""
	}
	return t.state, t.lastErr
}

// sourceInfo returns the source's track information. Tasks adopted after an
// API restart have no cached information yet, so it is read from the media
// server on demand.
func (m *manager) sourceInfo(ctx context.Context, tenant, camera, variant string) (mediaInfo, bool) {
	m.mu.Lock()
	t := m.tasks[appSource+"/"+m.streamName(tenant, camera, variant)]
	if t == nil || t.state != taskRunning {
		m.mu.Unlock()
		return mediaInfo{}, false
	}
	info, app, stream := t.info, t.app, t.stream
	m.mu.Unlock()
	if _, ok := info.video(); ok {
		return info, true
	}
	fresh, online, err := m.media.MediaInfo(ctx, app, stream)
	if err != nil || !online {
		return mediaInfo{}, false
	}
	m.mu.Lock()
	t.info = fresh
	m.mu.Unlock()
	return fresh, true
}

// restore reloads persisted sessions after an API restart. Their tasks are
// adopted as lost; reconcile and the next heartbeat bring them back.
func (m *manager) restore(ctx context.Context) error {
	rows, err := m.store.ListActiveVideoPlaySessions(ctx, m.now().UnixMilli())
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range rows {
		s := &liveSession{VideoPlaySession: row}
		m.sessions[row.ID] = s
		m.byToken[row.TokenHash] = row.ID
		key := row.App + "/" + row.StreamKey
		t := m.tasks[key]
		if t == nil {
			t = &task{app: row.App, stream: row.StreamKey, tenant: row.TenantID, camera: row.CameraID, variant: row.Stream, profile: row.Profile, viewers: map[string]bool{}, state: taskLost}
			if row.App == appTranscode {
				t.kind = "transcode"
				t.sourceKey = appSource + "/" + m.streamName(row.TenantID, row.CameraID, row.Stream)
				if m.tasks[t.sourceKey] == nil {
					m.tasks[t.sourceKey] = &task{app: appSource, stream: m.streamName(row.TenantID, row.CameraID, row.Stream), kind: "source", tenant: row.TenantID, camera: row.CameraID, variant: row.Stream, profile: profileDirect, viewers: map[string]bool{}, state: taskLost}
				}
			} else {
				t.kind = "source"
			}
			m.tasks[key] = t
		}
		t.viewers[row.ID] = true
	}
	return nil
}

// stopAll revokes every session and removes every managed media object.
func (m *manager) stopAll(reason string) {
	m.revokeWhere(func(model.VideoPlaySession) bool { return true }, reason)
	m.mu.Lock()
	tasks := make([]*task, 0, len(m.tasks))
	for key, t := range m.tasks {
		tasks = append(tasks, t)
		delete(m.tasks, key)
	}
	m.mu.Unlock()
	for _, t := range tasks {
		if t.kind == "transcode" {
			m.stopTask(t)
		}
	}
	for _, t := range tasks {
		if t.kind == "source" {
			m.stopTask(t)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = m.reconcile(ctx)
}

// probe pulls a stream once into a temporary media object to confirm the media
// server can receive decodable frames, then removes it.
func (m *manager) probe(ctx context.Context, spec sourceSpec, timeout time.Duration) (mediaInfo, error) {
	stream := "t" + randomToken(12)
	key := appTest + "/" + stream
	m.mu.Lock()
	m.probing[key] = true
	m.mu.Unlock()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if spec.GB != nil {
			if m.gb != nil {
				m.gb.stop(appTest, stream)
			}
		} else {
			_ = m.media.DelStreamProxy(cleanup, appTest, stream)
		}
		cancel()
		m.mu.Lock()
		delete(m.probing, key)
		m.mu.Unlock()
	}()
	if spec.GB != nil {
		if m.gb == nil {
			return mediaInfo{}, errGBUnavailable
		}
		if err := m.gb.start(ctx, appTest, stream, *spec.GB); err != nil {
			return mediaInfo{}, err
		}
	} else if err := m.media.AddStreamProxy(ctx, appTest, stream, spec.URL, spec.Cred, proxyOptions{RetryCount: 0, Timeout: timeout}); err != nil {
		return mediaInfo{}, err
	}
	t := &task{app: appTest, stream: stream}
	if err := m.waitReady(ctx, t); err != nil {
		return mediaInfo{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return t.info, nil
}

// stopCamera revokes a camera's sessions and removes its media objects now,
// used when the camera is deleted, re-linked, disabled or reconfigured.
func (m *manager) stopCamera(tenant, camera, reason string) int {
	n := m.revokeWhere(func(s model.VideoPlaySession) bool { return s.TenantID == tenant && s.CameraID == camera }, reason)
	m.mu.Lock()
	tasks := []*task{}
	for key, t := range m.tasks {
		if t.tenant == tenant && t.camera == camera {
			tasks = append(tasks, t)
			delete(m.tasks, key)
		}
	}
	m.mu.Unlock()
	for _, kind := range []string{"transcode", "source"} {
		for _, t := range tasks {
			if t.kind == kind {
				m.stopTask(t)
			}
		}
	}
	return n
}

func (m *manager) counts() (sessions, sources, transcodes int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.state == taskFailed {
			continue
		}
		if t.kind == "source" {
			sources++
		} else {
			transcodes++
		}
	}
	return len(m.sessions), sources, transcodes
}
