package core

import (
	"io"
	"log/slog"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

// newBusinessEngine returns an engine on a memory store with a scripted
// Harness; answer nil answers "{}".
func newBusinessEngine(t *testing.T, answer func(req ports.AIWorkflowRequest) (string, error)) (*Engine, *memory.Repository, *aitest.Workflows) {
	t.Helper()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	workflows := &aitest.Workflows{Answer: answer}
	e.AIWorkflows, e.HarnessTokens = workflows, aitest.Tokens()
	return e, repo, workflows
}
