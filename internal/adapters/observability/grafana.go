package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Grafana is the single source of truth for dashboards, folders and data
// sources. The platform authenticates with a service account token (preferred
// when set) or basic credentials held only on the server.
type Grafana struct{ c *client }

func NewGrafana(baseURL, token, user, password string, timeout time.Duration) *Grafana {
	g := &Grafana{c: newClient(baseURL, timeout)}
	if token != "" {
		g.c.headers["Authorization"] = "Bearer " + token
	} else if user != "" {
		g.c.user, g.c.pass = user, password
	}
	return g
}

func (g *Grafana) Configured() bool { return g != nil && g.c.configured() }

func (g *Grafana) Status(ctx context.Context) model.OpsComponentStatus {
	status := model.OpsComponentStatus{ID: "grafana", Name: "Grafana", Configured: g.Configured(), CheckedAt: time.Now().UnixMilli()}
	if !status.Configured {
		status.State, status.Message = "unconfigured", "未配置 Grafana 地址"
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var health struct {
		Database string `json:"database"`
		Version  string `json:"version"`
	}
	if err := g.c.getJSON(ctx, "/api/health", nil, &health); err != nil {
		status.State, status.Message = "down", describe(err)
		return status
	}
	status.Version = health.Version
	if health.Database != "ok" {
		status.State, status.Message = "degraded", "Grafana 数据库状态异常"
		return status
	}
	// /api/health is public; verify the platform credentials separately.
	if _, _, err := g.c.do(ctx, request{method: http.MethodGet, path: "/api/search", query: url.Values{"limit": {"1"}}}); err != nil {
		status.State, status.Message = "degraded", describe(err)
		return status
	}
	status.State = "ok"
	return status
}

func (g *Grafana) SearchDashboards(ctx context.Context, q ports.DashboardSearch) ([]model.OpsDashboardSummary, error) {
	values := url.Values{"type": {"dash-db"}}
	if q.Query != "" {
		values.Set("query", q.Query)
	}
	for _, uid := range q.FolderUIDs {
		values.Add("folderUIDs", uid)
	}
	for _, tag := range q.Tags {
		values.Add("tag", tag)
	}
	for _, uid := range q.UIDs {
		values.Add("dashboardUIDs", uid)
	}
	limit := q.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	values.Set("limit", strconv.Itoa(limit))
	var hits []struct {
		UID         string   `json:"uid"`
		Title       string   `json:"title"`
		FolderUID   string   `json:"folderUid"`
		FolderTitle string   `json:"folderTitle"`
		Tags        []string `json:"tags"`
	}
	if err := g.c.getJSON(ctx, "/api/search", values, &hits); err != nil {
		return nil, err
	}
	out := make([]model.OpsDashboardSummary, 0, len(hits))
	for _, h := range hits {
		tags := h.Tags
		if tags == nil {
			tags = []string{}
		}
		out = append(out, model.OpsDashboardSummary{UID: h.UID, Title: h.Title, FolderUID: h.FolderUID, FolderTitle: h.FolderTitle, Tags: tags})
	}
	return out, nil
}

func (g *Grafana) GetDashboard(ctx context.Context, uid string) (map[string]any, map[string]any, error) {
	var body struct {
		Dashboard map[string]any `json:"dashboard"`
		Meta      map[string]any `json:"meta"`
	}
	if err := g.c.getJSON(ctx, "/api/dashboards/uid/"+url.PathEscape(uid), nil, &body); err != nil {
		return nil, nil, err
	}
	meta := map[string]any{}
	for _, key := range []string{"folderUid", "folderTitle", "version", "created", "updated", "createdBy", "updatedBy", "provisioned", "canSave", "canEdit", "canDelete"} {
		if v, ok := body.Meta[key]; ok {
			meta[key] = v
		}
	}
	return body.Dashboard, meta, nil
}

func (g *Grafana) SaveDashboard(ctx context.Context, dashboard map[string]any, folderUID, message string, overwrite bool) (ports.DashboardSaveResult, error) {
	payload := map[string]any{"dashboard": dashboard, "folderUid": folderUID, "message": message, "overwrite": overwrite}
	data, _, err := g.c.do(ctx, request{method: http.MethodPost, path: "/api/dashboards/db", body: payload})
	if errors.Is(err, ports.ErrOpsConflict) {
		// Grafana answers 412 for a stale version and for a title or UID
		// clash in the folder; both require the user to reload or rename.
		return ports.DashboardSaveResult{}, fmt.Errorf("%w: 仪表盘已被其他人修改，或同一文件夹中已有同名仪表盘（%s）", ports.ErrOpsConflict, strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(err.Error(), ports.ErrOpsConflict.Error()), ":")))
	}
	if err != nil {
		return ports.DashboardSaveResult{}, err
	}
	var out ports.DashboardSaveResult
	return out, decodeJSON(data, &out)
}

func (g *Grafana) DeleteDashboard(ctx context.Context, uid string) error {
	_, _, err := g.c.do(ctx, request{method: http.MethodDelete, path: "/api/dashboards/uid/" + url.PathEscape(uid)})
	return err
}

