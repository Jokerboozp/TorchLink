package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/logkey"
	"log/slog"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/model"
)

// Contact is a resolved recipient.
type Contact struct {
	Name  string
	Email string
	Phone string
}

// Directory resolves alarms and people. It is implemented by the HTTP layer,
// which owns users, roles, device scopes and the fire safety roster.
type Directory interface {
	Alarm(ctx context.Context, tenant, id string) (model.Alarm, error)
	// DeviceProduct returns the product (device template) of a device.
	DeviceProduct(ctx context.Context, tenant, deviceID string) (string, error)
	// UserContacts returns the listed users and members of the listed roles
	// that are enabled and may see alarms of the device.
	UserContacts(ctx context.Context, tenant, deviceID string, users, roles []string) ([]Contact, error)
	// OnDutyContacts returns personnel on duty at the instant, optionally
	// limited to stations.
	OnDutyContacts(ctx context.Context, tenant string, at time.Time, stations []string) ([]Contact, error)
}

// notificationDelayBuckets bound alarm-to-first-notification times, seconds.
var notificationDelayBuckets = []float64{1, 2, 5, 10, 30, 60, 120, 300, 600}

// Metrics receives counters; *metrics.Registry implements it.
type Metrics interface{ Inc(string) }

type Service struct {
	Store     Store
	Directory Directory
	Cipher    *Cipher
	Sender    *Sender
	Metrics   Metrics
	Log       *slog.Logger
	// WebURL is the public console address used for alarm links.
	WebURL string
	Now    func() time.Time
	// MaxAttempts bounds delivery retries of one task.
	MaxAttempts int
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) count(name string) {
	if s.Metrics != nil {
		s.Metrics.Inc(name)
	}
}

func (s *Service) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Matches reports whether the policy covers the alarm.
func (p Policy) Matches(a model.Alarm, productID string) bool {
	if !p.Enabled || len(p.Stages) == 0 {
		return false
	}
	if len(p.Levels) > 0 && !slices.ContainsFunc(p.Levels, func(v string) bool { return strings.EqualFold(v, a.AlarmLevel) }) {
		return false
	}
	if len(p.AlarmTypes) > 0 && !slices.Contains(p.AlarmTypes, a.AlarmType) {
		return false
	}
	return len(p.ProductIDs) == 0 || slices.Contains(p.ProductIDs, productID)
}

// HandleReported queues the stages of every matching policy for the first
// report of an alarm. Repeated reports of the same active alarm do not
// notify again; a redelivered event is deduplicated by the task identity.
func (s *Service) HandleReported(ctx context.Context, payload []byte) error {
	var a model.Alarm
	if err := json.Unmarshal(payload, &a); err != nil {
		return model.Permanent(fmt.Errorf("decode alarm notification: %w", err))
	}
	if a.TriggerCount != 1 || a.Status != "ACTIVE" || a.TenantID == "" {
		return nil
	}
	return s.enqueue(ctx, a, KindTrigger)
}

// HandleRecovered queues recovery notices of policies that ask for them.
func (s *Service) HandleRecovered(ctx context.Context, payload []byte) error {
	var a model.Alarm
	if err := json.Unmarshal(payload, &a); err != nil {
		return model.Permanent(fmt.Errorf("decode alarm recovery: %w", err))
	}
	if a.Status != "RECOVERED" || a.TenantID == "" {
		return nil
	}
	return s.enqueue(ctx, a, KindRecovery)
}

func (s *Service) enqueue(ctx context.Context, a model.Alarm, kind string) error {
	policies, err := s.Store.ListPolicies(ctx, a.TenantID)
	if err != nil {
		return err
	}
	productID := ""
	for _, p := range policies {
		if len(p.ProductIDs) > 0 {
			if productID, err = s.Directory.DeviceProduct(ctx, a.TenantID, a.DeviceID); err != nil && !errors.Is(err, model.ErrNotFound) {
				return err
			}
			break
		}
	}
	now := s.now().UnixMilli()
	tasks := []Task{}
	for _, p := range policies {
		if !p.Matches(a, productID) || kind == KindRecovery && !p.NotifyRecovery {
			continue
		}
		for i, stage := range p.Stages {
			if kind == KindRecovery && i > 0 {
				break // recovery notices go to the first stage's audience only
			}
			next := now
			if kind == KindTrigger {
				next += int64(stage.DelaySeconds) * 1000
			}
			for _, channel := range stage.ChannelIDs {
				tasks = append(tasks, Task{TenantID: a.TenantID, AlarmID: a.ID, PolicyID: p.ID, PolicyName: p.Name, Stage: i, Kind: kind, ChannelID: channel, Status: StatusPending, NextAt: next, CreatedAt: now})
			}
		}
	}
	if len(tasks) == 0 {
		return nil
	}
	_, err = s.Store.EnqueueTasks(ctx, tasks)
	return err
}

