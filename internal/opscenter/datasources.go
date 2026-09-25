package opscenter

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Data sources are managed in Grafana. The platform edits an allow-listed set
// of fields for Prometheus and Loki data sources; everything else in the
// stored jsonData is preserved. Secrets (basic auth password, custom header
// values) are write-only: responses only report whether they are set.

type DataSourceHeader struct {
	Name  string          `json:"name"`
	Value model.OpsSecret `json:"value"`
}

type DataSourceInput struct {
	Name              string             `json:"name"`
	Type              string             `json:"type"`
	URL               string             `json:"url"`
	IsDefault         bool               `json:"isDefault"`
	BasicAuth         bool               `json:"basicAuth"`
	BasicAuthUser     string             `json:"basicAuthUser"`
	BasicAuthPassword model.OpsSecret    `json:"basicAuthPassword"`
	Headers           []DataSourceHeader `json:"headers"`
	TimeInterval      string             `json:"timeInterval"`
	QueryTimeout      string             `json:"queryTimeout"`
	HTTPMethod        string             `json:"httpMethod"`
	MaxLines          int                `json:"maxLines"`
	TLSSkipVerify     bool               `json:"tlsSkipVerify"`
}

// DataSources lists data sources. Connection details are included only for
// callers allowed to manage data sources.
func (s *Service) DataSources(ctx context.Context, withDetails bool) ([]model.OpsDataSource, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return nil, err
	}
	items, err := s.Dashboards.DataSources(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if !withDetails {
			items[i] = model.OpsDataSource{UID: items[i].UID, Name: items[i].Name, Type: items[i].Type, IsDefault: items[i].IsDefault, ReadOnly: items[i].ReadOnly, Supported: items[i].Supported}
		} else {
			items[i].JSONData = publicJSONData(items[i].JSONData)
		}
	}
	return items, nil
}

func (s *Service) DataSource(ctx context.Context, uid string) (model.OpsDataSource, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return model.OpsDataSource{}, err
	}
	if !dashboardUIDPattern.MatchString(uid) {
		return model.OpsDataSource{}, ports.ErrOpsNotFound
	}
	ds, err := s.Dashboards.DataSource(ctx, uid)
	ds.JSONData = publicJSONData(ds.JSONData)
	return ds, err
}

var dataSourceJSONKeys = []string{"timeInterval", "queryTimeout", "httpMethod", "maxLines", "tlsSkipVerify", "manageAlerts", "prometheusType", "prometheusVersion", "cacheLevel", "incrementalQuerying", "disableRecordingRules", "derivedFields"}

func publicJSONData(data map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range dataSourceJSONKeys {
		if v, ok := data[key]; ok {
			out[key] = v
		}
	}
	for key, v := range data {
		if strings.HasPrefix(key, "httpHeaderName") {
			out[key] = v
		}
	}
	return out
}

