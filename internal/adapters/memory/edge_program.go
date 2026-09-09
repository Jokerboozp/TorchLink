package memory

import (
	"context"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) GetEdgeProgram(_ context.Context, tenant, node string) (model.EdgeProgram, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.edgePrograms[key(tenant, node)]
	if !ok {
		v = model.EdgeProgram{TenantID: tenant, NodeID: node}
	}
	return clone(v), nil
}
func (r *Repository) SetEdgeProgram(_ context.Context, tenant, node string, expected int64, version string) (model.EdgeProgram, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.edgePrograms[key(tenant, node)]
	if v.Generation != expected {
		return v, model.ErrEdgeProgramConflict
	}
	if r.edgePrograms == nil {
		r.edgePrograms = map[string]model.EdgeProgram{}
	}
	v.TenantID = tenant
	v.NodeID = node
	v.TargetVersion = version
	v.Generation++
	v.UpdatedAt = time.Now().UnixMilli()
	r.edgePrograms[key(tenant, node)] = v
	return clone(v), nil
}
func (r *Repository) ReportEdgeProgram(_ context.Context, tenant, node string, status model.EdgeProgramStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.edgePrograms == nil {
		r.edgePrograms = map[string]model.EdgeProgram{}
	}
	v := r.edgePrograms[key(tenant, node)]
	v.TenantID = tenant
	v.NodeID = node
	status.LastSeenAt = time.Now().UnixMilli()
	v.Status = status
	r.edgePrograms[key(tenant, node)] = v
	return nil
}
