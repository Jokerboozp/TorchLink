package httpapi

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"iot-platform/internal/model"
	"net/http"
	"time"
)

func (s *Server) edgeRead(ctx context.Context, p model.DeviceAccessProfile, release model.ProtocolRelease) ([]model.RawMessage, error) {
	h, err := s.engine.Repo.GetEdgeHeartbeat(ctx, p.TenantID, p.EdgeNodeID)
	if err != nil || h.LastSeenAt < time.Now().Add(-30*time.Second).UnixMilli() {
		return nil, errors.New("Edge 节点没有近期心跳，无法执行实际读取")
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	j := model.EdgeReadJob{ID: uuid.NewString(), TenantID: p.TenantID, NodeID: p.EdgeNodeID, Task: model.EdgeTask{Profile: p, Release: release}, ExpiresAt: deadline.UnixMilli()}
	if err = s.engine.Repo.CreateEdgeReadJob(ctx, j); err != nil {
		return nil, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, errors.New("Edge 实际读取未在截止时间内完成")
			}
			result, err := s.engine.Repo.GetEdgeReadJob(ctx, j.TenantID, j.ID)
			if err != nil {
				return nil, err
			}
			if result.Status == "DONE" {
				if result.Error != "" {
					return nil, errors.New(result.Error)
				}
				if len(result.Raw) == 0 {
					return nil, errors.New("Edge 未返回实际报文")
				}
				return result.Raw, nil
			}
		}
	}
}

func (s *Server) edgeReadJobs(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateEdge(w, r) {
		return
	}
	tenant, node := r.PathValue("tenant"), r.PathValue("node")
	if r.Method == "GET" {
		j, err := s.engine.Repo.ClaimEdgeReadJob(r.Context(), tenant, node, uuid.NewString())
		if err != nil {
			problem(w, 503, "load edge diagnostic")
			return
		}
		write(w, 200, j)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var result model.EdgeReadJob
	if decode(w, r, &result) != nil {
		return
	}
	j, err := s.engine.Repo.GetEdgeReadJob(r.Context(), tenant, r.PathValue("job"))
	if err != nil || j.NodeID != node || j.Token != result.Token || j.ExpiresAt <= time.Now().UnixMilli() {
		problem(w, 409, "expired or unowned edge diagnostic")
		return
	}
	if len(result.Error) > 512 || len(result.Raw) > 256 || (result.Error == "" && len(result.Raw) == 0) {
		problem(w, 422, "invalid edge read result")
		return
	}
	for _, raw := range result.Raw {
		if raw.TenantID != tenant || raw.ProductID != j.Task.Profile.ProductID || raw.DeviceID != j.Task.Profile.DeviceID || raw.ProtocolID != j.Task.Release.ProtocolID || raw.ProtocolVersion != j.Task.Release.Version {
			problem(w, 422, "edge read identity mismatch")
			return
		}
	}
	j.Raw, j.Error = result.Raw, result.Error
	if err = s.engine.Repo.FinishEdgeReadJob(r.Context(), j); err != nil {
		problem(w, 409, "diagnostic was not accepted")
		return
	}
	write(w, 200, map[string]any{"id": j.ID, "status": "DONE"})
}