func (s *Service) SaveDataSource(ctx context.Context, uid string, in DataSourceInput) (model.OpsDataSource, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return model.OpsDataSource{}, err
	}
	var existing model.OpsDataSource
	var existingJSON map[string]any
	if uid != "" {
		if !dashboardUIDPattern.MatchString(uid) {
			return model.OpsDataSource{}, ports.ErrOpsNotFound
		}
		current, err := s.Dashboards.DataSource(ctx, uid)
		if err != nil {
			return model.OpsDataSource{}, err
		}
		if current.ReadOnly {
			return model.OpsDataSource{}, fmt.Errorf("%w: 该数据源由部署配置（provisioning）管理，请修改部署文件", ports.ErrOpsReadOnly)
		}
		if !current.Supported {
			return model.OpsDataSource{}, invalid("type", "平台只能编辑 Prometheus 与 Loki 数据源")
		}
		existing, existingJSON = current, current.JSONData
		in.Type = current.Type
	}
	if in.Type != "prometheus" && in.Type != "loki" {
		return model.OpsDataSource{}, invalid("type", "平台只支持创建 Prometheus 与 Loki 数据源")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 100 {
		return model.OpsDataSource{}, invalid("name", "名称为 1～100 个字符")
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return model.OpsDataSource{}, invalid("url", "地址必须是不含账号密码的 http(s) URL")
	}
	jsonData := map[string]any{}
	for k, v := range existingJSON {
		jsonData[k] = v
	}
	for k := range jsonData {
		if strings.HasPrefix(k, "httpHeaderName") {
			delete(jsonData, k)
		}
	}
	for key, value := range map[string]string{"timeInterval": in.TimeInterval, "queryTimeout": in.QueryTimeout} {
		if value = strings.TrimSpace(value); value == "" {
			delete(jsonData, key)
		} else if _, ok := parseDuration(value); !ok {
			return model.OpsDataSource{}, invalid(key, "时间格式无效，例如 15s")
		} else {
			jsonData[key] = value
		}
	}
	if in.Type == "prometheus" {
		switch in.HTTPMethod {
		case "", "POST", "GET":
			if in.HTTPMethod != "" {
				jsonData["httpMethod"] = in.HTTPMethod
			}
		default:
			return model.OpsDataSource{}, invalid("httpMethod", "HTTP 方法只能是 GET 或 POST")
		}
	}
	if in.Type == "loki" {
		if in.MaxLines < 0 || in.MaxLines > 50000 {
			return model.OpsDataSource{}, invalid("maxLines", "最大行数范围 0～50000")
		}
		if in.MaxLines > 0 {
			jsonData["maxLines"] = strconv.Itoa(in.MaxLines)
		} else {
			delete(jsonData, "maxLines")
		}
	}
	if in.TLSSkipVerify {
		jsonData["tlsSkipVerify"] = true
	} else {
		delete(jsonData, "tlsSkipVerify")
	}
	secure := map[string]any{}
	if in.BasicAuth {
		if strings.TrimSpace(in.BasicAuthUser) == "" {
			return model.OpsDataSource{}, invalid("basicAuthUser", "请填写 Basic 认证用户名")
		}
		had := existing.SecureFields["basicAuthPassword"]
		if err := applyDataSourceSecret("basicAuthPassword", in.BasicAuthPassword, had, true, secure); err != nil {
			return model.OpsDataSource{}, err
		}
	} else if existing.SecureFields["basicAuthPassword"] {
		secure["basicAuthPassword"] = ""
	}
	if len(in.Headers) > 10 {
		return model.OpsDataSource{}, invalid("headers", "最多 10 个自定义请求头")
	}
	oldHeaders := map[string]int{}
	for k, v := range existingJSON {
		if strings.HasPrefix(k, "httpHeaderName") {
			if n, err := strconv.Atoi(strings.TrimPrefix(k, "httpHeaderName")); err == nil {
				oldHeaders[strv(v)] = n
			}
		}
	}
	used := map[int]bool{}
	for i, h := range in.Headers {
		name := strings.TrimSpace(h.Name)
		if name == "" || strings.ContainsAny(name, " :\r\n") || strings.EqualFold(name, "Authorization") && in.BasicAuth {
			return model.OpsDataSource{}, invalid(fmt.Sprintf("headers[%d]", i), "请求头名称无效")
		}
		slot := i + 1
		jsonData["httpHeaderName"+strconv.Itoa(slot)] = name
		used[slot] = true
		old, had := oldHeaders[name]
		switch h.Value.Mode {
		case "replace":
			secure["httpHeaderValue"+strconv.Itoa(slot)] = h.Value.Value
		case "", "keep":
			if !had {
				return model.OpsDataSource{}, invalid(fmt.Sprintf("headers[%d]", i), "新请求头需要填写值")
			}
			if old != slot {
				return model.OpsDataSource{}, invalid(fmt.Sprintf("headers[%d]", i), "请求头顺序变化后需要重新填写值")
			}
		case "clear":
			secure["httpHeaderValue"+strconv.Itoa(slot)] = ""
		default:
			return model.OpsDataSource{}, invalid(fmt.Sprintf("headers[%d]", i), "未知的凭据操作")
		}
	}
	for _, slot := range oldHeaders {
		if !used[slot] {
			secure["httpHeaderValue"+strconv.Itoa(slot)] = ""
		}
	}
	payload := map[string]any{"name": in.Name, "type": in.Type, "access": "proxy", "url": u.String(), "isDefault": in.IsDefault, "basicAuth": in.BasicAuth, "basicAuthUser": strings.TrimSpace(in.BasicAuthUser), "jsonData": jsonData}
	if uid != "" {
		payload["uid"] = uid
	}
	if len(secure) > 0 {
		payload["secureJsonData"] = secure
	}
	saved, err := s.Dashboards.SaveDataSource(ctx, uid, payload)
	s.invalidateDashboard("")
	saved.JSONData = publicJSONData(saved.JSONData)
	return saved, err
}

func applyDataSourceSecret(key string, secret model.OpsSecret, had, required bool, secure map[string]any) error {
	switch secret.Mode {
	case "", "keep":
		if required && !had {
			return invalid(key, "请填写密码")
		}
	case "replace":
		if secret.Value == "" {
			return invalid(key, "新密码不能为空")
		}
		secure[key] = secret.Value
	case "clear":
		if required {
			return invalid(key, "启用 Basic 认证时密码不能清除")
		}
		secure[key] = ""
	default:
		return invalid(key, "未知的凭据操作")
	}
	return nil
}

func (s *Service) DeleteDataSource(ctx context.Context, uid string) error {
	if err := requireBackend(s.Dashboards); err != nil {
		return err
	}
	current, err := s.Dashboards.DataSource(ctx, uid)
	if err != nil {
		return err
	}
	if current.ReadOnly {
		return fmt.Errorf("%w: 该数据源由部署配置（provisioning）管理", ports.ErrOpsReadOnly)
	}
	s.invalidateDashboard("")
	return s.Dashboards.DeleteDataSource(ctx, uid)
}

type DataSourceTest struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (s *Service) TestDataSource(ctx context.Context, uid string) (DataSourceTest, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return DataSourceTest{}, err
	}
	if !dashboardUIDPattern.MatchString(uid) {
		return DataSourceTest{}, ports.ErrOpsNotFound
	}
	status, message, err := s.Dashboards.TestDataSource(ctx, uid)
	return DataSourceTest{Status: status, Message: message}, err
}
