package httpapi

import (
	"errors"
	"iot-platform/internal/model"
	"net/http"
	"sort"
	"strconv"
	"time"
)

func (s *Server) twinRoutes() {
	s.router.GET("/api/v1/device-twins/:id", s.authorize("viewer"), s.endpoint(s.deviceTwin, "id"))
	s.router.PATCH("/api/v1/device-twin-topology", s.authorize("operator"), s.endpoint(s.updateTwin))
}
func (s *Server) updateTwin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var request struct {
		ExpectedVersion int64                `json:"expectedVersion"`
		Add             []model.TwinRelation `json:"add"`
		Remove          []string             `json:"remove"`
	}
	if decode(w, r, &request) != nil {
		return
	}
	u := model.TwinUpdate{TenantID: claims(r).TenantID, ExpectedVersion: request.ExpectedVersion, Timestamp: time.Now().UnixMilli(), Add: request.Add, Remove: request.Remove}
	v, err := s.engine.Repo.UpdateTwinTopology(r.Context(), u)
	if err != nil {
		status := 422
		if errors.Is(err, model.ErrTwinConflict) {
			status = 409
		}
		problem(w, status, err.Error())
		return
	}
	s.audit(r, "device.twin.topology", "topology", u.TenantID, map[string]any{"version": v.Version, "add": request.Add, "remove": request.Remove})
	write(w, 200, map[string]any{"version": v.Version, "updatedAt": v.UpdatedAt})
}
func (s *Server) deviceTwin(w http.ResponseWriter, r *http.Request) {
	tenant, id := claims(r).TenantID, r.PathValue("id")
	d, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, id)
	if err != nil {
		problem(w, 404, "device not found")
		return
	}
	depth := 1
	if input := r.URL.Query().Get("depth"); input != "" {
		depth, err = strconv.Atoi(input)
		if err != nil || depth < 0 || depth > 3 {
			problem(w, 422, "twin depth must be 0 to 3")
			return
		}
	}
	graph, err := s.engine.Repo.GetTwinTopology(r.Context(), tenant)
	if err != nil {
		problem(w, 503, "load device topology failed")
		return
	}
	adjacency := map[string][]model.TwinRelation{}
	for _, relation := range graph.Relations {
		adjacency[relation.Source] = append(adjacency[relation.Source], relation)
		adjacency[relation.Target] = append(adjacency[relation.Target], relation)
	}
	selected := map[string]bool{id: true}
	frontier := []string{id}
	truncated := false
	for step := 0; step < depth; step++ {
		next := []string{}
		for _, node := range frontier {
			for _, edge := range adjacency[node] {
				for _, other := range []string{edge.Source, edge.Target} {
					if selected[other] {
						continue
					}
					if len(selected) >= 200 {
						truncated = true
						continue
					}
					selected[other] = true
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	ids := make([]string, 0, len(selected))
	for node := range selected {
		ids = append(ids, node)
	}
	sort.Strings(ids)
	nodes, err := s.engine.Repo.GetTwinNodes(r.Context(), tenant, ids)
	if err != nil {
		problem(w, 503, "load topology states failed")
		return
	}
	present := map[string]bool{}
	for _, n := range nodes {
		present[n.ID] = true
	}
	relations := []model.TwinRelation{}
	for _, edge := range graph.Relations {
		if present[edge.Source] && present[edge.Target] {
			relations = append(relations, edge)
		}
	}
	shadow, err := s.engine.Repo.GetDeviceShadow(r.Context(), tenant, id)
	if err != nil {
		problem(w, 503, "load twin shadow failed")
		return
	}
	var thingModel *model.ThingModel
	if product, e := s.engine.Repo.GetProduct(r.Context(), tenant, d.ProductID); e == nil {
		thingModel = product.ThingModel
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]any{"deviceId": id, "version": graph.Version, "updatedAt": graph.UpdatedAt, "depth": depth, "truncated": truncated, "nodes": nodes, "relations": relations, "shadow": shadow, "thingModel": thingModel})
}
