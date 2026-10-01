package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/firesafety"
	"iot-platform/internal/model"
)

var fireSafetyResources = []struct{ path, kind, name, save, remove string }{
	{"/api/v1/fire-stations", "stations", "消防站", "saveStation", "deleteStation"},
	{"/api/v1/fire-personnel", "personnel", "消防人员", "savePersonnel", "deletePersonnel"},
	{"/api/v1/fire-equipment", "equipment", "消防器材", "saveEquipment", "deleteEquipment"},
	{"/api/v1/duty/shifts", "shifts", "班次模板", "saveShift", "deleteShift"},
	{"/api/v1/duty/assignments", "assignments", "排班", "saveAssignment", "deleteAssignment"},
	{"/api/v1/extinguishers", "extinguishers", "灭火器", "saveExtinguisher", "deleteExtinguisher"},
}

func fireSafetyMenu(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/duty/"):
		return "duty"
	case strings.HasPrefix(path, "/api/v1/extinguisher"):
		return "extinguishers"
	case strings.HasPrefix(path, "/api/v1/fire-"):
		return "fireStations"
	default:
		return ""
	}
}

func fireSafetyAction(method, path string) string {
	for _, resource := range fireSafetyResources {
		switch method + " " + path {
		case "POST " + resource.path:
			return "新增" + resource.name
		case "PUT " + resource.path + "/:id":
			return "编辑" + resource.name
		case "DELETE " + resource.path + "/:id":
			return "删除" + resource.name
		}
	}
	return map[string]string{
		"POST /api/v1/fire-dispatches": "登记出勤", "POST /api/v1/fire-dispatches/:id/return": "登记归队",
		"POST /api/v1/duty/swaps": "申请换班", "POST /api/v1/duty/swaps/:id/review": "审批换班",
		"POST /api/v1/extinguisher-inspections": "创建巡检任务", "POST /api/v1/extinguisher-inspections/:id/inspect": "提交巡检结果",
		"POST /api/v1/extinguisher-inspections/:id/rectify": "提交整改", "POST /api/v1/extinguisher-inspections/:id/review": "复核整改",
		"POST /api/v1/extinguisher-inspections/:id/cancel": "取消巡检任务",
	}[method+" "+path]
}

func (s *Server) fireSafetyRoutes() {
	for _, resource := range fireSafetyResources {
		s.router.GET(resource.path, s.authorize("viewer"), s.endpoint(s.fireSafetyList(resource.kind)))
		s.router.GET(resource.path+"/:id", s.authorize("viewer"), s.endpoint(s.fireSafetyDetail(resource.kind), "id"))
		s.router.POST(resource.path, s.authorize("operator"), s.endpoint(s.fireSafetyMutation(resource.save)))
		s.router.PUT(resource.path+"/:id", s.authorize("operator"), s.endpoint(s.fireSafetyMutation(resource.save), "id"))
		s.router.DELETE(resource.path+"/:id", s.authorize("operator"), s.endpoint(s.fireSafetyMutation(resource.remove), "id"))
	}
	for _, resource := range []struct{ path, kind, action string }{
		{"/api/v1/fire-dispatches", "dispatches", "createDispatch"},
		{"/api/v1/duty/swaps", "swaps", "createSwap"},
		{"/api/v1/extinguisher-inspections", "inspections", "createInspection"},
	} {
		s.router.GET(resource.path, s.authorize("viewer"), s.endpoint(s.fireSafetyList(resource.kind)))
		s.router.GET(resource.path+"/:id", s.authorize("viewer"), s.endpoint(s.fireSafetyDetail(resource.kind), "id"))
		s.router.POST(resource.path, s.authorize("operator"), s.endpoint(s.fireSafetyMutation(resource.action)))
	}
	for _, route := range []struct{ path, action string }{
		{"/api/v1/fire-dispatches/:id/return", "returnDispatch"},
		{"/api/v1/duty/swaps/:id/review", "reviewSwap"},
		{"/api/v1/extinguisher-inspections/:id/inspect", "inspect"},
		{"/api/v1/extinguisher-inspections/:id/rectify", "rectify"},
		{"/api/v1/extinguisher-inspections/:id/review", "reviewInspection"},
		{"/api/v1/extinguisher-inspections/:id/cancel", "cancelInspection"},
	} {
		s.router.POST(route.path, s.authorize("operator"), s.endpoint(s.fireSafetyMutation(route.action), "id"))
	}
	s.router.GET("/api/v1/fire-safety/options", s.authorize("viewer"), s.endpoint(s.fireSafetyOptions))
	s.router.GET("/api/v1/fire-stations/statistics", s.authorize("viewer"), s.endpoint(s.fireStationStatistics))
	s.router.GET("/api/v1/extinguishers/statistics", s.authorize("viewer"), s.endpoint(s.extinguisherStatistics))
}

