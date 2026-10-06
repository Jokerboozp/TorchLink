package repositorytest

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
)

type knowledgeDocRepository interface {
	SaveKnowledgeDoc(context.Context, model.KnowledgeDoc) error
	GetKnowledgeDoc(context.Context, string, string) (model.KnowledgeDoc, error)
}

// KnowledgeDocLookupStaysInTenant checks the single-document lookup used by
// detail, delete and retry: it finds the tenant's document and never another
// tenant's document with the same ID.
func KnowledgeDocLookupStaysInTenant(t *testing.T, repo knowledgeDocRepository) {
	t.Helper()
	ctx := context.Background()
	doc := model.KnowledgeDoc{ID: "kdoc-lookup", TenantID: "kdoc-a", WorkflowID: "ops-assistant", Filename: "手册.pdf", Status: "INDEXED", ObjectBucket: "b", ObjectKey: "k", Metadata: map[string]any{"chunks": float64(3)}, CreatedAt: 1000}
	if err := repo.SaveKnowledgeDoc(ctx, doc); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetKnowledgeDoc(ctx, "kdoc-a", doc.ID)
	if err != nil || got.ID != doc.ID || got.Filename != doc.Filename || got.Status != "INDEXED" {
		t.Fatalf("lookup = %+v err=%v", got, err)
	}
	if _, err = repo.GetKnowledgeDoc(ctx, "kdoc-b", doc.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("another tenant must not see the document: %v", err)
	}
}
