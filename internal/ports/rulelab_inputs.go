package ports

import (
	"context"
	"iot-platform/internal/model"
)

// An independent read-only source contract keeps the experimental executor
// from acquiring production alarm writes, queues, realtime or command ports.
type RuleLabInputStore interface {
	RuleLabInputsRead(context.Context, string, func(RuleLabInputReader) error) error
}
type RuleLabInputReader interface {
	ListStandardInputs(model.RuleLabInputQuery) (model.FactPage[model.RuleLabInput], error)
}
