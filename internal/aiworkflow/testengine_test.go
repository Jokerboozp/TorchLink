package aiworkflow

import "iot-platform/internal/core"

// testEngine lets the tests set engine fields and call workflow methods on
// one value; both embedded parts share the same engine.
type testEngine struct {
	*core.Engine
	*Service
}

func wrap(e *core.Engine) *testEngine { return &testEngine{Engine: e, Service: New(e, nil)} }