// Run delivers due tasks until ctx ends. Several jobs processes may run it:
// tasks are leased, so each is sent by one of them.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.DeliverDue(ctx); err != nil && ctx.Err() == nil {
			s.logger().Warn("deliver alarm notifications", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// DeliverDue sends every task that is due now.
func (s *Service) DeliverDue(ctx context.Context) error {
	for {
		tasks, err := s.Store.ClaimDueTasks(ctx, s.now().UnixMilli(), 60_000, 20)
		if err != nil || len(tasks) == 0 {
			return err
		}
		for _, t := range tasks {
			if err = s.deliver(ctx, t); err != nil {
				return err
			}
		}
	}
}

func (s *Service) maxAttempts() int {
	if s.MaxAttempts > 0 {
		return s.MaxAttempts
	}
	return 8
}

func (s *Service) deliver(ctx context.Context, t Task) error {
	finish := func(status, reason string) error {
		t.Status, t.LastError = status, reason
		if status == StatusSent {
			t.SentAt = s.now().UnixMilli()
		}
		return s.Store.FinishTask(ctx, t)
	}
	alarm, err := s.Directory.Alarm(ctx, t.TenantID, t.AlarmID)
	if errors.Is(err, model.ErrNotFound) {
		return finish(StatusCancelled, "告警已不存在")
	}
	if err != nil {
		return s.retry(ctx, t, err)
	}
	// Escalation stops once someone handles the alarm.
	if t.Kind == KindTrigger && t.Stage > 0 && alarm.Status != "ACTIVE" {
		return finish(StatusCancelled, "告警已被处理，停止升级通知")
	}
	policy, err := s.Store.GetPolicy(ctx, t.TenantID, t.PolicyID)
	if errors.Is(err, ErrNotFound) || err == nil && (!policy.Enabled || t.Stage >= len(policy.Stages)) {
		return finish(StatusCancelled, "通知策略已删除或停用")
	}
	if err != nil {
		return s.retry(ctx, t, err)
	}
	channel, sealed, err := s.Store.GetChannel(ctx, t.TenantID, t.ChannelID)
	if errors.Is(err, ErrNotFound) || err == nil && !channel.Enabled {
		return finish(StatusCancelled, "通知渠道已删除或停用")
	}
	if err != nil {
		return s.retry(ctx, t, err)
	}
	secret, err := s.Cipher.Open(t.TenantID, t.ChannelID, sealed)
	if err != nil {
		return finish(StatusFailed, err.Error())
	}
	stage := policy.Stages[t.Stage]
	msg, recipients, err := s.render(ctx, alarm, policy, stage, t)
	if err != nil {
		return s.retry(ctx, t, err)
	}
	t.Recipients = recipients
	if channel.Type == ChannelSMTP && len(msg.Emails) == 0 {
		return finish(StatusFailed, "没有可用的收件邮箱")
	}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = s.Sender.Send(sendCtx, channel, secret, msg)
	cancel()
	if err != nil {
		return s.retry(ctx, t, err)
	}
	s.count("notification_sent_total")
	if t.Stage == 0 && t.CreatedAt > 0 {
		// The first stage is created when the alarm is reported, so this is
		// the time from the alarm to its first notification.
		if h, ok := s.Metrics.(interface {
			ObserveIn(string, []float64, float64)
		}); ok {
			h.ObserveIn("alarm_notification_delay_seconds", notificationDelayBuckets, time.Since(time.UnixMilli(t.CreatedAt)).Seconds())
		}
	}
	return finish(StatusSent, "")
}

func (s *Service) retry(ctx context.Context, t Task, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	t.Attempts++
	t.LastError = truncate(cause.Error(), 500)
	if t.Attempts >= s.maxAttempts() {
		t.Status = StatusFailed
		s.count("notification_failed_total")
		s.logger().Error("alarm notification failed", logkey.Tenant, t.TenantID, "alarmId", t.AlarmID, "channelId", t.ChannelID, "stage", t.Stage, "error", t.LastError)
	} else {
		t.Status = StatusPending
		t.NextAt = s.now().Add(min(time.Duration(1<<t.Attempts)*15*time.Second, 10*time.Minute)).UnixMilli()
		s.count("notification_retry_total")
	}
	return s.Store.FinishTask(ctx, t)
}

func truncate(v string, n int) string {
	if len(v) <= n {
		return v
	}
	return v[:n]
}

// render builds the message and resolves the stage's recipients.
func (s *Service) render(ctx context.Context, a model.Alarm, p Policy, stage Stage, t Task) (Message, []string, error) {
	contacts := []Contact{}
	if len(stage.Users) > 0 || len(stage.Roles) > 0 {
		users, err := s.Directory.UserContacts(ctx, a.TenantID, a.DeviceID, stage.Users, stage.Roles)
		if err != nil {
			return Message{}, nil, err
		}
		contacts = append(contacts, users...)
	}
	if stage.OnDuty {
		duty, err := s.Directory.OnDutyContacts(ctx, a.TenantID, s.now(), stage.StationIDs)
		if err != nil {
			return Message{}, nil, err
		}
		contacts = append(contacts, duty...)
	}
	emails, mobiles, recipients := []string{}, []string{}, []string{}
	seen := map[string]bool{}
	add := func(list *[]string, v string) {
		if v = strings.TrimSpace(v); v != "" && !seen[v] {
			seen[v] = true
			*list = append(*list, v)
		}
	}
	for _, c := range contacts {
		add(&emails, c.Email)
		add(&mobiles, c.Phone)
		if c.Name != "" {
			recipients = append(recipients, c.Name)
		}
	}
	for _, v := range stage.Emails {
		add(&emails, v)
	}
	for _, v := range stage.Mobiles {
		add(&mobiles, v)
	}
	device := a.DeviceName
	if device == "" {
		device = a.DeviceID
	}
	head := "【" + model.AlarmLevelName(a.AlarmLevel) + "】" + model.AlarmTypeName(a.AlarmType)
	if t.Kind == KindRecovery {
		head = "【已恢复】" + model.AlarmTypeName(a.AlarmType)
	}
	lines := []string{"设备：" + device}
	if a.Content != "" {
		lines = append(lines, "内容："+a.Content)
	}
	if location := strings.TrimSpace(strings.Join([]string{a.ComponentName, a.ComponentLocation}, " ")); location != "" {
		lines = append(lines, "位置："+location)
	}
	at := a.FirstTriggeredAt
	if t.Kind == KindRecovery && a.RecoveredAt > 0 {
		at = a.RecoveredAt
	}
	if at > 0 {
		lines = append(lines, "时间："+time.UnixMilli(at).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"))
	}
	if t.Kind == KindTrigger && t.Stage > 0 {
		lines = append(lines, fmt.Sprintf("升级通知（第 %d 级）：告警 %d 分钟内未确认", t.Stage+1, stage.DelaySeconds/60))
	}
	link := ""
	if s.WebURL != "" {
		link = strings.TrimRight(s.WebURL, "/") + "/alarms/" + a.ID
		lines = append(lines, "详情："+link)
	}
	event := map[string]any{"event": "alarm." + t.Kind, "tenantId": a.TenantID, "policyId": p.ID, "stage": t.Stage, "alarm": map[string]any{
		"alarmId": a.ID, "deviceId": a.DeviceID, "deviceName": a.DeviceName, "alarmType": a.AlarmType, "alarmLevel": a.AlarmLevel,
		"status": a.Status, "content": a.Content, "firstTriggeredAt": a.FirstTriggeredAt, "componentLocation": a.ComponentLocation,
	}}
	if len(recipients) == 0 {
		recipients = append(append(recipients, emails...), mobiles...)
	}
	return Message{Title: head + " " + device, Text: strings.Join(lines, "\n"), Link: link, Emails: emails, Mobiles: mobiles, Event: event}, recipients, nil
}

// Test sends a sample message through a channel.
func (s *Service) Test(ctx context.Context, tenant, channelID string, emails, mobiles []string) error {
	channel, sealed, err := s.Store.GetChannel(ctx, tenant, channelID)
	if err != nil {
		return err
	}
	secret, err := s.Cipher.Open(tenant, channelID, sealed)
	if err != nil {
		return err
	}
	msg := Message{Title: "【测试】炬联告警通知", Text: "这是一条测试消息，收到即表示通知渠道配置可用。", Emails: emails, Mobiles: mobiles, Event: map[string]any{"event": "test", "tenantId": tenant}}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return s.Sender.Send(sendCtx, channel, secret, msg)
}
