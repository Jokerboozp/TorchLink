package video

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ONVIFProfile is one media profile with its credential-free RTSP URI.
type ONVIFProfile struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	Encoding  string `json:"encoding,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	StreamURI string `json:"streamUri,omitempty"`
	Error     string `json:"error,omitempty"`
}

// onvifClient talks to one device address given by the administrator. It does
// not discover devices; every URL (including XAddrs and stream URIs that the
// device returns) passes the target guard before use.
type onvifClient struct {
	guard   targetGuard
	host    string
	port    int
	cred    Credentials
	offset  time.Duration
	http    *http.Client
	timeout time.Duration
}

func (g targetGuard) newONVIF(host string, port int, cred Credentials, timeout time.Duration) *onvifClient {
	if port == 0 {
		port = 80
	}
	c := &onvifClient{guard: g, host: host, port: port, cred: cred, timeout: timeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			h, p, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			portNumber, _ := strconv.Atoi(p)
			return g.dial(ctx, h, portNumber, timeout)
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: timeout,
	}
	c.http = &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("ONVIF redirects are not followed")
	}}
	return c
}

func (c *onvifClient) deviceURL() string {
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(strings.Trim(c.host, "[]"), strconv.Itoa(c.port)), Path: "/onvif/device_service"}).String()
}

// Profiles queries media profiles and, for each, the RTSP stream URI.
func (c *onvifClient) Profiles(ctx context.Context) ([]ONVIFProfile, error) {
	c.syncClock(ctx)
	var caps struct {
		Media string `xml:"Body>GetCapabilitiesResponse>Capabilities>Media>XAddr"`
	}
	if err := c.call(ctx, c.deviceURL(), `<tds:GetCapabilities xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Category>Media</tds:Category></tds:GetCapabilities>`, &caps); err != nil {
		return nil, err
	}
	mediaURL, err := c.checkedHTTPURL(ctx, strings.TrimSpace(caps.Media))
	if err != nil {
		return nil, err
	}
	var profiles struct {
		Items []struct {
			Token    string `xml:"token,attr"`
			Name     string `xml:"Name"`
			Encoding string `xml:"VideoEncoderConfiguration>Encoding"`
			Width    int    `xml:"VideoEncoderConfiguration>Resolution>Width"`
			Height   int    `xml:"VideoEncoderConfiguration>Resolution>Height"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	if err := c.call(ctx, mediaURL, `<trt:GetProfiles xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`, &profiles); err != nil {
		return nil, err
	}
	if len(profiles.Items) == 0 {
		return nil, &probeError{StatusStreamNotFound, "设备未返回媒体配置（Profile）"}
	}
	out := make([]ONVIFProfile, 0, len(profiles.Items))
	for i, p := range profiles.Items {
		if i >= 16 {
			break
		}
		item := ONVIFProfile{Token: p.Token, Name: p.Name, Encoding: strings.ToUpper(p.Encoding), Width: p.Width, Height: p.Height}
		uri, err := c.streamURI(ctx, mediaURL, p.Token)
		if err != nil {
			item.Error = err.Error()
		} else {
			item.StreamURI = uri
		}
		out = append(out, item)
	}
	return out, nil
}

// StreamURI returns the validated, credential-free RTSP URI for one profile.
func (c *onvifClient) StreamURI(ctx context.Context, token string) (string, error) {
	c.syncClock(ctx)
	var caps struct {
		Media string `xml:"Body>GetCapabilitiesResponse>Capabilities>Media>XAddr"`
	}
	if err := c.call(ctx, c.deviceURL(), `<tds:GetCapabilities xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Category>Media</tds:Category></tds:GetCapabilities>`, &caps); err != nil {
		return "", err
	}
	mediaURL, err := c.checkedHTTPURL(ctx, strings.TrimSpace(caps.Media))
	if err != nil {
		return "", err
	}
	return c.streamURI(ctx, mediaURL, token)
}

func (c *onvifClient) streamURI(ctx context.Context, mediaURL, token string) (string, error) {
	var resp struct {
		URI string `xml:"Body>GetStreamUriResponse>MediaUri>Uri"`
	}
	body := `<trt:GetStreamUri xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup><trt:ProfileToken>` + xmlEscape(token) + `</trt:ProfileToken></trt:GetStreamUri>`
	if err := c.call(ctx, mediaURL, body, &resp); err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimSpace(resp.URI))
	if err != nil || !strings.EqualFold(u.Scheme, "rtsp") {
		return "", errors.New("设备返回的不是 RTSP 地址")
	}
	// Devices sometimes embed credentials; they are never stored or echoed.
	u.User = nil
	clean := u.String()
	if _, err := c.guard.pinnedRTSP(ctx, clean); err != nil {
		return "", fmt.Errorf("设备返回的流地址未通过目标地址校验：%v", err)
	}
	return clean, nil
}

func (c *onvifClient) checkedHTTPURL(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", &probeError{StatusProtocolError, "设备未返回有效的媒体服务地址"}
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	if _, err := c.guard.resolve(ctx, u.Hostname(), port); err != nil {
		return "", &probeError{StatusTargetDenied, "设备返回的媒体服务地址未通过校验：" + err.Error()}
	}
	return u.String(), nil
}

