package protocolruntime /* 声明 protocolruntime 包。 */

// Sessions returns a tenant-scoped snapshot without exposing protocol state.
func (r *Listeners) Sessions(tenant, profile string) []map[string]any { /* 定义 Sessions 函数。 */
	r.mu.Lock()                                /* 执行当前语句并推进处理流程。 */
	h := r.hosts[listenerKey(tenant, profile)] /* 更新 h 的值。 */
	r.mu.Unlock()                              /* 执行当前语句并推进处理流程。 */
	out := []map[string]any{}                  /* 更新 out 的值。 */
	if h == nil {                              /* 判断条件并选择处理分支。 */
		return out /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Lock()                                              /* 执行当前语句并推进处理流程。 */
	sessions := make([]*listenerSession, 0, len(h.sessions)) /* 更新 sessions 的值。 */
	for _, s := range h.sessions {                           /* 循环处理当前数据。 */
		sessions = append(sessions, s) /* 更新 sessions 的值。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Unlock()                /* 执行当前语句并推进处理流程。 */
	for _, s := range sessions { /* 循环处理当前数据。 */
		s.mu.Lock()    /* 执行当前语句并推进处理流程。 */
		if !s.closed { /* 判断条件并选择处理分支。 */
			last := int64(0)          /* 更新 last 的值。 */
			if !s.lastSeen.IsZero() { /* 判断条件并选择处理分支。 */
				last = s.lastSeen.UnixMilli() /* 更新 last 的值。 */
			} /* 结束当前表达式或代码块。 */
			out = append(out, map[string]any{"deviceId": s.deviceID, "remoteAddress": s.remote, "lastSeenAt": last, "protocolId": s.release.ProtocolID, "protocolVersion": s.release.Version}) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
		s.mu.Unlock() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
