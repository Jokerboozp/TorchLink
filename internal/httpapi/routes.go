package httpapi

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"iot-platform/internal/mcpserver"
	"iot-platform/internal/version"
)

// routeModule registers the routes of one area of the API.
type routeModule func(*Server)

// routeModules are registered in this order; paths, roles and middleware are
// defined by each module.
var routeModules = []routeModule{
	(*Server).accessRoutes,
	(*Server).openAPIRoutes,
	(*Server).messageTopicRoutes,
	(*Server).deletionRoutes,
	(*Server).opsRoutes,
	(*Server).deadLetterRoutes,
	(*Server).notificationRoutes,
	(*Server).videoRoutes,
	(*Server).fireSafetyRoutes,
	(*Server).externalDataRoutes,
	(*Server).externalMediaRoutes,
	(*Server).connectorRoutes,
	(*Server).deviceOperationsRoutes,
	(*Server).deviceRegistryRoutes,
	(*Server).onboardingRoutes,
	(*Server).onboardingTaskRoutes,
	(*Server).templatePreparationRoutes,
	(*Server).deviceIngestRoutes,
	(*Server).platformRoutes,
	(*Server).cameraMappingRoutes,
	(*Server).productRoutes,
	(*Server).protocolRoutes,
	(*Server).rawMessageRoutes,
	(*Server).dashboardRoutes,
	(*Server).ruleRoutes,
	(*Server).alarmDispositionRoutes,
	(*Server).alarmAttachmentRoutes,
	(*Server).siteRoutes,
	(*Server).alarmRoutes,
	(*Server).backupRoutes,
	(*Server).aiRoutes,
	(*Server).knowledgeRoutes,
	(*Server).conversationRoutes,
	(*Server).mcpRoutes,
}

// routes registers every route module and the fallback handlers.
func (s *Server) routes() {
	for _, register := range routeModules {
		register(s)
	}
	s.router.NoRoute(func(c *gin.Context) { ginProblem(c, http.StatusNotFound, "route not found") })
	s.router.NoMethod(func(c *gin.Context) { ginProblem(c, http.StatusMethodNotAllowed, "method not allowed") })
}

// connectorRoutes registers connector types and status.
func (s *Server) connectorRoutes() {
	s.router.GET("/api/v1/connectors/types", s.authorize("viewer"), s.endpoint(s.connectorTypes))
	s.router.GET("/api/v1/connectors", s.authorize("viewer"), s.endpoint(s.connectorStatus))
}

// deviceRegistryRoutes registers registered devices, children, credentials and test devices.
func (s *Server) deviceRegistryRoutes() {
	s.router.GET("/api/v1/device-registry/:id/connection", s.authorize("viewer"), s.endpoint(s.deviceConnection, "id"))
	s.router.DELETE("/api/v1/device-registry/:id/credentials", s.authorize("admin"), s.endpoint(s.disableDeviceCredential, "id"))
	s.router.GET("/api/v1/device-registry/:id/children", s.authorize("viewer"), s.endpoint(s.deviceChildren, "id"))
	s.router.GET("/api/v1/device-registry", s.authorize("viewer"), s.endpoint(s.deviceRegistry))
	s.router.POST("/api/v1/device-registry", s.authorize("operator"), s.endpoint(s.saveManagedDevice))
	s.router.POST("/api/v1/device-registry/:id/children", s.authorize("operator"), s.endpoint(s.registerConfiguredChild, "id"))
	s.router.PUT("/api/v1/device-registry/:id", s.authorize("operator"), s.endpoint(s.saveManagedDevice, "id"))
	s.router.POST("/api/v1/test-devices/provision", s.authorize("operator"), s.endpoint(s.provisionTestDevice))
	s.router.POST("/api/v1/discovered-devices/:id/register", s.authorize("operator"), s.endpoint(s.registerDiscoveredDevice, "id"))
	s.router.POST("/api/v1/device-registry/:id/credentials", s.authorize("admin"), s.endpoint(s.rotateDeviceCredential, "id"))
	s.router.POST("/api/v1/device-registry/:id/debug", s.authorize("operator"), s.endpoint(s.debugDeviceIngest, "id"))
}

