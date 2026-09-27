package video

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// mediaServer is the part of ZLMediaKit's HTTP API the module uses. The
// secret and API address stay on the server; browsers never reach this API.
type mediaServer interface {
	Health(context.Context) error
	AddStreamProxy(ctx context.Context, app, stream, sourceURL string, cred Credentials, opt proxyOptions) error
	DelStreamProxy(ctx context.Context, app, stream string) error
	ListStreamProxies(ctx context.Context) ([]string, error)
	AddFFmpegSource(ctx context.Context, cmdKey, srcURL, dstURL string, timeout time.Duration) (string, error)
	DelFFmpegSource(ctx context.Context, key string) error
	ListFFmpegSources(ctx context.Context) ([]ffmpegSource, error)
	MediaInfo(ctx context.Context, app, stream string) (mediaInfo, bool, error)
	HLSReady(ctx context.Context, app, stream string) (bool, error)
	CloseStreams(ctx context.Context, app, stream string) error
	WHEP(ctx context.Context, app, stream, token, offer string) (answer, deleteQuery string, err error)
	DeleteWebRTC(ctx context.Context, deleteQuery string) error
	// GB28181 receivers: one RTP port per stream, filtered by SSRC.
	OpenRTPServer(ctx context.Context, app, stream string, tcp bool, ssrc string) (int, error)
	UpdateRTPServerSSRC(ctx context.Context, app, stream, ssrc string) error
	CloseRTPServer(ctx context.Context, app, stream string) error
	ListRTPServers(ctx context.Context) ([]string, error)
}

type proxyOptions struct {
	HLS        bool
	RTC        bool
	RetryCount int
	Timeout    time.Duration
}

type ffmpegSource struct {
	Key    string
	DstURL string
	Cmd    string
	CmdKey string
}

type mediaTrack struct {
	Video  bool
	Codec  string
	Ready  bool
	Width  int
	Height int
	FPS    float64
}

type mediaInfo struct {
	Tracks        []mediaTrack
	Readers       int
	AliveSeconds  int64
	OriginTypeStr string
}

func (m mediaInfo) video() (mediaTrack, bool) {
	for _, t := range m.Tracks {
		if t.Video {
			return t, true
		}
	}
	return mediaTrack{}, false
}

func (m mediaInfo) audio() (mediaTrack, bool) {
	for _, t := range m.Tracks {
		if !t.Video {
			return t, true
		}
	}
	return mediaTrack{}, false
}

type zlmClient struct {
	base   string
	secret string
	http   *http.Client
}

const zlmVhost = "__defaultVhost__"

func newZLM(base, secret string) *zlmClient {
	return &zlmClient{base: strings.TrimRight(base, "/"), secret: secret, http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("media API redirects are not followed")
	}}}
}

// errMediaAPI hides the media API response details, which may echo URLs.
type errMediaAPI struct {
	Code int
	Msg  string
}

func (e *errMediaAPI) Error() string {
	return fmt.Sprintf("媒体服务返回错误（code %d）：%s", e.Code, redactText(e.Msg))
}

func (z *zlmClient) call(ctx context.Context, path string, args url.Values, out any) error {
	return z.request(ctx, path, args, out, nil)
}

