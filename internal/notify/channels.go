package notify

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Cipher seals channel secrets with a key derived from the deployment
// secret; the additional data binds a sealed value to its tenant and channel.
type Cipher struct{ aead cipher.AEAD }

func NewCipher(secret string) (*Cipher, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("通知渠道凭据加密密钥未配置")
	}
	sum := sha256.Sum256([]byte("torchlink/notifications/channels/v1\x00" + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Seal(tenant, channel string, secret ChannelSecret) (string, error) {
	plain, err := json.Marshal(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plain, []byte(tenant+"\x00"+channel))
	return "v1:" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Open(tenant, channel, value string) (ChannelSecret, error) {
	var out ChannelSecret
	if value == "" {
		return out, nil
	}
	failed := errors.New("通知渠道凭据解密失败")
	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, "v1:"))
	if err != nil || !strings.HasPrefix(value, "v1:") || len(sealed) < c.aead.NonceSize() {
		return out, failed
	}
	plain, err := c.aead.Open(nil, sealed[:c.aead.NonceSize()], sealed[c.aead.NonceSize():], []byte(tenant+"\x00"+channel))
	if err != nil || json.Unmarshal(plain, &out) != nil {
		return out, failed
	}
	return out, nil
}

// Message is one rendered notification.
type Message struct {
	Title   string
	Text    string
	Link    string
	Emails  []string
	Mobiles []string
	// Event is the structured payload of signed webhooks.
	Event map[string]any
}

// officialHosts are the robot API hosts; other hosts must be allowed
// explicitly through IOT_NOTIFY_ALLOWED_CIDRS (for gateways and tests).
var officialHosts = map[string][]string{
	ChannelWeCom:    {"qyapi.weixin.qq.com"},
	ChannelDingTalk: {"oapi.dingtalk.com"},
	ChannelFeishu:   {"open.feishu.cn", "open.larksuite.com"},
}

// Sender delivers messages and guards outbound addresses: public addresses
// are allowed, private, loopback and link-local ones only inside Allowed.
type Sender struct {
	Allowed []*net.IPNet
	Timeout time.Duration
	client  *http.Client
}

func NewSender(allowed []*net.IPNet) *Sender {
	s := &Sender{Allowed: allowed, Timeout: 10 * time.Second}
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: s.control}
	s.client = &http.Client{
		Timeout:       s.Timeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return s
}

func (s *Sender) allowedIP(ip net.IP) bool {
	for _, n := range s.Allowed {
		if n.Contains(ip) {
			return true
		}
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast())
}

// control checks the address actually dialled, after DNS resolution, so a
// public name resolving to an internal address is refused.
func (s *Sender) control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !s.allowedIP(ip) {
		return fmt.Errorf("通知目标地址 %s 不在允许范围内", host)
	}
	return nil
}

