package ports

import (
	"context"
	"io"
	"time"

	"iot-platform/internal/model"
)

type RawFilter struct {
	TenantID, ProductID, DeviceID string
	Start, End                    int64
	Limit, Offset                 int
}

type AlarmFilter struct {
	TenantID, DeviceID, Status, Level, Source string
	Start, End                                int64
	Limit, Offset                             int
}

type Repository interface {
	SaveEdgeNode(context.Context, model.EdgeNode) error
	GetEdgeNode(context.Context, string, string) (model.EdgeNode, error)
	ListEdgeNodes(context.Context, string) ([]model.EdgeNode, error)
	ListDeviceStateEvents(context.Context, string, string, int, int) ([]model.DeviceStateEvent, int, error)
	ListDeviceMessages(context.Context, string, string, model.MessageType, int, int) ([]model.StandardMessage, int, error)
	ChangeDeviceCredential(context.Context, string, string, string, string, int64) (model.ManagedDevice, model.CredentialRevocation, error)
	ListCredentialRevocations(context.Context, string, string, bool) ([]model.CredentialRevocation, error)
	UpdateCredentialRevocation(context.Context, model.CredentialRevocation) error
	CreateDeviceCommand(context.Context, model.DeviceCommand) (model.DeviceCommand, bool, error)
	UpdateDeviceCommandDispatch(context.Context, string, string, string, string, int64) error
	CompleteDeviceCommand(context.Context, string, string, string, map[string]any, int64) error
	ListDeviceCommands(context.Context, string, string, int, int) ([]model.DeviceCommand, int, error)
	// Keep atomic onboarding in the repository contract so telemetry/cache
	// decorators forward it to durable storage rather than hiding the capability.
	SaveOnboarding(context.Context, model.OnboardingBundle) error
	SaveProduct(context.Context, model.Product) error
	GetProduct(context.Context, string, string) (model.Product, error)
	ListProducts(context.Context, string) ([]model.Product, error)
	ListProductsPage(context.Context, string, int, int) ([]model.Product, int, error)
	SaveProtocolPackage(context.Context, model.ProtocolPackage) error
	GetProtocolPackage(context.Context, string, string) (model.ProtocolPackage, error)
	ListProtocolPackages(context.Context, string) ([]model.ProtocolPackage, error)
	ListProtocolPackagesPage(context.Context, string, int, int) ([]model.ProtocolPackage, int, error)
	SaveProtocolDefinition(context.Context, model.ProtocolDefinition) error
	GetProtocolDefinition(context.Context, string, string) (model.ProtocolDefinition, error)
	ListProtocolDefinitions(context.Context, string) ([]model.ProtocolDefinition, error)
	CreateProtocolRelease(context.Context, model.ProtocolRelease) error
	GetProtocolRelease(context.Context, string, string, string) (model.ProtocolRelease, error)
	ListProtocolReleases(context.Context, string, string) ([]model.ProtocolRelease, error)
	UpdateProtocolReleaseStatus(context.Context, string, string, string, string, int64) error
	CreatePointTableRelease(context.Context, model.PointTableRelease) error
	GetPointTableRelease(context.Context, string, string, string) (model.PointTableRelease, error)
	SaveProductProtocolBinding(context.Context, model.ProductProtocolBinding) error
	GetProductProtocolBinding(context.Context, string, string) (model.ProductProtocolBinding, error)
	SaveDeviceAccessProfile(context.Context, model.DeviceAccessProfile) error
	GetDeviceAccessProfile(context.Context, string, string) (model.DeviceAccessProfile, error)
	ListDeviceAccessProfiles(context.Context, string) ([]model.DeviceAccessProfile, error)
	SaveManagedDevice(context.Context, model.ManagedDevice) error
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetManagedDeviceByAccessKey(context.Context, string) (model.ManagedDevice, error)
	ListManagedDevices(context.Context, string) ([]model.ManagedDevice, error)
	ListManagedDevicesPage(context.Context, string, int, int) ([]model.ManagedDevice, int, error)
	CountManagedDeviceChildren(context.Context, string, []string) (map[string]int, error)
	SaveRawIndex(context.Context, model.RawArchiveIndex) (bool, error)
	MarkRawParseResult(context.Context, string, string, int64, string) error
	MarkRawPublished(context.Context, string, string, int64, string) error
	ListPendingRawIndexes(context.Context, int) ([]model.RawArchiveIndex, error)
	GetRawIndex(context.Context, string, string) (model.RawArchiveIndex, error)
	ListRawIndexes(context.Context, RawFilter) ([]model.RawArchiveIndex, error)
	CountRawIndexes(context.Context, RawFilter) (int, error)
	SaveStandardMessage(context.Context, model.StandardMessage) error
	SaveStandardMessageIfAbsent(context.Context, model.StandardMessage) (bool, error)
	ClaimStandardMessage(context.Context, model.StandardMessage) (shouldProcess bool, created bool, err error)
	MarkStandardMessageProcessed(context.Context, string, string) error
	GetStandardMessageByRaw(context.Context, string, string) (model.StandardMessage, error)
	GetLatestMessage(context.Context, string, string) (model.StandardMessage, error)
	PropertyHistory(context.Context, string, string, string, int64, int64, int) ([]map[string]any, error)
	PropertyHistoryPage(context.Context, string, string, string, int64, int64, int, int) ([]map[string]any, int, error)
	UpsertDeviceState(context.Context, model.DeviceState) error
	GetDeviceState(context.Context, string, string) (model.DeviceState, error)
	ListDeviceStates(context.Context, string) ([]model.DeviceState, error)
	ListDeviceStatesPage(context.Context, string, int, int) ([]model.DeviceState, int, error)
	ListUnregisteredDeviceStatesPage(context.Context, string, int, int) ([]model.DeviceState, int, error)
	CountDeviceStates(context.Context, string, bool) (int, int, error)
	SaveDeviceStateEvent(context.Context, model.DeviceState) error
	SaveRule(context.Context, model.AlarmRule) error
	ListRules(context.Context, string) ([]model.AlarmRule, error)
	ListRulesPage(context.Context, string, int, int) ([]model.AlarmRule, int, error)
	DeleteRule(context.Context, string, string) error
	SaveRulePending(context.Context, string, string, string, int64) error
	GetRulePending(context.Context, string, string, string) (int64, bool, error)
	DeleteRulePending(context.Context, string, string, string) error
	DeleteRulePendings(context.Context, string, string) error
	UpsertAlarm(context.Context, model.Alarm) (model.Alarm, bool, error)
	GetAlarm(context.Context, string, string) (model.Alarm, error)
	ListAlarms(context.Context, AlarmFilter) ([]model.Alarm, error)
	CountAlarms(context.Context, AlarmFilter) (int, error)
	UpdateAlarm(context.Context, model.Alarm) error
	SaveVideoEvent(context.Context, model.VideoAlarmEvent) (bool, error)
	UpdateVideoEvent(context.Context, model.VideoAlarmEvent) error
	ListPendingVideoEvents(context.Context, int) ([]model.VideoAlarmEvent, error)
	SaveVideoCameraMapping(context.Context, model.VideoCameraMapping) error
	GetVideoCameraMapping(context.Context, string, string) (model.VideoCameraMapping, error)
	ListVideoCameraMappings(context.Context, string) ([]model.VideoCameraMapping, error)
	ListVideoCameraMappingsPage(context.Context, string, int, int) ([]model.VideoCameraMapping, int, error)
	ReplaceVideoCameraRelations(context.Context, string, string, []model.VideoCameraRelation) error
	ListVideoCameraRelations(context.Context, string, string) ([]model.VideoCameraRelation, error)
	ListVideoCameraRelationsByTarget(context.Context, string, string, string) ([]model.VideoCameraRelation, error)
	SaveAIAnalysis(context.Context, model.AIAnalysis) error
	GetAIAnalysis(context.Context, string, string) (model.AIAnalysis, error)
	SaveKnowledgeDoc(context.Context, model.KnowledgeDoc) error
	ListKnowledgeDocs(context.Context, string) ([]model.KnowledgeDoc, error)
	ListKnowledgeDocsPage(context.Context, string, int, int) ([]model.KnowledgeDoc, int, error)
	SaveWorkflowKnowledgeBinding(context.Context, model.WorkflowKnowledgeBinding) error
	GetWorkflowKnowledgeBinding(context.Context, string, string) (model.WorkflowKnowledgeBinding, error)
	SaveReplay(context.Context, model.ReplayRequest) error
	UpdateReplay(context.Context, model.ReplayRequest) error
	GetReplay(context.Context, string) (model.ReplayRequest, error)
	SaveAudit(context.Context, model.AuditLog) error
	SaveAIToolCall(context.Context, model.AIToolCallLog) error
	Health(context.Context) error
	Close() error
}

