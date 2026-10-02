package externaldata

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type FetchResult struct {
	Body       []byte
	Items      []json.RawMessage
	NextCursor string
	Done       bool
}

type cachedToken struct {
	value   string
	expires time.Time
}

// Client shares token caches but never cookies, credentials, or redirect authority
// across connectors. Only explicitly allowed target host:port pairs are called.
type Client struct {
	transport http.RoundTripper
	mu        sync.Mutex
	tokens    map[string]cachedToken
}

func NewHTTPClient() *Client {
	return &Client{transport: http.DefaultTransport.(*http.Transport).Clone(), tokens: make(map[string]cachedToken)}
}

func renderString(s string, vars map[string]string) string {
	for _, key := range []string{"from", "to", "page", "cursor", "pageSize", "secret"} {
		if val, ok := vars[key]; ok {
			s = strings.ReplaceAll(s, "{{"+key+"}}", val)
		}
	}
	return s
}

func renderValue(value any, vars map[string]string) any {
	switch v := value.(type) {
	case string:
		for _, key := range []string{"from", "to", "page", "pageSize"} {
			if v == "{{"+key+"}}" {
				if val, ok := vars[key]; ok {
					return json.Number(val)
				}
			}
		}
		return renderString(v, vars)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = renderValue(child, vars)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = renderValue(child, vars)
		}
		return out
	default:
		return value
	}
}

func usesTemplate(value any, name string) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, "{{"+name+"}}")
	case map[string]string:
		for _, child := range v {
			if usesTemplate(child, name) {
				return true
			}
		}
	case map[string]any:
		for _, child := range v {
			if usesTemplate(child, name) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if usesTemplate(child, name) {
				return true
			}
		}
	}
	return false
}

func (c *Client) do(ctx context.Context, rawURL, method string, body []byte, headers http.Header, hosts []string) ([]byte, int, error) {
	u, err := allowedURL(rawURL, hosts)
	if err != nil {
		return nil, 0, err
	}
	if len(body) > MaxBodyBytes {
		return nil, 0, errors.New("请求正文超过 1 MiB 限制")
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, errors.New("无法创建外部接口请求")
	}
	req.Header = headers.Clone()
	if gate, ok := ctx.Value(requestGateKey{}).(requestGate); ok {
		err = gate.Acquire(ctx)
		var rate *RateLimitError
		if gate.WaitBriefly && errors.As(err, &rate) {
			delay := time.Until(time.UnixMilli(rate.RetryAt))
			if delay > 0 && delay <= 2*time.Second {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, 0, errors.New("外部接口请求超时或已取消")
				case <-timer.C:
				}
				err = gate.Acquire(ctx)
			}
		}
		if err != nil {
			return nil, 0, err
		}
	}
	client := http.Client{Transport: c.transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("重定向次数过多")
		}
		if _, err := allowedURL(next.URL.String(), hosts); err != nil {
			return err
		}
		if next.URL.Scheme != u.Scheme || hostPort(next.URL) != hostPort(u) {
			return errors.New("禁止跨来源重定向")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, errors.New("外部接口请求超时或已取消")
		}
		return nil, 0, errors.New("外部接口请求失败，请检查网络、证书及重定向设置")
	}
	defer resp.Body.Close()
	var responseLimit error
	if resp.StatusCode == http.StatusTooManyRequests {
		until := retryAfterDeadline(resp.Header.Get("Retry-After"), time.Now())
		responseLimit = &RateLimitError{RetryAt: until, Remote: true}
		if gate, ok := ctx.Value(requestGateKey{}).(requestGate); ok {
			if err = gate.Cooldown(ctx, until); err != nil {
				responseLimit = errors.New("保存外部接口限流状态失败")
			}
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if responseLimit != nil {
		return data[:min(len(data), MaxBodyBytes)], resp.StatusCode, responseLimit
	}
	if err != nil {
		return data, resp.StatusCode, errors.New("读取外部接口响应失败")
	}
	if len(data) > MaxBodyBytes {
		return data[:MaxBodyBytes], resp.StatusCode, errors.New("外部接口响应超过 1 MiB 限制（原文仅保留前 1 MiB）")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, resp.StatusCode, fmt.Errorf("外部接口返回 HTTP %d", resp.StatusCode)
	}
	return data, resp.StatusCode, nil
}

func authCacheKey(auth Auth, hosts []string) string {
	encoded, _ := json.Marshal(struct {
		Auth  Auth
		Hosts []string
	}{auth, hosts})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (c *Client) token(ctx context.Context, a Auth, hosts []string, force bool) (string, error) {
	key := authCacheKey(a, hosts)
	c.mu.Lock()
	cached, ok := c.tokens[key]
	c.mu.Unlock()
	if !force && ok && time.Now().Before(cached.expires) {
		return cached.value, nil
	}
	if _, err := allowedURL(a.TokenURL, hosts); err != nil {
		return "", err
	}
	body, err := json.Marshal(renderValue(a.TokenBody, map[string]string{"secret": a.Secret}))
	if err != nil {
		return "", errors.New("令牌请求正文无效")
	}
	data, _, err := c.do(ctx, a.TokenURL, http.MethodPost, body, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}, hosts)
	if err != nil {
		return "", fmt.Errorf("获取访问令牌失败：%w", err)
	}
	root, err := decodeJSON(data)
	if err != nil {
		return "", errors.New("访问令牌响应必须为 JSON")
	}
	value, ok := lookup(root, a.TokenPath)
	if !ok {
		return "", errors.New("访问令牌响应缺少令牌字段")
	}
	token, err := valueString(value)
	if err != nil || strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("访问令牌响应字段无效")
	}
	lifetime := time.Duration(5) * time.Minute
	if a.TokenExpiresPath != "" {
		value, ok := lookup(root, a.TokenExpiresPath)
		if !ok {
			return "", errors.New("访问令牌响应缺少有效期字段")
		}
		seconds, err := numberValue(value)
		if err != nil || seconds <= 0 || seconds > 86400*365 {
			return "", errors.New("访问令牌有效期必须为正数秒数，且不超过一年")
		}
		lifetime = time.Duration(seconds * float64(time.Second))
	}
	// Refresh before expiry, while retaining short-lived tokens for most of their lifetime.
	margin := lifetime / 10
	if margin > 30*time.Second {
		margin = 30 * time.Second
	}
	c.mu.Lock()
	if c.tokens == nil {
		c.tokens = make(map[string]cachedToken)
	}
	c.tokens[key] = cachedToken{token, time.Now().Add(lifetime - margin)}
	c.mu.Unlock()
	return token, nil
}

