package ports

import (
	"context"
	"io"
	"time"

	"iot-platform/internal/externaldata"
	"iot-platform/internal/model"
)

type RawFilter struct {
	TenantID, ProductID, DeviceID                                        string
	MessageID, Protocol, PayloadFormat, ParseStatus, MessageType, Parser string
	Start, End                                                           int64
	Limit, Offset                                                        int
	// DeviceIDs, when non-nil, restricts results to these devices.
	DeviceIDs []string
}

type AlarmFilter struct {
	Summary                                   bool // omit telemetry details/camera payloads for notifications and AI summaries
	TenantID, DeviceID, Status, Level, Source string
	Start, End                                int64
	Limit, Offset                             int
	// DeviceIDs, when non-nil, restricts results to these devices.
	DeviceIDs []string
}

// ObjectCleanupStore queues object storage files whose records are gone,
// so a Jobs task deletes them even when the first delete failed.
type ObjectCleanupStore interface {
	EnqueueObjectCleanup(ctx context.Context, bucket, key string) error
	PendingObjectCleanups(ctx context.Context, limit int) ([]model.ObjectRef, error)
	FinishObjectCleanup(ctx context.Context, bucket, key string) error
}

// AlarmReportStore aggregates and streams alarms for statistics and exports
// without a row limit.
type AlarmReportStore interface {
	AlarmDispositionStats(context.Context, AlarmFilter) (model.AlarmDispositionStats, error)
	AlarmBreakdown(context.Context, AlarmFilter) (model.AlarmBreakdown, error)
	// EachAlarm calls fn for every matching alarm, newest first, reading in
	// batches; Limit and Offset are ignored. An error from fn stops the scan.
	EachAlarm(context.Context, AlarmFilter, func(model.Alarm) error) error
}

