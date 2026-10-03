// Package notify delivers fire alarm notifications to people through tenant
// configured channels (SMTP, WeCom, DingTalk, Feishu robots and signed
// webhooks), with staged escalation that stops when the alarm is handled.
// It is independent of the ops-center monitoring stack.
package notify

import (
	"context"
	"errors"
)

const (
	ChannelSMTP     = "smtp"
	ChannelWeCom    = "wecom"
	ChannelDingTalk = "dingtalk"
	ChannelFeishu   = "feishu"
	ChannelWebhook  = "webhook"

	KindTrigger  = "trigger"
	KindRecovery = "recovery"

	StatusPending   = "PENDING"
	StatusSending   = "SENDING"
	StatusSent      = "SENT"
	StatusFailed    = "FAILED"
	StatusCancelled = "CANCELLED"
)

var (
	ErrNotFound = errors.New("notification resource not found")
	ErrConflict = errors.New("notification resource changed or is still referenced")
)

// Channel is where notifications are sent. Credentials (the robot or webhook
// URL, signing secret, SMTP password) are stored sealed and never returned.
type Channel struct {
	ID        string        `json:"id"`
	TenantID  string        `json:"tenantId"`
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	Enabled   bool          `json:"enabled"`
	Config    ChannelConfig `json:"config"`
	SecretSet bool          `json:"secretSet"`
	Version   int64         `json:"version"`
	UpdatedAt int64         `json:"updatedAt"`
}

// ChannelConfig holds the non-secret settings shown to administrators.
type ChannelConfig struct {
	// URLHint is the scheme and host of the robot or webhook URL.
	URLHint string `json:"urlHint,omitempty"`
	// SMTP settings.
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Security string `json:"security,omitempty"` // starttls, tls or none
	Username string `json:"username,omitempty"`
	From     string `json:"from,omitempty"`
}

// ChannelSecret is sealed as one value.
type ChannelSecret struct {
	URL        string `json:"url,omitempty"`
	SignSecret string `json:"signSecret,omitempty"`
	Password   string `json:"password,omitempty"`
}

// Policy selects alarms and the staged notifications sent for them.
type Policy struct {
	ID             string   `json:"id"`
	TenantID       string   `json:"tenantId"`
	Name           string   `json:"name"`
	Enabled        bool     `json:"enabled"`
	Levels         []string `json:"levels"`
	AlarmTypes     []string `json:"alarmTypes"`
	ProductIDs     []string `json:"productIds"`
	NotifyRecovery bool     `json:"notifyRecovery"`
	Stages         []Stage  `json:"stages"`
	Version        int64    `json:"version"`
	UpdatedAt      int64    `json:"updatedAt"`
}

// Stage is one escalation level. Stage 0 is sent at once; a later stage is
// sent after DelaySeconds only while the alarm is still unacknowledged.
type Stage struct {
	DelaySeconds int      `json:"delaySeconds"`
	ChannelIDs   []string `json:"channelIds"`
	Users        []string `json:"users"`
	Roles        []string `json:"roles"`
	OnDuty       bool     `json:"onDuty"`
	StationIDs   []string `json:"stationIds"`
	Emails       []string `json:"emails"`
	Mobiles      []string `json:"mobiles"`
}

// Task is one delivery of an alarm stage through one channel.
type Task struct {
	ID         int64    `json:"id"`
	TenantID   string   `json:"tenantId"`
	AlarmID    string   `json:"alarmId"`
	PolicyID   string   `json:"policyId"`
	PolicyName string   `json:"policyName,omitempty"`
	Stage      int      `json:"stage"`
	Kind       string   `json:"kind"`
	ChannelID  string   `json:"channelId"`
	Status     string   `json:"status"`
	Attempts   int      `json:"attempts"`
	NextAt     int64    `json:"nextAt"`
	LastError  string   `json:"lastError,omitempty"`
	Recipients []string `json:"recipients,omitempty"`
	CreatedAt  int64    `json:"createdAt"`
	SentAt     int64    `json:"sentAt,omitempty"`
}

// Store persists channels, policies and the delivery queue.
type Store interface {
	ListChannels(ctx context.Context, tenant string) ([]Channel, error)
	GetChannel(ctx context.Context, tenant, id string) (Channel, string, error)
	// SaveChannel creates (Version 0) or updates the channel at its version.
	// A nil sealed secret keeps the stored one.
	SaveChannel(ctx context.Context, c Channel, sealed *string) (Channel, error)
	DeleteChannel(ctx context.Context, tenant, id string) error
	ListPolicies(ctx context.Context, tenant string) ([]Policy, error)
	GetPolicy(ctx context.Context, tenant, id string) (Policy, error)
	SavePolicy(ctx context.Context, p Policy) (Policy, error)
	DeletePolicy(ctx context.Context, tenant, id string) error
	// EnqueueTasks inserts tasks; a task already queued for the same alarm,
	// policy, stage, kind and channel is skipped, so redelivered events do
	// not notify twice.
	EnqueueTasks(ctx context.Context, tasks []Task) (int, error)
	// ClaimDueTasks leases due pending tasks (and tasks whose earlier lease
	// expired) for leaseMillis.
	ClaimDueTasks(ctx context.Context, now, leaseMillis int64, limit int) ([]Task, error)
	FinishTask(ctx context.Context, t Task) error
	ListAlarmTasks(ctx context.Context, tenant, alarmID string) ([]Task, error)
}