// onboardingRoutes registers device onboarding preflight and enrollment.
func (s *Server) onboardingRoutes() {
	s.router.GET("/api/v1/onboarding/preflight", s.authorize("operator"), s.endpoint(s.onboardingPreflight))
	s.router.POST("/api/v1/onboarding", s.authorize("operator"), s.endpoint(s.onboardingEnroll))
}

// deviceIngestRoutes registers device data received over HTTP.
func (s *Server) deviceIngestRoutes() {
	s.router.POST("/api/v1/device-ingest/standard/:tenantId/:productId/:deviceId/:kind", s.endpoint(s.standardDeviceIngest, "tenantId", "productId", "deviceId", "kind"))
	s.router.POST("/api/v1/device-ingest/:deviceId", s.endpoint(s.deviceIngest, "deviceId"))
}

// platformRoutes registers login, password change, health, metrics and MQTT credentials.
func (s *Server) platformRoutes() {
	s.router.POST("/api/v1/auth/login", s.endpoint(s.login))
	s.router.POST("/api/v1/auth/password", s.endpoint(s.changeOwnPassword))
	s.router.GET("/health/live", s.endpoint(func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]string{"status": "ok", "version": version.Version})
	}))
	s.router.GET("/health/ready", s.endpoint(s.ready))
	s.router.GET("/metrics", s.endpoint(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, s.metrics.Prometheus())
	}))
	s.router.POST("/api/v1/mqtt/token", s.authorize("viewer"), s.endpoint(s.mqttToken))
	s.router.POST("/api/v1/mqtt/load-token", s.authorize("admin"), s.endpoint(s.mqttLoadToken))
	s.router.POST("/api/v1/device-mqtt/token", s.endpoint(s.deviceMQTTToken))
}

// cameraMappingRoutes registers camera records and their device relations.
func (s *Server) cameraMappingRoutes() {
	s.router.GET("/api/v1/integrations/video/cameras", s.authorize("viewer"), s.endpoint(s.videoCameras))
	s.router.GET("/api/v1/integrations/video/relations", s.authorize("viewer"), s.endpoint(s.videoRelations))
	s.router.POST("/api/v1/integrations/video/cameras", s.authorize("operator"), s.endpoint(s.saveVideoCamera))
	s.router.PUT("/api/v1/integrations/video/cameras/:id", s.authorize("operator"), s.endpoint(s.saveVideoCamera, "id"))
}

// productRoutes registers device templates.
func (s *Server) productRoutes() {
	s.router.GET("/api/v1/products", s.authorize("viewer"), s.endpoint(s.products))
	s.router.GET("/api/v1/products/protocol-binding-check", s.authorize("viewer"), s.endpoint(s.productBindingCheck))
	s.router.POST("/api/v1/products", s.authorize("operator"), s.endpoint(s.saveProduct))
	s.router.PUT("/api/v1/products/:id", s.authorize("operator"), s.endpoint(s.saveProduct, "id"))
}

