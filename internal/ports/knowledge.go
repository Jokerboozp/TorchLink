package ports

import (
	"context"

	"iot-platform/internal/model"
)

// KnowledgeStore keeps knowledge documents and workflow knowledge bindings.
type KnowledgeStore interface {
	SaveKnowledgeDoc(context.Context, model.KnowledgeDoc) error
	ListKnowledgeDocs(context.Context, string) ([]model.KnowledgeDoc, error)
	ListKnowledgeDocsPage(context.Context, string, int, int) ([]model.KnowledgeDoc, int, error)
	KnowledgeDocSummary(context.Context, string) (model.KnowledgeSummary, error)
	SaveWorkflowKnowledgeBinding(context.Context, model.WorkflowKnowledgeBinding) error
	GetWorkflowKnowledgeBinding(context.Context, string, string) (model.WorkflowKnowledgeBinding, error)
}
