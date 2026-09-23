package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"net/http"                    /* 执行当前语句并推进处理流程。 */
	"sort"                        /* 执行当前语句并推进处理流程。 */
	"strconv"                     /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) { /* 定义 dashboard 函数。 */
	days := 7                                    /* 更新 days 的值。 */
	if v := r.URL.Query().Get("days"); v != "" { /* 判断条件并选择处理分支。 */
		n, err := strconv.Atoi(v)              /* 更新 err 的值。 */
		if err != nil || (n != 7 && n != 30) { /* 判断条件并选择处理分支。 */
			problem(w, 400, "days must be 7 or 30") /* 执行当前语句并推进处理流程。 */
			return                                  /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		days = n /* 更新 days 的值。 */
	} /* 结束当前表达式或代码块。 */
	offset := 480                                  /* 更新 offset 的值。 */
	if v := r.URL.Query().Get("offset"); v != "" { /* 判断条件并选择处理分支。 */
		n, err := strconv.Atoi(v)              /* 更新 err 的值。 */
		if err != nil || n < -720 || n > 840 { /* 判断条件并选择处理分支。 */
			problem(w, 400, "invalid timezone offset") /* 执行当前语句并推进处理流程。 */
			return                                     /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		offset = n /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().In(time.FixedZone("dashboard", offset*60))                                                      /* 更新 now 的值。 */
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1-days)          /* 更新 start 的值。 */
	groups, err := s.engine.Repo.DashboardCounts(r.Context(), claims(r).TenantID, start.UnixMilli(), now.UnixMilli()) /* 更新 err 的值。 */
	if err != nil {                                                                                                   /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	states, levels := map[string]int{}, map[string]int{} /* 更新 levels 的值。 */
	products := []model.DashboardCount{}                 /* 更新 products 的值。 */
	trend := make([]map[string]any, days)                /* 更新 trend 的值。 */
	for i := range trend {                               /* 循环处理当前数据。 */
		trend[i] = map[string]any{"date": start.AddDate(0, 0, i).Format("2006-01-02"), "count": 0} /* 更新 trend[i] 的值。 */
	} /* 结束当前表达式或代码块。 */
	devices, active, high := 0, 0, 0 /* 更新 high 的值。 */
	for _, v := range groups {       /* 循环处理当前数据。 */
		switch v.Kind { /* 根据条件选择处理路径。 */
		case "state": /* 处理当前分支。 */
			states[v.Key] += v.Count /* 更新 states[v.Key] 的值。 */
			devices += v.Count       /* 更新 devices 的值。 */
		case "level": /* 处理当前分支。 */
			levels[v.Key] += v.Count                    /* 更新 levels[v.Key] 的值。 */
			active += v.Count                           /* 更新 active 的值。 */
			if v.Key == "HIGH" || v.Key == "CRITICAL" { /* 判断条件并选择处理分支。 */
				high += v.Count /* 更新 high 的值。 */
			} /* 结束当前表达式或代码块。 */
		case "product": /* 处理当前分支。 */
			products = append(products, v) /* 更新 products 的值。 */
		case "day": /* 处理当前分支。 */
			i, e := strconv.Atoi(v.Key)         /* 更新 e 的值。 */
			if e == nil && i >= 0 && i < days { /* 判断条件并选择处理分支。 */
				trend[i]["count"] = v.Count /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(products, func(i, j int) bool { /* 执行当前语句并推进处理流程。 */
		if products[i].Count == products[j].Count { /* 判断条件并选择处理分支。 */
			return products[i].Key < products[j].Key /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return products[i].Count > products[j].Count /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"devices": devices, "online": states["ONLINE"], "activeAlarms": active, "highAlarms": high, "states": states, "levels": levels, "products": products, "trend": trend, "days": days, "offset": offset, "updatedAt": now.UnixMilli()}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
