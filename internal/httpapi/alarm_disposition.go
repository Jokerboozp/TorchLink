package httpapi

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	alarmStatisticsPath = "/api/v1/alarms/statistics/disposition"
	alarmExportPath     = "/api/v1/alarms/export"
	// alarmExportFlushRows is how often the export pushes rows to the client.
	alarmExportFlushRows = 500
)

func (s *Server) alarmDispositionRoutes() {
	s.router.POST("/api/v1/alarms/:id/disposition", s.authorize("operator"), s.endpoint(s.verifyAlarm, "id"))
	s.router.GET(alarmStatisticsPath, s.authorize("viewer"), s.endpoint(s.alarmStatistics))
	s.router.GET(alarmExportPath, s.authorize("viewer"), s.endpoint(s.exportAlarms))
}

func (s *Server) verifyAlarm(w http.ResponseWriter, r *http.Request) {
	var in model.AlarmDisposition
	if decode(w, r, &in) != nil {
		return
	}
	in.Result = strings.ToUpper(strings.TrimSpace(in.Result))
	in.Notes = strings.TrimSpace(in.Notes)
	if !model.ValidDispositionResult(in.Result) {
		problem(w, 422, "请选择核实结论：真实火警、误报、测试、检修或设备故障")
		return
	}
	if len([]rune(in.Notes)) > 2000 {
		problem(w, 422, "处置说明不超过 2000 字")
		return
	}
	if in.DispatchID != "" {
		state, err := s.fireSafety.Snapshot(r.Context(), claims(r).TenantID)
		found := false
		for _, d := range state.Dispatches {
			found = found || d.ID == in.DispatchID
		}
		if err != nil || !found {
			problem(w, 422, "关联的出勤记录不存在")
			return
		}
	}
	a, err := s.engine.VerifyAlarm(r.Context(), claims(r).TenantID, r.PathValue("id"), in, claims(r).Username)
	switch {
	case errors.Is(err, errDeviceScope):
		problem(w, 403, "无权处置该设备的告警")
	case errors.Is(err, model.ErrNotFound):
		problem(w, 404, "告警不存在")
	case err != nil:
		problem(w, 422, err.Error())
	default:
		write(w, 200, a)
	}
}

// reportFilter selects the alarms that triggered within [start, end); the
// repository applies the request's device scope.
func reportFilter(r *http.Request) ports.AlarmFilter {
	q := r.URL.Query()
	end := time.Now().UnixMilli()
	if v, err := strconv.ParseInt(q.Get("end"), 10, 64); err == nil && v > 0 {
		end = v
	}
	start := end - 30*24*time.Hour.Milliseconds()
	if v, err := strconv.ParseInt(q.Get("start"), 10, 64); err == nil && v > 0 {
		start = v
	}
	return ports.AlarmFilter{TenantID: claims(r).TenantID, Status: q.Get("status"), Level: q.Get("level"), DeviceID: q.Get("deviceId"), Start: start, End: end}
}

// alarmStatistics reports verification results, the false alarm rate, the
// devices with most false alarms, and how long acknowledgement and
// verification took. The store aggregates every matching alarm.
func (s *Server) alarmStatistics(w http.ResponseWriter, r *http.Request) {
	stats, err := s.engine.Repo.AlarmDispositionStats(r.Context(), reportFilter(r))
	if err != nil {
		s.log.Error("alarm statistics failed", "error", err)
		problem(w, 500, "读取告警失败")
		return
	}
	write(w, 200, stats)
}

var dispositionNames = map[string]string{model.DispositionRealFire: "真实火警", model.DispositionFalseAlarm: "误报", model.DispositionTest: "测试", model.DispositionMaintenance: "检修", model.DispositionFault: "设备故障"}
var alarmStatusNames = map[string]string{"ACTIVE": "活动", "ACKED": "已确认", "RECOVERED": "已恢复", "CLOSED": "已关闭", "SUPPRESSED": "已抑制"}

// exportAlarms streams the alarms of the period as UTF-8 CSV with a BOM, so
// spreadsheet software opens the Chinese text correctly. A failure after the
// first row aborts the response, so the client sees an error instead of a
// silently shortened file.
func (s *Server) exportAlarms(w http.ResponseWriter, r *http.Request) {
	filter := reportFilter(r)
	filter.Summary = true
	at := func(ms int64) string {
		if ms <= 0 {
			return ""
		}
		return time.UnixMilli(ms).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	}
	var out *csv.Writer
	rows := 0
	start := func() {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="alarms-%s.csv"`, time.Now().Format("20060102-150405")))
		_, _ = w.Write([]byte("\ufeff"))
		out = csv.NewWriter(w)
		_ = out.Write([]string{"告警编号", "设备编号", "设备名称", "告警类型", "等级", "状态", "内容", "部件位置", "首次发生", "最后发生", "触发次数", "确认时间", "恢复时间", "关闭时间", "核实结论", "核实人", "核实时间", "到场时间", "处置说明"})
	}
	err := s.engine.Repo.EachAlarm(r.Context(), filter, func(a model.Alarm) error {
		if out == nil {
			start()
		}
		result, handler, verifiedAt, arrived, notes := "", "", "", "", ""
		if d := a.Disposition; d != nil {
			result, handler, verifiedAt, arrived, notes = dispositionNames[d.Result], d.Handler, at(d.VerifiedAt), at(d.ArrivedAt), d.Notes
		}
		status := alarmStatusNames[a.Status]
		if status == "" {
			status = a.Status
		}
		rows++
		if err := out.Write([]string{a.ID, a.DeviceID, a.DeviceName, a.AlarmType, a.AlarmLevel, status, a.Content, a.ComponentLocation, at(a.FirstTriggeredAt), at(a.LastTriggeredAt), strconv.Itoa(a.TriggerCount), at(a.AckedAt), at(a.RecoveredAt), at(a.ClosedAt), result, handler, verifiedAt, arrived, notes}); err != nil {
			return err
		}
		if rows%alarmExportFlushRows == 0 {
			out.Flush()
			return out.Error()
		}
		return nil
	})
	if err != nil {
		s.log.Error("alarm export failed", "rows", rows, "error", err)
		if out == nil {
			problem(w, 500, "读取告警失败")
			return
		}
		panic(http.ErrAbortHandler)
	}
	if out == nil {
		start()
	}
	out.Flush()
	s.audit(r, "alarm.export", "alarm", "", map[string]any{"rows": rows})
}
