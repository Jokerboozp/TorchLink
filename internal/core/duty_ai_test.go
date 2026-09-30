package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type dutyWorkflowStub struct {
	request ports.AIWorkflowRequest
	answer  string
}

func (w *dutyWorkflowStub) Health(context.Context) error { return nil }
func (w *dutyWorkflowStub) ListWorkflows(context.Context) ([]ports.AIWorkflowPlugin, error) {
	return nil, nil
}

func (w *dutyWorkflowStub) StreamChat(_ context.Context, r ports.AIWorkflowRequest, _ func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	w.request = r
	return ports.AIWorkflowResult{Answer: w.answer, RunID: r.RunID, Model: "test-model"}, nil
}

func TestDutyAIInputPreservesFactsBoundsAndRedacts(t *testing.T) {
	r := model.DutyHandoverRevision{Evidence: []model.DutyEvidence{{ID: "event", Type: "event", Content: `{"deviceName":"烟感","alarmType":"FIRE","details":{"raw":"private-packet","token":"private-token"}}`}, {ID: "record", Type: "record", Label: "联系13812345678", Content: "联系13812345678 password=private-password"}}}
	r.Evidence = append(r.Evidence, model.DutyEvidence{ID: "quoted-record", Type: "record", Label: `'api_key': 'label secret'`, Content: `{"password":"JSON secret with spaces","nested":{"api_key":"nested secret"}}`}, model.DutyEvidence{ID: "yaml-notes", Type: "notes", Content: "'token': 'YAML secret with spaces'\n\"secret\": \"escaped \\\"secret\\\" value\""})
	v := BuildDutyAIInput("rev", r, 100)
	raw, _ := json.Marshal(v)
	for _, secret := range []string{"private-packet", "private-token", "private-password", "13812345678", "label secret", "JSON secret", "nested secret", "YAML secret", "escaped"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("AI input leaked %s", secret)
		}
	}
	if !strings.Contains(string(raw), "FIRE") || v.InputCount != 4 || v.Truncated {
		t.Fatalf("lost facts %#v", v)
	}
	for i := 0; i < 1000; i++ {
		r.Evidence = append(r.Evidence, model.DutyEvidence{ID: "large", Content: strings.Repeat("中文", 500)})
	}
	v = BuildDutyAIInput("rev", r, 300)
	raw, _ = json.Marshal(v)
	if len(raw) > 20<<10 || !v.Truncated || v.TotalCount != 1004 {
		t.Fatal("AI input budget/coverage not enforced")
	}
}

func TestDutyAIInputSelectsRecentFactsAcrossGroupedEvidence(t *testing.T) {
	r := model.DutyHandoverRevision{Evidence: []model.DutyEvidence{
		{ID: "latest-event", Type: "event", At: 900},
		{ID: "old-event", Type: "event", At: 100},
		{ID: "latest-record", Type: "record", At: 800},
		{ID: "older-record", Type: "record", At: 200},
		{ID: "old-item", Type: "item", At: 300},
	}}
	v := BuildDutyAIInput("revision", r, 2)
	if len(v.Evidence) != 2 || v.Evidence[0].ID != "latest-event" || v.Evidence[1].ID != "latest-record" || !v.Truncated {
		t.Fatalf("latest facts omitted: %#v", v)
	}
	if r.Evidence[1].ID != "old-event" {
		t.Fatal("modified immutable revision evidence order")
	}
}
func TestDutyAIResultRejectsUnknownEvidenceAndOwnsCoverage(t *testing.T) {
	input := DutyAIInput{Evidence: []model.DutyEvidence{{ID: "allowed"}}, InputCount: 1, TotalCount: 3, Truncated: true}
	if _, e := DecodeDutyAIResult(`{"highlights":[{"text":"事实","evidenceIds":["foreign"]}]}`, input); e == nil {
		t.Fatal("accepted foreign evidence")
	}
	v, e := DecodeDutyAIResult("```json\n{\"summary\":\"已确认所有设备安全\",\"highlights\":[{\"text\":\"待人工复核\",\"evidenceIds\":[\"allowed\"]}]}\n```", input)
	if e != nil {
		t.Fatal(e)
	}
	if v.InputCount != 1 || v.TotalCount != 3 || !v.Truncated || strings.Contains(v.Summary, "安全") {
		t.Fatal("model overrode coverage")
	}
}
func TestDutyWorkflowUsesBoundHarnessToken(t *testing.T) {
	workflow := &dutyWorkflowStub{answer: `{"summary":"完成","highlights":[],"suggestions":[],"missing":[]}`}
	tokens := auth.New("duty-test-key")
	e := &Engine{Repo: memory.NewRepository(), Clock: ports.RealClock{}, AIWorkflows: workflow, HarnessTokens: tokens}
	identity := ports.AIRunIdentity{TenantID: "t", Username: "u", DutyRevisionID: "rev", DutyJobID: "job", DutyLeaseOwner: "owner", Scopes: []string{auth.ScopeQueryDutySnapshot, auth.ScopeQueryKnowledgeBase, auth.ScopeQueryAlarmList}}
	ctx := ports.WithAIRunIdentity(context.Background(), identity)
	_, _, err := e.RunDutyHandover(ctx, "t", "rev", model.DutyHandoverRevision{}, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.Parse(workflow.request.MCPToken)
	if err != nil {
		t.Fatal(err)
	}
	if workflow.request.WorkflowID != WorkflowDutyHandover || claims.DutyRevisionID != "rev" || claims.DutyJobID != "job" || claims.HasScope(auth.ScopeQueryKnowledgeBase) || claims.HasScope(auth.ScopeQueryAlarmList) || !claims.HasScope(auth.ScopeQueryDutySnapshot) {
		t.Fatalf("incorrect workflow boundary %#v", claims)
	}
	if _, _, err = e.RunDutyHandover(ctx, "t", "another-rev", model.DutyHandoverRevision{}, 100, false); err == nil {
		t.Fatal("accepted unbound revision")
	}
}