func (c *onvifClient) syncClock(ctx context.Context) {
	var resp struct {
		Year   int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Date>Year"`
		Month  int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Date>Month"`
		Day    int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Date>Day"`
		Hour   int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Time>Hour"`
		Minute int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Time>Minute"`
		Second int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Time>Second"`
	}
	if err := c.post(ctx, c.deviceURL(), envelope("", `<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`), &resp, false); err != nil || resp.Year < 2000 {
		return
	}
	device := time.Date(resp.Year, time.Month(resp.Month), resp.Day, resp.Hour, resp.Minute, resp.Second, 0, time.UTC)
	c.offset = time.Until(device)
}

func (c *onvifClient) call(ctx context.Context, endpoint, body string, out any) error {
	return c.post(ctx, endpoint, envelope(c.security(), body), out, true)
}

func (c *onvifClient) post(ctx context.Context, endpoint, payload string, out any, allowDigest bool) error {
	send := func(auth string) (*http.Response, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8`)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return resp, data, err
	}
	resp, data, err := send("")
	if err != nil {
		if errors.Is(err, ErrTargetDenied) || strings.Contains(err.Error(), ErrTargetDenied.Error()) {
			return &probeError{StatusTargetDenied, "设备地址未通过目标地址校验"}
		}
		return &probeError{StatusUnreachable, "ONVIF 服务不可达：" + sanitizeNetError(err)}
	}
	// Some firmware requires HTTP Digest in addition to (or instead of) WS-Security.
	if resp.StatusCode == http.StatusUnauthorized && allowDigest && c.cred.Username != "" {
		if auth, authErr := httpDigest(resp.Header.Get("WWW-Authenticate"), c.cred, endpoint); authErr == nil {
			resp, data, err = send(auth)
			if err != nil {
				return &probeError{StatusUnreachable, "ONVIF 服务不可达：" + sanitizeNetError(err)}
			}
		}
	}
	var fault struct {
		Reason  string `xml:"Body>Fault>Reason>Text"`
		Subcode string `xml:"Body>Fault>Code>Subcode>Value"`
	}
	_ = xml.Unmarshal(data, &fault)
	lowerFault := strings.ToLower(fault.Subcode + " " + fault.Reason)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || strings.Contains(lowerFault, "notauthorized") || strings.Contains(lowerFault, "authority failure") || strings.Contains(lowerFault, "failedauthentication"):
		return &probeError{StatusAuthFailed, "ONVIF 认证失败：用户名、密码错误，或设备时间偏差过大"}
	case resp.StatusCode == http.StatusNotFound:
		return &probeError{StatusUnsupported, "该地址未提供 ONVIF 服务；仅支持厂商 App / 私有云的设备无法接入"}
	case fault.Reason != "" || fault.Subcode != "":
		return &probeError{StatusProtocolError, "ONVIF 请求被设备拒绝：" + truncate(fault.Reason, 120)}
	case resp.StatusCode != http.StatusOK:
		return &probeError{StatusProtocolError, fmt.Sprintf("ONVIF 服务返回 HTTP %d", resp.StatusCode)}
	}
	if err := xml.Unmarshal(data, out); err != nil {
		return &probeError{StatusProtocolError, "设备返回的 ONVIF 响应无法解析"}
	}
	return nil
}

// security builds a WS-Security UsernameToken with PasswordDigest as defined
// by the ONVIF Core Specification: Base64(SHA1(nonce + created + password)).
func (c *onvifClient) security() string {
	if c.cred.Username == "" {
		return ""
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	created := time.Now().Add(c.offset).UTC().Format("2006-01-02T15:04:05.000Z")
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(c.cred.Password))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return `<wsse:Security s:mustUnderstand="1" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"><wsse:UsernameToken><wsse:Username>` + xmlEscape(c.cred.Username) + `</wsse:Username><wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` + digest + `</wsse:Password><wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` + base64.StdEncoding.EncodeToString(nonce) + `</wsse:Nonce><wsu:Created>` + created + `</wsu:Created></wsse:UsernameToken></wsse:Security>`
}

func envelope(header, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Header>` + header + `</s:Header><s:Body>` + body + `</s:Body></s:Envelope>`
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func httpDigest(challenge string, cred Credentials, endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	scheme, params, _ := strings.Cut(strings.TrimSpace(challenge), " ")
	if !strings.EqualFold(scheme, "digest") {
		return "", errors.New("not digest")
	}
	values := parseAuthParams(params)
	uri := u.RequestURI()
	ha1 := md5hex(cred.Username + ":" + values["realm"] + ":" + cred.Password)
	ha2 := md5hex("POST:" + uri)
	if qopAuth(values["qop"]) {
		cnonce := randomToken(8)
		response := md5hex(ha1 + ":" + values["nonce"] + ":00000001:" + cnonce + ":auth:" + ha2)
		return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", qop=auth, nc=00000001, cnonce="%s", response="%s", opaque="%s"`, quoteEscape(cred.Username), quoteEscape(values["realm"]), quoteEscape(values["nonce"]), quoteEscape(uri), cnonce, response, quoteEscape(values["opaque"])), nil
	}
	response := md5hex(ha1 + ":" + values["nonce"] + ":" + ha2)
	return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`, quoteEscape(cred.Username), quoteEscape(values["realm"]), quoteEscape(values["nonce"]), quoteEscape(uri), response), nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
