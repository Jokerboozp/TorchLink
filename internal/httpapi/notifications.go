package httpapi

import (
	"context"
	"errors"
	"iot-platform/internal/devicescope"
	"net/http"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/notify"
)

// SetNotifications installs the fire alarm notification service.
func (s *Server) SetNotifications(n *notify.Service) { s.notifications = n }

// NotificationDirectory resolves alarms and recipients for the service.
func (s *Server) NotificationDirectory() notify.Directory { return notificationDirectory{s} }

type notificationDirectory struct{ s *Server }

func (d notificationDirectory) Alarm(ctx context.Context, tenant, id string) (model.Alarm, error) {
	return d.s.unscopedRepo().GetAlarm(ctx, tenant, id)
}

func (d notificationDirectory) DeviceProduct(ctx context.Context, tenant, deviceID string) (string, error) {
	device, err := d.s.unscopedRepo().GetManagedDevice(ctx, tenant, deviceID)
	if err != nil {
		return "", model.ErrNotFound
	}
	return device.ProductID, nil
}

// UserContacts includes a user only while the account is enabled, may open
// the alarm center and covers the alarm's device, so a notification never
// reveals an alarm the recipient could not see in the console.
func (d notificationDirectory) UserContacts(ctx context.Context, tenant, deviceID string, users, roles []string) ([]Contact, error) {
	state, err := d.s.authorizationAccess(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := []Contact{}
	for _, u := range state.Users {
		if !u.Enabled || !(slices.Contains(users, u.Username) || slices.ContainsFunc(u.RoleIDs, func(r string) bool { return slices.Contains(roles, r) })) {
			continue
		}
		permissions := effectivePermissions(state, u)
		d.s.stripOpsPermissions(tenant, permissions)
		scope := d.s.scopeFor(resolveUserDeviceScope(state, u), permissions, tenant)
		if !permissions["menu:alarms"] || !scope.Has(deviceID) {
			continue
		}
		name := u.DisplayName
		if name == "" {
			name = u.Username
		}
		out = append(out, Contact{Name: name, Email: u.Email, Phone: u.Phone})
	}
	return out, nil
}

// Contact aliases notify.Contact for readability in this file.
type Contact = notify.Contact

func (d notificationDirectory) OnDutyContacts(ctx context.Context, tenant string, at time.Time, stations []string) ([]Contact, error) {
	state, err := d.s.fireSafety.Snapshot(ctx, tenant)
	if err != nil {
		return nil, err
	}
	now := at.UnixMilli()
	onDuty := map[string]bool{}
	for _, a := range state.Assignments {
		if a.StartAt <= now && now < a.EndAt && (len(stations) == 0 || slices.Contains(stations, a.StationID)) {
			for _, id := range a.PersonnelIDs {
				onDuty[id] = true
			}
		}
	}
	out := []Contact{}
	for _, p := range state.Personnel {
		if p.Enabled && onDuty[p.ID] {
			out = append(out, Contact{Name: p.Name, Phone: p.Phone})
		}
	}
	return out, nil
}

func (s *Server) notificationRoutes() {
	a := s.authorize("admin")
	r, e := s.router, s.endpoint
	r.GET("/api/v1/notifications/channels", a, e(s.listNotificationChannels))
	r.POST("/api/v1/notifications/channels", a, e(s.saveNotificationChannel))
	r.PUT("/api/v1/notifications/channels/:id", a, e(s.saveNotificationChannel, "id"))
	r.DELETE("/api/v1/notifications/channels/:id", a, e(s.deleteNotificationChannel, "id"))
	r.POST("/api/v1/notifications/channels/:id/test", a, e(s.testNotificationChannel, "id"))
	r.GET("/api/v1/notifications/policies", a, e(s.listNotificationPolicies))
	r.POST("/api/v1/notifications/policies", a, e(s.saveNotificationPolicy))
	r.PUT("/api/v1/notifications/policies/:id", a, e(s.saveNotificationPolicy, "id"))
	r.DELETE("/api/v1/notifications/policies/:id", a, e(s.deleteNotificationPolicy, "id"))
	r.GET("/api/v1/notifications/options", a, e(s.notificationOptions))
	r.GET("/api/v1/alarms/:id/notifications", s.authorize("viewer"), e(s.alarmNotifications, "id"))
}

func (s *Server) notificationService(w http.ResponseWriter) (*notify.Service, bool) {
	if s.notifications == nil {
		problem(w, 409, "告警通知服务未启用")
		return nil, false
	}
	return s.notifications, true
}

func (s *Server) notificationProblem(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, notify.ErrNotFound):
		problem(w, 404, "通知渠道或策略不存在")
	case errors.Is(err, notify.ErrConflict):
		problem(w, 409, "内容已被修改或已存在，请刷新后重试")
	default:
		s.fail(w, r, err, "保存告警通知配置失败")
	}
}

