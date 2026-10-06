package video

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testGuard(t *testing.T, cidrs []string, ports ...int) targetGuard {
	t.Helper()
	g := targetGuard{ports: map[int]bool{}}
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatal(err)
		}
		g.networks = append(g.networks, n)
	}
	for _, p := range ports {
		g.ports[p] = true
	}
	return g
}

type fakeResolver map[string][]netip.Addr

func (r fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if v, ok := r[host]; ok {
		return v, nil
	}
	return nil, errors.New("no such host")
}

func TestSealerBindsCredentialToCamera(t *testing.T) {
	s := sealer{key: []byte(strings.Repeat("k", 32)), keyID: "k1"}
	sealed, err := s.seal("t1", "cam", Credentials{Username: "admin", Password: "p@ss:w/rd#1%"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed.Ciphertext), "p@ss") {
		t.Fatal("password stored in clear text")
	}
	got, err := s.open(sealed)
	if err != nil || got.Password != "p@ss:w/rd#1%" || got.Username != "admin" {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	moved := sealed
	moved.CameraID = "other"
	if _, err := s.open(moved); err == nil {
		t.Fatal("a sealed credential must not open for another camera")
	}
	if _, err := (sealer{}).seal("t1", "cam", Credentials{}); !errors.Is(err, errCredentialKey) {
		t.Fatalf("missing key must be rejected, got %v", err)
	}
	if _, err := (sealer{key: []byte(strings.Repeat("x", 32)), keyID: "k2"}).open(sealed); err == nil {
		t.Fatal("a different key must not open the credential")
	}
}

func TestTargetGuardRejectsNonCameraTargets(t *testing.T) {
	g := testGuard(t, []string{"10.0.0.0/8", "169.254.0.0/16"}, 554)
	g.resolver = fakeResolver{
		"cam.local":    {netip.MustParseAddr("10.1.2.3")},
		"rebind.local": {netip.MustParseAddr("10.1.2.3"), netip.MustParseAddr("127.0.0.1")},
		"meta.local":   {netip.MustParseAddr("169.254.169.254")},
	}
	ctx := context.Background()
	cases := map[string]bool{
		"rtsp://10.1.2.3:554/x":           true,
		"rtsp://cam.local:554/x":          true,
		"rtsp://127.0.0.1:554/x":          false, // not in the configured networks
		"rtsp://169.254.169.254:554/x":    false, // metadata stays denied even inside a configured CIDR
		"rtsp://meta.local:554/x":         false, // DNS name resolving to metadata
		"rtsp://rebind.local:554/x":       false, // any disallowed address rejects the name
		"rtsp://10.1.2.3:8080/x":          false, // port not allowed
		"rtsp://[::ffff:127.0.0.1]:554/x": false,
		"http://10.1.2.3:554/x":           false,
		"rtsp://admin:pw@10.1.2.3:554/x":  false, // credentials must not be embedded
	}
	for raw, allowed := range cases {
		pinned, err := g.pinnedRTSP(ctx, raw)
		if (err == nil) != allowed {
			t.Errorf("%s: allowed=%v err=%v", raw, allowed, err)
		}
		if err == nil && strings.Contains(pinned.Host, "cam.local") {
			t.Errorf("%s: host must be pinned to the validated IP, got %s", raw, pinned.Host)
		}
	}
}

func TestBrandTemplatesAndNVRChannels(t *testing.T) {
	cases := []struct {
		brand   string
		channel int
		sub     bool
		want    string
	}{
		{"hikvision", 1, false, "rtsp://10.0.0.5:554/Streaming/Channels/101"},
		{"hikvision", 12, true, "rtsp://10.0.0.5:554/Streaming/Channels/1202"},
		{"dahua", 3, true, "rtsp://10.0.0.5:554/cam/realmonitor?channel=3&subtype=1"},
		{"uniview", 2, false, "rtsp://10.0.0.5:554/unicast/c2/s0/live"},
	}
	for _, c := range cases {
		got, err := buildTemplateURL(c.brand, "10.0.0.5", 0, c.channel, c.sub)
		if err != nil || got != c.want {
			t.Errorf("%s ch%d sub=%v: got %q %v", c.brand, c.channel, c.sub, got, err)
		}
	}
	if _, err := buildTemplateURL("generic", "10.0.0.5", 554, 1, false); err == nil {
		t.Fatal("generic RTSP has no structured template")
	}
	if _, err := buildTemplateURL("hikvision", "user@10.0.0.5", 554, 1, false); err == nil {
		t.Fatal("host with userinfo must be rejected")
	}
}

// fakeRTSP answers DESCRIBE with Digest authentication.
func fakeRTSP(t *testing.T, user, password, path, sdp string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					parts := strings.Fields(line)
					headers := map[string]string{}
					for {
						h, _ := r.ReadString('\n')
						h = strings.TrimSpace(h)
						if h == "" {
							break
						}
						k, v, _ := strings.Cut(h, ":")
						headers[strings.ToLower(k)] = strings.TrimSpace(v)
					}
					cseq := headers["cseq"]
					auth := headers["authorization"]
					ok := false
					if strings.HasPrefix(auth, "Digest ") {
						v := parseAuthParams(strings.TrimPrefix(auth, "Digest "))
						ha1 := md5hex(v["username"] + ":fake:" + password)
						ok = v["username"] == user && v["response"] == md5hex(ha1+":n1:"+md5hex("DESCRIBE:"+v["uri"]))
					}
					switch {
					case !ok:
						fmt.Fprintf(c, "RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\nWWW-Authenticate: Basic realm=\"fake\"\r\nWWW-Authenticate: Digest realm=\"fake\", nonce=\"n1\"\r\n\r\n", cseq)
					case !strings.HasSuffix(parts[1], path):
						fmt.Fprintf(c, "RTSP/1.0 404 Not Found\r\nCSeq: %s\r\n\r\n", cseq)
					default:
						fmt.Fprintf(c, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", cseq, len(sdp), sdp)
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestDescribeClassifiesFailures(t *testing.T) {
	sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 packetization-mode=1;profile-level-id=42e01f\r\nm=audio 0 RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n"
	port := fakeRTSP(t, "admin", "p@ss:w/rd#1%", "/Streaming/Channels/101", sdp)
	g := testGuard(t, []string{"127.0.0.0/8"}, port)
	base := "rtsp://127.0.0.1:" + strconv.Itoa(port)
	ctx := context.Background()
	info, err := g.describe(ctx, base+"/Streaming/Channels/101", Credentials{Username: "admin", Password: "p@ss:w/rd#1%"}, 3*time.Second)
	if err != nil || info.VideoCodec != "H264" || info.AudioCodec != "PCMA" || info.H264Profile != "Constrained Baseline" {
		t.Fatalf("success case: %+v %v", info, err)
	}
	status := func(err error) string {
		var pe *probeError
		if errors.As(err, &pe) {
			return pe.Status
		}
		return fmt.Sprint(err)
	}
	if _, err := g.describe(ctx, base+"/Streaming/Channels/101", Credentials{Username: "admin", Password: "wrong"}, 3*time.Second); status(err) != StatusAuthFailed {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := g.describe(ctx, base+"/Streaming/Channels/701", Credentials{Username: "admin", Password: "p@ss:w/rd#1%"}, 3*time.Second); status(err) != StatusStreamNotFound {
		t.Fatalf("missing channel: %v", err)
	}
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	g.ports[closedPort] = true
	if _, err := g.describe(ctx, "rtsp://127.0.0.1:"+strconv.Itoa(closedPort)+"/x", Credentials{}, 2*time.Second); status(err) != StatusUnreachable {
		t.Fatalf("closed port: %v", err)
	}
	if _, err := testGuard(t, []string{"10.0.0.0/8"}, port).describe(ctx, base+"/x", Credentials{}, time.Second); status(err) != StatusTargetDenied {
		t.Fatalf("denied target: %v", err)
	}
}

func TestONVIFProfilesValidateDigestAndStreamURI(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		n, _ := r.Body.Read(buf)
		body := string(buf[:n])
		w.Header().Set("Content-Type", "application/soap+xml")
		if strings.Contains(body, "GetSystemDateAndTime") {
			now := time.Now().UTC()
			fmt.Fprintf(w, `<Envelope><Body><GetSystemDateAndTimeResponse><SystemDateAndTime><UTCDateTime><Date><Year>%d</Year><Month>%d</Month><Day>%d</Day></Date><Time><Hour>%d</Hour><Minute>%d</Minute><Second>%d</Second></Time></UTCDateTime></SystemDateAndTime></GetSystemDateAndTimeResponse></Body></Envelope>`, now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second())
			return
		}
		// Verify the WS-Security PasswordDigest for the camera password.
		nonce := between(body, `#Base64Binary">`, "</")
		created := between(body, "<wsu:Created>", "</")
		digest := between(body, `#PasswordDigest">`, "</")
		raw, _ := base64.StdEncoding.DecodeString(nonce)
		h := sha1.New()
		h.Write(raw)
		h.Write([]byte(created))
		h.Write([]byte("s3cret&<pw>"))
		if base64.StdEncoding.EncodeToString(h.Sum(nil)) != digest {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<Envelope><Body><Fault><Code><Subcode><Value>ter:NotAuthorized</Value></Subcode></Code><Reason><Text>Sender not authorized</Text></Reason></Fault></Body></Envelope>`)
			return
		}
		switch {
		case strings.Contains(body, "GetCapabilities"):
			fmt.Fprintf(w, `<Envelope><Body><GetCapabilitiesResponse><Capabilities><Media><XAddr>%s/onvif/media</XAddr></Media></Capabilities></GetCapabilitiesResponse></Body></Envelope>`, server.URL)
		case strings.Contains(body, "GetProfiles"):
			fmt.Fprint(w, `<Envelope><Body><GetProfilesResponse><Profiles token="main"><Name>mainStream</Name><VideoEncoderConfiguration><Encoding>H264</Encoding><Resolution><Width>1920</Width><Height>1080</Height></Resolution></VideoEncoderConfiguration></Profiles><Profiles token="evil"><Name>evil</Name></Profiles></GetProfilesResponse></Body></Envelope>`)
		case strings.Contains(body, ">main<"):
			fmt.Fprint(w, `<Envelope><Body><GetStreamUriResponse><MediaUri><Uri>rtsp://admin:leak@127.0.0.1:554/Streaming/Channels/101</Uri></MediaUri></GetStreamUriResponse></Body></Envelope>`)
		default:
			// A device returning an address outside the camera networks.
			fmt.Fprint(w, `<Envelope><Body><GetStreamUriResponse><MediaUri><Uri>rtsp://169.254.169.254:554/latest</Uri></MediaUri></GetStreamUriResponse></Body></Envelope>`)
		}
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	g := testGuard(t, []string{"127.0.0.0/8"}, port, 554)
	profiles, err := g.newONVIF("127.0.0.1", port, Credentials{Username: "admin", Password: "s3cret&<pw>"}, 3*time.Second).Profiles(context.Background())
	if err != nil || len(profiles) != 2 {
		t.Fatalf("profiles: %+v %v", profiles, err)
	}
	if profiles[0].StreamURI != "rtsp://127.0.0.1:554/Streaming/Channels/101" || profiles[0].Width != 1920 {
		t.Fatalf("main profile must carry a credential-free, validated URI: %+v", profiles[0])
	}
	if profiles[1].StreamURI != "" || profiles[1].Error == "" {
		t.Fatalf("a stream URI outside the camera networks must be rejected: %+v", profiles[1])
	}
	_, err = g.newONVIF("127.0.0.1", port, Credentials{Username: "admin", Password: "bad"}, 3*time.Second).Profiles(context.Background())
	var pe *probeError
	if !errors.As(err, &pe) || pe.Status != StatusAuthFailed {
		t.Fatalf("wrong password must be an authentication failure, got %v", err)
	}
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	if j := strings.Index(s, end); j >= 0 {
		return s[:j]
	}
	return s
}

func TestChooseProfile(t *testing.T) {
	webrtcH265 := clientCaps{WebRTCH265: true}
	cases := []struct {
		name, mode, fixed, codec string
		bFrames                  bool
		protocol                 string
		caps                     clientCaps
		transcode                bool
		want                     string
		needs                    bool
	}{
		{"h264 direct", "off", "", "H264", false, "webrtc", clientCaps{}, true, "direct", false},
		{"h265 with browser support", "off", "", "H265", false, "webrtc", webrtcH265, false, "direct", false},
		{"h265 without support, transcode off", "off", "", "H265", false, "webrtc", clientCaps{}, true, "", true},
		{"h265 without support, auto", "auto", "h264_480p", "H265", false, "webrtc", clientCaps{}, true, "h264_480p", false},
		{"h265 hls never assumed", "auto", "", "H265", false, "hls", webrtcH265, true, "h264_720p", false},
		{"b-frames on webrtc", "auto", "", "H264", true, "webrtc", clientCaps{}, true, "h264_720p", false},
		{"b-frames fine on hls", "auto", "", "H264", true, "hls", clientCaps{}, true, "direct", false},
		{"auto but transcode unavailable", "auto", "", "H265", false, "webrtc", clientCaps{}, false, "", true},
		{"fixed audio only", "fixed", "audio_aac", "H264", false, "hls", clientCaps{}, true, "audio_aac", false},
		{"auto never picks audio-only", "auto", "audio_aac", "H265", false, "hls", clientCaps{}, true, "h264_720p", false},
	}
	for _, c := range cases {
		got, _, err := chooseProfile(c.mode, c.fixed, c.codec, c.bFrames, c.protocol, c.caps, c.transcode)
		if got != c.want || errors.Is(err, errNeedsTranscode) != c.needs {
			t.Errorf("%s: got %q err %v", c.name, got, err)
		}
	}
	if _, _, err := chooseProfile("fixed", "h264_720p", "H264", false, "webrtc", clientCaps{}, false); err == nil {
		t.Error("fixed output requires the transcode switch")
	}
}

func TestRedactText(t *testing.T) {
	in := `play rtsp://admin:p@ss@10.0.0.1:554/x failed; token=abc123&vt=deadbeef rtsp_pwd=hunter2`
	out := redactText(in)
	for _, leak := range []string{"admin:p@ss", "abc123", "deadbeef", "hunter2"} {
		if strings.Contains(out, leak) {
			t.Fatalf("redacted text still contains %q: %s", leak, out)
		}
	}
}

func TestTargetGuardDeniedNetworksWinOverAllowed(t *testing.T) {
	g := testGuard(t, []string{"172.16.0.0/12"}, 554)
	_, compose, _ := net.ParseCIDR("172.18.0.0/16")
	g.denied = []*net.IPNet{compose}
	if _, err := g.pinnedRTSP(context.Background(), "rtsp://172.18.0.5:554/x"); err == nil {
		t.Fatal("an address in IOT_VIDEO_DENIED_CIDRS was reachable")
	}
	if _, err := g.pinnedRTSP(context.Background(), "rtsp://172.20.0.5:554/x"); err != nil {
		t.Fatalf("other allowed cameras must stay reachable: %v", err)
	}
}