type Repository interface {
	ExternalDataStore() externaldata.Store
	DeleteResource(context.Context, string, string, string) error
	DeleteProtocolRelease(context.Context, string, string, string) error
	AccessStore
	MessageTopicStore
	FireSafetyStore
	SiteStore
	AlarmReportStore
	ObjectCleanupStore
	OnboardingStore
	DashboardCounts(context.Context, string, int64, int64) ([]model.DashboardCount, error)
	DashboardCountsForDevices(context.Context, string, int64, int64, []string) ([]model.DashboardCount, error)
	RegisterProtocolDevice(context.Context, model.DeviceAccessProfile, string, string) (model.ManagedDevice, bool, error)
	RegisterProtocolChild(context.Context, model.DeviceAccessProfile, string, model.ChildIdentity) (model.ManagedDevice, bool, error)
	ListManagedDeviceChildren(context.Context, string, string, int, int) ([]model.ManagedDevice, int, error)
	// ReserveRawMessage returns the canonical reserved message and whether
	// this call created the reservation.
	ReserveRawMessage(context.Context, model.RawMessage) (model.RawMessage, bool, error)
	AcquireExecutionLease(context.Context, string, string, string, string, time.Duration) (model.ExecutionLease, bool, error)
	GetExecutionLease(context.Context, string, string) (model.ExecutionLease, error)
	ReleaseExecutionLease(context.Context, model.ExecutionLease) error
	ListDeviceStateEvents(context.Context, string, string, int, int) ([]model.DeviceStateEvent, int, error)
	ListDeviceMessages(context.Context, string, string, model.MessageType, int, int) ([]model.StandardMessage, int, error)
	ChangeDeviceCredential(context.Context, string, string, string, string, int64) (model.ManagedDevice, model.CredentialRevocation, error)
	ListCredentialRevocations(context.Context, string, string, bool) ([]model.CredentialRevocation, error)
	UpdateCredentialRevocation(context.Context, model.CredentialRevocation) error
	GetDeviceCommand(context.Context, string, string) (model.DeviceCommand, error)
	CreateDeviceCommand(context.Context, model.DeviceCommand) (model.DeviceCommand, bool, error)
	UpdateDeviceCommandDispatch(context.Context, string, string, string, string, int64) error
	CompleteDeviceCommand(context.Context, string, string, string, map[string]any, int64) error
	ListDeviceCommands(context.Context, string, string, int, int) ([]model.DeviceCommand, int, error)
	// Keep atomic onboarding in the repository contract so telemetry/cache
	// decorators forward it to durable storage rather than hiding the capability.
	SaveOnboarding(context.Context, model.OnboardingBundle) error
	SaveProduct(context.Context, model.Product) error
	GetProduct(context.Context, string, string) (model.Product, error)
	GetProductsByIDs(context.Context, string, []string) (map[string]model.Product, error)
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
	SaveDeviceAccessProfile(context.Context, model.DeviceAccessProfile, ...model.AccessProfileSaveOptions) error
	UpdateDeviceAccessStatus(context.Context, model.DeviceAccessProfile, string, string, int64) (bool, error)
	GetDeviceAccessProfile(context.Context, string, string) (model.DeviceAccessProfile, error)
	ListDeviceAccessProfiles(context.Context, string) ([]model.DeviceAccessProfile, error)
	SaveManagedDevice(context.Context, model.ManagedDevice) error
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetManagedDeviceByAccessKey(context.Context, string) (model.ManagedDevice, error)
	ListManagedDevices(context.Context, string) ([]model.ManagedDevice, error)
	ListManagedDevicesPage(context.Context, string, int, int) ([]model.ManagedDevice, int, error)
	CountManagedDeviceChildren(context.Context, string, []string) (map[string]int, error)
	// A nil child scope means all; a non-nil empty scope means none.
	CountManagedDeviceChildrenForDevices(context.Context, string, []string, []string) (map[string]int, error)
	SaveRawIndex(context.Context, model.RawArchiveIndex) (bool, error)
	MarkRawParseResult(context.Context, string, string, int64, string) error
	MarkRawPublished(context.Context, string, string, int64, string) error
	ListPendingRawIndexes(context.Context, int) ([]model.RawArchiveIndex, error)
	GetRawIndex(context.Context, string, string) (model.RawArchiveIndex, error)
	ListRawIndexes(context.Context, RawFilter) ([]model.RawArchiveIndex, error)
	CountRawIndexes(context.Context, RawFilter) (int, error)
	SaveStandardMessage(context.Context, model.StandardMessage) error
	SaveStandardMessageIfAbsent(context.Context, model.StandardMessage) (bool, error)
	// ClaimStandardMessage stores the message if absent and claims it for
	// owner for lease. Another live holder yields Busy; a processed message
	// yields ShouldProcess=false.
	ClaimStandardMessage(ctx context.Context, msg model.StandardMessage, owner string, lease time.Duration) (model.StandardClaim, error)
	// MarkStandardMessageProcessed records completion only for the latest
	// claim token; a taken-over claim returns model.ErrStaleClaim.
	MarkStandardMessageProcessed(ctx context.Context, tenant, messageID string, token int64) error
	GetStandardMessageByRaw(context.Context, string, string) (model.StandardMessage, error)
	GetStandardMessagesByRawIDs(context.Context, string, []string) (map[string]model.StandardMessage, error)
	GetLatestMessage(context.Context, string, string) (model.StandardMessage, error)
	PropertyHistory(context.Context, string, string, string, int64, int64, int) ([]map[string]any, error)
	PropertyHistoryPage(context.Context, string, string, string, int64, int64, int, int) ([]map[string]any, int, error)
	UpsertDeviceState(context.Context, model.DeviceState) error
	GetDeviceState(context.Context, string, string) (model.DeviceState, error)
	// GetDeviceStateFresh reads the stored row with its Version, bypassing caches.
	GetDeviceStateFresh(context.Context, string, string) (model.DeviceState, error)
	// UpsertDeviceStateIf writes only if the stored version equals v.Version
	// (0 = insert if absent) and reports whether it wrote.
	UpsertDeviceStateIf(context.Context, model.DeviceState) (bool, error)
	GetDeviceStatesByIDs(context.Context, string, []string) (map[string]model.DeviceState, error)
	// ListOfflineDue returns up to limit states of every tenant whose
	// OfflineCheckAt is set and before now.
	ListOfflineDue(ctx context.Context, now int64, limit int) ([]model.DeviceState, error)
	ListDeviceStates(context.Context, string) ([]model.DeviceState, error)
	ListDeviceStatesPage(context.Context, string, int, int) ([]model.DeviceState, int, error)
	ListDeviceStatesForDevicesPage(context.Context, string, []string, int, int) ([]model.DeviceState, int, error)
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
	ApplyComponentAlarm(context.Context, model.Alarm, model.ComponentAlarmState) (model.Alarm, string, error)
	UpsertAlarm(context.Context, model.Alarm) (model.Alarm, bool, error)
	// UpsertExternalAlarm uses an external event's deterministic alarm identity.
	// It returns created and triggerChanged, preserving terminal states and
	// committing each new trigger with its report event atomically.
	UpsertExternalAlarm(context.Context, model.Alarm) (model.Alarm, bool, bool, error)
	// Alarm upserts and ApplyComponentAlarm commit the alarm report event with the
	// alarm. DrainOutbox publishes pending events in order and removes each one
	// after publish succeeds, stopping at the first failure.
	DrainOutbox(ctx context.Context, limit int, publish func(model.OutboxEvent) error) (int, error)
	GetAlarm(context.Context, string, string) (model.Alarm, error)
	ListAlarms(context.Context, AlarmFilter) ([]model.Alarm, error)
	CountAlarms(context.Context, AlarmFilter) (int, error)
	HasOpenAlarm(context.Context, string, string) (bool, error)
	// LoadDeviceStateWithAlarms reads a device's state (model.ErrNotFound
	// when absent) and whether it has an open alarm, in one round trip.
	LoadDeviceStateWithAlarms(context.Context, string, string) (model.DeviceState, bool, error)
	// CompleteStandardMessage writes state when it is not nil, checked
	// against its version like UpsertDeviceStateIf, and marks the message
	// processed under its claim token in the same statement; the mark is
	// only made when the state was written. false means the state version
	// changed and nothing was written. A stale or lost claim after a written
	// state returns model.ErrStaleClaim or model.ErrNotFound.
	CompleteStandardMessage(ctx context.Context, state *model.DeviceState, tenant, messageID string, token int64) (bool, error)
	UpdateAlarm(context.Context, model.Alarm) error
	// UpdateAlarmIf writes only if the stored version equals v.Version.
	UpdateAlarmIf(context.Context, model.Alarm) (bool, error)
	SaveVideoEvent(context.Context, model.VideoAlarmEvent) (bool, error)
	GetVideoEvent(context.Context, string, string) (model.VideoAlarmEvent, error)
	UpdateVideoEvent(context.Context, model.VideoAlarmEvent) error
	ListPendingVideoEvents(context.Context, int) ([]model.VideoAlarmEvent, error)
	SaveVideoCameraMapping(context.Context, model.VideoCameraMapping) error
	GetVideoCameraMapping(context.Context, string, string) (model.VideoCameraMapping, error)
	ListVideoCameraMappings(context.Context, string) ([]model.VideoCameraMapping, error)
	ListVideoCameraMappingsByDeviceIDs(context.Context, string, []string) (map[string][]model.VideoCameraMapping, error)
	ListVideoCameraMappingsPage(context.Context, string, int, int) ([]model.VideoCameraMapping, int, error)
	ReplaceVideoCameraRelations(context.Context, string, string, []model.VideoCameraRelation) error
	ListVideoCameraRelations(context.Context, string, string) ([]model.VideoCameraRelation, error)
	ListVideoCameraRelationsByTarget(context.Context, string, string, string) ([]model.VideoCameraRelation, error)
	SaveAIAnalysis(context.Context, model.AIAnalysis) error
	GetAIAnalysis(ctx context.Context, tenantID, alarmID, knowledgeScope string) (model.AIAnalysis, error)
	// CreateHealthInspectionJob returns false when the tenant already has a
	// running inspection; at most one runs per tenant across all replicas.
	CreateHealthInspectionJob(context.Context, model.HealthInspectionJob) (bool, error)
	// UpdateRunningHealthInspectionJob changes a job only while the stored copy
	// is still running, so a job already marked interrupted is not revived.
	UpdateRunningHealthInspectionJob(context.Context, model.HealthInspectionJob) (bool, error)
	// LatestHealthInspectionJob returns the newest job, optionally with status.
	LatestHealthInspectionJob(ctx context.Context, tenantID, status string) (model.HealthInspectionJob, error)
	// Summary never loads device detail rows. Pages address an immutable report ID.
	LatestHealthInspectionSummary(context.Context, string, string) (model.HealthInspectionJob, error)
	HealthInspectionPage(context.Context, string, string, int, int) (model.HealthInspectionJob, error)
	// CreateAlarmAnalysisJob returns false while a job for the same alarm and
	// knowledge scope is running; finished jobs of that alarm and scope are
	// replaced, so only the newest result is kept.
	CreateAlarmAnalysisJob(context.Context, model.AlarmAnalysisJob) (bool, error)
	// UpdateRunningAlarmAnalysisJob changes a job only while it is still running.
	UpdateRunningAlarmAnalysisJob(context.Context, model.AlarmAnalysisJob) (bool, error)
	LatestAlarmAnalysisJob(ctx context.Context, tenantID, alarmID, knowledgeScope string) (model.AlarmAnalysisJob, error)
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

	// ListManagedDevicesFiltered filters before pagination and returns the filtered total.
	ListManagedDevicesFiltered(context.Context, DeviceFilter, int, int) ([]model.ManagedDevice, int, error)
	// SwitchProductProtocol writes a template's protocol binding, protocol reference
	// and compatibility package in one transaction.
	SwitchProductProtocol(context.Context, model.ProtocolSwitch) error
}

