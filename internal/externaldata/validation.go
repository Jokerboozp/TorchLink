package externaldata

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func configInvalid(message string) error { return fmt.Errorf("%w：%s", ErrInvalid, message) }

func hostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func parseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, configInvalid("地址须为不含内嵌凭据的 HTTP/HTTPS URL")
	}
	port := u.Port()
	if port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return nil, configInvalid("地址端口无效")
		}
	}
	return u, nil
}

func validateConfiguredURL(raw string) error {
	u, err := parseHTTPURL(raw)
	if err != nil {
		return err
	}
	for key := range u.Query() {
		if sensitiveKey(key) {
			return configInvalid("URL 中的认证参数须改用认证配置，不能保存明文凭据")
		}
	}
	return nil
}

func allowedURL(raw string, hosts []string) (*url.URL, error) {
	u, err := parseHTTPURL(raw)
	if err != nil {
		return nil, err
	}
	target := hostPort(u)
	for _, h := range hosts {
		if strings.ToLower(strings.TrimSpace(h)) == target {
			return u, nil
		}
	}
	return nil, configInvalid("目标地址不在外部系统允许的主机及端口中")
}

func validHeader(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return true
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(key))
	return key == "authorization" || key == "cookie" || key == "setcookie" || key == "xapikey" || key == "apikey" || key == "password" || key == "passwd" || key == "secret" || key == "clientsecret" || key == "token" || key == "accesstoken"
}

