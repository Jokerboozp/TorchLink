package video

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"iot-platform/internal/config"
	"iot-platform/internal/video/videotest"
)

type flakyMedia struct {
	mediaServer
	down bool
}

func (m *flakyMedia) Health(context.Context) error {
	if m.down {
		return errors.New("down")
	}
	return nil
}

// The module moves to a standby at once when the active media server fails,
// and back to the primary only after it stays healthy for failoverRise
// checks; only the active server's hooks are accepted.
func TestMediaFailover(t *testing.T) {
	ctx := context.Background()
	primary, standby := &flakyMedia{}, &flakyMedia{}
	f := newFailoverMedia([]mediaMember{{id: "media-1", mediaIP: "10.0.0.1", server: primary}, {id: "media-2", mediaIP: "10.0.0.2", server: standby}})
	var switches []string
	f.onSwitch = func(from, to mediaMember) { switches = append(switches, from.id+">"+to.id) }
	s := &Service{failover: f, cfg: config.VideoConfig{MediaServerID: "media-1"}, log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: time.Now}
	hook := func(id string) int {
		return s.Hook(ctx, "on_server_keepalive", HookBody{MediaServerID: id})["code"].(int)
	}
	if err := f.Health(ctx); err != nil || f.current().id != "media-1" || hook("media-2") != -1 || hook("media-1") != 0 {
		t.Fatalf("initial: %v %s", err, f.current().id)
	}
	primary.down = true
	if err := f.Health(ctx); err != nil || f.current().id != "media-2" || f.current().mediaIP != "10.0.0.2" {
		t.Fatalf("failover: %v %s", err, f.current().id)
	}
	if hook("media-1") != -1 || hook("media-2") != 0 {
		t.Fatal("hooks of the failed primary must be ignored")
	}
	primary.down = false
	for i := 1; i < failoverRise; i++ {
		if _ = f.Health(ctx); f.current().id != "media-2" {
			t.Fatalf("moved back after %d healthy checks", i)
		}
	}
	if _ = f.Health(ctx); f.current().id != "media-1" {
		t.Fatal("did not return to the recovered primary")
	}
	primary.down, standby.down = true, true
	if err := f.Health(ctx); err == nil {
		t.Fatal("no healthy media server must be reported")
	}
	if len(switches) != 2 || switches[0] != "media-1>media-2" || switches[1] != "media-2>media-1" {
		t.Fatalf("switches %v", switches)
	}
}

func TestStandbyMediaConfig(t *testing.T) {
	t.Setenv("IOT_VIDEO_MEDIA_API_URL", "http://10.0.0.1:80")
	t.Setenv("IOT_VIDEO_MEDIA_STANDBY_URLS", "http://10.0.0.2:80")
	t.Setenv("IOT_VIDEO_MEDIA_STANDBY_IDS", "media-2")
	cfg := config.Load().Video
	if len(cfg.Standbys) != 1 || cfg.Standbys[0].MediaIP != "10.0.0.2" || cfg.Standbys[0].ID != "media-2" {
		t.Fatalf("standbys %+v", cfg.Standbys)
	}
	t.Setenv("IOT_VIDEO_MEDIA_STANDBY_IDS", "")
	if err := config.Load().Video.Problem(); err == nil {
		t.Fatal("a standby without an ID must be refused")
	}
}

// With a standby configured, a failed primary moves playback to the standby:
// the next heartbeat rebuilds the pull there, as after a media restart.
func TestPlaybackMovesToStandbyMediaServer(t *testing.T) {
	standby := videotest.New(testSecret)
	t.Cleanup(standby.Close)
	f := newFixture(t, func(c *config.VideoConfig) {
		c.Standbys = []config.MediaEndpoint{{URL: standby.URL, ID: "torchlink-media-2", MediaIP: "10.0.0.51"}}
	})
	ctx := context.Background()
	f.camera(t, "t1", "cam", "rtsp://10.0.0.5:554/main", "off")
	g, err := f.svc.CreateSession(ctx, "t1", alice, "cam", PlayRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if proxies, _, _ := f.media.Snapshot(); proxies != 1 {
		t.Fatal("the pull runs on the primary")
	}
	f.media.SetDown(true)
	f.svc.checkHealth(ctx)
	if st := f.svc.Status(ctx); st.State == StateDegraded {
		t.Fatalf("the module stays available on the standby: %s", st.State)
	}
	hb, err := f.svc.Heartbeat(ctx, "t1", alice, g.SessionID, true)
	if err != nil || hb.MediaState != "restarted" {
		t.Fatalf("heartbeat after failover: %+v %v", hb, err)
	}
	if proxies, _, _ := standby.Snapshot(); proxies != 1 {
		t.Fatal("the pull must be rebuilt on the standby")
	}
	// The failed primary's start-up event must not reset the standby's tasks.
	if reply := f.svc.Hook(ctx, "on_server_started", HookBody{MediaServerID: "torchlink-media-1"}); reply["code"] != -1 {
		t.Fatalf("hook of the inactive server accepted: %v", reply)
	}
}
