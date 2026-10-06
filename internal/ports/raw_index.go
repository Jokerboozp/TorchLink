package ports

import (
	"context"

	"iot-platform/internal/model"
)

// RawIndexStore keeps the index and reservations of archived raw messages;
// payloads live in RawMessageStore.
type RawIndexStore interface {
	// ReserveRawMessage returns the canonical reserved message and whether
	// this call created the reservation.
	ReserveRawMessage(context.Context, model.RawMessage) (model.RawMessage, bool, error)
	SaveRawIndex(context.Context, model.RawArchiveIndex) (bool, error)
	// MarkRawParseResult(ctx, tenant, messageID, receivedAt, attemptedAt, error)
	// and MarkRawPublished(ctx, tenant, messageID, receivedAt, publishedAt,
	// error) take the message's receivedAt so the update reaches only its
	// monthly partition; 0 means unknown.
	MarkRawParseResult(context.Context, string, string, int64, int64, string) error
	MarkRawPublished(context.Context, string, string, int64, int64, string) error
	// ListPendingRawIndexes returns archived messages still waiting for the
	// queue, with exponential backoff per failed attempt, newest failures
	// first; messages at model.MaxRawPublishAttempts are left out.
	ListPendingRawIndexes(context.Context, int) ([]model.RawArchiveIndex, error)
	// CountStalledRawIndexes counts messages that reached
	// model.MaxRawPublishAttempts and need an operator.
	CountStalledRawIndexes(context.Context) (int, error)
	GetRawIndex(context.Context, string, string) (model.RawArchiveIndex, error)
	// GetRawIndexAt is GetRawIndex for a message whose receivedAt (Unix
	// milliseconds) is known, so the lookup reads one monthly partition; a
	// wrong or zero receivedAt still finds the message.
	GetRawIndexAt(ctx context.Context, tenant, messageID string, receivedAt int64) (model.RawArchiveIndex, error)
	ListRawIndexes(context.Context, RawFilter) ([]model.RawArchiveIndex, error)
	CountRawIndexes(context.Context, RawFilter) (int, error)
}