func (g *Grafana) Folders(ctx context.Context) ([]model.OpsFolder, error) {
	var hits []struct {
		UID       string `json:"uid"`
		Title     string `json:"title"`
		FolderUID string `json:"folderUid"`
	}
	if err := g.c.getJSON(ctx, "/api/search", url.Values{"type": {"dash-folder"}, "limit": {"5000"}}, &hits); err != nil {
		return nil, err
	}
	out := make([]model.OpsFolder, 0, len(hits))
	for _, h := range hits {
		out = append(out, model.OpsFolder{UID: h.UID, Title: h.Title, ParentUID: h.FolderUID, CanEdit: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

func (g *Grafana) SaveFolder(ctx context.Context, uid, title string, version int) (model.OpsFolder, error) {
	var body struct {
		UID       string `json:"uid"`
		Title     string `json:"title"`
		ParentUID string `json:"parentUid"`
		Version   int    `json:"version"`
	}
	var err error
	var data []byte
	if uid == "" {
		data, _, err = g.c.do(ctx, request{method: http.MethodPost, path: "/api/folders", body: map[string]any{"title": title}})
	} else {
		if version == 0 {
			var current struct {
				Version int `json:"version"`
			}
			if err := g.c.getJSON(ctx, "/api/folders/"+url.PathEscape(uid), nil, &current); err != nil {
				return model.OpsFolder{}, err
			}
			version = current.Version
		}
		data, _, err = g.c.do(ctx, request{method: http.MethodPut, path: "/api/folders/" + url.PathEscape(uid), body: map[string]any{"title": title, "version": version}})
	}
	if err != nil {
		return model.OpsFolder{}, err
	}
	if err := decodeJSON(data, &body); err != nil {
		return model.OpsFolder{}, err
	}
	return model.OpsFolder{UID: body.UID, Title: body.Title, ParentUID: body.ParentUID, Version: body.Version, CanEdit: true}, nil
}

func (g *Grafana) DeleteFolder(ctx context.Context, uid string) error {
	_, _, err := g.c.do(ctx, request{method: http.MethodDelete, path: "/api/folders/" + url.PathEscape(uid)})
	return err
}

type grafanaDataSource struct {
	ID               int             `json:"id"`
	UID              string          `json:"uid"`
	Name             string          `json:"name"`
	Type             string          `json:"type"`
	Access           string          `json:"access"`
	URL              string          `json:"url"`
	BasicAuth        bool            `json:"basicAuth"`
	BasicAuthUser    string          `json:"basicAuthUser"`
	IsDefault        bool            `json:"isDefault"`
	ReadOnly         bool            `json:"readOnly"`
	JSONData         map[string]any  `json:"jsonData"`
	SecureJSONFields map[string]bool `json:"secureJsonFields"`
	Version          int             `json:"version"`
}

// SupportedDataSourceTypes are the data source plugins whose queries the ops
// center can execute and render. Others are listed but not editable.
var SupportedDataSourceTypes = map[string]bool{"prometheus": true, "loki": true}

func (d grafanaDataSource) model() model.OpsDataSource {
	return model.OpsDataSource{UID: d.UID, Name: d.Name, Type: d.Type, IsDefault: d.IsDefault, ReadOnly: d.ReadOnly, Supported: SupportedDataSourceTypes[d.Type], URL: d.URL, Access: d.Access, BasicAuth: d.BasicAuth, BasicAuthUser: d.BasicAuthUser, JSONData: d.JSONData, SecureFields: d.SecureJSONFields}
}

func (g *Grafana) DataSources(ctx context.Context) ([]model.OpsDataSource, error) {
	var items []grafanaDataSource
	if err := g.c.getJSON(ctx, "/api/datasources", nil, &items); err != nil {
		return nil, err
	}
	out := make([]model.OpsDataSource, 0, len(items))
	for _, item := range items {
		out = append(out, item.model())
	}
	return out, nil
}

func (g *Grafana) DataSource(ctx context.Context, uid string) (model.OpsDataSource, error) {
	var item grafanaDataSource
	if err := g.c.getJSON(ctx, "/api/datasources/uid/"+url.PathEscape(uid), nil, &item); err != nil {
		return model.OpsDataSource{}, err
	}
	return item.model(), nil
}

// SaveDataSource creates (uid empty) or replaces a data source. The payload is
// assembled by the ops service from an allow-listed set of fields.
func (g *Grafana) SaveDataSource(ctx context.Context, uid string, payload map[string]any) (model.OpsDataSource, error) {
	method, path := http.MethodPost, "/api/datasources"
	if uid != "" {
		method, path = http.MethodPut, "/api/datasources/uid/"+url.PathEscape(uid)
	}
	data, _, err := g.c.do(ctx, request{method: method, path: path, body: payload})
	if err != nil {
		var upstream *ports.OpsUpstreamError
		if errors.As(err, &upstream) && upstream.Status == http.StatusForbidden && strings.Contains(strings.ToLower(upstream.Message), "read-only") {
			return model.OpsDataSource{}, ports.ErrOpsReadOnly
		}
		return model.OpsDataSource{}, err
	}
	var body struct {
		DataSource grafanaDataSource `json:"datasource"`
	}
	if err := decodeJSON(data, &body); err != nil {
		return model.OpsDataSource{}, err
	}
	return body.DataSource.model(), nil
}

func (g *Grafana) DeleteDataSource(ctx context.Context, uid string) error {
	_, _, err := g.c.do(ctx, request{method: http.MethodDelete, path: "/api/datasources/uid/" + url.PathEscape(uid)})
	return err
}

func (g *Grafana) TestDataSource(ctx context.Context, uid string) (string, string, error) {
	data, status, err := g.c.do(ctx, request{method: http.MethodGet, path: "/api/datasources/uid/" + url.PathEscape(uid) + "/health"})
	var body struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err != nil {
		// Health checks report a failing data source as HTTP 400 with a body.
		if status == http.StatusBadRequest && json.Unmarshal(data, &body) == nil && body.Status != "" {
			return body.Status, body.Message, nil
		}
		return "", "", err
	}
	_ = json.Unmarshal(data, &body)
	return body.Status, body.Message, nil
}

// QueryData runs panel queries through Grafana's /api/ds/query so that the
// data source configuration stays in Grafana. It returns frames and
// per-query errors keyed by refId.
func (g *Grafana) QueryData(ctx context.Context, q ports.PanelQueryRequest) (map[string][]model.DataFrame, map[string]string, error) {
	payload := map[string]any{"from": strconv.FormatInt(q.From.UnixMilli(), 10), "to": strconv.FormatInt(q.To.UnixMilli(), 10), "queries": q.Queries}
	for _, query := range q.Queries {
		if q.MaxDataPoints > 0 {
			query["maxDataPoints"] = q.MaxDataPoints
		}
		if q.IntervalMs > 0 {
			query["intervalMs"] = q.IntervalMs
		}
	}
	data, status, err := g.c.do(ctx, request{method: http.MethodPost, path: "/api/ds/query", body: payload})
	if err != nil && status != http.StatusBadRequest && status != http.StatusMultiStatus {
		return nil, nil, err
	}
	var body struct {
		Results map[string]struct {
			Status int            `json:"status"`
			Error  string         `json:"error"`
			Frames []grafanaFrame `json:"frames"`
		} `json:"results"`
		Message string `json:"message"`
	}
	if decodeErr := decodeJSON(data, &body); decodeErr != nil {
		return nil, nil, decodeErr
	}
	if len(body.Results) == 0 && err != nil {
		return nil, nil, err
	}
	frames, errs := map[string][]model.DataFrame{}, map[string]string{}
	for refID, result := range body.Results {
		if result.Error != "" {
			errs[refID] = result.Error
		}
		for _, f := range result.Frames {
			frames[refID] = append(frames[refID], f.model(refID))
		}
	}
	return frames, errs, nil
}

type grafanaFrame struct {
	Schema struct {
		RefID  string         `json:"refId"`
		Name   string         `json:"name"`
		Meta   map[string]any `json:"meta"`
		Fields []struct {
			Name   string            `json:"name"`
			Type   string            `json:"type"`
			Labels map[string]string `json:"labels"`
			Config map[string]any    `json:"config"`
		} `json:"fields"`
	} `json:"schema"`
	Data struct {
		Values [][]any `json:"values"`
	} `json:"data"`
}

func (f grafanaFrame) model(refID string) model.DataFrame {
	out := model.DataFrame{RefID: refID, Name: f.Schema.Name, Fields: make([]model.DataField, 0, len(f.Schema.Fields))}
	if meta := f.Schema.Meta; meta != nil {
		out.Meta = map[string]any{}
		for _, key := range []string{"type", "custom", "notices", "preferredVisualisationType"} {
			if v, ok := meta[key]; ok {
				out.Meta[key] = v
			}
		}
	}
	for i, field := range f.Schema.Fields {
		values := []any{}
		if i < len(f.Data.Values) && f.Data.Values[i] != nil {
			values = f.Data.Values[i]
		}
		config := map[string]any{}
		for _, key := range []string{"displayNameFromDS", "unit", "interval"} {
			if v, ok := field.Config[key]; ok {
				config[key] = v
			}
		}
		out.Fields = append(out.Fields, model.DataField{Name: field.Name, Type: field.Type, Labels: field.Labels, Config: config, Values: values})
	}
	return out
}

// DataSourceResource reads a data source plugin resource (for example
// Prometheus label values) through Grafana. Paths come from a fixed list in
// the ops service, never from the browser.
func (g *Grafana) DataSourceResource(ctx context.Context, uid, resource string, params map[string][]string) (json.RawMessage, error) {
	if strings.Contains(resource, "..") || strings.HasPrefix(resource, "/") {
		return nil, fmt.Errorf("invalid resource path")
	}
	data, _, err := g.c.do(ctx, request{method: http.MethodGet, path: "/api/datasources/uid/" + url.PathEscape(uid) + "/resources/" + resource, query: url.Values(params)})
	return data, err
}