// protocolRoutes registers protocol packages, releases, template bindings and access profiles.
func (s *Server) protocolRoutes() {
	s.router.GET("/api/v1/protocol-packages", s.authorize("viewer"), s.endpoint(s.protocolPackages))
	s.router.POST("/api/v1/protocol-packages", s.authorize("operator"), s.endpoint(s.saveProtocolPackage))
	s.router.PUT("/api/v1/protocol-packages/:id", s.authorize("operator"), s.endpoint(s.saveProtocolPackage, "id"))
	s.router.POST("/api/v1/protocol-packages/:id/test", s.authorize("operator"), s.endpoint(s.testProtocolPackage, "id"))
	s.router.GET("/api/v2/protocols", s.authorize("viewer"), s.endpoint(s.protocolDefinitionsV2))
	s.router.GET("/api/v2/protocol-source-template", s.authorize("viewer"), s.endpoint(s.protocolSourceTemplate))
	s.router.POST("/api/v2/protocols/:id/source-releases", s.authorize("operator"), s.endpoint(s.uploadProtocolSource, "id"))
	s.router.POST("/api/v2/protocols", s.authorize("operator"), s.endpoint(s.saveProtocolDefinitionV2))
	s.router.POST("/api/v2/protocols/:id/releases/:version/preview", s.authorize("operator"), s.endpoint(s.previewGeneratedRelease, "id", "version"))
	s.router.GET("/api/v2/protocols/:id/releases", s.authorize("viewer"), s.endpoint(s.protocolReleasesV2, "id"))
	s.router.GET("/api/v2/protocols/:id/releases/:version/source", s.authorize("operator"), s.endpoint(s.downloadProtocolSourceV2, "id", "version"))
	s.router.GET("/api/v2/protocols/:id/releases/:version/package", s.authorize("operator"), s.endpoint(s.downloadProtocolPackageV2, "id", "version"))
	s.router.POST("/api/v2/protocols/:id/releases", s.authorize("operator"), s.endpoint(s.createProtocolReleaseV2, "id"))
	s.router.POST("/api/v2/protocols/:id/package-releases", s.authorize("operator"), s.endpoint(s.uploadProtocolPackageV2, "id"))
	s.router.POST("/api/v2/protocols/:id/releases/:version/publish", s.authorize("operator"), s.endpoint(s.publishProtocolReleaseV2, "id", "version"))
	s.router.GET("/api/v2/products/:id/protocol-binding", s.authorize("viewer"), s.endpoint(s.getProductProtocolBinding, "id"))
	s.router.POST("/api/v2/products/:id/protocol-binding", s.authorize("operator"), s.endpoint(s.bindProductProtocolV2, "id"))
	s.router.POST("/api/v2/products/:id/protocol-binding/rollback", s.authorize("operator"), s.endpoint(s.rollbackProductProtocolV2, "id"))
	s.router.POST("/api/v2/modbus-tcp/import", s.authorize("operator"), s.endpoint(s.importModbusTCPV2))
	s.router.GET("/api/v2/device-access-profiles", s.authorize("viewer"), s.endpoint(s.deviceAccessProfilesV2))
	s.router.POST("/api/v2/device-access-profiles", s.authorize("operator"), s.endpoint(s.saveDeviceAccessProfileV2))
	s.router.PUT("/api/v2/device-access-profiles/:id", s.authorize("operator"), s.endpoint(s.saveDeviceAccessProfileV2, "id"))
	s.router.POST("/api/v2/device-access-profiles/:id/test", s.authorize("operator"), s.endpoint(s.testDeviceAccessProfileV2, "id"))
	s.router.POST("/api/v2/device-access-profiles/:id/devices/:deviceId/commands", s.authorize("operator"), s.endpoint(s.protocolDeviceCommand, "id", "deviceId"))
}

// rawMessageRoutes registers raw message archive, download and replay.
func (s *Server) rawMessageRoutes() {
	s.router.POST("/api/v1/raw-messages", s.authorize("operator"), s.endpoint(s.ingestRaw))
	s.router.GET("/api/v1/raw-messages", s.authorize("viewer"), s.endpoint(s.listRaw))
	s.router.POST("/api/v1/raw-messages/download", s.authorize("viewer"), s.endpoint(s.downloadRawBatch))
	s.router.GET("/api/v1/raw-messages/:id", s.authorize("viewer"), s.endpoint(s.rawDetail, "id"))
	s.router.GET("/api/v1/raw-messages/:id/download", s.authorize("viewer"), s.endpoint(s.downloadRaw, "id"))
	s.router.POST("/api/v1/raw-messages/replay", s.authorize("admin"), s.endpoint(s.startReplay))
	s.router.GET("/api/v1/replays/:id", s.authorize("viewer"), s.endpoint(s.getReplay, "id"))
}

// dashboardRoutes registers dashboard, device lists, latest data and history.
func (s *Server) dashboardRoutes() {
	s.router.GET("/api/v1/dashboard", s.authorize("viewer"), s.endpoint(s.dashboard))
	s.router.GET("/api/v1/devices", s.authorize("viewer"), s.endpoint(s.devices))
	s.router.GET("/api/v1/devices/:deviceId/latest", s.authorize("viewer"), s.endpoint(s.deviceLatest, "deviceId"))
	s.router.GET("/api/v1/devices/:deviceId/properties/history", s.authorize("viewer"), s.endpoint(s.history, "deviceId"))
	s.router.POST("/api/v1/device-states", s.authorize("operator"), s.endpoint(s.stateEvent))
}