func fireSafetyError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, firesafety.ErrValidation):
		status = http.StatusBadRequest
	case errors.Is(err, firesafety.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, firesafety.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, firesafety.ErrForbidden):
		status = http.StatusForbidden
	}
	if status == http.StatusInternalServerError {
		problem(w, status, "消防管理数据读取或保存失败，请稍后重试")
		return
	}
	problem(w, status, err.Error())
}

func (s *Server) fireSafetyMutation(action string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if r.Method == http.MethodDelete {
			version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
			if err != nil || version < 1 {
				problem(w, 400, "请提供当前记录版本后重试")
				return
			}
			body, _ = json.Marshal(map[string]int64{"version": version})
		} else if decode(w, r, &body) != nil {
			return
		}
		c := claims(r)
		result, err := s.fireSafety.Apply(r.Context(), c.TenantID, c.Username, action, r.PathValue("id"), body)
		if err != nil {
			fireSafetyError(w, err)
			return
		}
		targetID := r.PathValue("id")
		if targetID == "" {
			encoded, _ := json.Marshal(result)
			var created struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(encoded, &created)
			targetID = created.ID
		}
		s.audit(r, "fire-safety."+action, "fire-safety", targetID, nil)
		status := http.StatusOK
		if r.Method == http.MethodPost && r.PathValue("id") == "" {
			status = http.StatusCreated
		}
		write(w, status, result)
	}
}

func fireMaps[T any](items []T) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		body, _ := json.Marshal(item)
		var row map[string]any
		_ = json.Unmarshal(body, &row)
		out = append(out, row)
	}
	return out
}

func fireSafetyRows(state model.FireSafetyState, kind string, now time.Time, remindDays int) []map[string]any {
	var rows []map[string]any
	switch kind {
	case "stations":
		rows = fireMaps(state.Stations)
	case "personnel":
		rows = fireMaps(state.Personnel)
	case "equipment":
		rows = fireMaps(state.Equipment)
	case "dispatches":
		rows = fireMaps(state.Dispatches)
	case "shifts":
		rows = fireMaps(state.Shifts)
	case "assignments":
		rows = fireMaps(state.Assignments)
	case "swaps":
		rows = fireMaps(state.Swaps)
		assignments := map[string]model.DutyAssignment{}
		for _, item := range state.Assignments {
			assignments[item.ID] = item
		}
		for _, row := range rows {
			if item, ok := assignments[fireString(row, "assignmentId")]; ok {
				row["assignment"] = item
			}
		}
	case "inspections":
		rows = fireMaps(state.Inspections)
	case "extinguishers":
		rows = fireMaps(state.Extinguishers)
		for i, asset := range state.Extinguishers {
			reminders, last, next := extinguisherReminders(asset, state.Inspections, now, remindDays)
			rows[i]["reminders"], rows[i]["lastInspectedAt"], rows[i]["nextInspectionOn"] = reminders, last, next
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := fireNumber(rows[i], "createdAt"), fireNumber(rows[j], "createdAt")
		if a == b {
			return fireString(rows[i], "id") < fireString(rows[j], "id")
		}
		return a > b
	})
	return rows
}

func fireString(row map[string]any, key string) string { v, _ := row[key].(string); return v }
func fireNumber(row map[string]any, key string) int64  { v, _ := row[key].(float64); return int64(v) }