func validateCredentialTemplates(value any) error {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if sensitiveKey(key) && child != "{{secret}}" {
				return configInvalid("令牌请求中的凭据必须使用 {{secret}} 引用")
			}
			if err := validateCredentialTemplates(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := validateCredentialTemplates(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOrdinaryRequestBody(value any) error {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if sensitiveKey(key) {
				return configInvalid("请求正文中的认证凭据须改用认证配置")
			}
			if err := validateOrdinaryRequestBody(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := validateOrdinaryRequestBody(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateAuth(a Auth) error {
	switch a.Type {
	case "", "none", "bearer", "api_key", "basic", "hmac", "token":
	default:
		return configInvalid("认证方式不支持")
	}
	if a.Header != "" && !validHeader(a.Header) {
		return configInvalid("认证请求头名称无效")
	}
	if a.TimestampHeader != "" && !validHeader(a.TimestampHeader) {
		return configInvalid("时间戳请求头名称无效")
	}
	if a.Type == "hmac" {
		signatureHeader := a.Header
		if signatureHeader == "" {
			signatureHeader = "X-Signature"
		}
		timestampHeader := a.TimestampHeader
		if timestampHeader == "" {
			timestampHeader = "X-Timestamp"
		}
		if strings.EqualFold(signatureHeader, timestampHeader) {
			return configInvalid("签名和时间戳必须使用不同请求头")
		}
	}
	if strings.ContainsAny(a.Secret, "\r\n") {
		return configInvalid("认证凭据不能包含换行")
	}
	if a.Type == "api_key" && ((a.Header == "") == (a.Query == "")) {
		return configInvalid("API Key 需要指定一个请求头或查询参数")
	}
	if a.Query != "" && (len(a.Query) > 128 || strings.ContainsAny(a.Query, "\r\n")) {
		return configInvalid("认证查询参数无效")
	}
	if a.Type == "basic" && (a.Username == "" || strings.ContainsAny(a.Username, ":\r\n")) {
		return configInvalid("Basic 认证用户名无效")
	}
	if a.Type == "token" {
		if err := validateCredentialTemplates(a.TokenBody); err != nil {
			return err
		}
		if err := validateConfiguredURL(a.TokenURL); err != nil {
			return err
		}
		if a.TokenPath == "" {
			return configInvalid("Token 认证需要令牌字段路径")
		}
		if _, err := pathParts(a.TokenPath); err != nil {
			return configInvalid("令牌字段路径无效")
		}
		if a.TokenExpiresPath != "" {
			if _, err := pathParts(a.TokenExpiresPath); err != nil {
				return configInvalid("令牌有效期路径无效")
			}
		}
		if body, err := json.Marshal(a.TokenBody); err != nil || len(body) > MaxBodyBytes {
			return configInvalid("令牌请求正文无效或过大")
		}
	}
	return nil
}

func ValidateSource(s Source) error {
	if s.RequestIntervalMillis != 0 && (s.RequestIntervalMillis < 100 || s.RequestIntervalMillis > 3600000) {
		return configInvalid("来源请求间隔须为 100 至 3600000 毫秒，0 使用默认 1000 毫秒")
	}
	if strings.TrimSpace(s.Name) == "" || len(s.Name) > 200 {
		return configInvalid("外部系统名称须为 1 至 200 字符")
	}
	if strings.TrimSpace(s.Username) == "" || len(s.Username) > 200 {
		return configInvalid("必须绑定有效的平台用户")
	}
	if len(s.AllowedHosts) > 100 {
		return configInvalid("允许的主机数量超过 100")
	}
	for _, host := range s.AllowedHosts {
		h, p, err := net.SplitHostPort(host)
		if err != nil || h == "" || strings.ContainsAny(h, "/*@?# \t\r\n") {
			return configInvalid("允许地址须精确填写主机:端口")
		}
		port, err := strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != p {
			return configInvalid("允许地址端口无效")
		}
	}
	if err := validateAuth(s.Auth); err != nil {
		return err
	}
	if s.Auth.Type == "token" {
		if _, err := allowedURL(s.Auth.TokenURL, s.AllowedHosts); err != nil {
			return err
		}
	}
	return nil
}

func ValidateEndpoint(e Endpoint) error {
	if strings.TrimSpace(e.Name) == "" || len(e.Name) > 200 {
		return configInvalid("接口名称须为 1 至 200 字符")
	}
	if e.SourceID == "" {
		return configInvalid("请选择所属外部系统")
	}
	if e.Mode != "push" && e.Mode != "pull" {
		return configInvalid("接入方式须为推送或拉取")
	}
	switch e.Kind {
	case "video_alarm", "alarm", "property", "state", "event":
	default:
		return configInvalid("数据用途不支持")
	}
	if e.Auth != nil {
		if err := validateAuth(*e.Auth); err != nil {
			return err
		}
	}
	if e.Mode == "pull" {
		if err := validateConfiguredURL(e.URL); err != nil {
			return err
		}
		switch e.Method {
		case "", http.MethodGet, http.MethodPost, http.MethodPut:
		default:
			return configInvalid("拉取请求方法仅支持 GET、POST、PUT")
		}
	}
	if e.IntervalSeconds != 0 && (e.IntervalSeconds < 10 || e.IntervalSeconds > 86400*30) {
		return configInvalid("自动拉取间隔须为 10 秒至 30 天，0 表示仅手动")
	}
	if e.StartAt < 0 || e.OverlapSeconds < 0 || e.OverlapSeconds > 86400*30 {
		return configInvalid("增量开始时间或重叠时间无效")
	}
	if e.TimeoutSeconds < 0 || e.TimeoutSeconds > 120 {
		return configInvalid("请求超时须为 1 至 120 秒，0 使用默认值")
	}
	if e.MaxAttempts < 0 || e.MaxAttempts > 20 {
		return configInvalid("最大重试次数须在 0 至 20 之间")
	}
	if e.ResponseStatus != 0 && (e.ResponseStatus < 200 || e.ResponseStatus > 299) {
		return configInvalid("成功响应状态须为 2xx")
	}
	if len(e.ResponseBody) > 64*1024 || len(e.ResponseBody) > 0 && !json.Valid(e.ResponseBody) {
		return configInvalid("成功响应正文须为不超过 64 KiB 的 JSON")
	}
	if len(e.Headers) > 100 || len(e.Query) > 100 {
		return configInvalid("请求头或查询参数数量超过 100")
	}
	for k, v := range e.Headers {
		if !validHeader(k) || strings.ContainsAny(v, "\r\n") || strings.EqualFold(k, "Host") {
			return configInvalid("请求头无效，Host 由目标地址决定")
		}
		if sensitiveKey(k) {
			return configInvalid("认证请求头须使用认证配置，不能保存在普通请求头中")
		}
	}
	for key := range e.Query {
		if sensitiveKey(key) {
			return configInvalid("查询参数中的认证凭据须改用认证配置")
		}
	}
	if err := validateOrdinaryRequestBody(e.RequestBody); err != nil {
		return err
	}
	if raw, err := json.Marshal(e.RequestBody); err != nil || len(raw) > MaxBodyBytes {
		return configInvalid("请求正文无效或超过 1 MiB")
	}
	p := e.Pagination
	switch p.Mode {
	case "", "none", "page", "offset", "cursor":
	default:
		return configInvalid("分页方式不支持")
	}
	if p.Start < 0 || p.PageSize < 0 || p.PageSize > MaxItems || p.MaxPages < 0 || p.MaxPages > 10000 {
		return configInvalid("分页起点、页大小或页数上限无效")
	}
	if p.Mode == "cursor" && p.NextPath == "" {
		return configInvalid("游标分页需要下一游标字段路径")
	}
	if p.Parameter != "" && p.Parameter == p.SizeParameter {
		return configInvalid("分页位置和页大小不能使用同一参数")
	}
	for _, path := range []string{p.NextPath, p.TotalPath, e.Mapping.ItemsPath, e.Mapping.SuccessPath} {
		if _, err := pathParts(path); err != nil {
			return configInvalid("响应字段路径无效")
		}
	}
	if len(e.Mapping.Fields) == 0 || len(e.Mapping.Fields) > 200 {
		return configInvalid("请配置 1 至 200 个字段对应关系")
	}
	if len(e.Mapping.Filters) > 50 || len(e.Mapping.IDFields) > 20 {
		return configInvalid("过滤条件或组合编号字段过多")
	}
	targets := map[string]bool{}
	hasID := len(e.Mapping.IDFields) > 0
	hasTime := false
	for _, f := range e.Mapping.Fields {
		switch f.Target {
		case "id":
			hasID = true
		case "timestamp":
			hasTime = true
		case "objectId", "version", "status", "alarmType", "alarmLevel", "content", "confidence", "snapshotUrl", "videoClipUrl", "online", "data":
		default:
			if !strings.HasPrefix(f.Target, "data.") || strings.HasSuffix(f.Target, ".") || strings.Contains(f.Target, "..") {
				return configInvalid("字段目标不支持")
			}
		}
		if targets[f.Target] {
			return configInvalid("目标字段不能重复")
		}
		for target := range targets {
			if strings.HasPrefix(target, f.Target+".") || strings.HasPrefix(f.Target, target+".") {
				return configInvalid("目标字段存在父子冲突")
			}
		}
		targets[f.Target] = true
		if !f.Constant && f.Path == "" {
			return configInvalid("字段对应需要来源路径或常量")
		}
		if _, err := pathParts(f.Path); err != nil {
			return configInvalid("来源字段路径无效")
		}
		switch f.Type {
		case "", "string", "number", "boolean", "timestamp", "json":
		default:
			return configInvalid("字段转换类型不支持")
		}
		if f.Timezone != "" {
			if _, err := time.LoadLocation(f.Timezone); err != nil {
				return configInvalid("字段时区无效")
			}
		}
	}
	if !hasID || !hasTime {
		return configInvalid("必须配置事件编号和事件时间")
	}
	for _, p := range e.Mapping.IDFields {
		if p == "" {
			return configInvalid("组合编号字段路径不能为空")
		}
		if _, err := pathParts(p); err != nil {
			return configInvalid("组合编号字段路径无效")
		}
	}
	for _, f := range e.Mapping.Filters {
		if f.Path == "" {
			return configInvalid("过滤字段路径不能为空")
		}
		if _, err := pathParts(f.Path); err != nil {
			return configInvalid("过滤字段路径无效")
		}
		switch f.Operator {
		case "", "eq", "ne", "neq", "exists", "not_exists", "contains":
		case "in", "not_in":
			if _, ok := f.Value.([]any); !ok {
				return configInvalid("in 过滤值必须为数组")
			}
		case "gt", "gte", "lt", "lte":
			if _, err := numberValue(f.Value); err != nil {
				return configInvalid("比较过滤值必须为数字")
			}
		default:
			return configInvalid("过滤操作符不支持")
		}
	}
	return nil
}
