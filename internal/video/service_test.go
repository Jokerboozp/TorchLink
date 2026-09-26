package video

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/video/videotest"
)

const testSecret = "media-secret-0123456789abcdef"
const testHook = "hook-secret-0123456789abcdef0"

type fixture struct {
	svc     *Service
	media   *videotest.Fake
	store   *memory.Repository
	cameras map[string]model.VideoCameraMapping
	denied  map[string]bool
	mu      sync.Mutex
}

func newFixture(t *testing.T, mutate ...func(*config.VideoConfig)) *fixture {
	t.Helper()
	f := &fixture{media: videotest.New(testSecret), store: memory.NewRepository(), cameras: map[string]model.VideoCameraMapping{}, denied: map[string]bool{}}
	t.Cleanup(f.media.Close)
	_, lan, _ := net.ParseCIDR("10.0.0.0/8")
	cfg := config.VideoConfig{MediaAPIURL: f.media.URL, MediaSecret: testSecret, MediaServerID: "torchlink-media-1", HookSecret: testHook, CredentialKey: []byte(strings.Repeat("k", 32)), CredentialKeyID: "k1", AllowedCIDRs: []*net.IPNet{lan}, AllowedPorts: map[int]bool{554: true}, HLSPublicPath: "/media/hls", LeaseTTL: 45 * time.Second, IdleGrace: 20 * time.Second, StartTimeout: 2 * time.Second, MaxSessions: 50, MaxSourceStreams: 8, Transcode: true, MaxTranscodes: 1, HWAccel: "none"}
	for _, m := range mutate {
		m(&cfg)
	}
	if err := cfg.Problem(); err != nil {
		t.Fatal(err)
	}
	lookup := func(_ context.Context, tenant, camera string) (model.VideoCameraMapping, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		c, ok := f.cameras[tenant+"/"+camera]
		if !ok {
			return c, model.ErrNotFound
		}
		return c, nil
	}
	authorize := func(_ context.Context, tenant string, v Viewer, c model.VideoCameraMapping) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.denied[v.Username] || c.TenantID != tenant {
			return ErrForbidden
		}
		return nil
	}
	f.svc = New(cfg, f.store, lookup, authorize, nil)
	f.svc.checkHealth(context.Background())
	if err := f.store.SaveVideoModuleState(context.Background(), model.VideoModuleState{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) camera(t *testing.T, tenant, id, url string, mode string) {
	t.Helper()
	f.mu.Lock()
	f.cameras[tenant+"/"+id] = model.VideoCameraMapping{TenantID: tenant, CameraID: id, CameraName: id, Enabled: true}
	f.mu.Unlock()
	if _, err := f.svc.SaveConfig(context.Background(), tenant, id, LiveConfigInput{Enabled: true, AccessMode: "RTSP", BrandTemplate: "generic", ManualURL: true, MainStreamURL: url, TranscodeMode: mode, TranscodeProfile: "h264_720p", Username: "admin", Password: "p@ss:w/rd#1%"}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) deny(user string) { f.mu.Lock(); f.denied[user] = true; f.mu.Unlock() }

var alice = Viewer{Username: "alice", Managed: true, SessionVersion: 1}

func TestConcurrentFirstPlaysShareOneUpstream(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	var wg sync.WaitGroup
	grants := make([]PlayGrant, 8)
	errs := make([]error, 8)
	for i := range grants {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			grants[i], errs[i] = f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{Protocol: "webrtc"})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("play %d: %v", i, err)
		}
	}
	if n := f.media.Count("addStreamProxy"); n != 1 {
		t.Fatalf("concurrent first plays must create one pull, got %d", n)
	}
	proxy, _ := f.media.Proxy("src/" + strings.Split(grants[0].HLSURL, "/")[5])
	if proxy.Password != "p@ss:w/rd#1%" || strings.Contains(proxy.URL, "@") {
		t.Fatalf("credentials must travel as separate options, not in the URL: %+v", proxy)
	}
	if grants[0].Token == grants[1].Token || grants[0].SessionID == grants[1].SessionID {
		t.Fatal("every viewer gets its own session and token")
	}
}