func fireSafetyFilter(rows []map[string]any, kind string, r *http.Request, state model.FireSafetyState) []map[string]any {
	q := r.URL.Query()
	keyword := strings.ToLower(strings.TrimSpace(q.Get("q")))
	from, to := i64(q.Get("fromAt")), i64(q.Get("toAt"))
	station := q.Get("stationId")
	extStations := map[string]string{}
	assignmentStations := map[string]string{}
	for _, v := range state.Extinguishers {
		extStations[v.ID] = v.StationID
	}
	for _, v := range state.Assignments {
		assignmentStations[v.ID] = v.StationID
	}
	out := []map[string]any{}
	for _, row := range rows {
		rowStation := fireString(row, "stationId")
		if kind == "stations" {
			rowStation = fireString(row, "id")
		}
		if kind == "inspections" {
			rowStation = extStations[fireString(row, "extinguisherId")]
		}
		if kind == "swaps" {
			rowStation = assignmentStations[fireString(row, "assignmentId")]
		}
		if station != "" && station != rowStation {
			continue
		}
		if status := q.Get("status"); status != "" && status != fireString(row, "status") {
			continue
		}
		if keyword != "" {
			data, _ := json.Marshal(row)
			searchText := string(data)
			for _, v := range state.Stations {
				if v.ID == rowStation {
					searchText += " " + v.Name
				}
			}
			for _, v := range state.Shifts {
				if v.ID == fireString(row, "shiftId") {
					searchText += " " + v.Name
				}
			}
			for _, v := range state.Personnel {
				matches := v.ID == fireString(row, "assigneeId") || v.ID == fireString(row, "fromPersonnelId") || v.ID == fireString(row, "toPersonnelId")
				if ids, ok := row["personnelIds"].([]any); ok {
					for _, id := range ids {
						matches = matches || id == v.ID
					}
				}
				if matches {
					searchText += " " + v.Name
				}
			}
			for _, v := range state.Extinguishers {
				if v.ID == fireString(row, "extinguisherId") {
					searchText += " " + v.Code + " " + v.Location
				}
			}
			if !strings.Contains(strings.ToLower(searchText), keyword) {
				continue
			}
		}
		switch kind {
		case "assignments":
			if from > 0 && fireNumber(row, "endAt") <= from || to > 0 && fireNumber(row, "startAt") >= to {
				continue
			}
		case "dispatches", "inspections", "swaps":
			key := map[string]string{"dispatches": "startedAt", "inspections": "dueAt", "swaps": "createdAt"}[kind]
			at := fireNumber(row, key)
			if from > 0 && at < from || to > 0 && at >= to {
				continue
			}
		}
		if kind == "extinguishers" && q.Get("due") != "" {
			matched, overdue := false, false
			for _, reminder := range row["reminders"].([]fireReminder) {
				if reminder.Status == "overdue" {
					overdue = true
				}
				if reminder.Status == q.Get("due") {
					matched = true
				}
			}
			if !matched || q.Get("due") == "soon" && overdue {
				continue
			}
		}
		out = append(out, row)
	}
	return out
}

func (s *Server) fireSafetyList(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := s.fireSafety.Snapshot(r.Context(), claims(r).TenantID)
		if err != nil {
			fireSafetyError(w, err)
			return
		}
		rows := fireSafetyFilter(fireSafetyRows(state, kind, time.Now(), fireRemindDays(r)), kind, r, state)
		pagination := parseListPagination(r)
		items, total := pageItems(rows, pagination)
		writeList(w, 200, items, total, pagination, nil)
	}
}

func (s *Server) fireSafetyDetail(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := s.fireSafety.Snapshot(r.Context(), claims(r).TenantID)
		if err != nil {
			fireSafetyError(w, err)
			return
		}
		for _, row := range fireSafetyRows(state, kind, time.Now(), fireRemindDays(r)) {
			if fireString(row, "id") == r.PathValue("id") {
				write(w, 200, row)
				return
			}
		}
		problem(w, 404, "记录不存在")
	}
}

func (s *Server) fireSafetyOptions(w http.ResponseWriter, r *http.Request) {
	state, err := s.fireSafety.Snapshot(r.Context(), claims(r).TenantID)
	if err != nil {
		fireSafetyError(w, err)
		return
	}
	// Related pages receive only selector fields, never contacts or workflow history.
	stations, personnel := []map[string]any{}, []map[string]any{}
	for _, v := range state.Stations {
		stations = append(stations, map[string]any{"id": v.ID, "name": v.Name, "enabled": v.Enabled})
	}
	for _, v := range state.Personnel {
		personnel = append(personnel, map[string]any{"id": v.ID, "name": v.Name, "stationId": v.StationID, "enabled": v.Enabled})
	}
	out := map[string]any{"stations": stations, "personnel": personnel, "shifts": []model.DutyShift{}, "extinguishers": []map[string]any{}, "equipment": []map[string]any{}}
	if requestAllows(r, "GET", "/api/v1/duty/shifts") {
		out["shifts"] = state.Shifts
	}
	if requestAllows(r, "GET", "/api/v1/extinguishers") {
		items := []map[string]any{}
		for _, v := range state.Extinguishers {
			items = append(items, map[string]any{"id": v.ID, "code": v.Code, "stationId": v.StationID, "status": v.Status})
		}
		out["extinguishers"] = items
	}
	if requestAllows(r, "GET", "/api/v1/fire-equipment") {
		items := []map[string]any{}
		for _, v := range state.Equipment {
			items = append(items, map[string]any{"id": v.ID, "name": v.Name, "stationId": v.StationID, "quantity": v.Quantity, "status": v.Status})
		}
		out["equipment"] = items
	}
	write(w, 200, out)
}

type fireReminder struct {
	Kind   string `json:"kind"`
	DueOn  string `json:"dueOn"`
	Status string `json:"status"`
}

func fireRemindDays(r *http.Request) int {
	return max(0, min(365, intval(r.URL.Query().Get("remindDays"), 30)))
}

