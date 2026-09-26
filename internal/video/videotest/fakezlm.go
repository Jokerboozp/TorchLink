// Package videotest provides a fake ZLMediaKit HTTP API for tests. It models
// only the calls the live module makes and records them for assertions.
package videotest

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Proxy struct {
	URL, User, Password string
}

type FFmpeg struct {
	Key, CmdKey, Src, Dst, Cmd string
}

// Fake is an in-memory media server.
type Fake struct {
	*httptest.Server
	Secret string

	mu        sync.Mutex
	Proxies   map[string]Proxy  // app/stream
	FFmpegs   map[string]FFmpeg // key
	Media     map[string]string // app/stream -> video codec
	Calls     map[string]int    // api name -> count
	Deleted   []string          // delete_webrtc queries
	Codec     map[string]string // source URL -> codec (default H264)
	FailURL   map[string]bool   // source URLs that fail to pull
	Templates map[string]string // cmd key -> command (missing key uses the default command)
	Down      bool
	Delay     time.Duration
}

// New starts a fake media server with the platform's transcode templates.
func New(secret string) *Fake {
	f := &Fake{Secret: secret, Proxies: map[string]Proxy{}, FFmpegs: map[string]FFmpeg{}, Media: map[string]string{}, Calls: map[string]int{}, Codec: map[string]string{}, FailURL: map[string]bool{}, Templates: map[string]string{
		"ffmpeg.iot_h264_1080p": "%s -i %s -c:v libx264 -c:a aac -f rtsp %s",
		"ffmpeg.iot_h264_720p":  "%s -i %s -c:v libx264 -c:a aac -f rtsp %s",
		"ffmpeg.iot_h264_480p":  "%s -i %s -c:v libx264 -c:a aac -f rtsp %s",
		"ffmpeg.iot_audio_aac":  "%s -i %s -c:v copy -c:a aac -f rtsp %s",
	}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *Fake) Count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls[name]
}

// Proxy returns the pull proxy registered for app/stream.
func (f *Fake) Proxy(key string) (Proxy, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.Proxies[key]
	return p, ok
}

// DeletedPeers returns how many WebRTC peers were closed.
func (f *Fake) DeletedPeers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Deleted)
}

func (f *Fake) SetDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Down = down
}

// Restart simulates a media server restart: every object disappears.
func (f *Fake) Restart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Proxies, f.FFmpegs, f.Media = map[string]Proxy{}, map[string]FFmpeg{}, map[string]string{}
}

// AddOrphan adds a proxy the platform does not know about.
func (f *Fake) AddOrphan(app, stream string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Proxies[app+"/"+stream] = Proxy{URL: "rtsp://10.0.0.9/orphan"}
	f.Media[app+"/"+stream] = "H264"
}

func (f *Fake) Snapshot() (proxies, ffmpegs, media int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Proxies), len(f.FFmpegs), len(f.Media)
}