func TestStreamIDsAreTenantIsolated(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	f.camera(t, "t2", "cam", "rtsp://10.0.0.6:554/main", "off")
	a, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.svc.CreateSession(context.Background(), "t2", alice, "cam", PlayRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Split(a.HLSURL, "/")[5] == strings.Split(b.HLSURL, "/")[5] {
		t.Fatal("equal camera IDs in different tenants must not share a stream")
	}
	if f.svc.MediaAuth(context.Background(), strings.Replace(a.HLSURL, strings.Split(a.HLSURL, "/")[5], strings.Split(b.HLSURL, "/")[5], 1)) {
		t.Fatal("a token must not open another tenant's stream")
	}
}

func TestViewerCountingGraceAndTranscodeDependency(t *testing.T) {
	f := newFixture(t)
	f.media.Codec["rtsp://10.0.0.5:554/main"] = "H265"
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "auto")
	now := time.Now()
	f.svc.mgr.now = func() time.Time { return now }
	g1, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{Protocol: "webrtc"})
	if err != nil || g1.Profile != "h264_720p" {
		t.Fatalf("H.265 without browser support must auto-transcode: %+v %v", g1, err)
	}
	g2, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{Protocol: "hls"})
	if err != nil || g2.Profile != "h264_720p" {
		t.Fatalf("second viewer: %+v %v", g2, err)
	}
	if n := f.media.Count("addFFmpegSource"); n != 1 {
		t.Fatalf("the same output profile shares one transcode, got %d", n)
	}
	// Direct H.265 viewer shares the source with the transcode.
	g3, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{Protocol: "webrtc", Caps: clientCaps{WebRTCH265: true}})
	if err != nil || g3.Profile != "direct" {
		t.Fatalf("H.265 capable browser plays direct: %+v %v", g3, err)
	}
	for _, id := range []string{g1.SessionID, g2.SessionID} {
		if err := f.svc.Stop("t1", alice, id); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(10 * time.Second)
	f.svc.mgr.reap()
	if _, ffmpegs, _ := f.media.Snapshot(); ffmpegs != 1 {
		t.Fatal("the transcode must survive within the grace period")
	}
	now = now.Add(15 * time.Second)
	f.svc.mgr.reap()
	proxies, ffmpegs, _ := f.media.Snapshot()
	if ffmpegs != 0 || proxies != 1 {
		t.Fatalf("after grace the transcode stops while the direct viewer keeps the source: proxies=%d ffmpegs=%d", proxies, ffmpegs)
	}
	if err := f.svc.Stop("t1", alice, g3.SessionID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(21 * time.Second)
	f.svc.mgr.reap()
	if proxies, _, _ := f.media.Snapshot(); proxies != 0 {
		t.Fatal("the FFmpeg reader alone must not keep the source alive")
	}
}

func TestLeaseExpiryReleasesWithoutStopRequest(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	now := time.Now()
	f.svc.mgr.now = func() time.Time { return now }
	g, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{Protocol: "hls"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.WHEP(context.Background(), "t1", alice, g.SessionID, "v=0\r\n"); err != nil {
		t.Fatal(err)
	}
	// Browser crashed: no heartbeat, no stop request.
	now = now.Add(46 * time.Second)
	f.svc.mgr.reap()
	if f.svc.MediaAuth(context.Background(), g.HLSURL) {
		t.Fatal("an expired lease must not authorize media requests")
	}
	if f.media.DeletedPeers() != 1 {
		t.Fatal("the WebRTC peer must be closed on the media server when the lease expires")
	}
	now = now.Add(21 * time.Second)
	f.svc.mgr.reap()
	if proxies, _, _ := f.media.Snapshot(); proxies != 0 {
		t.Fatal("the pull must be released after the lease expired and the grace passed")
	}
}

func TestHeartbeatRechecksAccessAndRestartsLostTasks(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	g, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Media server restarted: objects are gone; the next heartbeat rebuilds.
	f.media.Restart()
	f.svc.Hook(context.Background(), "on_server_started", HookBody{MediaServerID: "torchlink-media-1"})
	hb, err := f.svc.Heartbeat(context.Background(), "t1", alice, g.SessionID, true)
	if err != nil || hb.MediaState != "restarted" {
		t.Fatalf("heartbeat after media restart: %+v %v", hb, err)
	}
	if proxies, _, _ := f.media.Snapshot(); proxies != 1 {
		t.Fatal("the pull must be restored")
	}
	if cfg, _ := f.store.GetCameraLiveConfig(context.Background(), "t1", "cam"); cfg.LastPlayableAt == 0 {
		t.Fatal("a rendering heartbeat records that the camera actually played")
	}
	// Other users cannot use the session.
	if _, err := f.svc.Heartbeat(context.Background(), "t1", Viewer{Username: "bob", Managed: true}, g.SessionID, false); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("another user must not renew the session: %v", err)
	}
	// Permission revoked: the heartbeat ends the session.
	f.deny("alice")
	if _, err := f.svc.Heartbeat(context.Background(), "t1", alice, g.SessionID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked access must end playback: %v", err)
	}
	if f.svc.MediaAuth(context.Background(), g.HLSURL) {
		t.Fatal("revoked session must not fetch HLS")
	}
}

func TestSweepAndRevalidateEndPlaybackAfterPermissionChange(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	g, _ := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	f.deny("alice")
	f.svc.RevalidateTenant(context.Background(), "t1")
	if f.svc.MediaAuth(context.Background(), g.HLSURL) {
		t.Fatal("RevalidateTenant must revoke sessions that are no longer permitted")
	}
}

func TestModuleDisableStopsEverythingButKeepsConfig(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	g, _ := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	if err := f.svc.SetModule(context.Background(), false, "admin"); err != nil {
		t.Fatal(err)
	}
	if proxies, ffmpegs, _ := f.media.Snapshot(); proxies+ffmpegs != 0 {
		t.Fatal("disabling must remove pulls and transcodes")
	}
	if f.svc.MediaAuth(context.Background(), g.HLSURL) {
		t.Fatal("disabling must invalidate media access")
	}
	if _, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{}); !errors.Is(err, ErrModuleDisabled) {
		t.Fatalf("new plays must be rejected: %v", err)
	}
	if cfg, err := f.store.GetCameraLiveConfig(context.Background(), "t1", "cam"); err != nil || !cfg.Enabled {
		t.Fatal("configuration must be kept")
	}
	if st := f.svc.Status(context.Background()); st.State != StateDisabled {
		t.Fatalf("state: %s", st.State)
	}
	_ = f.svc.SetModule(context.Background(), true, "admin")
	if _, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{}); err != nil {
		t.Fatalf("re-enabled module plays with the kept configuration: %v", err)
	}
}

