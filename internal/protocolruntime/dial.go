package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"net"     /* 执行当前语句并推进处理流程。 */
	"strconv" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// SetAllowedCIDRs uses the same outbound network policy as central Modbus.
func (r *Listeners) SetAllowedCIDRs(cidrs []string) { r.allowedCIDRs = append([]string(nil), cidrs...) } /* 定义 SetAllowedCIDRs 函数。 */

func (h *protocolListener) dial() { /* 定义 dial 函数。 */
	backoff := time.Second   /* 更新 backoff 的值。 */
	for h.ctx.Err() == nil { /* 循环处理当前数据。 */
		p := h.snapshot()                                                                              /* 更新 p 的值。 */
		ctx, cancel := context.WithTimeout(h.ctx, time.Duration(max(1, p.TimeoutMs))*time.Millisecond) /* 更新 cancel 的值。 */
		host, err := ResolveAllowedTarget(ctx, p.Host, h.owner.allowedCIDRs)                           /* 更新 err 的值。 */
		var conn net.Conn                                                                              /* 声明 conn。 */
		if err == nil {                                                                                /* 判断条件并选择处理分支。 */
			conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(p.Port))) /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
		cancel()        /* 执行当前语句并推进处理流程。 */
		if err == nil { /* 判断条件并选择处理分支。 */
			s := h.newSession(conn.RemoteAddr().String(), conn, nil) /* 更新 s 的值。 */
			h.mu.Lock()                                              /* 执行当前语句并推进处理流程。 */
			if h.ctx.Err() != nil {                                  /* 判断条件并选择处理分支。 */
				h.mu.Unlock() /* 执行当前语句并推进处理流程。 */
				conn.Close()  /* 执行当前语句并推进处理流程。 */
				return        /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			h.lastError = ""         /* 更新 h.lastError 的值。 */
			h.sessions[s.remote] = s /* 更新 h.sessions[s.remote] 的值。 */
			h.mu.Unlock()            /* 执行当前语句并推进处理流程。 */
			go s.poll()              /* 执行当前语句并推进处理流程。 */
			s.receiveTCP()           /* 执行当前语句并推进处理流程。 */
			backoff = time.Second    /* 更新 backoff 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			h.mu.Lock()                                /* 执行当前语句并推进处理流程。 */
			h.lastError = limitError(err.Error(), 512) /* 更新 h.lastError 的值。 */
			h.mu.Unlock()                              /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		timer := time.NewTimer(backoff) /* 更新 timer 的值。 */
		select {                        /* 根据条件选择处理路径。 */
		case <-h.ctx.Done(): /* 处理当前分支。 */
			timer.Stop() /* 执行当前语句并推进处理流程。 */
			return       /* 返回当前处理结果。 */
		case <-timer.C: /* 处理当前分支。 */
		} /* 结束当前表达式或代码块。 */
		backoff = min(30*time.Second, backoff*2) /* 更新 backoff 的值。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *listenerSession) poll() { /* 定义 poll 函数。 */
	tick := time.NewTicker(200 * time.Millisecond) /* 更新 tick 的值。 */
	defer tick.Stop()                              /* 安排函数结束时执行清理。 */
	last := map[string]time.Time{}                 /* 更新 last 的值。 */
	for {                                          /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case <-s.done: /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case <-s.host.ctx.Done(): /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case now := <-tick.C: /* 处理当前分支。 */
			p := s.host.snapshot()                          /* 更新 p 的值。 */
			s.mu.Lock()                                     /* 执行当前语句并推进处理流程。 */
			device := s.deviceID                            /* 更新 device 的值。 */
			s.mu.Unlock()                                   /* 执行当前语句并推进处理流程。 */
			if device == "" && p.ConnectionMode == "dial" { /* 判断条件并选择处理分支。 */
				device = p.DeviceID /* 更新 device 的值。 */
			} /* 结束当前表达式或代码块。 */
			if device == "" { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			for _, query := range p.Queries { /* 循环处理当前数据。 */
				if now.Sub(last[query.Type]) < time.Duration(query.IntervalSec)*time.Second { /* 判断条件并选择处理分支。 */
					continue /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				command := map[string]any{}      /* 更新 command 的值。 */
				for k, v := range query.Params { /* 循环处理当前数据。 */
					command[k] = v /* 更新 command[k] 的值。 */
				} /* 结束当前表达式或代码块。 */
				command["type"] = query.Type                                                  /* 执行当前语句并推进处理流程。 */
				command["_scheduled"] = true                                                  /* 执行当前语句并推进处理流程。 */
				last[query.Type] = now                                                        /* 更新 last[query.Type] 的值。 */
				_, err := s.host.owner.Command(s.host.ctx, p.TenantID, p.ID, device, command) /* 更新 err 的值。 */
				if err != nil {                                                               /* 判断条件并选择处理分支。 */
					s.host.owner.warn("scheduled protocol query", err) /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				select { /* 根据条件选择处理路径。 */
				case <-s.done: /* 处理当前分支。 */
					return /* 返回当前处理结果。 */
				default: /* 处理当前分支。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
