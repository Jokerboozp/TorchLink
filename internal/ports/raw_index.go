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
	MarkRawParseResult(context.Context, string, string, int64, string) error
	MarkRawPublished(context.Context, string, string, int64, string) error
	ListPendingRawIndexes(context.Context, int) ([]model.RawArchiveIndex, error)
	GetRawIndex(context.Context, string, string) (model.RawArchiveIndex, error)
	ListRawIndexes(context.Context, RawFilter) ([]model.RawArchiveIndex, error)
	CountRawIndexes(context.Context, RawFilter) (int, error)
}