func TestMediaDownDegradesOnlyLiveFeatures(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	f.media.SetDown(true)
	f.svc.checkHealth(context.Background())
	if st := f.svc.Status(context.Background()); st.State != StateDegraded {
		t.Fatalf("state: %s", st.State)
	}
	if _, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{}); !errors.Is(err, ErrMediaDown) {
		t.Fatalf("play must report the media outage: %v", err)
	}
	if _, err := f.svc.SaveConfig(context.Background(), "t1", "cam", LiveConfigInput{Enabled: true, AccessMode: "RTSP", BrandTemplate: "generic", ManualURL: true, MainStreamURL: "rtsp://10.0.0.5:554/main"}); err != nil {
		t.Fatalf("configuration stays editable while the media server is down: %v", err)
	}
}

func TestFailedPullBacksOffAndHidesCredentials(t *testing.T) {
	f := newFixture(t)
	f.media.FailURL["rtsp://10.0.0.5:554/main"] = true
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	_, err := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	if err == nil || strings.Contains(err.Error(), "p@ss") {
		t.Fatalf("pull failure must be reported without credentials: %v", err)
	}
	_, err = f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	if !IsLimit(err) || f.media.Count("addStreamProxy") != 1 {
		t.Fatalf("an immediate retry must back off instead of reconnecting: %v (calls %d)", err, f.media.Count("addStreamProxy"))
	}
}