type Archive interface {
	PutObject(context.Context, string, string, io.Reader, int64, string) (string, error)
	GetObject(context.Context, string, string) (io.ReadCloser, error)
	Health(context.Context) error
}

// RawMessageStore keeps the raw device payload available for parsing, replay
// and the daily object-storage backup. It is deliberately separate from
// Archive: MinIO is an object store for completed backups and media, not the
// per-message write path.
type RawMessageStore interface {
	PutRaw(context.Context, model.RawMessage) (model.RawArchiveIndex, error)
	GetRaw(context.Context, model.RawArchiveIndex) (model.RawMessage, error)
}

// RawMessageDatabase is implemented by the PostgreSQL and ClickHouse
// adapters. The raw store chooses one database per device according to its
// configured or observed reporting frequency.
type RawMessageDatabase interface {
	SaveRawMessage(context.Context, model.RawMessage) error
	GetRawMessage(context.Context, string, string) (model.RawMessage, error)
}

// RawMessageReader is used only for reading legacy MinIO raw objects created
// before the database-backed raw log path was introduced.
type RawMessageReader interface {
	GetRaw(context.Context, model.RawArchiveIndex) (model.RawMessage, error)
}

type Handler func(context.Context, []byte) error

type EventBus interface {
	Publish(context.Context, string, string, []byte) error
	Subscribe(context.Context, string, string, Handler) error
	Health(context.Context) error
	Close() error
}

