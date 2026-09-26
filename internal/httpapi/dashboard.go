package httpapi

import (
	"iot-platform/internal/model"
	"net/http"
	"sort"
	"strconv"
	"time"
)

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || (n != 7 && n != 30) {
			problem(w, 400, "days must be 7 or 30")
			return
		}
		days = n
	}
	offset := 480
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < -720 || n > 840 {
			problem(w, 400, "invalid timezone offset")
			return
		}
		offset = n
	}
	now := time.Now().In(time.FixedZone("dashboard", offset*60))
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1-days)
	groups, err := s.engine.Repo.DashboardCounts(r.Context(), claims(r).TenantID, start.UnixMilli(), now.UnixMilli())
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	states, levels := map[string]int{}, map[string]int{}
	connections, dataStatuses := map[string]int{}, map[string]int{}
	alarmStatuses, alarmTypes := map[string]int{}, map[string]int{}
	products := []model.DashboardCount{}
	trend := make([]map[string]any, days)
	for i := range trend {
		trend[i] = map[string]any{"date": start.AddDate(0, 0, i).Format("2006-01-02"), "count": 0}
	}
	devices, active, high := 0, 0, 0
	for _, v := range groups {
		switch v.Kind {
		case "connection":
			connections[v.Key] += v.Count
		case "dataStatus":
			dataStatuses[v.Key] += v.Count
		case "alarmStatus":
			alarmStatuses[v.Key] += v.Count
		case "alarmType":
			alarmTypes[v.Key] += v.Count
		case "state":
			states[v.Key] += v.Count
			devices += v.Count
		case "level":
			levels[v.Key] += v.Count
			active += v.Count
			if v.Key == "HIGH" || v.Key == "CRITICAL" {
				high += v.Count
			}
		case "product":
			products = append(products, v)
		case "day":
			i, e := strconv.Atoi(v.Key)
			if e == nil && i >= 0 && i < days {
				trend[i]["count"] = v.Count
			}
		}
	}
	sort.Slice(products, func(i, j int) bool {
		if products[i].Count == products[j].Count {
			return products[i].Key < products[j].Key
		}
		return products[i].Count > products[j].Count
	})
	write(w, 200, map[string]any{
		"devices": devices, "online": states["ONLINE"], "activeAlarms": active, "highAlarms": high,
		"states": states, "levels": levels, "products": products, "trend": trend,
		"connections": connections, "dataStatuses": dataStatuses, "alarmStatuses": alarmStatuses, "alarmTypes": alarmTypes,
		"days": days, "offset": offset, "updatedAt": now.UnixMilli(),
	})
}
