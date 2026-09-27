package model

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrResourceInUse = errors.New("resource is referenced")
var ErrInvalidIngress = errors.New("invalid ingress message")

// ErrBackpressure rejects new raw messages while the parse and storage backlog
// is above its limit; senders retry later and the backlog drains first.
var ErrBackpressure = errors.New("ingest paused: processing backlog above limit, retry later")

// ErrStaleClaim is returned when a processing claim was taken over after its
// lease expired; the fenced holder must not record completion.
var ErrStaleClaim = errors.New("processing claim was taken over by another worker")

// ErrConcurrentUpdate is returned when optimistic retries were exhausted.
var ErrConcurrentUpdate = errors.New("concurrent update; retry")

// StandardClaim is the outcome of claiming a standard message for business
// processing across workers.
type StandardClaim struct {
	// ShouldProcess is true when this worker holds the claim.
	ShouldProcess bool
	// Created is true when the message row was stored by this call.
	Created bool
	// Token fences completion: only the latest claim may mark it processed.
	Token int64
	// Busy is true when another live worker holds the claim; retry after
	// RetryAfter.
	Busy       bool
	HeldBy     string
	RetryAfter time.Duration
}
