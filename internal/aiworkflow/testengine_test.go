package aiworkflow

import (
	"context"

	"iot-platform/internal/core"
)

// testEngine lets the tests set engine fields and call workflow methods on
// one value; both embedded parts share the same engine.
type testEngine struct {
	*core.Engine
	*Service
}

func wrap(e *core.Engine) *testEngine { return &testEngine{Engine: e, Service: New(e, nil)} }

// authorizerFunc adapts a function to Authorizer.
type authorizerFunc func(context.Context, string, string) (context.Context, error)

func (f authorizerFunc) AuthorizeRun(ctx context.Context, tenantID, workflowID string) (context.Context, error) {
	return f(ctx, tenantID, workflowID)
}
