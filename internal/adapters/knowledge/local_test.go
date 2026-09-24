package knowledge /* 声明 knowledge 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestLocalDeleteKnowledgeDocumentKeepsOtherDocumentsAndTenants(t *testing.T) {
	index := NewLocal()
	ctx := context.Background()
	for _, item := range []ports.KnowledgeIndexInput{
		{TenantID: "one", DocumentID: "remove", ChunkID: "a", Content: []byte("one")},
		{TenantID: "one", DocumentID: "keep", ChunkID: "b", Content: []byte("two")},
		{TenantID: "two", DocumentID: "remove", ChunkID: "c", Content: []byte("three")},
	} {
		if err := index.IndexKnowledge(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := index.DeleteKnowledgeDocument(ctx, "one", "remove", ""); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		tenant, document string
		want             int
	}{{"one", "remove", 0}, {"one", "keep", 1}, {"two", "remove", 1}} {
		chunks, err := index.ListKnowledgeChunks(ctx, check.tenant, check.document)
		if err != nil || len(chunks) != check.want {
			t.Fatalf("%s/%s: %d chunks, %v", check.tenant, check.document, len(chunks), err)
		}
	}
}

func TestLocalKnowledgeAppliesWorkflowMetadataFilters(t *testing.T) { /* 定义 TestLocalKnowledgeAppliesWorkflowMetadataFilters 函数。 */
	index := NewLocal()                                /* 更新 index 的值。 */
	for _, input := range []ports.KnowledgeIndexInput{ /* 循环处理当前数据。 */
		{TenantID: "tenant-a", WorkflowID: "ops-assistant", ProductID: "smoke", Category: "alarm-sop", Tags: []string{"certified", "fire"}, DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("烟雾 告警 现场 复核 处置")}, /* 执行当前语句并推进处理流程。 */
		{TenantID: "tenant-a", WorkflowID: "camera-assistant", ProductID: "camera", Category: "manual", Tags: []string{"video"}, DocumentID: "doc-2", ChunkID: "chunk-2", Content: []byte("烟雾 告警 摄像头 联动")},              /* 执行当前语句并推进处理流程。 */
		{TenantID: "tenant-b", WorkflowID: "ops-assistant", ProductID: "smoke", Category: "alarm-sop", Tags: []string{"certified", "fire"}, DocumentID: "doc-3", ChunkID: "chunk-3", Content: []byte("烟雾 告警 其他租户")},     /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if err := index.IndexKnowledge(context.Background(), input); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	hits, err := index.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: "tenant-a", WorkflowID: "ops-assistant", Question: "烟雾 告警", ProductIDs: []string{"smoke"}, Categories: []string{"alarm-sop"}, Tags: []string{"certified"}, Limit: 5, MinScore: .5}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(hits) != 1 || hits[0].DocumentID != "doc-1" || hits[0].ProductID != "smoke" || hits[0].Score < .5 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected filtered hits: %#v", hits) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	otherAgent, err := index.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: "tenant-a", WorkflowID: "camera-assistant", Question: "烟雾 告警", Limit: 5}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(otherAgent) != 1 || otherAgent[0].DocumentID != "doc-2" { /* 判断条件并选择处理分支。 */
		t.Fatalf("workflow association leaked across Agents: %#v", otherAgent) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestLocalKnowledgeSupportsSingleCharacterChineseQueries(t *testing.T) { /* 定义 TestLocalKnowledgeSupportsSingleCharacterChineseQueries 函数。 */
	index := NewLocal()                                                                                                                                                                             /* 更新 index 的值。 */
	if err := index.IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{TenantID: "tenant-a", DocumentID: "doc-water", ChunkID: "chunk-water", Content: []byte("水压异常处置")}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	hits, err := index.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: "tenant-a", Question: "水", Limit: 5}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(hits) != 1 || hits[0].DocumentID != "doc-water" { /* 判断条件并选择处理分支。 */
		t.Fatalf("single-character query returned %#v", hits) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestLocalKnowledgeListsStoredChunkDetails(t *testing.T) { /* 定义 TestLocalKnowledgeListsStoredChunkDetails 函数。 */
	index := NewLocal()                                /* 更新 index 的值。 */
	for _, input := range []ports.KnowledgeIndexInput{ /* 循环处理当前数据。 */
		{TenantID: "tenant-a", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "doc-1-chunk-0001", ChunkIndex: 1, StartChar: 0, EndChar: 6, CharacterCount: 6, Content: []byte("甲乙丙丁戊己")},                   /* 执行当前语句并推进处理流程。 */
		{TenantID: "tenant-a", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "doc-1-chunk-0002", ChunkIndex: 2, StartChar: 4, EndChar: 10, CharacterCount: 6, OverlapChars: 2, Content: []byte("戊己庚辛壬癸")}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if err := index.IndexKnowledge(context.Background(), input); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	chunks, err := index.ListKnowledgeChunks(context.Background(), "tenant-a", "doc-1") /* 更新 err 的值。 */
	if err != nil || len(chunks) != 2 {                                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected chunks=%#v err=%v", chunks, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if chunks[1].Index != 2 || chunks[1].StartChar != 4 || chunks[1].OverlapChars != 2 || chunks[1].Vectorized { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected chunk details %#v", chunks) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
