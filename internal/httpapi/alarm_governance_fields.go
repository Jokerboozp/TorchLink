package httpapi

import (
	"iot-platform/internal/ports"
	"net/http"
)

func (s *Server) governanceSourceFields(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	query := r.URL.Query()
	filter := ports.AlarmObservationFilter{DeviceID: query.Get("deviceId"), Start: governanceInt(query.Get("start"), 0), End: governanceInt(query.Get("end"), 0)}
	if filter.DeviceID == "" || filter.Start <= 0 || filter.End <= filter.Start || filter.End-filter.Start > s.analysis.Limits.MaxRange.Milliseconds() {
		problem(w, 422, "源字段目录须明确设备和有界成功样本时间区间")
		return
	}
	items, err := s.governance.SourceFields(r.Context(), governanceActor(r), filter)
	if err != nil {
		governanceProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "limit": 100})
}