type RealtimePublisher interface {
	Publish(context.Context, string, []byte, byte, bool) error
	Health(context.Context) error
	Close() error
}

type AIClient interface {
	AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error)
	Chat(context.Context, string, string) (string, error)
	RuleDraft(context.Context, string, string) (model.AlarmRule, error)
	Health(context.Context) error
}

// AIJSONGenerator is an optional structured-output capability. Keeping it
// separate from AIClient preserves compatibility with provider implementations
// that only support ordinary chat while allowing protocol and inspection
// workflows to request machine-readable output.
type AIJSONGenerator interface {
	GenerateJSON(context.Context, string, string, string) (string, error)
}

// VideoPreviewService resolves direct or vendor-SDK camera sources and, when
// configured, proxies them through ZLMediaKit into a browser playback URL.
type VideoPreviewService interface {
	Preview(context.Context, model.VideoCameraMapping) (model.VideoPreview, error)
	Eligible(model.VideoCameraMapping, []string) bool
}

type AIPluginConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl,omitempty"`
	Model    string `json:"model,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
}

type AIPluginInfo struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	DefaultBaseURL string   `json:"defaultBaseUrl,omitempty"`
	DefaultModel   string   `json:"defaultModel,omitempty"`
	Model          string   `json:"model,omitempty"`
	RequiresAPIKey bool     `json:"requiresApiKey"`
	Enabled        bool     `json:"enabled"`
	Capabilities   []string `json:"capabilities"`
}

type AIInspectable interface {
	ProviderInfo() AIPluginInfo
}

type AIPluginRegistry interface {
	List() []AIPluginInfo
	Create(AIPluginConfig) (AIClient, error)
}

// AIProviderRuntime is the live provider selected for all Eino based AI
// operations.  The implementation swaps the client atomically so a provider
// change made by an administrator applies to new requests without restarting
// the API process.
type AIProviderRuntime interface {
	AIClient
	AIInspectable
	CurrentConfig() AIPluginConfig
	Configure(context.Context, AIPluginConfig) error
}

// AIWorkflowProviderRuntime keeps the Harness sidecar's model provider in
// sync with the platform provider.  Implementations may reject a change while
// an active workflow is still using the sidecar.
type AIWorkflowProviderRuntime interface {
	ConfigureProvider(context.Context, AIPluginConfig) error
}

// AIProviderConfigStore persists the selected provider independently from the
// process environment.  The store is deliberately small because the active
// provider is a single platform-wide setting; API responses redact API keys.
type AIProviderConfigStore interface {
	LoadAIProviderConfig(context.Context) (AIPluginConfig, bool, error)
	SaveAIProviderConfig(context.Context, AIPluginConfig) error
}

// AIWorkflowPlugin describes a business workflow exposed by an external AI
// runtime. Provider plugins and workflow plugins deliberately use separate
// contracts: providers generate text, workflows may orchestrate read-only MCP
// tools and stream progress events.
type AIWorkflowPlugin struct {
	SchemaVersion    int      `json:"schemaVersion,omitempty"`
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	Version          string   `json:"version,omitempty"`
	DefaultModel     string   `json:"defaultModel,omitempty"`
	MaxTokens        int      `json:"maxTokens,omitempty"`
	Enabled          bool     `json:"enabled"`
	Capabilities     []string `json:"capabilities,omitempty"`
	KnowledgeEnabled bool     `json:"knowledgeEnabled"`
}

type AIWorkflowManifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Version       string   `json:"version"`
	Enabled       bool     `json:"enabled"`
	Persona       string   `json:"persona"`
	DefaultModel  string   `json:"defaultModel"`
	MaxTokens     int      `json:"maxTokens"`
	Capabilities  []string `json:"capabilities"`
	AllowedTools  []string `json:"allowedTools"`
}

type AIWorkflowRequest struct {
	RunID          string `json:"runId"`
	ConversationID string `json:"conversationId"`
	WorkflowID     string `json:"workflowId"`
	Question       string `json:"question"`
	MCPURL         string `json:"mcpUrl"`
	Model          string `json:"model,omitempty"`
	MaxTokens      int    `json:"maxTokens"`
	MCPToken       string `json:"-"`
}

type AIWorkflowEvent struct {
	Type       string         `json:"type"`
	RunID      string         `json:"runId,omitempty"`
	WorkflowID string         `json:"workflowId,omitempty"`
	Model      string         `json:"model,omitempty"`
	Text       string         `json:"text,omitempty"`
	Delta      string         `json:"delta,omitempty"`
	Answer     string         `json:"answer,omitempty"`
	Message    string         `json:"message,omitempty"`
	Tool       string         `json:"tool,omitempty"`
	CallID     string         `json:"callId,omitempty"`
	Status     string         `json:"status,omitempty"`
	Success    *bool          `json:"success,omitempty"`
	Code       string         `json:"code,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

