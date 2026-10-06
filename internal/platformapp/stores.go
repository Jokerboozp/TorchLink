package platformapp

import "iot-platform/internal/ports"

// stores are the optional capabilities of the opened repository beyond
// ports.Repository. The ClickHouse and Redis decorators embed only
// ports.Repository, so a capability is looked up on the layer that provides
// it rather than captured before the decorators are added.
type stores struct {
	opsPrefs          ports.OpsPreferenceStore
	videoStore        ports.VideoStore
	knowledgeStore    ports.KnowledgeReindexStore
	aiRunStore        ports.AIRunStore
	conversationStore ports.AIConversationStore
	manifestStore     ports.AIWorkflowManifestStore
	signalStore       ports.DeviceSignalStore
	telemetryStats    ports.DeviceTelemetryStats
	aiProviderStore   ports.AIProviderConfigStore
	postgresRaw       ports.RawMessageDatabase
}

// unwrapper is a repository decorator that can return the repository it wraps.
type unwrapper interface{ Unwrap() ports.Repository }

// storesOf finds the capabilities of repo. Business stores come from the base
// repository (PostgreSQL, or memory without a database); telemetry statistics
// come from the outermost layer that computes them, ClickHouse when it holds
// the telemetry.
func storesOf(repo ports.Repository) stores {
	base := repo
	var telemetry ports.DeviceTelemetryStats
	for {
		if t, ok := base.(ports.DeviceTelemetryStats); ok && telemetry == nil {
			telemetry = t
		}
		u, ok := base.(unwrapper)
		if !ok {
			break
		}
		base = u.Unwrap()
	}
	var s stores
	s.opsPrefs, _ = base.(ports.OpsPreferenceStore)
	s.videoStore, _ = base.(ports.VideoStore)
	s.knowledgeStore, _ = base.(ports.KnowledgeReindexStore)
	s.aiRunStore, _ = base.(ports.AIRunStore)
	s.conversationStore, _ = base.(ports.AIConversationStore)
	s.manifestStore, _ = base.(ports.AIWorkflowManifestStore)
	s.signalStore, _ = base.(ports.DeviceSignalStore)
	s.telemetryStats = telemetry
	s.aiProviderStore, _ = base.(ports.AIProviderConfigStore)
	s.postgresRaw, _ = base.(ports.RawMessageDatabase)
	return s
}