func (c *Client) authenticate(ctx context.Context, a Auth, u *url.URL, body []byte, headers http.Header, hosts []string, refresh bool) error {
	if err := validateAuth(a); err != nil {
		return err
	}
	if a.Type != "" && a.Type != "none" && a.Secret == "" {
		return errors.New("外部接口认证凭据未配置")
	}
	switch a.Type {
	case "", "none":
	case "bearer":
		headers.Set("Authorization", "Bearer "+a.Secret)
	case "api_key":
		if a.Query != "" {
			q := u.Query()
			q.Set(a.Query, a.Secret)
			u.RawQuery = q.Encode()
		} else {
			headers.Set(a.Header, a.Secret)
		}
	case "basic":
		req := &http.Request{Header: headers}
		req.SetBasicAuth(a.Username, a.Secret)
	case "hmac":
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		header := a.Header
		if header == "" {
			header = "X-Signature"
		}
		timestampHeader := a.TimestampHeader
		if timestampHeader == "" {
			timestampHeader = "X-Timestamp"
		}
		mac := hmac.New(sha256.New, []byte(a.Secret))
		_, _ = mac.Write([]byte(timestamp))
		_, _ = mac.Write(body)
		headers.Set(timestampHeader, timestamp)
		headers.Set(header, hex.EncodeToString(mac.Sum(nil)))
	case "token":
		token, err := c.token(ctx, a, hosts, refresh)
		if err != nil {
			return err
		}
		header := a.Header
		if header == "" {
			header = "Authorization"
		}
		if strings.EqualFold(header, "Authorization") {
			token = "Bearer " + token
		}
		headers.Set(header, token)
	}
	return nil
}

func paginationPosition(p Pagination, j Job) (int, int) {
	size := p.PageSize
	if size <= 0 {
		size = 100
	}
	position := j.Page
	if j.Pages == 0 {
		position = p.Start
		if p.Mode == "page" && position == 0 {
			position = 1
		}
	}
	return position, size
}

