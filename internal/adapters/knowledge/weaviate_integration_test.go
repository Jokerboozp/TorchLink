package knowledge

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/ports"
)

func TestWeaviateLiveIndexAndScopeIsolation(t *testing.T) {
	endpoint := strings.TrimRight(os.Getenv("IOT_TEST_WEAVIATE_URL"), "/")
	if endpoint == "" {
		t.Skip("IOT_TEST_WEAVIATE_URL not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	id := "audit-" + uuid.NewString()
	in := ports.KnowledgeIndexInput{TenantID: id, WorkflowID: "audit-workflow", DocumentID: id, ChunkID: id, Content: []byte("联调专用资料：巡检柜第六路输入状态为一，模拟测试已通过。")}
	objectID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{in.TenantID, in.WorkflowID, in.ChunkID}, "\x00")))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(cleanup, http.MethodDelete, endpoint+"/v1/objects/IotKnowledge/"+objectID.String(), nil)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error("temporary knowledge object cleanup failed")
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 204 && response.StatusCode != 404 {
			t.Errorf("temporary knowledge cleanup HTTP %d", response.StatusCode)
		}
	})
	w := NewWeaviate(endpoint)
	if err := w.IndexKnowledge(ctx, in); err != nil {
		t.Fatal("live knowledge indexing failed")
	}
	chunks, err := w.ListKnowledgeChunks(ctx, id, id)
	if err != nil || len(chunks) != 1 || chunks[0].Content != string(in.Content) {
		t.Fatal("indexed chunk readback mismatch")
	}
	query := ports.KnowledgeSearchRequest{TenantID: id, WorkflowID: in.WorkflowID, Question: "第六路输入状态", Limit: 3}
	hits, err := w.SearchKnowledge(ctx, query)
	if err != nil || len(hits) != 1 || hits[0].DocumentID != id {
		t.Fatal("live scoped vector retrieval failed")
	}
	query.WorkflowID = "unrelated-workflow"
	hits, err = w.SearchKnowledge(ctx, query)
	if err != nil || len(hits) != 0 {
		t.Fatal("knowledge leaked across workflow scope")
	}
	query.WorkflowID = in.WorkflowID
	query.TenantID = id + "-other"
	hits, err = w.SearchKnowledge(ctx, query)
	if err != nil || len(hits) != 0 {
		t.Fatal("knowledge leaked across tenant scope")
	}
}
