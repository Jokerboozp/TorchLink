package observability

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Alertmanager is the single notification path for infrastructure alerts
// from Prometheus and the Loki ruler. Its configuration file is managed by
// the ops service; this adapter only uses the v2 API and /-/reload.
type Alertmanager struct{ c *client }

func NewAlertmanager(baseURL string, timeout time.Duration) *Alertmanager {
	return &Alertmanager{c: newClient(baseURL, timeout)}
}

func (a *Alertmanager) Configured() bool { return a != nil && a.c.configured() }

func (a *Alertmanager) Status(ctx context.Context) model.OpsComponentStatus {
	status := model.OpsComponentStatus{ID: "alertmanager", Name: "Alertmanager", Configured: a.Configured(), CheckedAt: time.Now().UnixMilli()}
	if !status.Configured {
		status.State, status.Message = "unconfigured", "未配置 Alertmanager 地址"
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var body struct {
		Cluster struct {
			Status string `json:"status"`
		} `json:"cluster"`
		VersionInfo struct {
			Version string `json:"version"`
		} `json:"versionInfo"`
		Uptime string `json:"uptime"`
	}
	if err := a.c.getJSON(ctx, "/api/v2/status", nil, &body); err != nil {
		status.State, status.Message = "down", describe(err)
		return status
	}
	status.State, status.Version = "ok", body.VersionInfo.Version
	status.Details = map[string]any{"cluster": body.Cluster.Status, "uptime": body.Uptime}
	if body.Cluster.Status != "" && body.Cluster.Status != "ready" && body.Cluster.Status != "disabled" {
		status.State, status.Message = "degraded", "集群状态："+body.Cluster.Status
	}
	return status
}

type amAlert struct {
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
	EndsAt      string            `json:"endsAt"`
	UpdatedAt   string            `json:"updatedAt"`
	Status      struct {
		State       string   `json:"state"`
		SilencedBy  []string `json:"silencedBy"`
		InhibitedBy []string `json:"inhibitedBy"`
	} `json:"status"`
	Receivers []struct {
		Name string `json:"name"`
	} `json:"receivers"`
	GeneratorURL string `json:"generatorURL"`
}

// model drops generatorURL, which points at an internal host, keeping only
// the rule expression it carries.
func (al amAlert) model() model.OpsAlert {
	out := model.OpsAlert{Fingerprint: al.Fingerprint, Labels: nonNilLabels(al.Labels), Annotations: nonNilLabels(al.Annotations), StartsAt: al.StartsAt, EndsAt: al.EndsAt, UpdatedAt: al.UpdatedAt, State: al.Status.State, SilencedBy: emptyIfNil(al.Status.SilencedBy), InhibitedBy: emptyIfNil(al.Status.InhibitedBy), Receivers: []string{}}
	for _, r := range al.Receivers {
		out.Receivers = append(out.Receivers, r.Name)
	}
	if u, err := url.Parse(al.GeneratorURL); err == nil {
		out.Expr = u.Query().Get("g0.expr")
	}
	return out
}

func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func alertQuery(f ports.AlertFilter) url.Values {
	values := url.Values{"active": {strconv.FormatBool(f.Active)}, "silenced": {strconv.FormatBool(f.Silenced)}, "inhibited": {strconv.FormatBool(f.Inhibited)}}
	for _, m := range f.Matchers {
		values.Add("filter", m)
	}
	if f.Receiver != "" {
		values.Set("receiver", f.Receiver)
	}
	return values
}

func (a *Alertmanager) Alerts(ctx context.Context, f ports.AlertFilter) ([]model.OpsAlert, error) {
	var items []amAlert
	if err := a.c.getJSON(ctx, "/api/v2/alerts", alertQuery(f), &items); err != nil {
		return nil, err
	}
	out := make([]model.OpsAlert, 0, len(items))
	for _, item := range items {
		out = append(out, item.model())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt > out[j].StartsAt })
	return out, nil
}

func (a *Alertmanager) AlertGroups(ctx context.Context, f ports.AlertFilter) ([]model.OpsAlertGroup, error) {
	var items []struct {
		Labels   map[string]string `json:"labels"`
		Receiver struct {
			Name string `json:"name"`
		} `json:"receiver"`
		Alerts []amAlert `json:"alerts"`
	}
	if err := a.c.getJSON(ctx, "/api/v2/alerts/groups", alertQuery(f), &items); err != nil {
		return nil, err
	}
	out := make([]model.OpsAlertGroup, 0, len(items))
	for _, item := range items {
		group := model.OpsAlertGroup{Labels: nonNilLabels(item.Labels), Receiver: item.Receiver.Name, Alerts: []model.OpsAlert{}}
		for _, alert := range item.Alerts {
			group.Alerts = append(group.Alerts, alert.model())
		}
		out = append(out, group)
	}
	return out, nil
}

func (a *Alertmanager) Silences(ctx context.Context) ([]model.OpsSilence, error) {
	var items []struct {
		ID        string             `json:"id"`
		Matchers  []model.OpsMatcher `json:"matchers"`
		StartsAt  string             `json:"startsAt"`
		EndsAt    string             `json:"endsAt"`
		UpdatedAt string             `json:"updatedAt"`
		CreatedBy string             `json:"createdBy"`
		Comment   string             `json:"comment"`
		Status    struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := a.c.getJSON(ctx, "/api/v2/silences", nil, &items); err != nil {
		return nil, err
	}
	out := make([]model.OpsSilence, 0, len(items))
	for _, item := range items {
		matchers := item.Matchers
		if matchers == nil {
			matchers = []model.OpsMatcher{}
		}
		out = append(out, model.OpsSilence{ID: item.ID, Matchers: matchers, StartsAt: item.StartsAt, EndsAt: item.EndsAt, UpdatedAt: item.UpdatedAt, CreatedBy: item.CreatedBy, Comment: item.Comment, State: item.Status.State})
	}
	order := map[string]int{"active": 0, "pending": 1, "expired": 2}
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].State] != order[out[j].State] {
			return order[out[i].State] < order[out[j].State]
		}
		return out[i].EndsAt > out[j].EndsAt
	})
	return out, nil
}

func (a *Alertmanager) SaveSilence(ctx context.Context, s model.OpsSilence) (string, error) {
	payload := map[string]any{"matchers": s.Matchers, "startsAt": s.StartsAt, "endsAt": s.EndsAt, "createdBy": s.CreatedBy, "comment": s.Comment}
	if s.ID != "" {
		payload["id"] = s.ID
	}
	data, _, err := a.c.do(ctx, request{method: http.MethodPost, path: "/api/v2/silences", body: payload})
	if err != nil {
		return "", err
	}
	var body struct {
		SilenceID string `json:"silenceID"`
	}
	return body.SilenceID, decodeJSON(data, &body)
}

func (a *Alertmanager) ExpireSilence(ctx context.Context, id string) error {
	_, _, err := a.c.do(ctx, request{method: http.MethodDelete, path: "/api/v2/silence/" + url.PathEscape(id)})
	return err
}

func (a *Alertmanager) PostAlerts(ctx context.Context, alerts []map[string]any) error {
	_, _, err := a.c.do(ctx, request{method: http.MethodPost, path: "/api/v2/alerts", body: alerts})
	return err
}

// Reload asks Alertmanager to re-read its configuration. A failed reload
// keeps the previous configuration active and returns the parse error.
func (a *Alertmanager) Reload(ctx context.Context) error {
	_, _, err := a.c.do(ctx, request{method: http.MethodPost, path: "/-/reload", accept: "text/plain", timeout: 20 * time.Second})
	return err
}
