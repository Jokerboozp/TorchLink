package core

import (
	"errors"
	"time"

	"iot-platform/internal/netguard"
	"iot-platform/internal/ports"
)

// Deps are the collaborators a process wires into the engine. The process
// collects them while it opens storage, AI and knowledge, then installs them
// once, before the API and the engine's consumers start; nothing replaces an
// engine field after that. A zero field leaves the capability off, as on an
// engine built by New alone.
type Deps struct {
	// RawStore, when nil, keeps the archive-backed store New found.
	RawStore        ports.RawMessageStore
	Metrics         interface{ Inc(string) }
	AIRuns          ports.AIRunStore
	AIConversations ports.AIConversationStore
	DeviceSignals   ports.DeviceSignalStore
	TelemetryStats  ports.DeviceTelemetryStats
	SignalOptions   DeviceSignalOptions
	AI              ports.AIClient
	AIPlugins       ports.AIPluginRegistry
	AIWorkflows     ports.AIWorkflowRuntime
	// HarnessTokens signs the MCP credentials of chat and business runs; the
	// API verifies them with the same issuer.
	HarnessTokens      ports.HarnessTokenIssuer
	BusinessRunTimeout time.Duration
	ChatRunTimeout     time.Duration
	KB                 ports.KnowledgeBase
	KnowledgeReindex   *KnowledgeReindexer
	// PublishExternalTopics keeps publishing parsed messages to the
	// property/event/parsed topics for external subscribers.
	PublishExternalTopics bool
	MediaOutbound         netguard.Policy
}

// errInstalledLate reports an Install after the engine started: its background
// readers would race with the field writes.
var errInstalledLate = errors.New("engine dependencies must be installed before the engine starts")

// Install sets the engine's collaborators from d. It must run before
// StartWith.
func (e *Engine) Install(d Deps) error {
	if e.runCtx != nil {
		return errInstalledLate
	}
	if d.RawStore != nil {
		e.RawStore = d.RawStore
	}
	e.Metrics = d.Metrics
	e.AIRuns, e.AIConversations = d.AIRuns, d.AIConversations
	e.DeviceSignals, e.TelemetryStats, e.SignalOptions = d.DeviceSignals, d.TelemetryStats, d.SignalOptions
	e.AI, e.AIPlugins, e.AIWorkflows = d.AI, d.AIPlugins, d.AIWorkflows
	e.HarnessTokens = d.HarnessTokens
	e.BusinessRunTimeout, e.ChatRunTimeout = d.BusinessRunTimeout, d.ChatRunTimeout
	e.KB, e.KnowledgeReindex = d.KB, d.KnowledgeReindex
	e.PublishExternalTopics = d.PublishExternalTopics
	e.MediaOutbound = d.MediaOutbound
	return nil
}