// request calls the API; out receives "data", top the whole reply (some
// APIs, such as openRtpServer, answer with top-level fields).
func (z *zlmClient) request(ctx context.Context, path string, args url.Values, out, top any) error {
	if args == nil {
		args = url.Values{}
	}
	args.Set("secret", z.secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, z.base+path, strings.NewReader(args.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := z.http.Do(req)
	if err != nil {
		return errors.New("媒体服务不可用")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return errors.New("媒体服务响应读取失败")
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &envelope) != nil {
		return fmt.Errorf("媒体服务响应异常（HTTP %d）", resp.StatusCode)
	}
	if envelope.Code != 0 {
		return &errMediaAPI{Code: envelope.Code, Msg: envelope.Msg}
	}
	if top != nil {
		if err := json.Unmarshal(body, top); err != nil {
			return err
		}
	}
	if out != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

func (z *zlmClient) Health(ctx context.Context) error {
	return z.call(ctx, "/index/api/version", nil, nil)
}

func (z *zlmClient) AddStreamProxy(ctx context.Context, app, stream, sourceURL string, cred Credentials, opt proxyOptions) error {
	args := url.Values{"vhost": {zlmVhost}, "app": {app}, "stream": {stream}, "url": {sourceURL}}
	// Credentials travel as separate player options rather than URL userinfo,
	// so special characters need no URL escaping and URLs stay credential-free.
	if cred.Username != "" {
		args.Set("rtsp_user", cred.Username)
		args.Set("rtsp_pwd", cred.Password)
	}
	args.Set("rtp_type", "0") // RTP over RTSP/TCP: robust through NAT and firewalls.
	args.Set("retry_count", strconv.Itoa(opt.RetryCount))
	args.Set("timeout_sec", strconv.Itoa(int(opt.Timeout.Seconds())))
	args.Set("enable_hls", boolArg(opt.HLS))
	args.Set("enable_rtsp", "1")
	args.Set("enable_rtmp", "0")
	args.Set("enable_ts", "0")
	args.Set("enable_fmp4", "0")
	args.Set("enable_mp4", "0")
	args.Set("enable_audio", "1")
	args.Set("add_mute_audio", "0")
	args.Set("auto_close", "0")
	err := z.call(ctx, "/index/api/addStreamProxy", args, nil)
	var apiErr *errMediaAPI
	if errors.As(err, &apiErr) && strings.Contains(apiErr.Msg, "already exists") {
		return nil
	}
	return err
}

func (z *zlmClient) DelStreamProxy(ctx context.Context, app, stream string) error {
	return z.call(ctx, "/index/api/delStreamProxy", url.Values{"key": {zlmVhost + "/" + app + "/" + stream}}, nil)
}

func (z *zlmClient) ListStreamProxies(ctx context.Context) ([]string, error) {
	var rows []struct {
		Key string `json:"key"`
	}
	if err := z.call(ctx, "/index/api/listStreamProxy", nil, &rows); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Key)
	}
	return out, nil
}

func (z *zlmClient) AddFFmpegSource(ctx context.Context, cmdKey, srcURL, dstURL string, timeout time.Duration) (string, error) {
	var data struct {
		Key string `json:"key"`
	}
	args := url.Values{"src_url": {srcURL}, "dst_url": {dstURL}, "timeout_ms": {strconv.FormatInt(timeout.Milliseconds(), 10)}, "enable_hls": {"0"}, "enable_mp4": {"0"}, "ffmpeg_cmd_key": {cmdKey}}
	err := z.call(ctx, "/index/api/addFFmpegSource", args, &data)
	return data.Key, err
}

func (z *zlmClient) DelFFmpegSource(ctx context.Context, key string) error {
	return z.call(ctx, "/index/api/delFFmpegSource", url.Values{"key": {key}}, nil)
}

func (z *zlmClient) ListFFmpegSources(ctx context.Context) ([]ffmpegSource, error) {
	var rows []struct {
		Key    string `json:"key"`
		DstURL string `json:"dst_url"`
		Cmd    string `json:"cmd"`
		CmdKey string `json:"ffmpeg_cmd_key"`
	}
	if err := z.call(ctx, "/index/api/listFFmpegSource", nil, &rows); err != nil {
		return nil, err
	}
	out := make([]ffmpegSource, 0, len(rows))
	for _, r := range rows {
		out = append(out, ffmpegSource{Key: r.Key, DstURL: r.DstURL, Cmd: r.Cmd, CmdKey: r.CmdKey})
	}
	return out, nil
}

func (z *zlmClient) MediaInfo(ctx context.Context, app, stream string) (mediaInfo, bool, error) {
	var rows []struct {
		Schema        string `json:"schema"`
		TotalReader   int    `json:"totalReaderCount"`
		AliveSecond   int64  `json:"aliveSecond"`
		OriginTypeStr string `json:"originTypeStr"`
		Tracks        []struct {
			CodecType int     `json:"codec_type"`
			CodecName string  `json:"codec_id_name"`
			Ready     bool    `json:"ready"`
			Width     int     `json:"width"`
			Height    int     `json:"height"`
			FPS       float64 `json:"fps"`
		} `json:"tracks"`
	}
	if err := z.call(ctx, "/index/api/getMediaList", url.Values{"vhost": {zlmVhost}, "app": {app}, "stream": {stream}, "schema": {"rtsp"}}, &rows); err != nil {
		return mediaInfo{}, false, err
	}
	if len(rows) == 0 {
		return mediaInfo{}, false, nil
	}
	row := rows[0]
	info := mediaInfo{Readers: row.TotalReader, AliveSeconds: row.AliveSecond, OriginTypeStr: row.OriginTypeStr}
	for _, t := range row.Tracks {
		info.Tracks = append(info.Tracks, mediaTrack{Video: t.CodecType == 0, Codec: normalizeCodec(strings.ToUpper(t.CodecName)), Ready: t.Ready, Width: t.Width, Height: t.Height, FPS: t.FPS})
	}
	return info, true, nil
}

// HLSReady reports whether the HLS output exists; it registers after the
// first segment is written, so a playlist request before that would 404.
func (z *zlmClient) HLSReady(ctx context.Context, app, stream string) (bool, error) {
	var rows []json.RawMessage
	if err := z.call(ctx, "/index/api/getMediaList", url.Values{"vhost": {zlmVhost}, "app": {app}, "stream": {stream}, "schema": {"hls"}}, &rows); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func (z *zlmClient) CloseStreams(ctx context.Context, app, stream string) error {
	args := url.Values{"vhost": {zlmVhost}, "app": {app}, "force": {"1"}}
	if stream != "" {
		args.Set("stream", stream)
	}
	return z.call(ctx, "/index/api/close_streams", args, nil)
}

// WHEP forwards the browser's SDP offer. Only SDP text crosses the API; media
// flows directly between the browser and the media server.
func (z *zlmClient) WHEP(ctx context.Context, app, stream, token, offer string) (string, string, error) {
	q := url.Values{"app": {app}, "stream": {stream}, "vt": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, z.base+"/index/api/whep?"+q.Encode(), strings.NewReader(offer))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/sdp")
	resp, err := z.http.Do(req)
	if err != nil {
		return "", "", errors.New("媒体服务不可用")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("WebRTC 协商失败（HTTP %d）：%s", resp.StatusCode, truncate(redactText(string(body)), 160))
	}
	deleteQuery := ""
	if loc, err := url.Parse(resp.Header.Get("Location")); err == nil {
		deleteQuery = loc.RawQuery
	}
	return string(body), deleteQuery, nil
}

func (z *zlmClient) DeleteWebRTC(ctx context.Context, deleteQuery string) error {
	if deleteQuery == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, z.base+"/index/api/delete_webrtc?"+deleteQuery, nil)
	if err != nil {
		return err
	}
	resp, err := z.http.Do(req)
	if err != nil {
		return errors.New("媒体服务不可用")
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("关闭 WebRTC 连接失败（HTTP %d）", resp.StatusCode)
}

// OpenRTPServer opens a GB28181 receiver for app/stream on a port from the
// configured range and returns that port. tcp selects TCP passive mode.
func (z *zlmClient) OpenRTPServer(ctx context.Context, app, stream string, tcp bool, ssrc string) (int, error) {
	var reply struct {
		Port int `json:"port"`
	}
	mode := "0"
	if tcp {
		mode = "1"
	}
	args := url.Values{"vhost": {zlmVhost}, "app": {app}, "stream_id": {stream}, "port": {"0"}, "tcp_mode": {mode}, "ssrc": {ssrc}}
	if err := z.request(ctx, "/index/api/openRtpServer", args, nil, &reply); err != nil {
		return 0, err
	}
	if reply.Port <= 0 {
		return 0, errors.New("媒体服务未分配 RTP 端口")
	}
	return reply.Port, nil
}

func (z *zlmClient) UpdateRTPServerSSRC(ctx context.Context, app, stream, ssrc string) error {
	return z.call(ctx, "/index/api/updateRtpServerSSRC", url.Values{"vhost": {zlmVhost}, "app": {app}, "stream_id": {stream}, "ssrc": {ssrc}}, nil)
}

func (z *zlmClient) CloseRTPServer(ctx context.Context, app, stream string) error {
	return z.call(ctx, "/index/api/closeRtpServer", url.Values{"vhost": {zlmVhost}, "app": {app}, "stream_id": {stream}}, nil)
}

// ListRTPServers returns open receivers as app/stream keys.
func (z *zlmClient) ListRTPServers(ctx context.Context) ([]string, error) {
	var rows []struct {
		App    string `json:"app"`
		Stream string `json:"stream_id"`
	}
	if err := z.call(ctx, "/index/api/listRtpServer", nil, &rows); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.App+"/"+r.Stream)
	}
	return out, nil
}

func boolArg(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// redactText removes URL userinfo and token query values from free text.
func redactText(s string) string {
	for _, marker := range []string{"rtsp://", "rtmp://", "http://", "https://"} {
		for start := 0; ; {
			i := strings.Index(s[start:], marker)
			if i < 0 {
				break
			}
			i += start + len(marker)
			end := strings.IndexAny(s[i:], " \t\r\n\"'<>")
			if end < 0 {
				end = len(s) - i
			}
			segment := s[i : i+end]
			// Passwords may contain '/', so any '@' in the URL marks userinfo.
			if at := strings.LastIndex(segment, "@"); at >= 0 {
				segment = "***@" + segment[at+1:]
			}
			s = s[:i] + segment + s[i+end:]
			start = i + len(segment)
		}
	}
	for _, key := range []string{"vt=", "token=", "secret=", "rtsp_pwd="} {
		for start := 0; ; {
			i := strings.Index(s[start:], key)
			if i < 0 {
				break
			}
			i += start + len(key)
			end := strings.IndexAny(s[i:], "& \t\r\n\"'")
			if end < 0 {
				end = len(s) - i
			}
			s = s[:i] + "***" + s[i+end:]
			start = i + 3
		}
	}
	return s
}
