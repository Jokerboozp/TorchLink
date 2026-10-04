package httpapi

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	alarmStatisticsPath = "/api/v1/alarms/statistics/disposition"
	alarmExportPath     = "/api/v1/alarms/export"
	// maxAlarmReport bounds the alarms one statistics or export request reads.
	maxAlarmReport = 50_000
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

// reportAlarms reads the alarms that triggered within [start, end) through
// the request's device scope.
func (s *Server) reportAlarms(r *http.Request) ([]model.Alarm, bool, error) {
	q := r.URL.Query()
	end := time.Now().UnixMilli()
	if v, err := strconv.ParseInt(q.Get("end"), 10, 64); err == nil && v > 0 {
		end = v
	}
	start := end - 30*24*time.Hour.Milliseconds()
	if v, err := strconv.ParseInt(q.Get("start"), 10, 64); err == nil && v > 0 {
		start = v
	}
	filter := ports.AlarmFilter{TenantID: claims(r).TenantID, Status: q.Get("status"), Level: q.Get("level"), DeviceID: q.Get("deviceId"), Start: start, End: end}
	out := []model.Alarm{}
	for offset := 0; offset < maxAlarmReport; offset += 1000 {
		filter.Limit, filter.Offset = 1000, offset
		page, err := s.engine.Repo.ListAlarms(r.Context(), filter)
		if err != nil {
			return nil, false, err
		}
		out = append(out, page...)
		if len(page) < 1000 {
			return out, false, nil
		}
	}
	return out, true, nil
}

type durationStats struct {
	Count int64 `json:"count"`
	AvgMs int64 `json:"avgMs"`
	P90Ms int64 `json:"p90Ms"`
}

func summarize(values []int64) durationStats {
	if len(values) == 0 {
		return durationStats{}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var sum int64
	for _, v := range values {
		sum += v
	}
	index := (len(values)*9+9)/10 - 1 // nearest-rank 90th percentile
	return durationStats{Count: int64(len(values)), AvgMs: sum / int64(len(values)), P90Ms: values[index]}
}

// alarmStatistics reports verification results, the false alarm rate, the
// devices with most false alarms, and how long acknowledgement and
// verification took.
func (s *Server) alarmStatistics(w http.ResponseWriter, r *http.Request) {
	alarms, truncated, err := s.reportAlarms(r)
	if err != nil {
		problem(w, 500, "读取告警失败")
		return
	}
	byResult := map[string]int64{}
	var verified, requiring, unverified int64
	acks, verifies := []int64{}, []int64{}
	type deviceCount struct {
		DeviceID    string `json:"deviceId"`
		DeviceName  string `json:"deviceName"`
		FalseAlarms int64  `json:"falseAlarms"`
		Alarms      int64  `json:"alarms"`
	}
	devices := map[string]*deviceCount{}
	for _, a := range alarms {
		d := devices[a.DeviceID]
		if d == nil {
			d = &deviceCount{DeviceID: a.DeviceID, DeviceName: a.DeviceName}
			devices[a.DeviceID] = d
		}
		d.Alarms++
		if a.AckedAt > 0 && a.AckedAt >= a.FirstTriggeredAt {
			acks = append(acks, a.AckedAt-a.FirstTriggeredAt)
		}
		if a.RequiresVerification() {
			requiring++
		}
		if a.Disposition == nil {
			if a.RequiresVerification() {
				unverified++
			}
			continue
		}
		verified++
		byResult[a.Disposition.Result]++
		if a.Disposition.Result == model.DispositionFalseAlarm {
			d.FalseAlarms++
		}
		if a.Disposition.VerifiedAt >= a.FirstTriggeredAt {
			verifies = append(verifies, a.Disposition.VerifiedAt-a.FirstTriggeredAt)
		}
	}
	top := []deviceCount{}
	for _, d := range devices {
		if d.FalseAlarms > 0 {
			top = append(top, *d)
		}
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].FalseAlarms != top[j].FalseAlarms {
			return top[i].FalseAlarms > top[j].FalseAlarms
		}
		return top[i].DeviceID < top[j].DeviceID
	})
	if len(top) > 10 {
		top = top[:10]
	}
	rate := 0.0
	if verified > 0 {
		rate = float64(byResult[model.DispositionFalseAlarm]) / float64(verified)
	}
	write(w, 200, map[string]any{
		"total": len(alarms), "truncated": truncated, "verified": verified, "requiringVerification": requiring, "unverified": unverified,
		"byResult": byResult, "falseAlarmRate": rate, "acknowledge": summarize(acks), "verify": summarize(verifies), "topFalseAlarmDevices": top,
	})
}

var dispositionNames = map[string]string{model.DispositionRealFire: "真实火警", model.DispositionFalseAlarm: "误报", model.DispositionTest: "测试", model.DispositionMaintenance: "检修", model.DispositionFault: "设备故障"}
var alarmStatusNames = map[string]string{"ACTIVE": "活动", "ACKED": "已确认", "RECOVERED": "已恢复", "CLOSED": "已关闭", "SUPPRESSED": "已抑制"}

// exportAlarms writes the alarms of the period as UTF-8 CSV with a BOM, so
// spreadsheet software opens the Chinese text correctly.
func (s *Server) exportAlarms(w http.ResponseWriter, r *http.Request) {
	alarms, truncated, err := s.reportAlarms(r)
	if err != nil {
		problem(w, 500, "读取告警失败")
		return
	}
	at := func(ms int64) string {
		if ms <= 0 {
			return ""
		}
		return time.UnixMilli(ms).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="alarms-%s.csv"`, time.Now().Format("20060102-150405")))
	if truncated {
		w.Header().Set("X-Export-Truncated", "true")
	}
	_, _ = w.Write([]byte("\ufeff"))
	out := csv.NewWriter(w)
	_ = out.Write([]string{"告警编号", "设备编号", "设备名称", "告警类型", "等级", "状态", "内容", "部件位置", "首次发生", "最后发生", "触发次数", "确认时间", "恢复时间", "关闭时间", "核实结论", "核实人", "核实时间", "到场时间", "处置说明"})
	for _, a := range alarms {
		result, handler, verifiedAt, arrived, notes := "", "", "", "", ""
		if d := a.Disposition; d != nil {
			result, handler, verifiedAt, arrived, notes = dispositionNames[d.Result], d.Handler, at(d.VerifiedAt), at(d.ArrivedAt), d.Notes
		}
		status := alarmStatusNames[a.Status]
		if status == "" {
			status = a.Status
		}
		_ = out.Write([]string{a.ID, a.DeviceID, a.DeviceName, a.AlarmType, a.AlarmLevel, status, a.Content, a.ComponentLocation, at(a.FirstTriggeredAt), at(a.LastTriggeredAt), strconv.Itoa(a.TriggerCount), at(a.AckedAt), at(a.RecoveredAt), at(a.ClosedAt), result, handler, verifiedAt, arrived, notes})
	}
	out.Flush()
	s.audit(r, "alarm.export", "alarm", "", map[string]any{"rows": len(alarms), "truncated": truncated})
}