// ruleRoutes registers alarm rules.
func (s *Server) ruleRoutes() {
	s.router.GET("/api/v1/rules", s.authorize("viewer"), s.endpoint(s.rules))
	s.router.GET("/api/v1/rules/fields", s.authorize("viewer"), s.endpoint(s.ruleFields))
	s.router.POST("/api/v1/rules", s.authorize("operator"), s.endpoint(s.saveRule))
	s.router.PUT("/api/v1/rules/:id", s.authorize("operator"), s.endpoint(s.saveRule, "id"))
	s.router.DELETE("/api/v1/rules/:id", s.authorize("operator"), s.endpoint(s.deleteRule, "id"))
}

// alarmRoutes registers alarm lists, details and actions.
func (s *Server) alarmRoutes() {
	s.router.GET("/api/v1/alarms", s.authorize("viewer"), s.endpoint(s.alarms))
	s.router.GET("/api/v1/alarms/:id", s.authorize("viewer"), s.endpoint(s.alarm, "id"))
	s.router.POST("/api/v1/alarms/:id/actions", s.authorize("operator"), s.endpoint(s.alarmAction, "id"))
}

// backupRoutes registers backups and restores.
func (s *Server) backupRoutes() {
	s.router.GET("/api/v1/backups/:id/files/:filename", s.authorize("admin"), s.endpoint(s.downloadBackupFile, "id", "filename"))
	s.router.GET("/api/v1/backups/:id/files", s.authorize("viewer"), s.endpoint(s.backupFiles, "id"))
	s.router.POST("/api/v1/backups/:id/restore-drill", s.authorize("admin"), s.endpoint(s.restoreBackup, "id"))
	s.router.POST("/api/v1/backups/:id/restore", s.authorize("admin"), s.endpoint(s.restoreBackupToTarget, "id"))
	s.router.GET("/api/v1/backups/:id", s.authorize("viewer"), s.endpoint(s.getBackup, "id"))
	s.router.GET("/api/v1/backups", s.authorize("viewer"), s.endpoint(s.listBackups))
	s.router.POST("/api/v1/backups", s.authorize("admin"), s.endpoint(s.runBackup))
}