func TestTranscodeLimitAndTemplateVerification(t *testing.T) {
	f := newFixture(t)
	for _, u := range []string{"rtsp://10.0.0.5:554/a", "rtsp://10.0.0.6:554/b"} {
		f.media.Codec[u] = "H265"
	}
	f.camera(t, "t1", "a", "rtsp://10.0.0.5:554/a", "auto")
	f.camera(t, "t1", "b", "rtsp://10.0.0.6:554/b", "auto")
	if _, err := f.svc.CreateSession(context.Background(), "t1", alice, "a", PlayRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateSession(context.Background(), "t1", alice, "b", PlayRequest{}); !IsLimit(err) {
		t.Fatalf("the transcode limit must be enforced: %v", err)
	}
	// A missing template makes ZLMediaKit fall back to its default command;
	// the platform must detect the wrong encoder and stop.
	g := newFixture(t)
	delete(g.media.Templates, "ffmpeg.iot_audio_aac")
	g.camera(t, "t1", "c", "rtsp://10.0.0.7:554/c", "off")
	_, _ = g.svc.SaveConfig(context.Background(), "t1", "c", LiveConfigInput{Enabled: true, AccessMode: "RTSP", BrandTemplate: "generic", ManualURL: true, MainStreamURL: "rtsp://10.0.0.7:554/c", TranscodeMode: "fixed", TranscodeProfile: "audio_aac"})
	if _, err := g.svc.CreateSession(context.Background(), "t1", alice, "c", PlayRequest{Protocol: "hls"}); err == nil {
		t.Fatal("a transcode whose command lacks the expected encoder must fail")
	}
	if _, ffmpegs, _ := g.media.Snapshot(); ffmpegs != 0 {
		t.Fatal("the mismatched transcode must be removed")
	}
}

func TestReconcileRemovesOrphansAndRestoresSessions(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	g, _ := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	f.media.AddOrphan("src", "corphan")
	f.media.AddOrphan("other", "keep") // not managed by the platform
	// API restart: a new service over the same store and media server.
	restarted := New(f.svc.cfg, f.store, f.svc.cameras, f.svc.authorize, nil)
	restarted.checkHealth(context.Background())
	if err := restarted.mgr.restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.mgr.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.media.Proxy("src/corphan"); ok {
		t.Fatal("orphan platform pulls must be removed")
	}
	if _, ok := f.media.Proxy("other/keep"); !ok {
		t.Fatal("objects outside the platform apps must be left alone")
	}
	if !restarted.MediaAuth(context.Background(), g.HLSURL) {
		t.Fatal("a valid session must survive an API restart")
	}
	hb, err := restarted.Heartbeat(context.Background(), "t1", alice, g.SessionID, false)
	if err != nil || hb.MediaState != "running" {
		t.Fatalf("restored session keeps running: %+v %v", hb, err)
	}
	// A new viewer joins the adopted source (no cached track info after restart).
	if _, err := restarted.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{}); err != nil {
		t.Fatalf("a new viewer must join a source adopted after restart: %v", err)
	}
	if n := f.media.Count("addStreamProxy"); n != 1 {
		t.Fatalf("the adopted pull must be reused, got %d pulls", n)
	}
}