func (f *Fake) reply(w http.ResponseWriter, v map[string]any) {
	if _, ok := v["code"]; !ok {
		v["code"] = 0
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *Fake) handle(w http.ResponseWriter, r *http.Request) {
	if f.Delay > 0 {
		time.Sleep(f.Delay)
	}
	_ = r.ParseForm()
	name := strings.TrimPrefix(r.URL.Path, "/index/api/")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	f.Calls[name]++
	args := r.Form
	if name != "whep" && name != "delete_webrtc" && args.Get("secret") != f.Secret {
		f.reply(w, map[string]any{"code": -100, "msg": "secret error"})
		return
	}
	key := args.Get("app") + "/" + args.Get("stream")
	switch name {
	case "version":
		f.reply(w, map[string]any{"data": map[string]any{"branchName": "fake"}})
	case "addStreamProxy":
		if _, ok := f.Proxies[key]; ok {
			f.reply(w, map[string]any{"code": -1, "msg": "This stream already exists"})
			return
		}
		if f.FailURL[args.Get("url")] {
			f.reply(w, map[string]any{"code": -1, "msg": "play rtsp://" + args.Get("rtsp_user") + ":" + args.Get("rtsp_pwd") + "@camera failed"})
			return
		}
		f.Proxies[key] = Proxy{URL: args.Get("url"), User: args.Get("rtsp_user"), Password: args.Get("rtsp_pwd")}
		codec := f.Codec[args.Get("url")]
		if codec == "" {
			codec = "H264"
		}
		f.Media[key] = codec
		f.reply(w, map[string]any{"data": map[string]any{"key": "__defaultVhost__/" + key}})
	case "delStreamProxy":
		k := strings.TrimPrefix(args.Get("key"), "__defaultVhost__/")
		_, ok := f.Proxies[k]
		delete(f.Proxies, k)
		delete(f.Media, k)
		f.reply(w, map[string]any{"data": map[string]any{"flag": ok}})
	case "listStreamProxy":
		rows := []map[string]any{}
		for k := range f.Proxies {
			rows = append(rows, map[string]any{"key": "__defaultVhost__/" + k})
		}
		f.reply(w, map[string]any{"data": rows})
	case "addFFmpegSource":
		sum := md5.Sum([]byte(args.Get("dst_url")))
		k := hex.EncodeToString(sum[:])
		cmd, ok := f.Templates[args.Get("ffmpeg_cmd_key")]
		if !ok {
			cmd = "%s -re -i %s -c:a aac -c:v libx264 -f flv %s" // default fallback, like ZLMediaKit
			if strings.Contains(args.Get("ffmpeg_cmd_key"), "audio") {
				cmd = "%s -i %s -c copy -f flv %s"
			}
		}
		dst, _ := url.Parse(args.Get("dst_url"))
		f.FFmpegs[k] = FFmpeg{Key: k, CmdKey: args.Get("ffmpeg_cmd_key"), Src: args.Get("src_url"), Dst: args.Get("dst_url"), Cmd: fmt.Sprintf(cmd, "/usr/bin/ffmpeg", args.Get("src_url"), args.Get("dst_url"))}
		f.Media[strings.TrimPrefix(dst.Path, "/")] = "H264"
		f.reply(w, map[string]any{"data": map[string]any{"key": k}})
	case "delFFmpegSource":
		if s, ok := f.FFmpegs[args.Get("key")]; ok {
			dst, _ := url.Parse(s.Dst)
			delete(f.Media, strings.TrimPrefix(dst.Path, "/"))
		}
		delete(f.FFmpegs, args.Get("key"))
		f.reply(w, map[string]any{"data": map[string]any{"flag": true}})
	case "listFFmpegSource":
		rows := []map[string]any{}
		for _, s := range f.FFmpegs {
			rows = append(rows, map[string]any{"key": s.Key, "dst_url": s.Dst, "src_url": s.Src, "cmd": s.Cmd, "ffmpeg_cmd_key": s.CmdKey})
		}
		f.reply(w, map[string]any{"data": rows})
	case "getMediaList":
		codec, ok := f.Media[key]
		if !ok {
			f.reply(w, map[string]any{"data": []any{}})
			return
		}
		f.reply(w, map[string]any{"data": []any{map[string]any{"app": args.Get("app"), "stream": args.Get("stream"), "totalReaderCount": 1, "aliveSecond": 3, "tracks": []any{
			map[string]any{"codec_type": 0, "codec_id_name": codec, "ready": true, "width": 1280, "height": 720, "fps": 25.0},
			map[string]any{"codec_type": 1, "codec_id_name": "PCMA", "ready": true},
		}}}})
	case "close_streams":
		f.reply(w, map[string]any{"count_hit": 0, "count_closed": 0})
	case "whep":
		body, _ := io.ReadAll(r.Body)
		if _, ok := f.Media[r.URL.Query().Get("app")+"/"+r.URL.Query().Get("stream")]; !ok || !strings.HasPrefix(string(body), "v=0") {
			http.Error(w, "stream not found", http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Location", "http://media/index/api/delete_webrtc?id=peer-"+fmt.Sprint(f.Calls["whep"])+"&token=t")
		w.Header().Set("Content-Type", "application/sdp")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "v=0\r\no=- answer\r\n")
	case "delete_webrtc":
		f.Deleted = append(f.Deleted, r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
	default:
		f.reply(w, map[string]any{"code": -1, "msg": "unknown api " + name})
	}
}