type Archive interface {
	PutObject(context.Context, string, string, io.Reader, int64, string) (string, error)
	GetObject(context.Context, string, string) (io.ReadCloser, error)
	Health(context.Context) error
}

type ObjectDeleter interface {
	DeleteObject(context.Context, string, string) error
}
type KnowledgeDocumentDeleter interface {
	DeleteKnowledgeDocument(context.Context, string, string, string) error
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
	Chat(context.Context, string, string) (string, error)
	Health(context.Context) error
}

type AIPluginConfig struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"baseUrl,omitempty"`
	Model     string `json:"model,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
	MaxTokens int    `json:"maxTokens,omitempty"`
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

// AIProviderRuntime supplies model connection tests and health checks. Business
// model calls run through Harness with this provider's synchronized configuration.
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

// EmbeddingConfig is independent of the conversational model. API responses
// must redact APIKey; changing a vector space requires a new knowledge index.
type EmbeddingConfig struct {
	BaseURL          string `json:"baseUrl"`
	Model            string `json:"model"`
	APIKey           string `json:"apiKey,omitempty"`
	Dimensions       int    `json:"dimensions"`
	BatchSize        int    `json:"batchSize"`
	QueryInstruction string `json:"queryInstruction"`
	TimeoutSeconds   int    `json:"timeoutSeconds"`
}

