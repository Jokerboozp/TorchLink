package ports

import (
	"context"
	"time"

	"iot-platform/internal/model"
)

// StandardMessageStore keeps parsed standard messages, their processing claims
// and property history.
type StandardMessageStore interface {
	ListDeviceMessages(context.Context, string, string, model.MessageType, int, int) ([]model.StandardMessage, int, error)
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
	// CompleteStandardMessage writes state when it is not nil, checked
	// against its version like UpsertDeviceStateIf, and marks the message
	// processed under its claim token in the same statement; the mark is
	// only made when the state was written. false means the state version
	// changed and nothing was written. A stale or lost claim after a written
	// state returns model.ErrStaleClaim or model.ErrNotFound.
	CompleteStandardMessage(ctx context.Context, state *model.DeviceState, tenant, messageID string, token int64) (bool, error)
}
