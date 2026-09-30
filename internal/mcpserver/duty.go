package mcpserver

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type dutyReader interface {
	DutyRead(context.Context, string, func(ports.DutyTx) error) error
}

func registerDutyTool(s *server.MCPServer, engine *core.Engine) {
	s.AddTool(mcp.NewTool("query_duty_snapshot", mcp.WithDescription("只读取当前已签名值班 AI 任务绑定的不可变交接事实；无需参数，不能指定其他班次、任务、租户或设备。")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if len(req.GetArguments()) != 0 {
			return auditedResult(ctx, engine, "query_duty_snapshot", map[string]any{}, nil, errors.New("值班快照不接受任务、版本或设备参数"))
		}
		output, err := readDutySnapshot(ctx, engine)
		return auditedResult(ctx, engine, "query_duty_snapshot", map[string]any{}, output, err)
	})
}
func readDutySnapshot(ctx context.Context, engine *core.Engine) (core.DutyAIInput, error) {
	denied := errors.New("值班任务或交接版本未获授权")
	tenant, err := tenantForTool(ctx, auth.ScopeQueryDutySnapshot, true)
	if err != nil {
		return core.DutyAIInput{}, err
	}
	claims, _ := auth.ClaimsFromContext(ctx)
	if claims.Workflow != core.WorkflowDutyHandover || claims.DutyRevisionID == "" || claims.DutyJobID == "" || claims.DutyLeaseOwner == "" || claims.RunID == "" || claims.Username == "" {
		return core.DutyAIInput{}, denied
	}
	if claims.ManagedUser {
		for _, permission := range []string{"menu:duty", "action:duty:ai"} {
			if !slices.Contains(claims.Permissions, permission) && !slices.Contains(claims.Permissions, "*") {
				return core.DutyAIInput{}, denied
			}
		}
	}
	reader, ok := engine.Repo.(dutyReader)
	if !ok {
		return core.DutyAIInput{}, errors.New("值班快照仓储不可用")
	}
	var job model.DutyAIJob
	var revision model.DutyHandoverRevision
	var version int64
	validate := func(tx ports.DutyTx) error {
		now := time.Now().UnixMilli()
		if engine.Clock != nil {
			now = engine.Clock.Now().UnixMilli()
		}
		doc, e := tx.Get(model.DutyAIJobKind, claims.DutyJobID)
		if e != nil {
			return denied
		}
		current, e := model.DutyBody[model.DutyAIJob](doc)
		if e != nil {
			return denied
		}
		if current.RequesterID != claims.Username || current.ManagedUser != claims.ManagedUser || current.SessionVersion != claims.SessionVersion || current.Status != "RUNNING" || current.LeaseOwner != claims.DutyLeaseOwner || current.LeaseUntil <= now || current.RevisionID != claims.DutyRevisionID || current.WorkflowID != core.WorkflowDutyHandover || (current.HarnessRunID != "" && current.HarnessRunID != claims.RunID) {
			return denied
		}
		if version != 0 && version != doc.Version {
			return denied
		}
		rd, e := tx.Get(model.DutyRevisionKind, claims.DutyRevisionID)
		if e != nil {
			return denied
		}
		rev, e := model.DutyBody[model.DutyHandoverRevision](rd)
		if e != nil || !rev.Frozen || rev.HandoverID != current.HandoverID || rev.StationID != current.StationID || rev.RunID != current.RunID || !sameDutyIDs(rev.Snapshot.DeviceIDs, current.DeviceIDs) {
			return denied
		}
		hd, e := tx.Get(model.DutyHandoverKind, current.HandoverID)
		if e != nil {
			return denied
		}
		h, e := model.DutyBody[model.DutyHandover](hd)
		if e != nil || h.CurrentRevisionID != claims.DutyRevisionID || (h.Status != "DRAFT" && h.Status != "RETURNED") || h.RunID != current.RunID || h.StationID != current.StationID || !sameDutyIDs(h.DeviceIDs, current.DeviceIDs) {
			return denied
		}
		job = current
		revision = rev
		version = doc.Version
		return nil
	}
	if err = reader.DutyRead(ctx, tenant, validate); err != nil {
		return core.DutyAIInput{}, err
	}
	// Repository scope checks run outside DutyRead to avoid nested PostgreSQL
	// transactions and memory repository lock re-entry. Nothing from a scope
	// broader than the stored job's device set can reach the model.
	ids := append([]string{}, revision.Snapshot.DeviceIDs...)
	for _, d := range revision.Snapshot.Devices {
		ids = append(ids, d.ID)
	}
	for _, state := range revision.Snapshot.States {
		ids = append(ids, state.DeviceID)
	}
	for _, alarm := range revision.Snapshot.Alarms {
		ids = append(ids, alarm.DeviceID)
	}
	for _, evidence := range revision.Evidence {
		if evidence.DeviceID != "" {
			ids = append(ids, evidence.DeviceID)
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || !slices.Contains(job.DeviceIDs, id) {
			return core.DutyAIInput{}, denied
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err = engine.Repo.GetManagedDevice(ctx, tenant, id); err != nil {
			return core.DutyAIInput{}, denied
		}
	}
	// Stop, edit, lease takeover or task replacement while checking the device
	// scope invalidates the entire read rather than returning partial evidence.
	if err = reader.DutyRead(ctx, tenant, validate); err != nil {
		return core.DutyAIInput{}, err
	}
	return core.BuildDutyAIInput(claims.DutyRevisionID, revision, job.InputLimit), nil
}
func sameDutyIDs(a, b []string) bool {
	aa := append([]string{}, a...)
	bb := append([]string{}, b...)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(aa, bb)
}
