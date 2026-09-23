package ports /* 声明 ports 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"io"      /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type RawFilter struct { /* 定义 RawFilter 类型。 */
	TenantID, ProductID, DeviceID string /* 执行当前语句并推进处理流程。 */
	Start, End                    int64  /* 执行当前语句并推进处理流程。 */
	Limit, Offset                 int    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AlarmFilter struct { /* 定义 AlarmFilter 类型。 */
	TenantID, DeviceID, Status, Level, Source string /* 执行当前语句并推进处理流程。 */
	Start, End                                int64  /* 执行当前语句并推进处理流程。 */
	Limit, Offset                             int    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Repository interface { /* 定义 Repository 类型。 */
	AccessStore                                                                                                                             /* 执行当前语句并推进处理流程。 */
	DashboardCounts(context.Context, string, int64, int64) ([]model.DashboardCount, error)                                                  /* 执行当前语句并推进处理流程。 */
	DashboardCountsForDevices(context.Context, string, int64, int64, []string) ([]model.DashboardCount, error)                              /* 执行当前语句并推进处理流程。 */
	RegisterProtocolDevice(context.Context, model.DeviceAccessProfile, string, string) (model.ManagedDevice, bool, error)                   /* 执行当前语句并推进处理流程。 */
	RegisterProtocolChild(context.Context, model.DeviceAccessProfile, string, model.ChildIdentity) (model.ManagedDevice, bool, error)       /* 执行当前语句并推进处理流程。 */
	ListManagedDeviceChildren(context.Context, string, string, int, int) ([]model.ManagedDevice, int, error)                                /* 执行当前语句并推进处理流程。 */
	ReserveRawMessage(context.Context, model.RawMessage) (model.RawMessage, error)                                                          /* 执行当前语句并推进处理流程。 */
	AcquireExecutionLease(context.Context, string, string, string, string, time.Duration) (model.ExecutionLease, bool, error)               /* 执行当前语句并推进处理流程。 */
	GetExecutionLease(context.Context, string, string) (model.ExecutionLease, error)                                                        /* 执行当前语句并推进处理流程。 */
	ReleaseExecutionLease(context.Context, model.ExecutionLease) error                                                                      /* 执行当前语句并推进处理流程。 */
	ListDeviceStateEvents(context.Context, string, string, int, int) ([]model.DeviceStateEvent, int, error)                                 /* 执行当前语句并推进处理流程。 */
	ListDeviceMessages(context.Context, string, string, model.MessageType, int, int) ([]model.StandardMessage, int, error)                  /* 执行当前语句并推进处理流程。 */
	ChangeDeviceCredential(context.Context, string, string, string, string, int64) (model.ManagedDevice, model.CredentialRevocation, error) /* 执行当前语句并推进处理流程。 */
	ListCredentialRevocations(context.Context, string, string, bool) ([]model.CredentialRevocation, error)                                  /* 执行当前语句并推进处理流程。 */
	UpdateCredentialRevocation(context.Context, model.CredentialRevocation) error                                                           /* 执行当前语句并推进处理流程。 */
	GetDeviceCommand(context.Context, string, string) (model.DeviceCommand, error)                                                          /* 执行当前语句并推进处理流程。 */
	CreateDeviceCommand(context.Context, model.DeviceCommand) (model.DeviceCommand, bool, error)                                            /* 执行当前语句并推进处理流程。 */
	UpdateDeviceCommandDispatch(context.Context, string, string, string, string, int64) error                                               /* 执行当前语句并推进处理流程。 */
	CompleteDeviceCommand(context.Context, string, string, string, map[string]any, int64) error                                             /* 执行当前语句并推进处理流程。 */
	ListDeviceCommands(context.Context, string, string, int, int) ([]model.DeviceCommand, int, error)                                       /* 执行当前语句并推进处理流程。 */
	// Keep atomic onboarding in the repository contract so telemetry/cache
	// decorators forward it to durable storage rather than hiding the capability.
	SaveOnboarding(context.Context, model.OnboardingBundle) error                                                        /* 执行当前语句并推进处理流程。 */
	SaveProduct(context.Context, model.Product) error                                                                    /* 执行当前语句并推进处理流程。 */
	GetProduct(context.Context, string, string) (model.Product, error)                                                   /* 执行当前语句并推进处理流程。 */
	GetProductsByIDs(context.Context, string, []string) (map[string]model.Product, error)                                /* 执行当前语句并推进处理流程。 */
	ListProducts(context.Context, string) ([]model.Product, error)                                                       /* 执行当前语句并推进处理流程。 */
	ListProductsPage(context.Context, string, int, int) ([]model.Product, int, error)                                    /* 执行当前语句并推进处理流程。 */
	SaveProtocolPackage(context.Context, model.ProtocolPackage) error                                                    /* 执行当前语句并推进处理流程。 */
	GetProtocolPackage(context.Context, string, string) (model.ProtocolPackage, error)                                   /* 执行当前语句并推进处理流程。 */
	ListProtocolPackages(context.Context, string) ([]model.ProtocolPackage, error)                                       /* 执行当前语句并推进处理流程。 */
	ListProtocolPackagesPage(context.Context, string, int, int) ([]model.ProtocolPackage, int, error)                    /* 执行当前语句并推进处理流程。 */
	SaveProtocolDefinition(context.Context, model.ProtocolDefinition) error                                              /* 执行当前语句并推进处理流程。 */
	GetProtocolDefinition(context.Context, string, string) (model.ProtocolDefinition, error)                             /* 执行当前语句并推进处理流程。 */
	ListProtocolDefinitions(context.Context, string) ([]model.ProtocolDefinition, error)                                 /* 执行当前语句并推进处理流程。 */
	CreateProtocolRelease(context.Context, model.ProtocolRelease) error                                                  /* 执行当前语句并推进处理流程。 */
	GetProtocolRelease(context.Context, string, string, string) (model.ProtocolRelease, error)                           /* 执行当前语句并推进处理流程。 */
	ListProtocolReleases(context.Context, string, string) ([]model.ProtocolRelease, error)                               /* 执行当前语句并推进处理流程。 */
	UpdateProtocolReleaseStatus(context.Context, string, string, string, string, int64) error                            /* 执行当前语句并推进处理流程。 */
	CreatePointTableRelease(context.Context, model.PointTableRelease) error                                              /* 执行当前语句并推进处理流程。 */
	GetPointTableRelease(context.Context, string, string, string) (model.PointTableRelease, error)                       /* 执行当前语句并推进处理流程。 */
	SaveProductProtocolBinding(context.Context, model.ProductProtocolBinding) error                                      /* 执行当前语句并推进处理流程。 */
	GetProductProtocolBinding(context.Context, string, string) (model.ProductProtocolBinding, error)                     /* 执行当前语句并推进处理流程。 */
	SaveDeviceAccessProfile(context.Context, model.DeviceAccessProfile) error                                            /* 执行当前语句并推进处理流程。 */
	UpdateDeviceAccessStatus(context.Context, model.DeviceAccessProfile, string, string, int64) (bool, error)            /* 执行当前语句并推进处理流程。 */
	GetDeviceAccessProfile(context.Context, string, string) (model.DeviceAccessProfile, error)                           /* 执行当前语句并推进处理流程。 */
	ListDeviceAccessProfiles(context.Context, string) ([]model.DeviceAccessProfile, error)                               /* 执行当前语句并推进处理流程。 */
	SaveManagedDevice(context.Context, model.ManagedDevice) error                                                        /* 执行当前语句并推进处理流程。 */
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)                                       /* 执行当前语句并推进处理流程。 */
	GetManagedDeviceByAccessKey(context.Context, string) (model.ManagedDevice, error)                                    /* 执行当前语句并推进处理流程。 */
	ListManagedDevices(context.Context, string) ([]model.ManagedDevice, error)                                           /* 执行当前语句并推进处理流程。 */
	ListManagedDevicesPage(context.Context, string, int, int) ([]model.ManagedDevice, int, error)                        /* 执行当前语句并推进处理流程。 */
	CountManagedDeviceChildren(context.Context, string, []string) (map[string]int, error)                                /* 执行当前语句并推进处理流程。 */
	SaveRawIndex(context.Context, model.RawArchiveIndex) (bool, error)                                                   /* 执行当前语句并推进处理流程。 */
	MarkRawParseResult(context.Context, string, string, int64, string) error                                             /* 执行当前语句并推进处理流程。 */
	MarkRawPublished(context.Context, string, string, int64, string) error                                               /* 执行当前语句并推进处理流程。 */
	ListPendingRawIndexes(context.Context, int) ([]model.RawArchiveIndex, error)                                         /* 执行当前语句并推进处理流程。 */
	GetRawIndex(context.Context, string, string) (model.RawArchiveIndex, error)                                          /* 执行当前语句并推进处理流程。 */
	ListRawIndexes(context.Context, RawFilter) ([]model.RawArchiveIndex, error)                                          /* 执行当前语句并推进处理流程。 */
	CountRawIndexes(context.Context, RawFilter) (int, error)                                                             /* 执行当前语句并推进处理流程。 */
	SaveStandardMessage(context.Context, model.StandardMessage) error                                                    /* 执行当前语句并推进处理流程。 */
	SaveStandardMessageIfAbsent(context.Context, model.StandardMessage) (bool, error)                                    /* 执行当前语句并推进处理流程。 */
	ClaimStandardMessage(context.Context, model.StandardMessage) (shouldProcess bool, created bool, err error)           /* 执行当前语句并推进处理流程。 */
	MarkStandardMessageProcessed(context.Context, string, string) error                                                  /* 执行当前语句并推进处理流程。 */
	GetStandardMessageByRaw(context.Context, string, string) (model.StandardMessage, error)                              /* 执行当前语句并推进处理流程。 */
	GetStandardMessagesByRawIDs(context.Context, string, []string) (map[string]model.StandardMessage, error)             /* 执行当前语句并推进处理流程。 */
	GetLatestMessage(context.Context, string, string) (model.StandardMessage, error)                                     /* 执行当前语句并推进处理流程。 */
	PropertyHistory(context.Context, string, string, string, int64, int64, int) ([]map[string]any, error)                /* 执行当前语句并推进处理流程。 */
	PropertyHistoryPage(context.Context, string, string, string, int64, int64, int, int) ([]map[string]any, int, error)  /* 执行当前语句并推进处理流程。 */
	UpsertDeviceState(context.Context, model.DeviceState) error                                                          /* 执行当前语句并推进处理流程。 */
	GetDeviceState(context.Context, string, string) (model.DeviceState, error)                                           /* 执行当前语句并推进处理流程。 */
	GetDeviceStatesByIDs(context.Context, string, []string) (map[string]model.DeviceState, error)                        /* 执行当前语句并推进处理流程。 */
	ListDeviceStates(context.Context, string) ([]model.DeviceState, error)                                               /* 执行当前语句并推进处理流程。 */
	ListDeviceStatesPage(context.Context, string, int, int) ([]model.DeviceState, int, error)                            /* 执行当前语句并推进处理流程。 */
	ListUnregisteredDeviceStatesPage(context.Context, string, int, int) ([]model.DeviceState, int, error)                /* 执行当前语句并推进处理流程。 */
	CountDeviceStates(context.Context, string, bool) (int, int, error)                                                   /* 执行当前语句并推进处理流程。 */
	SaveDeviceStateEvent(context.Context, model.DeviceState) error                                                       /* 执行当前语句并推进处理流程。 */
	SaveRule(context.Context, model.AlarmRule) error                                                                     /* 执行当前语句并推进处理流程。 */
	ListRules(context.Context, string) ([]model.AlarmRule, error)                                                        /* 执行当前语句并推进处理流程。 */
	ListRulesPage(context.Context, string, int, int) ([]model.AlarmRule, int, error)                                     /* 执行当前语句并推进处理流程。 */
	DeleteRule(context.Context, string, string) error                                                                    /* 执行当前语句并推进处理流程。 */
	SaveRulePending(context.Context, string, string, string, int64) error                                                /* 执行当前语句并推进处理流程。 */
	GetRulePending(context.Context, string, string, string) (int64, bool, error)                                         /* 执行当前语句并推进处理流程。 */
	DeleteRulePending(context.Context, string, string, string) error                                                     /* 执行当前语句并推进处理流程。 */
	DeleteRulePendings(context.Context, string, string) error                                                            /* 执行当前语句并推进处理流程。 */
	ApplyComponentAlarm(context.Context, model.Alarm, model.ComponentAlarmState) (model.Alarm, string, error)            /* 执行当前语句并推进处理流程。 */
	UpsertAlarm(context.Context, model.Alarm) (model.Alarm, bool, error)                                                 /* 执行当前语句并推进处理流程。 */
	GetAlarm(context.Context, string, string) (model.Alarm, error)                                                       /* 执行当前语句并推进处理流程。 */
	ListAlarms(context.Context, AlarmFilter) ([]model.Alarm, error)                                                      /* 执行当前语句并推进处理流程。 */
	CountAlarms(context.Context, AlarmFilter) (int, error)                                                               /* 执行当前语句并推进处理流程。 */
	UpdateAlarm(context.Context, model.Alarm) error                                                                      /* 执行当前语句并推进处理流程。 */
	SaveVideoEvent(context.Context, model.VideoAlarmEvent) (bool, error)                                                 /* 执行当前语句并推进处理流程。 */
	UpdateVideoEvent(context.Context, model.VideoAlarmEvent) error                                                       /* 执行当前语句并推进处理流程。 */
	ListPendingVideoEvents(context.Context, int) ([]model.VideoAlarmEvent, error)                                        /* 执行当前语句并推进处理流程。 */
	SaveVideoCameraMapping(context.Context, model.VideoCameraMapping) error                                              /* 执行当前语句并推进处理流程。 */
	GetVideoCameraMapping(context.Context, string, string) (model.VideoCameraMapping, error)                             /* 执行当前语句并推进处理流程。 */
	ListVideoCameraMappings(context.Context, string) ([]model.VideoCameraMapping, error)                                 /* 执行当前语句并推进处理流程。 */
	ListVideoCameraMappingsByDeviceIDs(context.Context, string, []string) (map[string][]model.VideoCameraMapping, error) /* 执行当前语句并推进处理流程。 */
	ListVideoCameraMappingsPage(context.Context, string, int, int) ([]model.VideoCameraMapping, int, error)              /* 执行当前语句并推进处理流程。 */
	ReplaceVideoCameraRelations(context.Context, string, string, []model.VideoCameraRelation) error                      /* 执行当前语句并推进处理流程。 */
	ListVideoCameraRelations(context.Context, string, string) ([]model.VideoCameraRelation, error)                       /* 执行当前语句并推进处理流程。 */
	ListVideoCameraRelationsByTarget(context.Context, string, string, string) ([]model.VideoCameraRelation, error)       /* 执行当前语句并推进处理流程。 */
	SaveAIAnalysis(context.Context, model.AIAnalysis) error                                                              /* 执行当前语句并推进处理流程。 */
	GetAIAnalysis(context.Context, string, string) (model.AIAnalysis, error)                                             /* 执行当前语句并推进处理流程。 */
	SaveKnowledgeDoc(context.Context, model.KnowledgeDoc) error                                                          /* 执行当前语句并推进处理流程。 */
	ListKnowledgeDocs(context.Context, string) ([]model.KnowledgeDoc, error)                                             /* 执行当前语句并推进处理流程。 */
	ListKnowledgeDocsPage(context.Context, string, int, int) ([]model.KnowledgeDoc, int, error)                          /* 执行当前语句并推进处理流程。 */
	SaveWorkflowKnowledgeBinding(context.Context, model.WorkflowKnowledgeBinding) error                                  /* 执行当前语句并推进处理流程。 */
	GetWorkflowKnowledgeBinding(context.Context, string, string) (model.WorkflowKnowledgeBinding, error)                 /* 执行当前语句并推进处理流程。 */
	SaveReplay(context.Context, model.ReplayRequest) error                                                               /* 执行当前语句并推进处理流程。 */
	UpdateReplay(context.Context, model.ReplayRequest) error                                                             /* 执行当前语句并推进处理流程。 */
	GetReplay(context.Context, string) (model.ReplayRequest, error)                                                      /* 执行当前语句并推进处理流程。 */
	SaveAudit(context.Context, model.AuditLog) error                                                                     /* 执行当前语句并推进处理流程。 */
	SaveAIToolCall(context.Context, model.AIToolCallLog) error                                                           /* 执行当前语句并推进处理流程。 */
	Health(context.Context) error                                                                                        /* 执行当前语句并推进处理流程。 */
	Close() error                                                                                                        /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Archive interface { /* 定义 Archive 类型。 */
	PutObject(context.Context, string, string, io.Reader, int64, string) (string, error) /* 执行当前语句并推进处理流程。 */
	GetObject(context.Context, string, string) (io.ReadCloser, error)                    /* 执行当前语句并推进处理流程。 */
	Health(context.Context) error                                                        /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// RawMessageStore keeps the raw device payload available for parsing, replay
// and the daily object-storage backup. It is deliberately separate from
// Archive: MinIO is an object store for completed backups and media, not the
// per-message write path.
type RawMessageStore interface { /* 定义 RawMessageStore 类型。 */
	PutRaw(context.Context, model.RawMessage) (model.RawArchiveIndex, error) /* 执行当前语句并推进处理流程。 */
	GetRaw(context.Context, model.RawArchiveIndex) (model.RawMessage, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// RawMessageDatabase is implemented by the PostgreSQL and ClickHouse
// adapters. The raw store chooses one database per device according to its
// configured or observed reporting frequency.
type RawMessageDatabase interface { /* 定义 RawMessageDatabase 类型。 */
	SaveRawMessage(context.Context, model.RawMessage) error                  /* 执行当前语句并推进处理流程。 */
	GetRawMessage(context.Context, string, string) (model.RawMessage, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// RawMessageReader is used only for reading legacy MinIO raw objects created
// before the database-backed raw log path was introduced.
type RawMessageReader interface { /* 定义 RawMessageReader 类型。 */
	GetRaw(context.Context, model.RawArchiveIndex) (model.RawMessage, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Handler func(context.Context, []byte) error /* 定义 Handler 类型。 */

type EventBus interface { /* 定义 EventBus 类型。 */
	Publish(context.Context, string, string, []byte) error    /* 执行当前语句并推进处理流程。 */
	Subscribe(context.Context, string, string, Handler) error /* 执行当前语句并推进处理流程。 */
	Health(context.Context) error                             /* 执行当前语句并推进处理流程。 */
	Close() error                                             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type RealtimePublisher interface { /* 定义 RealtimePublisher 类型。 */
	Publish(context.Context, string, []byte, byte, bool) error /* 执行当前语句并推进处理流程。 */
	Health(context.Context) error                              /* 执行当前语句并推进处理流程。 */
	Close() error                                              /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIClient interface { /* 定义 AIClient 类型。 */
	AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) /* 执行当前语句并推进处理流程。 */
	Chat(context.Context, string, string) (string, error)                                            /* 执行当前语句并推进处理流程。 */
	RuleDraft(context.Context, string, string) (model.AlarmRule, error)                              /* 执行当前语句并推进处理流程。 */
	Health(context.Context) error                                                                    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// AIJSONGenerator is an optional structured-output capability. Keeping it
// separate from AIClient preserves compatibility with provider implementations
// that only support ordinary chat while allowing protocol and inspection
// workflows to request machine-readable output.
type AIJSONGenerator interface { /* 定义 AIJSONGenerator 类型。 */
	GenerateJSON(context.Context, string, string, string) (string, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// VideoPreviewService resolves direct or vendor-SDK camera sources and, when
// configured, proxies them through ZLMediaKit into a browser playback URL.
type VideoPreviewService interface { /* 定义 VideoPreviewService 类型。 */
	Preview(context.Context, model.VideoCameraMapping) (model.VideoPreview, error) /* 执行当前语句并推进处理流程。 */
	Eligible(model.VideoCameraMapping, []string) bool                              /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIPluginConfig struct { /* 定义 AIPluginConfig 类型。 */
	Provider string `json:"provider"`          /* 执行当前语句并推进处理流程。 */
	BaseURL  string `json:"baseUrl,omitempty"` /* 执行当前语句并推进处理流程。 */
	Model    string `json:"model,omitempty"`   /* 执行当前语句并推进处理流程。 */
	APIKey   string `json:"apiKey,omitempty"`  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIPluginInfo struct { /* 定义 AIPluginInfo 类型。 */
	ID             string   `json:"id"`                       /* 执行当前语句并推进处理流程。 */
	Name           string   `json:"name"`                     /* 执行当前语句并推进处理流程。 */
	Description    string   `json:"description"`              /* 执行当前语句并推进处理流程。 */
	DefaultBaseURL string   `json:"defaultBaseUrl,omitempty"` /* 执行当前语句并推进处理流程。 */
	DefaultModel   string   `json:"defaultModel,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Model          string   `json:"model,omitempty"`          /* 执行当前语句并推进处理流程。 */
	RequiresAPIKey bool     `json:"requiresApiKey"`           /* 执行当前语句并推进处理流程。 */
	Enabled        bool     `json:"enabled"`                  /* 执行当前语句并推进处理流程。 */
	Capabilities   []string `json:"capabilities"`             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIInspectable interface { /* 定义 AIInspectable 类型。 */
	ProviderInfo() AIPluginInfo /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIPluginRegistry interface { /* 定义 AIPluginRegistry 类型。 */
	List() []AIPluginInfo                    /* 执行当前语句并推进处理流程。 */
	Create(AIPluginConfig) (AIClient, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// AIProviderRuntime is the live provider selected for all Eino based AI
// operations.  The implementation swaps the client atomically so a provider
// change made by an administrator applies to new requests without restarting
// the API process.
type AIProviderRuntime interface { /* 定义 AIProviderRuntime 类型。 */
	AIClient                                         /* 执行当前语句并推进处理流程。 */
	AIInspectable                                    /* 执行当前语句并推进处理流程。 */
	CurrentConfig() AIPluginConfig                   /* 执行当前语句并推进处理流程。 */
	Configure(context.Context, AIPluginConfig) error /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// AIWorkflowProviderRuntime keeps the Harness sidecar's model provider in
// sync with the platform provider.  Implementations may reject a change while
// an active workflow is still using the sidecar.
type AIWorkflowProviderRuntime interface { /* 定义 AIWorkflowProviderRuntime 类型。 */
	ConfigureProvider(context.Context, AIPluginConfig) error /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// AIProviderConfigStore persists the selected provider independently from the
// process environment.  The store is deliberately small because the active
// provider is a single platform-wide setting; API responses redact API keys.
type AIProviderConfigStore interface { /* 定义 AIProviderConfigStore 类型。 */
	LoadAIProviderConfig(context.Context) (AIPluginConfig, bool, error) /* 执行当前语句并推进处理流程。 */
	SaveAIProviderConfig(context.Context, AIPluginConfig) error         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// AIWorkflowPlugin describes a business workflow exposed by an external AI
// runtime. Provider plugins and workflow plugins deliberately use separate
// contracts: providers generate text, workflows may orchestrate read-only MCP
// tools and stream progress events.
type AIWorkflowPlugin struct { /* 定义 AIWorkflowPlugin 类型。 */
	SchemaVersion    int      `json:"schemaVersion,omitempty"` /* 执行当前语句并推进处理流程。 */
	ID               string   `json:"id"`                      /* 执行当前语句并推进处理流程。 */
	Name             string   `json:"name"`                    /* 执行当前语句并推进处理流程。 */
	Description      string   `json:"description,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Version          string   `json:"version,omitempty"`       /* 执行当前语句并推进处理流程。 */
	DefaultModel     string   `json:"defaultModel,omitempty"`  /* 执行当前语句并推进处理流程。 */
	MaxTokens        int      `json:"maxTokens,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Enabled          bool     `json:"enabled"`                 /* 执行当前语句并推进处理流程。 */
	Capabilities     []string `json:"capabilities,omitempty"`  /* 执行当前语句并推进处理流程。 */
	KnowledgeEnabled bool     `json:"knowledgeEnabled"`        /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIWorkflowManifest struct { /* 定义 AIWorkflowManifest 类型。 */
	SchemaVersion int      `json:"schemaVersion"` /* 执行当前语句并推进处理流程。 */
	ID            string   `json:"id"`            /* 执行当前语句并推进处理流程。 */
	Name          string   `json:"name"`          /* 执行当前语句并推进处理流程。 */
	Description   string   `json:"description"`   /* 执行当前语句并推进处理流程。 */
	Version       string   `json:"version"`       /* 执行当前语句并推进处理流程。 */
	Enabled       bool     `json:"enabled"`       /* 执行当前语句并推进处理流程。 */
	Persona       string   `json:"persona"`       /* 执行当前语句并推进处理流程。 */
	DefaultModel  string   `json:"defaultModel"`  /* 执行当前语句并推进处理流程。 */
	MaxTokens     int      `json:"maxTokens"`     /* 执行当前语句并推进处理流程。 */
	Capabilities  []string `json:"capabilities"`  /* 执行当前语句并推进处理流程。 */
	AllowedTools  []string `json:"allowedTools"`  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIWorkflowRequest struct { /* 定义 AIWorkflowRequest 类型。 */
	RunID          string `json:"runId"`           /* 执行当前语句并推进处理流程。 */
	ConversationID string `json:"conversationId"`  /* 执行当前语句并推进处理流程。 */
	WorkflowID     string `json:"workflowId"`      /* 执行当前语句并推进处理流程。 */
	Question       string `json:"question"`        /* 执行当前语句并推进处理流程。 */
	MCPURL         string `json:"mcpUrl"`          /* 执行当前语句并推进处理流程。 */
	Model          string `json:"model,omitempty"` /* 执行当前语句并推进处理流程。 */
	MaxTokens      int    `json:"maxTokens"`       /* 执行当前语句并推进处理流程。 */
	MCPToken       string `json:"-"`               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIWorkflowEvent struct { /* 定义 AIWorkflowEvent 类型。 */
	Type       string         `json:"type"`                 /* 执行当前语句并推进处理流程。 */
	RunID      string         `json:"runId,omitempty"`      /* 执行当前语句并推进处理流程。 */
	WorkflowID string         `json:"workflowId,omitempty"` /* 执行当前语句并推进处理流程。 */
	Model      string         `json:"model,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Text       string         `json:"text,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Delta      string         `json:"delta,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Answer     string         `json:"answer,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Message    string         `json:"message,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Tool       string         `json:"tool,omitempty"`       /* 执行当前语句并推进处理流程。 */
	CallID     string         `json:"callId,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Status     string         `json:"status,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Success    *bool          `json:"success,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Code       string         `json:"code,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Data       map[string]any `json:"data,omitempty"`       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIWorkflowResult struct { /* 定义 AIWorkflowResult 类型。 */
	RunID      string `json:"runId"`                /* 执行当前语句并推进处理流程。 */
	WorkflowID string `json:"workflowId,omitempty"` /* 执行当前语句并推进处理流程。 */
	Model      string `json:"model,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Answer     string `json:"answer"`               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIWorkflowRuntime interface { /* 定义 AIWorkflowRuntime 类型。 */
	ListWorkflows(context.Context) ([]AIWorkflowPlugin, error)                                            /* 执行当前语句并推进处理流程。 */
	StreamChat(context.Context, AIWorkflowRequest, func(AIWorkflowEvent) error) (AIWorkflowResult, error) /* 执行当前语句并推进处理流程。 */
	Health(context.Context) error                                                                         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIWorkflowManager interface { /* 定义 AIWorkflowManager 类型。 */
	SaveWorkflow(context.Context, AIWorkflowManifest) (AIWorkflowPlugin, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// AIWorkflowAdminManager exposes the full manifest catalog and destructive
// operations to the platform administration API. The public runtime contract
// intentionally remains read-only and only returns enabled plugin metadata.
type AIWorkflowAdminManager interface { /* 定义 AIWorkflowAdminManager 类型。 */
	ListWorkflowManifests(context.Context) ([]AIWorkflowManifest, error) /* 执行当前语句并推进处理流程。 */
	DeleteWorkflow(context.Context, string) error                        /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type KnowledgeBase interface { /* 定义 KnowledgeBase 类型。 */
	Index(context.Context, string, string, string, []byte) error   /* 执行当前语句并推进处理流程。 */
	Search(context.Context, string, string, int) ([]string, error) /* 执行当前语句并推进处理流程。 */
	Health(context.Context) error                                  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type KnowledgeIndexInput struct { /* 定义 KnowledgeIndexInput 类型。 */
	TenantID       string   /* 执行当前语句并推进处理流程。 */
	WorkflowID     string   /* 执行当前语句并推进处理流程。 */
	ProductID      string   /* 执行当前语句并推进处理流程。 */
	Category       string   /* 执行当前语句并推进处理流程。 */
	Tags           []string /* 执行当前语句并推进处理流程。 */
	DocumentID     string   /* 执行当前语句并推进处理流程。 */
	ChunkID        string   /* 执行当前语句并推进处理流程。 */
	ChunkIndex     int      /* 执行当前语句并推进处理流程。 */
	StartChar      int      /* 执行当前语句并推进处理流程。 */
	EndChar        int      /* 执行当前语句并推进处理流程。 */
	CharacterCount int      /* 执行当前语句并推进处理流程。 */
	OverlapChars   int      /* 执行当前语句并推进处理流程。 */
	Content        []byte   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type KnowledgeSearchRequest struct { /* 定义 KnowledgeSearchRequest 类型。 */
	TenantID   string   /* 执行当前语句并推进处理流程。 */
	WorkflowID string   /* 执行当前语句并推进处理流程。 */
	Question   string   /* 执行当前语句并推进处理流程。 */
	ProductIDs []string /* 执行当前语句并推进处理流程。 */
	Categories []string /* 执行当前语句并推进处理流程。 */
	Tags       []string /* 执行当前语句并推进处理流程。 */
	Limit      int      /* 执行当前语句并推进处理流程。 */
	MinScore   float64  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type KnowledgeHit struct { /* 定义 KnowledgeHit 类型。 */
	DocumentID string   `json:"documentId,omitempty"` /* 执行当前语句并推进处理流程。 */
	ChunkID    string   `json:"chunkId,omitempty"`    /* 执行当前语句并推进处理流程。 */
	WorkflowID string   `json:"workflowId,omitempty"` /* 执行当前语句并推进处理流程。 */
	ProductID  string   `json:"productId,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Category   string   `json:"category,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Tags       []string `json:"tags,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Content    string   `json:"content"`              /* 执行当前语句并推进处理流程。 */
	Score      float64  `json:"score"`                /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// FilteredKnowledgeBase is implemented by indexes that support workflow-bound
// metadata filters. Keeping it separate preserves compatibility with custom
// KnowledgeBase adapters.
type FilteredKnowledgeBase interface { /* 定义 FilteredKnowledgeBase 类型。 */
	IndexKnowledge(context.Context, KnowledgeIndexInput) error                       /* 执行当前语句并推进处理流程。 */
	SearchKnowledge(context.Context, KnowledgeSearchRequest) ([]KnowledgeHit, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// InspectableKnowledgeBase exposes the stored text slices without exposing
// the high-dimensional embedding vector itself. It is used by the knowledge
// management UI to explain how a document was indexed.
type InspectableKnowledgeBase interface { /* 定义 InspectableKnowledgeBase 类型。 */
	ListKnowledgeChunks(context.Context, string, string) ([]model.KnowledgeChunk, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Clock interface{ Now() time.Time } /* 定义 Clock 类型。 */
type RealClock struct{}                 /* 定义 RealClock 类型。 */

func (RealClock) Now() time.Time { return time.Now() } /* 定义 Now 函数。 */