func (s *Server) listNotificationChannels(w http.ResponseWriter, r *http.Request) {
	n, ok := s.notificationService(w)
	if !ok {
		return
	}
	items, err := n.Store.ListChannels(r.Context(), claims(r).TenantID)
	if err != nil {
		s.notificationProblem(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) saveNotificationChannel(w http.ResponseWriter, r *http.Request) {
	n, ok := s.notificationService(w)
	if !ok {
		return
	}
	var in struct {
		notify.Channel
		Secret *notify.ChannelSecret `json:"secret"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	c := in.Channel
	c.TenantID = claims(r).TenantID
	if id := r.PathValue("id"); id != "" {
		c.ID = id
	} else {
		c.Version = 0
		if c.ID == "" {
			c.ID = "channel_" + randomHex(6)
		}
	}
	c.Name = strings.TrimSpace(c.Name)
	if err := notify.ValidateChannel(c); err != nil {
		problem(w, 422, err.Error())
		return
	}
	secret := notify.ChannelSecret{}
	if c.Version != 0 {
		old, sealed, err := n.Store.GetChannel(r.Context(), c.TenantID, c.ID)
		if err != nil {
			s.notificationProblem(w, r, err)
			return
		}
		if old.Type != c.Type {
			problem(w, 422, "不能修改渠道类型，请新建渠道")
			return
		}
		if secret, err = n.Cipher.Open(c.TenantID, c.ID, sealed); err != nil {
			s.fail(w, r, err, "")
			return
		}
	}
	// Empty secret fields keep the stored values.
	if in.Secret != nil {
		for _, field := range []struct{ from, to *string }{{&in.Secret.URL, &secret.URL}, {&in.Secret.SignSecret, &secret.SignSecret}, {&in.Secret.Password, &secret.Password}} {
			if v := strings.TrimSpace(*field.from); v != "" {
				*field.to = v
			}
		}
	}
	if err := n.Sender.ValidateTarget(c.Type, c.Config, secret); err != nil {
		problem(w, 422, err.Error())
		return
	}
	c.Config.URLHint = notify.URLHint(secret.URL)
	var sealedPtr *string
	if c.Version == 0 || in.Secret != nil {
		sealed, err := n.Cipher.Seal(c.TenantID, c.ID, secret)
		if err != nil {
			s.fail(w, r, err, "加密渠道凭据失败")
			return
		}
		sealedPtr = &sealed
	}
	saved, err := n.Store.SaveChannel(r.Context(), c, sealedPtr)
	if err != nil {
		s.notificationProblem(w, r, err)
		return
	}
	s.audit(r, "notification.channel.save", "notification_channel", saved.ID, map[string]any{"type": saved.Type, "enabled": saved.Enabled, "secretChanged": in.Secret != nil})
	write(w, 200, saved)
}

func (s *Server) deleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	n, ok := s.notificationService(w)
	if !ok {
		return
	}
	tenant, id := claims(r).TenantID, r.PathValue("id")
	policies, err := n.Store.ListPolicies(r.Context(), tenant)
	if err != nil {
		s.notificationProblem(w, r, err)
		return
	}
	if used := notify.ReferencedBy(policies, id); len(used) > 0 {
		problem(w, 409, "渠道仍被通知策略使用："+strings.Join(used, "、"))
		return
	}
	if err = n.Store.DeleteChannel(r.Context(), tenant, id); err != nil {
		s.notificationProblem(w, r, err)
		return
	}
	s.audit(r, "notification.channel.delete", "notification_channel", id, nil)
	write(w, 200, map[string]bool{"success": true})
}

func (s *Server) testNotificationChannel(w http.ResponseWriter, r *http.Request) {
	n, ok := s.notificationService(w)
	if !ok {
		return
	}
	var in struct {
		Emails  []string `json:"emails"`
		Mobiles []string `json:"mobiles"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if len(in.Emails) > 10 || len(in.Mobiles) > 10 {
		problem(w, 422, "测试接收人最多 10 个")
		return
	}
	err := n.Test(r.Context(), claims(r).TenantID, r.PathValue("id"), in.Emails, in.Mobiles)
	s.audit(r, "notification.channel.test", "notification_channel", r.PathValue("id"), map[string]any{"success": err == nil})
	if errors.Is(err, notify.ErrNotFound) {
		s.notificationProblem(w, r, err)
		return
	}
	if err != nil {
		problem(w, 502, "发送失败："+err.Error())
		return
	}
	write(w, 200, map[string]bool{"sent": true})
}

func (s *Server) listNotificationPolicies(w http.ResponseWriter, r *http.Request) {
	n, ok := s.notificationService(w)
	if !ok {
		return
	}
	items, err := n.Store.ListPolicies(r.Context(), claims(r).TenantID)
	if err != nil {
		s.notificationProblem(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) saveNotificationPolicy(w http.ResponseWriter, r *http.Request) {
	n, ok := s.notificationService(w)
	if !ok {
		return
	}
	var p notify.Policy
	if decode(w, r, &p) != nil {
		return
	}
	p.TenantID = claims(r).TenantID
	if id := r.PathValue("id"); id != "" {
		p.ID = id
	} else {
		p.Version = 0
		if p.ID == "" {
			p.ID = "policy_" + randomHex(6)
		}
	}
	p.Name = strings.TrimSpace(p.Name)
	channels, err := n.Store.ListChannels(r.Context(), p.TenantID)
	if err != nil {
		s.notificationProblem(w, r, err)
		return
	}
	byID := map[string]notify.Channel{}
	for _, c := range channels {
		byID[c.ID] = c
	}
	if err = notify.ValidatePolicy(p, byID); err != nil {
		problem(w, 422, err.Error())
		return
	}
	saved, err := n.Store.SavePolicy(r.Context(), p)
	if err != nil {
		s.notificationProblem(w, r, err)
		return
	}
	s.audit(r, "notification.policy.save", "notification_policy", saved.ID, map[string]any{"enabled": saved.Enabled, "stages": len(saved.Stages)})
	write(w, 200, saved)
}

func (s *Server) deleteNotificationPolicy(w http.ResponseWriter, r *http.Request) {
	n, ok := s.notificationService(w)
	if !ok {
		return
	}
	if err := n.Store.DeletePolicy(r.Context(), claims(r).TenantID, r.PathValue("id")); err != nil {
		s.notificationProblem(w, r, err)
		return
	}
	s.audit(r, "notification.policy.delete", "notification_policy", r.PathValue("id"), nil)
	write(w, 200, map[string]bool{"success": true})
}

// notificationOptions lists the users, roles and fire stations a policy can
// address, without permissions or credentials.
func (s *Server) notificationOptions(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	type option struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Contact bool   `json:"contact,omitempty"`
	}
	users, roles, stations := []option{}, []option{}, []option{}
	if store, err := s.accessStore(); err == nil {
		if state, err := store.LoadAccessState(r.Context(), tenant); err == nil {
			for _, u := range state.Users {
				name := u.DisplayName
				if name == "" {
					name = u.Username
				}
				users = append(users, option{ID: u.Username, Name: name, Contact: u.Email != "" || u.Phone != ""})
			}
			for _, role := range state.Roles {
				roles = append(roles, option{ID: role.ID, Name: role.Name})
			}
		}
	}
	if state, err := s.fireSafety.Snapshot(r.Context(), tenant); err == nil {
		for _, st := range state.Stations {
			if st.Enabled {
				stations = append(stations, option{ID: st.ID, Name: st.Name})
			}
		}
	}
	write(w, 200, map[string]any{"users": users, "roles": roles, "stations": stations})
}

// alarmNotifications is the delivery timeline of one alarm. The alarm is
// read through the request's device scope first.
func (s *Server) alarmNotifications(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	if _, err := s.engine.Repo.GetAlarm(r.Context(), tenant, r.PathValue("id")); err != nil {
		if errors.Is(err, devicescope.ErrDenied) {
			problemCode(w, 403, codeDeviceScopeDenied, "无权查看该设备的告警")
			return
		}
		problem(w, 404, "告警不存在")
		return
	}
	if s.notifications == nil {
		write(w, 200, map[string]any{"items": []notify.Task{}, "enabled": false})
		return
	}
	items, err := s.notifications.Store.ListAlarmTasks(r.Context(), tenant, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err, "读取通知记录失败")
		return
	}
	names := map[string]string{}
	if channels, err := s.notifications.Store.ListChannels(r.Context(), tenant); err == nil {
		for _, c := range channels {
			names[c.ID] = c.Name
		}
	}
	type item struct {
		notify.Task
		ChannelName string `json:"channelName"`
	}
	out := make([]item, 0, len(items))
	for _, t := range items {
		out = append(out, item{Task: t, ChannelName: names[t.ChannelID]})
	}
	write(w, 200, map[string]any{"items": out, "enabled": true})
}
