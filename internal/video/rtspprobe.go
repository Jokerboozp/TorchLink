package video

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Probe statuses. They are stable codes shown to the UI with Chinese text.
const (
	StatusTargetDenied      = "TARGET_DENIED"
	StatusUnreachable       = "UNREACHABLE"
	StatusAuthFailed        = "AUTH_FAILED"
	StatusStreamNotFound    = "STREAM_NOT_FOUND"
	StatusProtocolError     = "PROTOCOL_ERROR"
	StatusMediaUnavailable  = "MEDIA_UNAVAILABLE"
	StatusMediaFailed       = "MEDIA_PULL_FAILED"
	StatusCodecIncompatible = "CODEC_INCOMPATIBLE"
	StatusTranscodeRequired = "TRANSCODE_REQUIRED"
	StatusPlayable          = "PLAYABLE"
	StatusUnsupported       = "UNSUPPORTED"
)

// sdpInfo is what the camera advertises in its RTSP DESCRIBE answer.
type sdpInfo struct {
	VideoCodec  string
	H264Profile string
	AudioCodec  string
}

type probeError struct {
	Status string
	Detail string
}

func (e *probeError) Error() string { return e.Detail }

// describe performs an RTSP DESCRIBE (with Basic or Digest authentication) to
// classify reachability, authentication and stream existence before the media
// server is involved. A successful DESCRIBE only proves the camera answered;
// it is not evidence that video frames are playable.
func (g targetGuard) describe(ctx context.Context, rawURL string, cred Credentials, timeout time.Duration) (sdpInfo, error) {
	u, err := parseStreamURL(rawURL)
	if err != nil {
		return sdpInfo{}, &probeError{StatusProtocolError, err.Error()}
	}
	port := 554
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := g.dial(ctx, u.Hostname(), port, timeout)
	if err != nil {
		if errors.Is(err, ErrTargetDenied) {
			return sdpInfo{}, &probeError{StatusTargetDenied, err.Error()}
		}
		return sdpInfo{}, &probeError{StatusUnreachable, "摄像头地址不可达：" + sanitizeNetError(err)}
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	reader := bufio.NewReader(io.LimitReader(conn, 1<<20))
	target := u.String()
	cseq := 1
	status, headers, body, err := rtspRequest(conn, reader, "DESCRIBE", target, cseq, "")
	if err != nil {
		return sdpInfo{}, describeFailure(err)
	}
	if status == 401 {
		auth, authErr := authorization(headers["www-authenticate"], cred, "DESCRIBE", target)
		if authErr != nil {
			return sdpInfo{}, &probeError{StatusAuthFailed, authErr.Error()}
		}
		cseq++
		status, _, body, err = rtspRequest(conn, reader, "DESCRIBE", target, cseq, auth)
		if err != nil {
			return sdpInfo{}, describeFailure(err)
		}
	}
	switch {
	case status == 200:
		return parseSDP(body), nil
	case status == 401 || status == 403:
		return sdpInfo{}, &probeError{StatusAuthFailed, "用户名或密码错误，或账号没有取流权限"}
	case status == 404 || status == 454 || status == 400:
		return sdpInfo{}, &probeError{StatusStreamNotFound, fmt.Sprintf("通道或码流不存在（RTSP %d）", status)}
	default:
		return sdpInfo{}, &probeError{StatusProtocolError, fmt.Sprintf("摄像头返回 RTSP %d", status)}
	}
}

// describeFailure classifies a failure after the TCP connection succeeded.
func describeFailure(err error) *probeError {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return &probeError{StatusProtocolError, "端口可连接，但摄像头未在限定时间内应答 RTSP 请求（通道可能不存在或设备繁忙）"}
	}
	return &probeError{StatusProtocolError, "端口可连接，但对方未按 RTSP 协议应答：" + sanitizeNetError(err)}
}

func rtspRequest(conn net.Conn, reader *bufio.Reader, method, target string, cseq int, auth string) (int, map[string]string, string, error) {
	req := fmt.Sprintf("%s %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: TorchLink\r\nAccept: application/sdp\r\n", method, target, cseq)
	if auth != "" {
		req += "Authorization: " + auth + "\r\n"
	}
	if _, err := io.WriteString(conn, req+"\r\n"); err != nil {
		return 0, nil, "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, nil, "", err
	}
	parts := strings.Fields(line)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "RTSP/") {
		return 0, nil, "", errors.New("not an RTSP response")
	}
	status, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, nil, "", errors.New("invalid RTSP status")
	}
	headers := map[string]string{}
	for i := 0; i < 64; i++ {
		h, err := reader.ReadString('\n')
		if err != nil {
			return 0, nil, "", err
		}
		h = strings.TrimRight(h, "\r\n")
		if h == "" {
			break
		}
		if k, v, ok := strings.Cut(h, ":"); ok {
			name := strings.ToLower(strings.TrimSpace(k))
			value := strings.TrimSpace(v)
			// Prefer Digest when a camera offers several schemes.
			if name == "www-authenticate" && headers[name] != "" && !strings.HasPrefix(strings.ToLower(value), "digest") {
				continue
			}
			headers[name] = value
		}
	}
	length, _ := strconv.Atoi(headers["content-length"])
	if length < 0 || length > 64<<10 {
		return 0, nil, "", errors.New("RTSP body too large")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return 0, nil, "", err
	}
	return status, headers, string(body), nil
}