func TestHooksAreAuthenticatedAndIdempotent(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	g, _ := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	parts := strings.Split(g.HLSURL, "/")
	stream := parts[5]
	if f.svc.VerifyHookSecret("wrong") || !f.svc.VerifyHookSecret(testHook) {
		t.Fatal("hook secret check")
	}
	play := func(body HookBody) int { return f.svc.Hook(context.Background(), "on_play", body)["code"].(int) }
	if play(HookBody{App: "src", Stream: stream, Params: "vt=" + g.Token, IP: "172.18.0.5"}) != 0 {
		t.Fatal("the session token authorizes its own stream")
	}
	if play(HookBody{App: "src", Stream: "cother", Params: "vt=" + g.Token, IP: "172.18.0.5"}) == 0 {
		t.Fatal("the token must not authorize another stream")
	}
	internal := f.svc.mgr.internalToken("src", stream)
	if play(HookBody{App: "src", Stream: stream, Params: "vt=" + internal, IP: "172.18.0.5"}) == 0 {
		t.Fatal("internal tokens are accepted from loopback only")
	}
	if play(HookBody{App: "src", Stream: stream, Params: "vt=" + internal, IP: "127.0.0.1"}) != 0 {
		t.Fatal("the media server's own FFmpeg reader must be allowed")
	}
	if play(HookBody{MediaServerID: "other", App: "src", Stream: stream, Params: "vt=" + g.Token}) == 0 {
		t.Fatal("unknown media server IDs are rejected")
	}
	if f.svc.Hook(context.Background(), "on_publish", HookBody{App: "src", Stream: stream, IP: "10.1.1.1"})["code"].(int) == 0 {
		t.Fatal("nobody may publish into the platform apps")
	}
	// Duplicate / out-of-order hooks do not change viewer state.
	for i := 0; i < 3; i++ {
		if reply := f.svc.Hook(context.Background(), "on_stream_none_reader", HookBody{App: "src", Stream: stream}); reply["close"] != false {
			t.Fatal("the media server must never close a stream on its own reader count")
		}
		f.svc.Hook(context.Background(), "on_server_keepalive", HookBody{})
	}
	if !f.svc.MediaAuth(context.Background(), g.HLSURL) {
		t.Fatal("hooks must not affect sessions")
	}
}

func TestMediaAuthRejectsNonMediaPaths(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	g, _ := f.svc.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{})
	base := strings.TrimSuffix(g.HLSURL, "hls.m3u8")
	for path, want := range map[string]bool{
		g.HLSURL:                          true,
		base + "2026-09-26/15/53-08_1.ts": true,
		base + "../x.m3u8":                false,
		base + "index.html":               false,
		"/media/hls/" + g.Token:           false,
		"/index/api/getServerConfig":      false,
	} {
		if got := f.svc.MediaAuth(context.Background(), path); got != want {
			t.Errorf("%s: got %v want %v", path, got, want)
		}
	}
}

func TestConfigPasswordIsWriteOnly(t *testing.T) {
	f := newFixture(t)
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	cfg, err := f.svc.GetConfig(context.Background(), "t1", "cam")
	if err != nil || !cfg.HasPassword || cfg.Username != "admin" {
		t.Fatalf("config: %+v %v", cfg, err)
	}
	// Changing the username with an empty password keeps the password.
	if _, err := f.svc.SaveConfig(context.Background(), "t1", "cam", LiveConfigInput{Enabled: true, AccessMode: "RTSP", BrandTemplate: "generic", ManualURL: true, MainStreamURL: "rtsp://10.0.0.5:554/main", Username: "operator"}); err != nil {
		t.Fatal(err)
	}
	cred, _ := f.svc.credentials(context.Background(), "t1", "cam")
	if cred.Username != "operator" || cred.Password != "p@ss:w/rd#1%" {
		t.Fatalf("empty password keeps the stored one: %+v", cred)
	}
	if _, err := f.svc.SaveConfig(context.Background(), "t1", "cam", LiveConfigInput{Enabled: true, AccessMode: "RTSP", BrandTemplate: "generic", ManualURL: true, MainStreamURL: "rtsp://10.0.0.5:554/main", Username: "operator", ClearPassword: true}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = f.svc.GetConfig(context.Background(), "t1", "cam")
	if cfg.HasPassword {
		t.Fatal("ClearPassword removes the password")
	}
	if _, err := f.svc.SaveConfig(context.Background(), "t1", "cam", LiveConfigInput{Enabled: true, AccessMode: "RTSP", BrandTemplate: "generic", ManualURL: true, MainStreamURL: "rtsp://169.254.169.254:554/x"}); err == nil {
		t.Fatal("metadata addresses must be rejected on save")
	}
}

func TestNotDeployedReportsState(t *testing.T) {
	s := New(config.VideoConfig{}, memory.NewRepository(), nil, nil, nil)
	if st := s.Status(context.Background()); st.State != StateNotDeployed {
		t.Fatalf("state: %s", st.State)
	}
	if _, err := s.CreateSession(context.Background(), "t1", alice, "cam", PlayRequest{}); !errors.Is(err, ErrNotDeployed) {
		t.Fatalf("play: %v", err)
	}
	s.CameraChanged("t1", "cam", "x") // must be a no-op
}