// ValidateTarget checks a channel's URL or SMTP host before it is saved.
func (s *Sender) ValidateTarget(kind string, cfg ChannelConfig, secret ChannelSecret) error {
	if kind == ChannelSMTP {
		if cfg.Host == "" || cfg.Port <= 0 || cfg.Port > 65535 || cfg.From == "" {
			return errors.New("请填写 SMTP 服务器、端口与发件人")
		}
		if cfg.Security != "starttls" && cfg.Security != "tls" && cfg.Security != "none" {
			return errors.New("SMTP 加密方式须为 starttls、tls 或 none")
		}
		return nil
	}
	u, err := url.Parse(secret.URL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("请填写有效的 HTTP(S) 地址")
	}
	hosts, official := officialHosts[kind]
	if !official {
		return nil
	}
	for _, h := range hosts {
		if strings.EqualFold(u.Hostname(), h) {
			if u.Scheme != "https" {
				return errors.New("机器人地址须使用 HTTPS")
			}
			return nil
		}
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && len(s.Allowed) > 0 {
		for _, n := range s.Allowed {
			if n.Contains(ip) {
				return nil
			}
		}
	}
	return fmt.Errorf("机器人地址须为 %s", strings.Join(hosts, " 或 "))
}

// URLHint keeps only the scheme and host of a secret URL.
func URLHint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (s *Sender) Send(ctx context.Context, c Channel, secret ChannelSecret, m Message) error {
	switch c.Type {
	case ChannelSMTP:
		return s.sendSMTP(ctx, c.Config, secret, m)
	case ChannelWeCom:
		body := map[string]any{"msgtype": "text", "text": map[string]any{"content": m.Title + "\n" + m.Text, "mentioned_mobile_list": m.Mobiles}}
		return s.postRobot(ctx, secret.URL, body, "errcode")
	case ChannelDingTalk:
		target := secret.URL
		if secret.SignSecret != "" {
			ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
			mac := hmac.New(sha256.New, []byte(secret.SignSecret))
			mac.Write([]byte(ts + "\n" + secret.SignSecret))
			target += "&timestamp=" + ts + "&sign=" + url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		}
		text := m.Title + "\n" + m.Text
		for _, mobile := range m.Mobiles {
			text += " @" + mobile
		}
		body := map[string]any{"msgtype": "text", "text": map[string]any{"content": text}, "at": map[string]any{"atMobiles": m.Mobiles}}
		return s.postRobot(ctx, target, body, "errcode")
	case ChannelFeishu:
		body := map[string]any{"msg_type": "text", "content": map[string]any{"text": m.Title + "\n" + m.Text}}
		if secret.SignSecret != "" {
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			mac := hmac.New(sha256.New, []byte(ts+"\n"+secret.SignSecret))
			body["timestamp"], body["sign"] = ts, base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
		return s.postRobot(ctx, secret.URL, body, "code")
	case ChannelWebhook:
		return s.postWebhook(ctx, secret, m)
	}
	return fmt.Errorf("unsupported channel type %q", c.Type)
}

func (s *Sender) post(ctx context.Context, target string, body []byte, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, sanitize(err, target)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return out, fmt.Errorf("接收方返回 HTTP %d", resp.StatusCode)
	}
	return out, nil
}

// sanitize removes the URL, which carries the robot token, from errors.
func sanitize(err error, target string) error {
	text := strings.ReplaceAll(err.Error(), target, URLHint(target))
	if u, parseErr := url.Parse(target); parseErr == nil && u.RawQuery != "" {
		text = strings.ReplaceAll(text, u.RawQuery, "***")
	}
	return errors.New(text)
}

func (s *Sender) postRobot(ctx context.Context, target string, body map[string]any, codeField string) error {
	data, _ := json.Marshal(body)
	out, err := s.post(ctx, target, data, nil)
	if err != nil {
		return err
	}
	var result map[string]any
	if json.Unmarshal(out, &result) != nil {
		return nil
	}
	if code, ok := result[codeField].(float64); ok && code != 0 {
		msg, _ := result["errmsg"].(string)
		if msg == "" {
			msg, _ = result["msg"].(string)
		}
		return fmt.Errorf("机器人返回错误 %v：%s", code, msg)
	}
	return nil
}

// postWebhook signs the body: X-IoT-Signature = hex(HMAC-SHA256(secret,
// timestamp + "." + body)) with X-IoT-Timestamp in Unix seconds.
func (s *Sender) postWebhook(ctx context.Context, secret ChannelSecret, m Message) error {
	event := m.Event
	if event == nil {
		event = map[string]any{}
	}
	event["title"], event["text"], event["link"] = m.Title, m.Text, m.Link
	data, _ := json.Marshal(event)
	headers := map[string]string{}
	if secret.SignSecret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secret.SignSecret))
		mac.Write([]byte(ts + "." + string(data)))
		headers["X-IoT-Timestamp"], headers["X-IoT-Signature"] = ts, hex.EncodeToString(mac.Sum(nil))
	}
	_, err := s.post(ctx, secret.URL, data, headers)
	return err
}

func (s *Sender) sendSMTP(ctx context.Context, cfg ChannelConfig, secret ChannelSecret, m Message) error {
	if len(m.Emails) == 0 {
		return errors.New("没有可用的收件邮箱")
	}
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: s.control}
	deadline := time.Now().Add(s.Timeout)
	var conn net.Conn
	var err error
	if cfg.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	if cfg.Security == "starttls" {
		if err = client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if cfg.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", cfg.Username, secret.Password, cfg.Host)); err != nil {
			return fmt.Errorf("SMTP 认证失败：%w", err)
		}
	}
	if err = client.Mail(cfg.From); err != nil {
		return err
	}
	for _, to := range m.Emails {
		if err = client.Rcpt(to); err != nil {
			return fmt.Errorf("收件人 %s 被拒绝：%w", to, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	body := m.Text
	if m.Link != "" {
		body += "\n\n" + m.Link
	}
	msg := "From: " + cfg.From + "\r\nTo: " + strings.Join(m.Emails, ", ") + "\r\nSubject: " + mime.BEncoding.Encode("UTF-8", m.Title) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\n\r\n" +
		wrap(base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(body, "\n", "\r\n"))))
	if _, err = w.Write([]byte(msg)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func wrap(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s + "\r\n")
	return b.String()
}