func authorization(challenge string, cred Credentials, method, uri string) (string, error) {
	if cred.Username == "" {
		return "", errors.New("摄像头要求认证，请填写用户名和密码")
	}
	scheme, params, _ := strings.Cut(strings.TrimSpace(challenge), " ")
	switch strings.ToLower(scheme) {
	case "basic":
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(cred.Username+":"+cred.Password)), nil
	case "digest":
		values := parseAuthParams(params)
		realm, nonce := values["realm"], values["nonce"]
		if nonce == "" {
			return "", errors.New("摄像头返回的 Digest 认证参数不完整")
		}
		ha1 := md5hex(cred.Username + ":" + realm + ":" + cred.Password)
		ha2 := md5hex(method + ":" + uri)
		// Most RTSP cameras use RFC 2069 Digest without qop; honour qop=auth
		// when offered (RFC 2617).
		response := md5hex(ha1 + ":" + nonce + ":" + ha2)
		qopPart := ""
		if qopAuth(values["qop"]) {
			cnonce := randomToken(8)
			response = md5hex(ha1 + ":" + nonce + ":00000001:" + cnonce + ":auth:" + ha2)
			qopPart = fmt.Sprintf(`, qop=auth, nc=00000001, cnonce="%s"`, cnonce)
		}
		out := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"%s`, quoteEscape(cred.Username), quoteEscape(realm), quoteEscape(nonce), quoteEscape(uri), response, qopPart)
		if opaque := values["opaque"]; opaque != "" {
			out += fmt.Sprintf(`, opaque="%s"`, quoteEscape(opaque))
		}
		return out, nil
	default:
		return "", errors.New("摄像头使用了不支持的 RTSP 认证方式")
	}
}

func parseAuthParams(s string) map[string]string {
	out := map[string]string{}
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ,")
		k, rest, ok := strings.Cut(s, "=")
		if !ok {
			break
		}
		k = strings.ToLower(strings.TrimSpace(k))
		rest = strings.TrimSpace(rest)
		var v string
		if strings.HasPrefix(rest, `"`) {
			end := strings.Index(rest[1:], `"`)
			if end < 0 {
				break
			}
			v, s = rest[1:end+1], rest[end+2:]
		} else {
			v, s, _ = strings.Cut(rest, ",")
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}

func qopAuth(v string) bool {
	for _, item := range strings.Split(v, ",") {
		if strings.TrimSpace(item) == "auth" {
			return true
		}
	}
	return false
}

func md5hex(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }

func quoteEscape(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }

// parseSDP reads codec names from rtpmap lines of the first video and audio
// media sections.
func parseSDP(body string) sdpInfo {
	var info sdpInfo
	section := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "m=video"):
			section = "video"
		case strings.HasPrefix(line, "m=audio"):
			section = "audio"
		case strings.HasPrefix(line, "m="):
			section = ""
		case strings.HasPrefix(line, "a=rtpmap:"):
			_, enc, ok := strings.Cut(line, " ")
			if !ok {
				continue
			}
			codec := strings.ToUpper(strings.SplitN(enc, "/", 2)[0])
			if section == "video" && info.VideoCodec == "" {
				info.VideoCodec = normalizeCodec(codec)
			} else if section == "audio" && info.AudioCodec == "" {
				info.AudioCodec = normalizeCodec(codec)
			}
		case strings.HasPrefix(line, "a=fmtp:") && section == "video" && info.H264Profile == "":
			if i := strings.Index(strings.ToLower(line), "profile-level-id="); i >= 0 {
				value := line[i+len("profile-level-id="):]
				if j := strings.IndexAny(value, "; "); j >= 0 {
					value = value[:j]
				}
				info.H264Profile = h264ProfileName(value)
			}
		}
	}
	return info
}

func normalizeCodec(codec string) string {
	switch codec {
	case "H264", "AVC":
		return "H264"
	case "H265", "HEVC":
		return "H265"
	case "MPEG4-GENERIC", "MP4A-LATM", "AAC":
		return "AAC"
	}
	return codec
}

func h264ProfileName(hexID string) string {
	if len(hexID) < 4 {
		return ""
	}
	idc, err := strconv.ParseUint(hexID[:2], 16, 8)
	if err != nil {
		return ""
	}
	constraint, _ := strconv.ParseUint(hexID[2:4], 16, 8)
	switch idc {
	case 66:
		if constraint&0x40 != 0 {
			return "Constrained Baseline"
		}
		return "Baseline"
	case 77:
		return "Main"
	case 100:
		return "High"
	case 110, 122, 244:
		return "High (10-bit / 4:2:2 / 4:4:4)"
	}
	return "profile " + strconv.FormatUint(idc, 10)
}

// sanitizeNetError drops URLs (which may carry credentials) from errors.
func sanitizeNetError(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return "连接超时"
		}
		if strings.Contains(opErr.Error(), "refused") {
			return "端口拒绝连接"
		}
		return "网络错误"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) {
		return "连接超时或被关闭"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return "请求失败"
	}
	return "通信失败"
}
