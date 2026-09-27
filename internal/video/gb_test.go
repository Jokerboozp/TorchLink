package video

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/video/gb28181"
	"iot-platform/internal/video/gb28181/gbtest"
)

const (
	gbDeviceID  = "34020000001320000001"
	gbChannelID = "34020000001310000001"
)

// gbFixture runs the real SIP server on loopback against the fake media
// server and a simulated device.
func gbFixture(t *testing.T, answer func(gbtest.Invite) (int, string)) (*fixture, *gbtest.Device) {
	t.Helper()
	f := newFixture(t, func(c *config.VideoConfig) {
		_, loopbackNet, _ := net.ParseCIDR("127.0.0.0/8")
		c.AllowedCIDRs = append(c.AllowedCIDRs, loopbackNet)
		c.IdleGrace = 0
		c.GB28181 = config.GB28181Config{Enabled: true, ServerID: "34020000002000000001", Domain: "3402000000", Listen: "127.0.0.1:0", SIPHost: "127.0.0.1", MediaIP: "10.0.0.50"}
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f.svc.Start(ctx)
	if st := f.svc.Status(ctx); st.GB28181 == nil || !st.GB28181.Running {
		t.Fatalf("SIP server must run: %+v", st.GB28181)
	}
	if _, err := f.svc.SaveGBDevice(ctx, "t1", gbDeviceID, GBDeviceInput{Name: "东门 NVR", Enabled: true, Password: "gb-pass-1"}); err != nil {
		t.Fatal(err)
	}
	device, err := gbtest.Start(ctx, gbtest.Config{
		ID: gbDeviceID, Password: "gb-pass-1", Domain: "3402000000",
		Server:   "127.0.0.1:" + strconv.Itoa(f.svc.gb.sip.LocalPort()),
		Channels: []gbtest.Channel{{ID: gbChannelID, Name: "大厅通道"}, {ID: "34020000001310000002", Name: "车库"}},
		Answer:   answer,
		// A device that starts sending once the call is confirmed.
		OnAck: func(inv gbtest.Invite) {
			ssrc := inv.Offer.SSRC
			if answer != nil {
				if _, own := answer(inv); own != "" {
					ssrc = own
				}
			}
			f.media.FeedPort(inv.Offer.Port, ssrc, "H264")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, err := device.Register(ctx, 3600); err != nil || code != 200 {
		t.Fatalf("register: %d %v", code, err)
	}
	return f, device
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *fixture) gbCamera(t *testing.T, tenant, id, channel string) {
	t.Helper()
	f.mu.Lock()
	f.cameras[tenant+"/"+id] = model.VideoCameraMapping{TenantID: tenant, CameraID: id, CameraName: id, Enabled: true}
	f.mu.Unlock()
	if _, err := f.svc.SaveConfig(context.Background(), tenant, id, LiveConfigInput{Enabled: true, AccessMode: "GB28181", GBDeviceID: gbDeviceID, GBChannelID: channel, TranscodeMode: "off"}); err != nil {
		t.Fatal(err)
	}
}

func TestGBRegistrationCatalogAndAuthentication(t *testing.T) {
	f, device := gbFixture(t, nil)
	ctx := context.Background()
	var listed []model.GBDevice
	eventually(t, "catalog", func() bool {
		listed, _ = f.svc.ListGBDevices(ctx, "t1")
		return len(listed) == 1 && len(listed[0].State.Channels) == 2 && listed[0].State.Model != ""
	})
	d := listed[0]
	if !d.Online || !d.HasPassword || d.Password != nil || d.State.Channels[0].Name != "大厅通道" || d.State.Model != "SIM-1" {
		t.Fatalf("device view: %+v", d)
	}
	if code, err := device.Keepalive(ctx); err != nil || code != 200 {
		t.Fatalf("keepalive of a registered device: %d %v", code, err)
	}

	// A wrong password and an unknown device are refused.
	bad, err := gbtest.Start(ctx, gbtest.Config{ID: gbDeviceID, Password: "nope", Domain: "3402000000", Server: "127.0.0.1:" + strconv.Itoa(f.svc.gb.sip.LocalPort())})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := bad.Register(ctx, 3600); code != 403 {
		t.Fatalf("wrong password must be refused, got %d", code)
	}
	stranger, _ := gbtest.Start(ctx, gbtest.Config{ID: "34020000001320000099", Password: "x", Domain: "3402000000", Server: "127.0.0.1:" + strconv.Itoa(f.svc.gb.sip.LocalPort())})
	if code, _ := stranger.Register(ctx, 3600); code != 403 {
		t.Fatalf("unknown devices must be refused, got %d", code)
	}

	// Device IDs are global: another tenant cannot claim or use this device.
	if _, err := f.svc.SaveGBDevice(ctx, "t2", gbDeviceID, GBDeviceInput{Enabled: true, Password: "x"}); !errors.Is(err, model.ErrGBDeviceTaken) {
		t.Fatalf("device ID of another tenant: %v", err)
	}
	f.mu.Lock()
	f.cameras["t2/cam"] = model.VideoCameraMapping{TenantID: "t2", CameraID: "cam", Enabled: true}
	f.mu.Unlock()
	if _, err := f.svc.SaveConfig(ctx, "t2", "cam", LiveConfigInput{Enabled: true, AccessMode: "GB28181", GBDeviceID: gbDeviceID, GBChannelID: gbChannelID}); err == nil {
		t.Fatal("a camera must not use another tenant's device")
	}
	if rows, _ := f.svc.ListGBDevices(ctx, "t2"); len(rows) != 0 {
		t.Fatal("devices must not leak across tenants")
	}

	// Unregistering takes the device offline.
	if code, err := device.Register(ctx, 0); err != nil || code != 200 {
		t.Fatalf("unregister: %d %v", code, err)
	}
	if d, _ := f.svc.GetGBDevice(ctx, "t1", gbDeviceID); d.Online {
		t.Fatal("unregistered device must be offline")
	}
	if code, _ := device.Keepalive(ctx); code != 403 {
		t.Fatalf("keepalive after unregister must ask to register again, got %d", code)
	}
}

func TestGBLivePlayLifecycle(t *testing.T) {
	f, device := gbFixture(t, nil)
	ctx := context.Background()
	f.gbCamera(t, "t1", "cam", gbChannelID)

	test, err := f.svc.Test(ctx, "t1", "cam", nil)
	if err != nil || test.Status != StatusPlayable || !test.MediaVerified {
		t.Fatalf("test: %+v %v", test, err)
	}
	eventually(t, "probe released", func() bool { return device.Active() == 0 && f.media.Receivers() == 0 })

	g, err := f.svc.CreateSession(ctx, "t1", alice, "cam", PlayRequest{})
	if err != nil {
		t.Fatal(err)
	}
	invites := device.Invites()
	inv := invites[len(invites)-1]
	if inv.ChannelID != gbChannelID || inv.Offer.IP != "10.0.0.50" || !strings.HasPrefix(inv.Subject, gbChannelID+":") || !strings.HasPrefix(inv.Offer.SSRC, "020000") {
		t.Fatalf("invite: %+v", inv)
	}
	stream := f.svc.mgr.streamName("t1", "cam", "main")
	if r, ok := f.media.Receiver("src/" + stream); !ok || r.SSRC != inv.Offer.SSRC || r.TCP {
		t.Fatalf("receiver: %+v %v", r, ok)
	}
	if g.Streams[0] != "main" || len(g.Streams) != 1 || g.VideoCodec != "H264" {
		t.Fatalf("grant: %+v", g)
	}
	// Only streams the platform INVITEd may publish.
	if reply := f.svc.Hook(ctx, "on_publish", HookBody{App: "src", Stream: stream, IP: "10.0.0.7"}); reply["code"] != 0 {
		t.Fatalf("expected GB publish: %v", reply)
	}
	if reply := f.svc.Hook(ctx, "on_publish", HookBody{App: "src", Stream: "cforged", IP: "10.0.0.7"}); reply["code"] == 0 {
		t.Fatal("unexpected publish must be denied")
	}

	// The device ends the stream: the task is lost and the next heartbeat INVITEs again.
	if err := device.HangUp(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "receiver closed after device BYE", func() bool { _, ok := f.media.Receiver("src/" + stream); return !ok })
	hb, err := f.svc.Heartbeat(ctx, "t1", alice, g.SessionID, true)
	if err != nil || hb.MediaState != "restarted" {
		t.Fatalf("heartbeat: %+v %v", hb, err)
	}
	if len(device.Invites()) != len(invites)+1 {
		t.Fatal("restart must INVITE again")
	}

	// Leaving releases the call with BYE and closes the receiver.
	byes := device.Byes()
	if err := f.svc.Stop("t1", alice, g.SessionID); err != nil {
		t.Fatal(err)
	}
	f.svc.mgr.reap()
	if device.Byes() != byes+1 || f.media.Receivers() != 0 || device.Active() != 0 {
		t.Fatalf("release: byes %d→%d receivers %d active %d", byes, device.Byes(), f.media.Receivers(), device.Active())
	}
}

func TestGBAnswerErrorsAndOwnSSRC(t *testing.T) {
	f, device := gbFixture(t, func(inv gbtest.Invite) (int, string) {
		if inv.ChannelID == "34020000001310000404" {
			return 404, ""
		}
		return 200, "0100000077" // the device picks its own SSRC
	})
	ctx := context.Background()
	f.gbCamera(t, "t1", "cam", gbChannelID)
	g, err := f.svc.CreateSession(ctx, "t1", alice, "cam", PlayRequest{})
	if err != nil {
		t.Fatalf("stream with the device's own SSRC: %v", err)
	}
	stream := f.svc.mgr.streamName("t1", "cam", "main")
	if r, _ := f.media.Receiver("src/" + stream); r.SSRC != "0100000077" {
		t.Fatalf("receiver must follow the answered SSRC: %+v", r)
	}
	_ = f.svc.Stop("t1", alice, g.SessionID)
	f.svc.mgr.reap()

	f.gbCamera(t, "t1", "missing", "34020000001310000404")
	if res, _ := f.svc.Test(ctx, "t1", "missing", nil); res.Status != StatusStreamNotFound {
		t.Fatalf("404 channel: %+v", res)
	}
	if f.media.Receivers() != 0 {
		t.Fatal("a refused INVITE must close its receiver")
	}

	// A device that stopped sending keepalives is offline: no INVITE is sent.
	f.svc.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	before := len(device.Invites())
	if res, _ := f.svc.Test(ctx, "t1", "cam", nil); res.Status != StatusUnreachable {
		t.Fatalf("offline device: %+v", res)
	}
	if len(device.Invites()) != before {
		t.Fatal("offline devices must not be invited")
	}
}

func TestGBReconcileClosesReceiversWithoutCall(t *testing.T) {
	f, _ := gbFixture(t, nil)
	ctx := context.Background()
	// A receiver left by a previous API process: its SIP dialog is gone.
	if _, err := f.svc.media.OpenRTPServer(ctx, "src", "cleftover", false, "0200000009"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.mgr.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if f.media.Receivers() != 0 {
		t.Fatal("orphan receivers must be closed")
	}
	if !gb28181.ValidID(gbDeviceID) {
		t.Fatal("fixture IDs must be valid")
	}
}