type EmbeddingConfigStore interface {
	LoadEmbeddingConfig(context.Context, bool) (EmbeddingConfig, bool, error)
	SaveEmbeddingConfig(context.Context, EmbeddingConfig, bool) error
}

type EmbeddingRuntime interface {
	CurrentConfig() EmbeddingConfig
	Configure(context.Context, EmbeddingConfig) error
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
	TenantID       string `json:"tenantId,omitempty"`
	Actor          string `json:"actor,omitempty"`
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
	Filename       string   `json:"filename,omitempty"`
	DocumentID     string   `json:"documentId,omitempty"`
	ChunkID        string   `json:"chunkId,omitempty"`
	WorkflowID     string   `json:"workflowId,omitempty"`
	ProductID      string   `json:"productId,omitempty"`
	Category       string   `json:"category,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Content        string   `json:"content"`
	Score          float64  `json:"score"`
	ChunkIndex     int      `json:"chunkIndex"`
	StartChar      int      `json:"startChar"`
	EndChar        int      `json:"endChar"`
	CharacterCount int      `json:"characterCount"`
	OverlapChars   int      `json:"overlapChars"`
}

// KnowledgeDocumentJobs is used by the trusted background indexer. Claims
// and updates are durable and conditional so deleted documents cannot return.
type KnowledgeDocumentJobs interface {
	ClaimKnowledgeDocument(context.Context) (model.KnowledgeDoc, bool, error)
	UpdateKnowledgeDocument(context.Context, model.KnowledgeDoc) (bool, error)
}

type KnowledgeIndexProgress func(done, total int)

func WithKnowledgeIndexProgress(ctx context.Context, progress KnowledgeIndexProgress) context.Context {
	return context.WithValue(ctx, knowledgeIndexProgressKey{}, progress)
}

type knowledgeIndexProgressKey struct{}

func ReportKnowledgeIndexProgress(ctx context.Context, done, total int) {
	if progress, ok := ctx.Value(knowledgeIndexProgressKey{}).(KnowledgeIndexProgress); ok {
		progress(done, total)
	}
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

// BatchKnowledgeBase indexes every chunk of one document in a single call so
// the embedding service can batch requests.
type BatchKnowledgeBase interface {
	IndexKnowledgeBatch(context.Context, []KnowledgeIndexInput) error
}

// RebuildableKnowledgeBase is implemented by persistent indexes whose
// vectors depend on the configured embedding model.
type RebuildableKnowledgeBase interface {
	NeedsRebuild(context.Context) (bool, error)
	ResetIndex(context.Context) error
	ActivateIndex(context.Context) error
	EmbeddingModel() string
}

// KnowledgeReindexStore lists documents across tenants for an index rebuild
// and serializes rebuilds between API replicas.
type KnowledgeReindexStore interface {
	ListAllKnowledgeDocs(context.Context) ([]model.KnowledgeDoc, error)
	TryKnowledgeReindexLock(context.Context) (release func(), locked bool, err error)
}

// EmbedPurpose distinguishes retrieval queries from indexed documents; some
// embedding models expect an instruction prefix for queries only.
type EmbedPurpose string

const (
	EmbedQuery    EmbedPurpose = "query"
	EmbedDocument EmbedPurpose = "document"
)

// Embedder turns text into vectors through an external embedding
// service. Model identifies the vector space so stored indexes can detect
// a model change.
type Embedder interface {
	Embed(context.Context, []string, EmbedPurpose) ([][]float32, error)
	Model() string
	Health(context.Context) error
}

type Clock interface{ Now() time.Time }
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