// aiRoutes registers AI analysis, inspection, protocol assistant, providers, workflows and chat.
func (s *Server) aiRoutes() {
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId", s.authorize("viewer"), s.endpoint(s.aiAnalysis, "alarmId"))
	s.router.POST("/api/v1/ai/alarm-analysis/:alarmId/run", s.authorize("operator"), s.endpoint(s.runAIAlarmAnalysis, "alarmId"))
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId/progress", s.authorize("viewer"), s.endpoint(s.aiAlarmAnalysisProgress, "alarmId"))
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId/progress/:jobId", s.authorize("viewer"), s.endpoint(s.aiAlarmAnalysisProgress, "alarmId", "jobId"))
	s.router.POST("/api/v1/ai/health-inspection", s.authorize("viewer"), s.endpoint(s.healthInspection))
	s.router.POST("/api/v1/ai/health-inspection/run", s.authorize("viewer"), s.endpoint(s.runHealthInspection))
	s.router.GET("/api/v1/ai/health-inspection/progress", s.authorize("viewer"), s.endpoint(s.healthInspectionProgress))
	s.router.GET("/api/v1/ai/health-inspection/reports/:jobId", s.authorize("viewer"), s.endpoint(s.healthInspectionPage, "jobId"))
	s.router.GET("/api/v1/ai/health-inspection/progress/:jobId", s.authorize("viewer"), s.endpoint(s.healthInspectionProgress, "jobId"))
	s.router.POST("/api/v1/ai/health-inspection/pdf", s.authorize("viewer"), s.endpoint(s.healthInspectionPDF))
	s.router.POST("/api/v1/ai/protocol-assistant/generate", s.authorize("operator"), s.endpoint(s.generateProtocolAssistant))
	s.router.POST("/api/v1/ai/protocol-assistant/preview", s.authorize("operator"), s.endpoint(s.previewProtocolAssistant))
	s.router.POST("/api/v1/ai/protocol-assistant/publish", s.authorize("operator"), s.endpoint(s.publishProtocolAssistant))
	s.router.GET("/api/v1/ai/providers", s.authorize("viewer"), s.endpoint(s.aiProviders))
	s.router.GET("/api/v1/ai/providers/config", s.authorize("viewer"), s.endpoint(s.aiProviderConfig))
	s.router.PUT("/api/v1/ai/providers/config", s.authorize("admin"), s.endpoint(s.updateAIProviderConfig))
	s.router.POST("/api/v1/ai/providers/test", s.authorize("admin"), s.endpoint(s.testAIProvider))
	s.router.GET("/api/v1/ai/embedding-config", s.authorize("admin"), s.endpoint(s.embeddingConfig))
	s.router.PUT("/api/v1/ai/embedding-config", s.authorize("admin"), s.endpoint(s.updateEmbeddingConfig))
	s.router.POST("/api/v1/ai/embedding-test", s.authorize("admin"), s.endpoint(s.testEmbeddingConfig))
	s.router.GET("/api/v1/ai/workflows", s.authorize("viewer"), s.endpoint(s.aiWorkflows))
	s.router.GET("/api/v1/ai/runs", s.authorize("admin"), s.endpoint(s.aiWorkflowRuns))
	s.router.GET("/api/v1/ai/runs/history", s.authorize("admin"), s.endpoint(s.aiRunHistory))
	s.router.GET("/api/v1/ai/runs/usage", s.authorize("admin"), s.endpoint(s.aiRunUsage))
	s.router.POST("/api/v1/ai/runs/:id/stop", s.authorize("admin"), s.endpoint(s.stopAIWorkflowRun, "id"))
	s.router.GET("/api/v1/ai/workflows/admin", s.authorize("admin"), s.endpoint(s.aiWorkflowManifests))
	s.router.POST("/api/v1/ai/workflows", s.authorize("admin"), s.endpoint(s.saveAIWorkflow))
	s.router.PUT("/api/v1/ai/workflows/:id", s.authorize("admin"), s.endpoint(s.updateAIWorkflow, "id"))
	s.router.DELETE("/api/v1/ai/workflows/:id", s.authorize("admin"), s.endpoint(s.deleteAIWorkflow, "id"))
	s.router.GET("/api/v1/ai/workflows/:id/knowledge-binding", s.authorize("viewer"), s.endpoint(s.workflowKnowledgeBinding, "id"))
	s.router.PUT("/api/v1/ai/workflows/:id/knowledge-binding", s.authorize("operator"), s.endpoint(s.workflowKnowledgeBinding, "id"))
	s.router.POST("/api/v1/ai/workflows/:id/knowledge-binding/test", s.authorize("viewer"), s.endpoint(s.testWorkflowKnowledge, "id"))
	s.router.POST("/api/v1/ai/chat", s.authorize("viewer"), s.endpoint(s.aiChat))
	s.router.POST("/api/v1/ai/chat/stream", s.authorize("viewer"), s.endpoint(s.aiChatStream))
	s.router.POST("/api/v1/ai/rule-draft", s.authorize("operator"), s.endpoint(s.aiRuleDraft))
	s.router.POST("/api/v1/ai/reports", s.authorize("viewer"), s.endpoint(s.aiReport))
}

// knowledgeRoutes registers knowledge documents.
func (s *Server) knowledgeRoutes() {
	s.router.POST("/api/v1/knowledge/documents/:id/retry", s.authorize("operator"), s.endpoint(s.retryKnowledgeDocument, "id"))
	s.router.GET("/api/v1/knowledge/documents", s.authorize("viewer"), s.endpoint(s.knowledgeDocs))
	s.router.GET("/api/v1/knowledge/documents/:id", s.authorize("viewer"), s.endpoint(s.knowledgeDocumentDetail, "id"))
	s.router.POST("/api/v1/knowledge/documents", s.authorize("operator"), s.endpoint(s.knowledgeUpload))
}

// mcpRoutes registers MCP endpoints for browser assistants and the Harness.
func (s *Server) mcpRoutes() {
	mcpHandler := gin.WrapH(mcpserver.New(s.engine))
	s.router.GET("/mcp", s.authorize("viewer"), mcpHandler)
	s.router.POST("/mcp", s.authorize("viewer"), mcpHandler)
	s.router.DELETE("/mcp", s.authorize("viewer"), mcpHandler)
	harnessMCPHandler := gin.WrapH(mcpserver.NewHarness(s.engine))
	s.router.POST("/mcp/harness", s.authorizeHarness(), harnessMCPHandler)
}
