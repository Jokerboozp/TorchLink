package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func dutyFixture(t *testing.T) (*memory.Repository, auth.Claims) {
	t.Helper()
	r := memory.NewRepository()
	ctx := context.Background()
	if err := r.SaveManagedDevice(ctx, model.ManagedDevice{ID: "d", TenantID: "t", AccessKey: "d", Name: "探测器"}); err != nil {
		t.Fatal(err)
	}
	revision := model.DutyHandoverRevision{Frozen: true, HandoverID: "h", RunID: "run", StationID: "s", Snapshot: model.DutySnapshot{DeviceIDs: []string{"d"}}, Evidence: []model.DutyEvidence{{ID: "e1", Type: "record", DeviceID: "d", Label: "人工记录", Content: "已经联系维护", At: 1}, {ID: "e2", Type: "record", DeviceID: "d", Label: "人工记录", Content: "原因尚未确认", At: 2}}}
	job := model.DutyAIJob{ManagedUser: true, SessionVersion: 1, RequesterID: "alice", Status: "RUNNING", LeaseOwner: "worker", LeaseUntil: time.Now().Add(time.Hour).UnixMilli(), WorkflowID: core.WorkflowDutyHandover, RevisionID: "rev", HandoverID: "h", StationID: "s", RunID: "run", DeviceIDs: []string{"d"}, InputLimit: 1}
	handover := model.DutyHandover{CurrentRevisionID: "rev", Status: "DRAFT", StationID: "s", RunID: "run", DeviceIDs: []string{"d"}}
	if err := r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		for _, doc := range []model.DutyDocument{model.NewDutyDocument(model.DutyRevisionKind, "rev", revision), model.NewDutyDocument(model.DutyAIJobKind, "job", job), model.NewDutyDocument(model.DutyHandoverKind, "h", handover)} {
			if _, err := tx.Put(doc, 0); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	claims := auth.Claims{Username: "alice", TenantID: "t", TokenUse: "harness", RunID: "hrun", Workflow: core.WorkflowDutyHandover, DutyJobID: "job", DutyRevisionID: "rev", DutyLeaseOwner: "worker", ManagedUser: true, SessionVersion: 1, Permissions: []string{"menu:duty", "action:duty:ai"}, Scopes: []string{auth.ScopeQueryDutySnapshot}, RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{auth.HarnessAudience}}}
	return r, claims
}
func dutyMCPCall(t *testing.T, engine *core.Engine, c auth.Claims, args map[string]any) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "query_duty_snapshot", "arguments": args}})
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithClaims(context.Background(), c))
	out := httptest.NewRecorder()
	NewHarness(engine).ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	return out.Body.String()
}
func TestDutyMCPOnlyReadsSignedJobAndBoundedSnapshot(t *testing.T) {
	r, c := dutyFixture(t)
	out := dutyMCPCall(t, &core.Engine{Repo: r}, c, map[string]any{})
	if strings.Contains(out, `"isError":true`) || !strings.Contains(out, `\"revisionId\":\"rev\"`) || !strings.Contains(out, `\"inputCount\":1`) || strings.Contains(out, `\"id\":\"e1\"`) {
		t.Fatal(out)
	}
	out = dutyMCPCall(t, &core.Engine{Repo: r}, c, map[string]any{"revisionId": "another", "jobId": "other", "deviceIds": []string{"secret"}})
	if !strings.Contains(out, `"isError":true`) {
		t.Fatal("caller override accepted", out)
	}
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithClaims(context.Background(), c))
	response := httptest.NewRecorder()
	New(&core.Engine{Repo: r}).ServeHTTP(response, req)
	if strings.Contains(response.Body.String(), `"name":"query_duty_snapshot"`) {
		t.Fatal("duty snapshot exposed outside Harness")
	}
}
func TestDutyMCPRejectsCrossUserTaskTenantScopeAndExpiredLease(t *testing.T) {
	tests := []struct {
		name   string
		change func(*auth.Claims)
	}{{"user", func(c *auth.Claims) { c.Username = "bob" }}, {"tenant", func(c *auth.Claims) { c.TenantID = "other" }}, {"job", func(c *auth.Claims) { c.DutyJobID = "other" }}, {"revision", func(c *auth.Claims) { c.DutyRevisionID = "other" }}, {"workflow", func(c *auth.Claims) { c.Workflow = "ops-assistant" }}, {"token-use", func(c *auth.Claims) { c.TokenUse = "browser" }}, {"scope", func(c *auth.Claims) { c.Scopes = []string{auth.ScopeQueryAlarmList} }}, {"permission", func(c *auth.Claims) { c.Permissions = []string{"menu:duty"} }}, {"session", func(c *auth.Claims) { c.SessionVersion = 2 }}, {"audience", func(c *auth.Claims) { c.Audience = jwt.ClaimStrings{"browser"} }}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, c := dutyFixture(t)
			tc.change(&c)
			out := dutyMCPCall(t, &core.Engine{Repo: r}, c, map[string]any{})
			if !strings.Contains(out, `"isError":true`) || strings.Contains(out, "原因尚未确认") {
				t.Fatal(out)
			}
		})
	}
	for _, status := range []string{"CANCELLED", "SUCCEEDED", "expired"} {
		t.Run(status, func(t *testing.T) {
			r, c := dutyFixture(t)
			if err := r.DutyTransaction(context.Background(), "t", func(tx ports.DutyTx) error {
				d, err := tx.Get(model.DutyAIJobKind, "job")
				if err != nil {
					return err
				}
				j, _ := model.DutyBody[model.DutyAIJob](d)
				if status == "expired" {
					j.LeaseUntil = 1
				} else {
					j.Status = status
				}
				d.Body = model.NewDutyDocument(d.Kind, d.ID, j).Body
				_, err = tx.Put(d, d.Version)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			out := dutyMCPCall(t, &core.Engine{Repo: r}, c, map[string]any{})
			if !strings.Contains(out, `"isError":true`) {
				t.Fatal(out)
			}
		})
	}
}

type scopedDutyReadOnly struct {
	ports.Repository
	reader ports.DutyStore
	deny   bool
	onRead func()
}

func (r *scopedDutyReadOnly) DutyRead(ctx context.Context, tenant string, fn func(ports.DutyTx) error) error {
	return r.reader.DutyRead(ctx, tenant, fn)
}
func (r *scopedDutyReadOnly) GetManagedDevice(ctx context.Context, tenant, id string) (model.ManagedDevice, error) {
	if r.deny {
		return model.ManagedDevice{}, errors.New("device scope revoked")
	}
	if r.onRead != nil {
		r.onRead()
		r.onRead = nil
	}
	return r.Repository.GetManagedDevice(ctx, tenant, id)
}
func TestDutyMCPLeaseTakeoverInvalidatesOldRunToken(t *testing.T) {
	r, c := dutyFixture(t)
	if err := r.DutyTransaction(context.Background(), "t", func(tx ports.DutyTx) error {
		d, err := tx.Get(model.DutyAIJobKind, "job")
		if err != nil {
			return err
		}
		j, _ := model.DutyBody[model.DutyAIJob](d)
		j.LeaseOwner = "worker-2"
		d.Body = model.NewDutyDocument(d.Kind, d.ID, j).Body
		_, err = tx.Put(d, d.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	out := dutyMCPCall(t, &core.Engine{Repo: r}, c, map[string]any{})
	if !strings.Contains(out, `"isError":true`) {
		t.Fatal("stale lease token accepted", out)
	}
	c.DutyLeaseOwner = "worker-2"
	out = dutyMCPCall(t, &core.Engine{Repo: r}, c, map[string]any{})
	if strings.Contains(out, `"isError":true`) {
		t.Fatal("new lease token rejected", out)
	}
	c.DutyLeaseOwner = ""
	out = dutyMCPCall(t, &core.Engine{Repo: r}, c, map[string]any{})
	if !strings.Contains(out, `"isError":true`) {
		t.Fatal("unbound lease token accepted", out)
	}
}
func TestDutyMCPExistingOtherUserJobCannotBeSubstituted(t *testing.T) {
	r, c := dutyFixture(t)
	if err := r.DutyTransaction(context.Background(), "t", func(tx ports.DutyTx) error {
		d, err := tx.Get(model.DutyAIJobKind, "job")
		if err != nil {
			return err
		}
		j, _ := model.DutyBody[model.DutyAIJob](d)
		j.RequesterID = "bob"
		_, err = tx.Put(model.NewDutyDocument(model.DutyAIJobKind, "bob-job", j), 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c.DutyJobID = "bob-job"
	out := dutyMCPCall(t, &core.Engine{Repo: r}, c, map[string]any{})
	if !strings.Contains(out, `"isError":true`) || strings.Contains(out, "原因尚未确认") {
		t.Fatal("another user's live job disclosed evidence", out)
	}
}
func TestDutyMCPRechecksDeviceScopeAndJobAfterRead(t *testing.T) {
	for _, mode := range []string{"revoked-device", "stopped-during-authorization", "readonly-forwarder"} {
		t.Run(mode, func(t *testing.T) {
			r, c := dutyFixture(t)
			wrapped := &scopedDutyReadOnly{Repository: r, reader: r, deny: mode == "revoked-device"}
			if mode == "stopped-during-authorization" {
				wrapped.onRead = func() {
					if err := r.DutyTransaction(context.Background(), "t", func(tx ports.DutyTx) error {
						d, err := tx.Get(model.DutyAIJobKind, "job")
						if err != nil {
							return err
						}
						j, _ := model.DutyBody[model.DutyAIJob](d)
						j.Status = "CANCELLED"
						d.Body = model.NewDutyDocument(d.Kind, d.ID, j).Body
						_, err = tx.Put(d, d.Version)
						return err
					}); err != nil {
						t.Fatal(err)
					}
				}
			}
			out := dutyMCPCall(t, &core.Engine{Repo: wrapped}, c, map[string]any{})
			wantError := mode != "readonly-forwarder"
			if strings.Contains(out, `"isError":true`) != wantError {
				t.Fatal(out)
			}
		})
	}
}
