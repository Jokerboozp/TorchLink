package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
	"sync"                        /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type heldExecution struct { /* 定义 heldExecution 类型。 */
	lease   model.ExecutionLease /* 执行当前语句并推进处理流程。 */
	ctx     context.Context      /* 执行当前语句并推进处理流程。 */
	cancel  context.CancelFunc   /* 执行当前语句并推进处理流程。 */
	touched time.Time            /* 执行当前语句并推进处理流程。 */
	timer   *time.Timer          /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// Coordinator fences an execution session on database failure or lost ownership.
// It uses local monotonic deadlines shorter than the database lease, so stale
// sockets stop before another process may acquire the same resource.
type Coordinator struct { /* 定义 Coordinator 类型。 */
	repo            ports.Repository          /* 执行当前语句并推进处理流程。 */
	owner, endpoint string                    /* 执行当前语句并推进处理流程。 */
	mu              sync.Mutex                /* 执行当前语句并推进处理流程。 */
	held            map[string]*heldExecution /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewCoordinator(repo ports.Repository, owner, endpoint string) *Coordinator { /* 定义 NewCoordinator 函数。 */
	return &Coordinator{repo: repo, owner: owner, endpoint: endpoint, held: map[string]*heldExecution{}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (c *Coordinator) Claim(ctx context.Context, p model.DeviceAccessProfile) (context.Context, bool) { /* 定义 Claim 函数。 */
	c.mu.Lock()                                         /* 执行当前语句并推进处理流程。 */
	defer c.mu.Unlock()                                 /* 安排函数结束时执行清理。 */
	k := listenerKey(p.TenantID, p.ID)                  /* 更新 k 的值。 */
	if h := c.held[k]; h != nil && h.ctx.Err() == nil { /* 判断条件并选择处理分支。 */
		h.touched = time.Now() /* 更新 h.touched 的值。 */
		return h.ctx, true     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	start := time.Now()                                                                                                   /* 更新 start 的值。 */
	lease, ok, err := c.repo.AcquireExecutionLease(ctx, p.TenantID, "profile/"+p.ID, c.owner, c.endpoint, 10*time.Second) /* 更新 err 的值。 */
	if err != nil || !ok {                                                                                                /* 判断条件并选择处理分支。 */
		return ctx, false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	session, cancel := context.WithCancel(ctx)                                                           /* 更新 cancel 的值。 */
	timer := time.AfterFunc(time.Until(start.Add(8*time.Second)), cancel)                                /* 更新 timer 的值。 */
	c.held[k] = &heldExecution{lease: lease, ctx: session, cancel: cancel, touched: start, timer: timer} /* 更新 c.held[k] 的值。 */
	return session, true                                                                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (c *Coordinator) Run(ctx context.Context) { /* 定义 Run 函数。 */
	tick := time.NewTicker(time.Second) /* 更新 tick 的值。 */
	defer tick.Stop()                   /* 安排函数结束时执行清理。 */
	defer func() {                      /* 安排函数结束时执行清理。 */
		c.mu.Lock()                /* 执行当前语句并推进处理流程。 */
		defer c.mu.Unlock()        /* 安排函数结束时执行清理。 */
		for _, h := range c.held { /* 循环处理当前数据。 */
			h.timer.Stop()                                                               /* 执行当前语句并推进处理流程。 */
			h.cancel()                                                                   /* 执行当前语句并推进处理流程。 */
			releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second) /* 更新 cancel 的值。 */
			_ = c.repo.ReleaseExecutionLease(releaseCtx, h.lease)                        /* 更新 _ 的值。 */
			cancel()                                                                     /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	for { /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case <-tick.C: /* 处理当前分支。 */
		} /* 结束当前表达式或代码块。 */
		c.mu.Lock()                /* 执行当前语句并推进处理流程。 */
		for k, h := range c.held { /* 循环处理当前数据。 */
			if time.Since(h.touched) > 3*time.Second || h.ctx.Err() != nil { /* 判断条件并选择处理分支。 */
				h.timer.Stop()                                 /* 执行当前语句并推进处理流程。 */
				h.cancel()                                     /* 执行当前语句并推进处理流程。 */
				_ = c.repo.ReleaseExecutionLease(ctx, h.lease) /* 更新 _ 的值。 */
				delete(c.held, k)                              /* 执行当前语句并推进处理流程。 */
				continue                                       /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			renewStarted := time.Now()                                                                                                        /* 更新 renewStarted 的值。 */
			renewCtx, cancel := context.WithTimeout(ctx, time.Second)                                                                         /* 更新 cancel 的值。 */
			lease, ok, err := c.repo.AcquireExecutionLease(renewCtx, h.lease.TenantID, h.lease.Resource, c.owner, c.endpoint, 10*time.Second) /* 更新 err 的值。 */
			cancel()                                                                                                                          /* 执行当前语句并推进处理流程。 */
			if err != nil || !ok || lease.Token != h.lease.Token {                                                                            /* 判断条件并选择处理分支。 */
				h.timer.Stop()    /* 执行当前语句并推进处理流程。 */
				h.cancel()        /* 执行当前语句并推进处理流程。 */
				delete(c.held, k) /* 执行当前语句并推进处理流程。 */
			} else { /* 结束当前表达式或代码块。 */
				h.lease = lease                                              /* 更新 h.lease 的值。 */
				h.timer.Reset(time.Until(renewStarted.Add(8 * time.Second))) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		c.mu.Unlock() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (c *Coordinator) Validate(ctx context.Context, p model.DeviceAccessProfile) error { /* 定义 Validate 函数。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c.mu.Lock()                                /* 执行当前语句并推进处理流程。 */
	h := c.held[listenerKey(p.TenantID, p.ID)] /* 更新 h 的值。 */
	var token int64                            /* 声明 token。 */
	if h != nil {                              /* 判断条件并选择处理分支。 */
		token = h.lease.Token /* 更新 token 的值。 */
	} /* 结束当前表达式或代码块。 */
	c.mu.Unlock()                                                              /* 执行当前语句并推进处理流程。 */
	current, err := c.repo.GetExecutionLease(ctx, p.TenantID, "profile/"+p.ID) /* 更新 err 的值。 */
	if err != nil || current.Owner != c.owner || current.Token != token {      /* 判断条件并选择处理分支。 */
		return errors.New("execution ownership lost") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	profile, err := c.repo.GetDeviceAccessProfile(ctx, p.TenantID, p.ID)                /* 更新 err 的值。 */
	if err != nil || profile.Configuration() != p.Configuration() || !profile.Enabled { /* 判断条件并选择处理分支。 */
		return errors.New("execution configuration changed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
