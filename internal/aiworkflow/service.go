// Package aiworkflow runs the platform's business AI workflows (alarm
// analysis, device inspection, operations reports, protocol assistant and rule
// drafts) as Harness runs. It reads platform data through the core engine but
// is kept out of the message pipeline, which does not depend on it.
package aiworkflow

import (
	"context"

	"iot-platform/internal/core"
)

// Authorizer checks that the identity in ctx may still start a workflow and
// returns the context the run acts in (current permissions and device scope).
type Authorizer interface {
	AuthorizeRun(ctx context.Context, tenantID, workflowID string) (context.Context, error)
}

// Service runs business workflows over the engine's stores and Harness
// configuration.
type Service struct {
	engine *core.Engine
	// Authorizer, when set, re-checks the requester before data is read and
	// again before it is sent to the model.
	Authorizer Authorizer
	// Quota, when set, admits each run against the tenant's AI budget.
	Quota Quota
}

// Quota decides whether a tenant may start another AI run; a refusal is a
// ports.AIRequestError the user sees.
type Quota interface {
	AdmitAIRun(ctx context.Context, tenantID string) error
}

func (e *Service) admit(ctx context.Context, tenantID string) error {
	if e.Quota == nil {
		return nil
	}
	return e.Quota.AdmitAIRun(ctx, tenantID)
}

// New returns the workflows of engine.
func New(engine *core.Engine, authorizer Authorizer) *Service {
	return &Service{engine: engine, Authorizer: authorizer}
}

// Engine returns the engine the workflows read from.
func (e *Service) Engine() *core.Engine { return e.engine }

// authorize applies the Authorizer when one is configured.
func (e *Service) authorize(ctx context.Context, tenantID, workflowID string) (context.Context, error) {
	if e.Authorizer == nil {
		return ctx, nil
	}
	return e.Authorizer.AuthorizeRun(ctx, tenantID, workflowID)
}
