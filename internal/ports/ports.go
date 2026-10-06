package ports

import (
	"context"
	"errors"
	"io"
	"strings"
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
	// CountLimit, when positive, stops CountRawIndexes at this many rows.
	CountLimit int
	// After, when set, makes ListRawIndexes return the rows that follow this
	// one in the listing order (newest first) and ignore Offset, so paging
	// forward does not rescan the skipped rows. It does not affect counts.
	After *RawCursor
}

// RawCursor marks a row of the raw message listing by its sort key.
type RawCursor struct {
	ReceivedAt int64
	MessageID  string
}

type AlarmFilter struct {
	Summary                                   bool // omit telemetry details/camera payloads for notifications and AI summaries
	TenantID, DeviceID, Status, Level, Source string
	Start, End                                int64
	Limit, Offset                             int
	// DeviceIDs, when non-nil, restricts results to these devices.
	DeviceIDs []string
	// AlarmType, when set, restricts results to one alarm type.
	AlarmType string
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
	// AlarmOverviewCounts counts matching alarms by status, level and source;
	// Recent counts those last triggered at or after since.
	AlarmOverviewCounts(ctx context.Context, f AlarmFilter, since int64) (model.AlarmOverview, error)
	// AIAnalysisOutcomes counts verified alarms by the AI risk level they had
	// when verified, the verification result and the prompt version; an empty
	// promptVersion matches every version.
	AIAnalysisOutcomes(ctx context.Context, f AlarmFilter, promptVersion string) ([]model.AIAnalysisOutcome, error)
}

// DeviceOverviewStore counts registered devices and their states in the
// store. With restrict, only deviceIDs are counted.
type DeviceOverviewStore interface {
	DeviceOverviewCounts(ctx context.Context, tenant string, restrict bool, deviceIDs []string) (model.DeviceOverview, error)
}

// Repository is the platform store: the domain stores below plus resource
// deletion, external data and lifecycle. Callers should depend on the
// smallest domain store they use.
type Repository interface {
	ExternalDataStore() externaldata.Store
	DeleteResource(context.Context, string, string, string) error
	DeleteProtocolRelease(context.Context, string, string, string) error
	AccessStore
	MessageTopicStore
	FireSafetyStore
	SiteStore
	AlarmReportStore
	DeviceOverviewStore
	ObjectCleanupStore
	OnboardingStore
	DeviceStore
	ProductStore
	ProtocolStore
	RawIndexStore
	StandardMessageStore
	DeviceStateStore
	RuleStore
	AlarmStore
	VideoEventStore
	CameraMappingStore
	AIStore
	KnowledgeStore
	OperationsStore
	Health(context.Context) error
	Close() error
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
	// OneShot marks a run without conversation history (business runs), which
	// any Harness instance may serve when its preferred instance is busy.
	OneShot bool `json:"-"`
	// Timeout bounds the whole run; zero uses the client default.
	Timeout time.Duration `json:"-"`
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
	// Usage and ToolCalls are reported on run.completed and run.failed.
	Usage     *model.AIUsage `json:"usage,omitempty"`
	ToolCalls int            `json:"toolCalls,omitempty"`
	// WorkflowVersion is the Agent manifest version, sent on run.started.
	WorkflowVersion string `json:"workflowVersion,omitempty"`
}

type AIWorkflowResult struct {
	RunID      string `json:"runId"`
	WorkflowID string `json:"workflowId,omitempty"`
	Model      string `json:"model,omitempty"`
	Answer     string `json:"answer"`
	// Usage is the provider's token usage; UsageReported is false when the
	// Harness did not report it.
	Usage         model.AIUsage `json:"usage"`
	UsageReported bool          `json:"usageReported,omitempty"`
	ToolCalls     int           `json:"toolCalls,omitempty"`
	// WorkflowVersion is the Agent manifest version that handled the run.
	WorkflowVersion string `json:"workflowVersion,omitempty"`
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

// BuiltinAIWorkflowIDs are the read-only Agents shipped with the Harness.
var BuiltinAIWorkflowIDs = []string{"alarm-handler", "ops-assistant", "system-observer", "device-health-inspector", "protocol-assistant", "rule-drafter"}

// IsBuiltinAIWorkflow reports whether id names a shipped, read-only Agent.
func IsBuiltinAIWorkflow(id string) bool {
	for _, builtin := range BuiltinAIWorkflowIDs {
		if id == builtin {
			return true
		}
	}
	return false
}

// StoredAIWorkflowManifest is the desired state of one dynamic Agent. A
// deleted entry is kept so an instance that missed the delete is cleaned up
// instead of spreading the Agent again.
type StoredAIWorkflowManifest struct {
	ID       string
	Manifest AIWorkflowManifest
	Deleted  bool
}

// AIWorkflowManifestStore keeps the dynamic Agents every Harness instance is
// reconciled to; the instances' own plugin directories are copies.
type AIWorkflowManifestStore interface {
	ListAIWorkflowManifests(context.Context) ([]StoredAIWorkflowManifest, error)
	SaveAIWorkflowManifest(context.Context, StoredAIWorkflowManifest) error
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
	// KeywordOnly marks a hit found by keyword matching alone because the
	// vector service was unavailable; Score is then the keyword match share.
	KeywordOnly bool `json:"keywordOnly,omitempty"`
}

// IsTransient reports errors that mark themselves as worth retrying later
// (an unreachable or rate-limited external service).
func IsTransient(err error) bool {
	var t interface{ Transient() bool }
	return errors.As(err, &t) && t.Transient()
}

// Reranker orders retrieved passages by relevance to a question; scores are
// in (0, 1) and follow the document order.
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]float64, error)
}

// MaxKnowledgeQueryRunes bounds a retrieval question: it is embedded as one
// input, and a question longer than a few sentences only dilutes the match.
const MaxKnowledgeQueryRunes = 512

// BoundKnowledgeQuery trims a retrieval question to MaxKnowledgeQueryRunes.
func BoundKnowledgeQuery(q string) string {
	q = strings.TrimSpace(q)
	if r := []rune(q); len(r) > MaxKnowledgeQueryRunes {
		q = strings.TrimSpace(string(r[:MaxKnowledgeQueryRunes]))
	}
	return q
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
// and serializes rebuilds between API replicas. Document jobs share a lock
// that only a rebuild excludes, so replicas index documents in parallel while
// a rebuild never swaps the index under a running job.
type KnowledgeReindexStore interface {
	ListAllKnowledgeDocs(context.Context) ([]model.KnowledgeDoc, error)
	TryKnowledgeReindexLock(context.Context) (release func(), locked bool, err error)
	TryKnowledgeDocumentLock(context.Context) (release func(), locked bool, err error)
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