func extinguisherReminders(asset model.Extinguisher, inspections []model.FireInspection, now time.Time, days int) ([]fireReminder, int64, string) {
	reminders := []fireReminder{}
	last, accepted := int64(0), int64(0)
	for _, task := range inspections {
		if task.ExtinguisherID != asset.ID {
			continue
		}
		last = max(last, task.InspectedAt)
		if task.Status == "completed" {
			at := task.InspectedAt
			if task.Result == "fail" && len(task.Rectifications) > 0 {
				at = task.Rectifications[len(task.Rectifications)-1].ReviewedAt
			}
			accepted = max(accepted, at)
		}
	}
	if asset.Status == "retired" {
		return reminders, last, ""
	}
	base := time.UnixMilli(max(accepted, asset.CreatedAt)).In(time.Local)
	next := base.AddDate(0, 0, asset.InspectionCycleDays).Format("2006-01-02")
	today := now.In(time.Local).Format("2006-01-02")
	soon := now.In(time.Local).AddDate(0, 0, days).Format("2006-01-02")
	for _, pair := range [][2]string{{"service", asset.ServiceDueOn}, {"retire", asset.RetireOn}, {"inspection", next}} {
		if pair[1] == "" {
			continue
		}
		status := "ok"
		if pair[1] < today {
			status = "overdue"
		} else if pair[1] <= soon {
			status = "soon"
		}
		reminders = append(reminders, fireReminder{pair[0], pair[1], status})
	}
	return reminders, last, next
}

func (s *Server) extinguisherStatistics(w http.ResponseWriter, r *http.Request) {
	state, err := s.fireSafety.Snapshot(r.Context(), claims(r).TenantID)
	if err != nil {
		fireSafetyError(w, err)
		return
	}
	now := time.Now()
	rows := fireSafetyFilter(fireSafetyRows(state, "extinguishers", now, fireRemindDays(r)), "extinguishers", r, state)
	out := map[string]int{"total": len(rows), "active": 0, "maintenance": 0, "retired": 0, "overdue": 0, "soon": 0, "pendingInspections": 0, "overdueInspections": 0, "rectifying": 0, "reviewing": 0}
	ids := map[string]bool{}
	for _, row := range rows {
		ids[fireString(row, "id")] = true
		out[fireString(row, "status")]++
		seen := map[string]bool{}
		for _, reminder := range row["reminders"].([]fireReminder) {
			seen[reminder.Status] = true
		}
		if seen["overdue"] {
			out["overdue"]++
		} else if seen["soon"] {
			out["soon"]++
		}
	}
	for _, task := range state.Inspections {
		if !ids[task.ExtinguisherID] {
			continue
		}
		if task.Status == "pending" {
			out["pendingInspections"]++
		}
		if task.Status == "rectifying" || task.Status == "reviewing" {
			out[task.Status]++
		}
		if task.Status != "completed" && task.Status != "cancelled" && task.DueAt < now.UnixMilli() {
			out["overdueInspections"]++
		}
	}
	write(w, 200, out)
}

func (s *Server) fireStationStatistics(w http.ResponseWriter, r *http.Request) {
	state, err := s.fireSafety.Snapshot(r.Context(), claims(r).TenantID)
	if err != nil {
		fireSafetyError(w, err)
		return
	}
	byStation := []map[string]any{}
	totals := map[string]int{"stations": 0, "personnel": 0, "equipment": 0, "activeDispatches": 0, "dispatches": 0, "returnedDispatches": 0}
	from, to := i64(r.URL.Query().Get("fromAt")), i64(r.URL.Query().Get("toAt"))
	for _, station := range state.Stations {
		if id := r.URL.Query().Get("stationId"); id != "" && station.ID != id {
			continue
		}
		totals["stations"]++
		counts := map[string]int{"personnel": 0, "equipment": 0, "activeDispatches": 0, "dispatches": 0, "returnedDispatches": 0}
		for _, person := range state.Personnel {
			if person.StationID == station.ID {
				counts["personnel"]++
			}
		}
		for _, item := range state.Equipment {
			if item.StationID == station.ID && item.Status != "retired" {
				counts["equipment"] += item.Quantity
			}
		}
		for _, dispatch := range state.Dispatches {
			if dispatch.StationID != station.ID {
				continue
			}
			if dispatch.Status == "dispatched" {
				counts["activeDispatches"]++
			}
			if from > 0 && dispatch.StartedAt < from || to > 0 && dispatch.StartedAt >= to {
				continue
			}
			counts["dispatches"]++
			if dispatch.Status == "returned" {
				counts["returnedDispatches"]++
			}
		}
		row := map[string]any{"stationId": station.ID, "name": station.Name}
		for key, count := range counts {
			row[key] = count
			totals[key] += count
		}
		byStation = append(byStation, row)
	}
	out := map[string]any{"byStation": byStation, "fromAt": from, "toAt": to}
	for key, count := range totals {
		out[key] = count
	}
	write(w, 200, out)
}