type AIWorkflowResult struct {
	RunID      string `json:"runId"`
	WorkflowID string `json:"workflowId,omitempty"`
	Model      string `json:"model,omitempty"`
	Answer     string `json:"answer"`
}

type AIWorkflowRuntime interface {
	ListWorkflows(context.Context) ([]AIWorkflowPlugin, error)
	StreamChat(context.Context, AIWorkflowRequest, func(AIWorkflowEvent) error) (AIWorkflowResult, error)
	Health(context.Context) error
}

type AIWorkflowManager interface {
	SaveWorkflow(context.Context, AIWorkflowManifest) (AIWorkflowPlugin, error)
}

// AIWorkflowAdminManager exposes the full manifest catalog and destructive
// operations to the platform administration API. The public runtime contract
// intentionally remains read-only and only returns enabled plugin metadata.
type AIWorkflowAdminManager interface {
	ListWorkflowManifests(context.Context) ([]AIWorkflowManifest, error)
	DeleteWorkflow(context.Context, string) error
}

type KnowledgeBase interface {
	Index(context.Context, string, string, string, []byte) error
	Search(context.Context, string, string, int) ([]string, error)
	Health(context.Context) error
}

type KnowledgeIndexInput struct {
	TenantID       string
	WorkflowID     string
	ProductID      string
	Category       string
	Tags           []string
	DocumentID     string
	ChunkID        string
	ChunkIndex     int
	StartChar      int
	EndChar        int
	CharacterCount int
	OverlapChars   int
	Content        []byte
}

type KnowledgeSearchRequest struct {
	TenantID   string
	WorkflowID string
	Question   string
	ProductIDs []string
	Categories []string
	Tags       []string
	Limit      int
	MinScore   float64
}

type KnowledgeHit struct {
	DocumentID string   `json:"documentId,omitempty"`
	ChunkID    string   `json:"chunkId,omitempty"`
	WorkflowID string   `json:"workflowId,omitempty"`
	ProductID  string   `json:"productId,omitempty"`
	Category   string   `json:"category,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Content    string   `json:"content"`
	Score      float64  `json:"score"`
}

// FilteredKnowledgeBase is implemented by indexes that support workflow-bound
// metadata filters. Keeping it separate preserves compatibility with custom
// KnowledgeBase adapters.
type FilteredKnowledgeBase interface {
	IndexKnowledge(context.Context, KnowledgeIndexInput) error
	SearchKnowledge(context.Context, KnowledgeSearchRequest) ([]KnowledgeHit, error)
}

// InspectableKnowledgeBase exposes the stored text slices without exposing
// the high-dimensional embedding vector itself. It is used by the knowledge
// management UI to explain how a document was indexed.
type InspectableKnowledgeBase interface {
	ListKnowledgeChunks(context.Context, string, string) ([]model.KnowledgeChunk, error)
}

type Clock interface{ Now() time.Time }
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