func (c *Client) Fetch(ctx context.Context, s Source, e Endpoint, j Job) (FetchResult, error) {
	if e.Mode != "pull" {
		return FetchResult{}, errors.New("仅拉取接口可执行请求")
	}
	if err := ValidateSource(s); err != nil {
		return FetchResult{}, err
	}
	if err := ValidateEndpoint(e); err != nil {
		return FetchResult{}, err
	}
	if j.Page < 0 || j.Pages < 0 || j.From < 0 || j.To < j.From {
		return FetchResult{}, errors.New("拉取任务位置或时间范围无效")
	}
	maxPages := e.Pagination.MaxPages
	if maxPages == 0 {
		maxPages = 1000
	}
	if j.Pages >= maxPages {
		return FetchResult{}, errors.New("拉取页数达到上限，请缩小时间范围")
	}
	timeout := e.TimeoutSeconds
	if timeout == 0 {
		timeout = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	u, err := allowedURL(e.URL, s.AllowedHosts)
	if err != nil {
		return FetchResult{}, err
	}
	position, size := paginationPosition(e.Pagination, j)
	vars := map[string]string{"from": strconv.FormatInt(j.From, 10), "to": strconv.FormatInt(j.To, 10), "page": strconv.Itoa(position), "cursor": j.Cursor, "pageSize": strconv.Itoa(size)}
	query := u.Query()
	for key, value := range e.Query {
		query.Set(key, renderString(value, vars))
	}
	p := e.Pagination
	if p.Mode != "" && p.Mode != "none" {
		positionTemplate := "page"
		if p.Mode == "cursor" {
			positionTemplate = "cursor"
		}
		positionExplicit := usesTemplate(e.Query, positionTemplate) || usesTemplate(e.RequestBody, positionTemplate)
		sizeExplicit := usesTemplate(e.Query, "pageSize") || usesTemplate(e.RequestBody, "pageSize")
		parameter := p.Parameter
		if parameter == "" {
			switch p.Mode {
			case "page":
				parameter = "page"
			case "offset":
				parameter = "offset"
			case "cursor":
				parameter = "cursor"
			}
		}
		value := strconv.Itoa(position)
		if p.Mode == "cursor" {
			value = j.Cursor
		}
		if p.Parameter != "" || !positionExplicit {
			query.Set(parameter, value)
		}
		sizeParameter := p.SizeParameter
		if sizeParameter == "" {
			sizeParameter = "pageSize"
		}
		if p.SizeParameter != "" || !sizeExplicit {
			query.Set(sizeParameter, strconv.Itoa(size))
		}
	}
	u.RawQuery = query.Encode()
	var body []byte
	if e.RequestBody != nil {
		body, err = json.Marshal(renderValue(e.RequestBody, vars))
		if err != nil {
			return FetchResult{}, errors.New("请求正文模板无效")
		}
	}
	method := e.Method
	if method == "" {
		method = http.MethodGet
	}
	headers := make(http.Header)
	for key, value := range e.Headers {
		headers.Set(key, value)
	}
	headers.Set("Accept", "application/json")
	if body != nil && headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	auth := s.Auth
	if e.Auth != nil {
		auth = *e.Auth
	}
	if err = c.authenticate(ctx, auth, u, body, headers, s.AllowedHosts, false); err != nil {
		return FetchResult{}, err
	}
	data, status, err := c.do(ctx, u.String(), method, body, headers, s.AllowedHosts)
	if status == http.StatusUnauthorized && auth.Type == "token" {
		c.mu.Lock()
		delete(c.tokens, authCacheKey(auth, s.AllowedHosts))
		c.mu.Unlock()
		if authErr := c.authenticate(ctx, auth, u, body, headers, s.AllowedHosts, true); authErr != nil {
			// Preserve only the data endpoint's response. The token endpoint's
			// response is deliberately confined to token() and never archived.
			return FetchResult{Body: data}, authErr
		}
		previousResponse := data
		var retryStatus int
		data, retryStatus, err = c.do(ctx, u.String(), method, body, headers, s.AllowedHosts)
		if err != nil && retryStatus == 0 {
			// A transport failure on the retry must not erase the original
			// business response that caused the refresh.
			data = previousResponse
		}
	}
	if err != nil {
		return FetchResult{Body: data}, err
	}
	items, err := Extract(e.Mapping, data)
	if err != nil {
		return FetchResult{Body: data}, err
	}
	result := FetchResult{Body: data, Items: items, Done: true}
	if p.Mode == "" || p.Mode == "none" {
		return result, nil
	}
	root, err := decodeJSON(data)
	if err != nil {
		return FetchResult{Body: data}, err
	}
	result.Done = len(items) < size
	if p.Mode == "cursor" {
		value, ok := lookup(root, p.NextPath)
		if ok && value != nil {
			result.NextCursor, err = valueString(value)
			if err != nil {
				return FetchResult{Body: data}, errors.New("下一游标必须为字符串或数字")
			}
		}
		result.Done = result.NextCursor == ""
		if !result.Done && (result.NextCursor == j.Cursor || len(items) == 0) {
			return FetchResult{Body: data}, errors.New("游标未推进或空页仍返回下一游标")
		}
	}
	if p.TotalPath != "" {
		value, ok := lookup(root, p.TotalPath)
		if !ok {
			return FetchResult{Body: data}, errors.New("响应缺少总数字段")
		}
		total, err := numberValue(value)
		if err != nil || total < 0 || total != float64(int64(total)) {
			return FetchResult{Body: data}, errors.New("响应总数必须为非负整数")
		}
		consumed := j.Received + len(items)
		result.Done = float64(consumed) >= total
		if !result.Done && len(items) == 0 {
			return FetchResult{Body: data}, errors.New("响应总数表明仍有数据，但当前页为空")
		}
		if p.Mode == "cursor" && !result.Done && result.NextCursor == "" {
			return FetchResult{Body: data}, errors.New("响应总数表明仍有数据，但缺少下一游标")
		}
	}
	if !result.Done && j.Pages+1 >= maxPages {
		return FetchResult{Body: data}, errors.New("拉取页数达到上限且仍有后续数据，请缩小时间范围")
	}
	return result, nil
}
