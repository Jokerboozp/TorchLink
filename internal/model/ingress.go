package model

import "errors"

var ErrNotFound = errors.New("not found")
var ErrResourceInUse = errors.New("resource is referenced")
var ErrInvalidIngress = errors.New("invalid ingress message")

// ErrBackpressure rejects new raw messages while the parse and storage backlog
// is above its limit; senders retry later and the backlog drains first.
var ErrBackpressure = errors.New("ingest paused: processing backlog above limit, retry later")
