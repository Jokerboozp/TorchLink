package ports

import (
	"context"
	"iot-platform/internal/model"
)

// RuntimeCleanupCounts reports acknowledged cleanup work separately from
// durable business deletion. Skipped/unsupported work always carries warnings.
type RuntimeCleanupCounts struct {
	Inbox                  int64    `json:"inbox"`
	InboxSkipped           int64    `json:"inboxSkipped"`
	RetainedRequests       int64    `json:"retainedRequests"`
	QueueOffsetSpan        int64    `json:"queueOffsetSpan"`
	QueueSkippedPartitions int64    `json:"queueSkippedPartitions"`
	Warnings               []string `json:"warnings,omitempty"`
}

func (c *RuntimeCleanupCounts) Add(o RuntimeCleanupCounts) {
	c.Inbox += o.Inbox
	c.InboxSkipped += o.InboxSkipped
	c.RetainedRequests += o.RetainedRequests
	c.QueueOffsetSpan += o.QueueOffsetSpan
	c.QueueSkippedPartitions += o.QueueSkippedPartitions
	c.Warnings = append(c.Warnings, o.Warnings...)
}

type CapacityInboxCleaner interface {
	CleanupCapacityInbox(context.Context, string, model.CapacityCleanupBatch) (RuntimeCleanupCounts, error)
}

type CapacityRetainedCleaner interface {
	ClearCapacityRetained(context.Context, string, model.CapacityCleanupBatch) (RuntimeCleanupCounts, error)
}

// CapacityQueuePlan is computed by the broker adapter and must be revalidated
// against live groups, committed offsets and the same exclusive fixture scope.
// DeleteBefore is exclusive; records at or after it remain available.
type CapacityQueuePlan struct {
	ScopeHash  string                   `json:"scopeHash"`
	Groups     []string                 `json:"groups"`
	Partitions []CapacityQueuePartition `json:"partitions"`
	Warnings   []string                 `json:"warnings,omitempty"`
}

type CapacityQueuePartition struct {
	Topic        string `json:"topic"`
	Partition    int    `json:"partition"`
	Start        int64  `json:"start"`
	End          int64  `json:"end"`
	DeleteBefore int64  `json:"deleteBefore"`
	Reason       string `json:"reason,omitempty"`
}

type CapacityQueueCleaner interface {
	PreviewCapacityQueue(context.Context, string, model.CapacityCleanupBatch) (CapacityQueuePlan, error)
	CleanupCapacityQueue(context.Context, string, model.CapacityCleanupBatch, CapacityQueuePlan) (RuntimeCleanupCounts, error)
}
