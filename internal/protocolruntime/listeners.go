package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"log/slog"      /* 执行当前语句并推进处理流程。 */
	"net"           /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	listenerMaxBuffer    = protocolworker.MaxFrameBytes /* 更新 listenerMaxBuffer 的值。 */
	listenerMaxState     = 64 << 10                     /* 更新 listenerMaxState 的值。 */
	listenerMaxSessions  = 128                          /* 更新 listenerMaxSessions 的值。 */
	listenerIdle         = 2 * time.Minute              /* 更新 listenerIdle 的值。 */
	listenerFrameTimeout = 30 * time.Second             /* 更新 listenerFrameTimeout 的值。 */
) /* 结束当前表达式或代码块。 */

type listenerCall func(context.Context, string, model.ProtocolRelease, protocolworker.Request) (protocolworker.Response, error) /* 定义 listenerCall 类型。 */

// Listeners owns generic TCP/UDP sockets. Protocol packages own framing,
// session state and wire encoding; the host owns tenant/product identity and
// only acknowledges an incoming frame after the ingest callback succeeds.
type Listeners struct { /* 定义 Listeners 类型。 */
	allowedCIDRs       []string                                                                                      /* 执行当前语句并推进处理流程。 */
	registerDevice     func(context.Context, model.DeviceAccessProfile, string, string) (model.ManagedDevice, error) /* 执行当前语句并推进处理流程。 */
	coordinator        *Coordinator                                                                                  /* 执行当前语句并推进处理流程。 */
	connectionMu       sync.Mutex                                                                                    /* 执行当前语句并推进处理流程。 */
	connectionCounts   map[string]int                                                                                /* 执行当前语句并推进处理流程。 */
	connectionReporter func(context.Context, string, string, string, bool, int64) error                              /* 执行当前语句并推进处理流程。 */
	repo               ports.Repository                                                                              /* 执行当前语句并推进处理流程。 */
	root               string                                                                                        /* 执行当前语句并推进处理流程。 */
	ingest             IngestFunc                                                                                    /* 执行当前语句并推进处理流程。 */
	log                *slog.Logger                                                                                  /* 执行当前语句并推进处理流程。 */
	call               listenerCall                                                                                  /* 执行当前语句并推进处理流程。 */
	workers            chan struct{}                                                                                 /* 执行当前语句并推进处理流程。 */
	mu                 sync.Mutex                                                                                    /* 执行当前语句并推进处理流程。 */
	hosts              map[string]*protocolListener                                                                  /* 执行当前语句并推进处理流程。 */
	failures           map[string]string                                                                             /* 执行当前语句并推进处理流程。 */
	registerMu         sync.Mutex                                                                                    /* 执行当前语句并推进处理流程。 */
	once               sync.Once                                                                                     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type protocolListener struct { /* 定义 protocolListener 类型。 */
	lastAcceptedAt int64                       /* 执行当前语句并推进处理流程。 */
	lastError      string                      /* 执行当前语句并推进处理流程。 */
	owner          *Listeners                  /* 执行当前语句并推进处理流程。 */
	ctx            context.Context             /* 执行当前语句并推进处理流程。 */
	cancel         context.CancelFunc          /* 执行当前语句并推进处理流程。 */
	mu             sync.Mutex                  /* 执行当前语句并推进处理流程。 */
	profile        model.DeviceAccessProfile   /* 执行当前语句并推进处理流程。 */
	tcp            net.Listener                /* 执行当前语句并推进处理流程。 */
	udp            net.PacketConn              /* 执行当前语句并推进处理流程。 */
	sessions       map[string]*listenerSession /* 执行当前语句并推进处理流程。 */
	identified     map[string]*listenerSession /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type listenerSession struct { /* 定义 listenerSession 类型。 */
	commandChild *model.ManagedDevice          /* 执行当前语句并推进处理流程。 */
	childRelease model.ProtocolRelease         /* 执行当前语句并推进处理流程。 */
	host         *protocolListener             /* 执行当前语句并推进处理流程。 */
	remote       string                        /* 执行当前语句并推进处理流程。 */
	conn         net.Conn                      /* 执行当前语句并推进处理流程。 */
	addr         net.Addr                      /* 执行当前语句并推进处理流程。 */
	packets      chan []byte                   /* 执行当前语句并推进处理流程。 */
	done         chan struct{}                 /* 执行当前语句并推进处理流程。 */
	once         sync.Once                     /* 执行当前语句并推进处理流程。 */
	mu           sync.Mutex                    /* 执行当前语句并推进处理流程。 */
	closed       bool                          /* 执行当前语句并推进处理流程。 */
	deviceID     string                        /* 执行当前语句并推进处理流程。 */
	state        json.RawMessage               /* 执行当前语句并推进处理流程。 */
	release      model.ProtocolRelease         /* 执行当前语句并推进处理流程。 */
	partial      bool                          /* 执行当前语句并推进处理流程。 */
	frameStarted time.Time                     /* 执行当前语句并推进处理流程。 */
	lastSeen     time.Time                     /* 执行当前语句并推进处理流程。 */
	pending      map[string]chan commandResult /* 执行当前语句并推进处理流程。 */
	commandBusy  bool                          /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type commandResult struct { /* 定义 commandResult 类型。 */
	value map[string]any /* 执行当前语句并推进处理流程。 */
	err   error          /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewListeners(repo ports.Repository, root string, ingest IngestFunc, log *slog.Logger) *Listeners { /* 定义 NewListeners 函数。 */
	return &Listeners{repo: repo, root: root, ingest: ingest, log: log, call: protocolworker.Call, workers: make(chan struct{}, 32), hosts: make(map[string]*protocolListener), failures: make(map[string]string)} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Listeners) SetCoordinator(c *Coordinator) { r.coordinator = c } /* 定义 SetCoordinator 函数。 */

func (r *Listeners) Start(ctx context.Context) { /* 定义 Start 函数。 */
	r.once.Do(func() { /* 执行当前语句并推进处理流程。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			r.reconcile(ctx)                      /* 执行当前语句并推进处理流程。 */
			ticker := time.NewTicker(time.Second) /* 更新 ticker 的值。 */
			defer ticker.Stop()                   /* 安排函数结束时执行清理。 */
			defer r.stop()                        /* 安排函数结束时执行清理。 */
			for {                                 /* 循环处理当前数据。 */
				select { /* 根据条件选择处理路径。 */
				case <-ctx.Done(): /* 处理当前分支。 */
					return /* 返回当前处理结果。 */
				case <-ticker.C: /* 处理当前分支。 */
					r.reconcile(ctx) /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func listenerKey(tenant, profile string) string { return tenant + "\x00" + profile } /* 定义 listenerKey 函数。 */

// Status reports the sockets in this process, without overwriting profile
// configuration when an operator edits or disables it concurrently.
func (r *Listeners) Status(tenant, profile string) (string, string, int64) { /* 定义 Status 函数。 */
	key := listenerKey(tenant, profile)         /* 更新 key 的值。 */
	r.mu.Lock()                                 /* 执行当前语句并推进处理流程。 */
	h, failure := r.hosts[key], r.failures[key] /* 更新 failure 的值。 */
	r.mu.Unlock()                               /* 执行当前语句并推进处理流程。 */
	if h == nil {                               /* 判断条件并选择处理分支。 */
		if failure != "" { /* 判断条件并选择处理分支。 */
			return "ERROR", failure, 0 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "PENDING", "", 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	p := h.snapshot()                              /* 更新 p 的值。 */
	if _, err := r.release(h.ctx, p); err != nil { /* 判断条件并选择处理分支。 */
		return "ERROR", err.Error(), 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Lock()                                      /* 执行当前语句并推进处理流程。 */
	last, lastError := h.lastAcceptedAt, h.lastError /* 更新 lastError 的值。 */
	h.mu.Unlock()                                    /* 执行当前语句并推进处理流程。 */
	if p.ConnectionMode == "dial" {                  /* 判断条件并选择处理分支。 */
		h.mu.Lock()                                    /* 执行当前语句并推进处理流程。 */
		count, message := len(h.sessions), h.lastError /* 更新 message 的值。 */
		h.mu.Unlock()                                  /* 执行当前语句并推进处理流程。 */
		if count == 0 {                                /* 判断条件并选择处理分支。 */
			return "CONNECTING", message, last /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "CONNECTED", "", last /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "LISTENING", lastError, last /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Listeners) reconcile(ctx context.Context) { /* 定义 reconcile 函数。 */
	profiles, err := r.repo.ListDeviceAccessProfiles(ctx, "") /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		r.warn("list protocol listeners", err) /* 执行当前语句并推进处理流程。 */
		return                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.Lock()                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()             /* 安排函数结束时执行清理。 */
	wanted := make(map[string]bool) /* 更新 wanted 的值。 */
	for _, p := range profiles {    /* 循环处理当前数据。 */
		if !p.Enabled || p.EdgeNodeID != "" || !strings.EqualFold(p.Mode, "listener") { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		executionCtx := ctx       /* 更新 executionCtx 的值。 */
		if r.coordinator != nil { /* 判断条件并选择处理分支。 */
			var owned bool                                    /* 声明 owned。 */
			executionCtx, owned = r.coordinator.Claim(ctx, p) /* 更新 owned 的值。 */
			if !owned {                                       /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		p.Network = strings.ToLower(strings.TrimSpace(p.Network)) /* 更新 p.Network 的值。 */
		key := listenerKey(p.TenantID, p.ID)                      /* 更新 key 的值。 */
		wanted[key] = true                                        /* 更新 wanted[key] 的值。 */
		if current := r.hosts[key]; current != nil {              /* 判断条件并选择处理分支。 */
			current.mu.Lock()                                                                                                                                                                                                     /* 执行当前语句并推进处理流程。 */
			old := current.profile                                                                                                                                                                                                /* 更新 old 的值。 */
			unchanged := current.ctx.Err() == nil && old.ConnectionMode == p.ConnectionMode && old.Host == p.Host && old.Port == p.Port && old.Network == p.Network && old.ProductID == p.ProductID && old.DeviceID == p.DeviceID /* 更新 unchanged 的值。 */
			if unchanged {                                                                                                                                                                                                        /* 判断条件并选择处理分支。 */
				current.profile = p /* 更新 current.profile 的值。 */
			} /* 结束当前表达式或代码块。 */
			current.mu.Unlock() /* 执行当前语句并推进处理流程。 */
			if unchanged {      /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			current.stop()       /* 执行当前语句并推进处理流程。 */
			delete(r.hosts, key) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if model.ValidateProtocolAccess(p) != nil || p.TenantID == "" || p.ProductID == "" || p.Port < 1 || p.Port > 65535 || (p.Network != "tcp" && p.Network != "udp") { /* 判断条件并选择处理分支。 */
			r.warn("invalid protocol listener profile", fmt.Errorf("profile %s", p.ID)) /* 验证实际结果符合预期。 */
			continue                                                                    /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, err := r.release(ctx, p); err != nil { /* 判断条件并选择处理分支。 */
			r.failures[key] = err.Error()            /* 更新 r.failures[key] 的值。 */
			r.warn("protocol listener binding", err) /* 执行当前语句并推进处理流程。 */
			continue                                 /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		hostCtx, cancel := context.WithCancel(executionCtx)                                                                                                                    /* 更新 cancel 的值。 */
		h := &protocolListener{owner: r, ctx: hostCtx, cancel: cancel, profile: p, sessions: make(map[string]*listenerSession), identified: make(map[string]*listenerSession)} /* 更新 h 的值。 */
		address := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))                                                                                                              /* 更新 address 的值。 */
		if p.ConnectionMode == "dial" {                                                                                                                                        /* 判断条件并选择处理分支。 */
			err = nil /* 检查错误并决定后续处理。 */
		} else if p.Network == "tcp" { /* 结束当前表达式或代码块。 */
			h.tcp, err = net.Listen("tcp", address) /* 更新 err 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			h.udp, err = net.ListenPacket("udp", address) /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			cancel()                                                                  /* 执行当前语句并推进处理流程。 */
			r.failures[key] = err.Error()                                             /* 更新 r.failures[key] 的值。 */
			r.warn("open protocol listener", fmt.Errorf("profile %s: %w", p.ID, err)) /* 验证实际结果符合预期。 */
			continue                                                                  /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		context.AfterFunc(hostCtx, h.stop) /* 执行当前语句并推进处理流程。 */
		r.hosts[key] = h                   /* 更新 r.hosts[key] 的值。 */
		delete(r.failures, key)            /* 执行当前语句并推进处理流程。 */
		if p.ConnectionMode == "dial" {    /* 判断条件并选择处理分支。 */
			go h.dial() /* 执行当前语句并推进处理流程。 */
		} else if h.tcp != nil { /* 结束当前表达式或代码块。 */
			go h.accept() /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			go h.receiveUDP() /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for key, h := range r.hosts { /* 循环处理当前数据。 */
		if !wanted[key] { /* 判断条件并选择处理分支。 */
			h.stop()             /* 执行当前语句并推进处理流程。 */
			delete(r.hosts, key) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for key := range r.failures { /* 循环处理当前数据。 */
		if !wanted[key] { /* 判断条件并选择处理分支。 */
			delete(r.failures, key) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (r *Listeners) stop() { /* 定义 stop 函数。 */
	r.mu.Lock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()           /* 安排函数结束时执行清理。 */
	for key, h := range r.hosts { /* 循环处理当前数据。 */
		h.stop()             /* 执行当前语句并推进处理流程。 */
		delete(r.hosts, key) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (r *Listeners) warn(message string, err error) { /* 定义 warn 函数。 */
	if r.log != nil { /* 判断条件并选择处理分支。 */
		r.log.Warn(message, "error", err) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (r *Listeners) release(ctx context.Context, p model.DeviceAccessProfile) (model.ProtocolRelease, error) { /* 定义 release 函数。 */
	binding, err := r.repo.GetProductProtocolBinding(ctx, p.TenantID, p.ProductID) /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		return model.ProtocolRelease{}, fmt.Errorf("product binding: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release, err := r.repo.GetProtocolRelease(ctx, p.TenantID, binding.ProtocolID, binding.Version) /* 更新 err 的值。 */
	if err != nil {                                                                                 /* 判断条件并选择处理分支。 */
		return release, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.Status != "PUBLISHED" { /* 判断条件并选择处理分支。 */
		return release, errors.New("listener protocol release is not published") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.ParserType != parser.GoProtocolParserName || release.Artifact["runtime"] != protocolworker.Runtime || !protocolworker.HasCapability(release, "ingress") { /* 判断条件并选择处理分支。 */
		return release, errors.New("listener requires a complete Go protocol package") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !strings.EqualFold(release.Transport, p.Network) && release.Transport != "TCP_UDP" { /* 判断条件并选择处理分支。 */
		return release, errors.New("listener network does not match bound protocol transport") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return release, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *protocolListener) snapshot() model.DeviceAccessProfile { /* 定义 snapshot 函数。 */
	h.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer h.mu.Unlock() /* 安排函数结束时执行清理。 */
	return h.profile    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *protocolListener) stop() { /* 定义 stop 函数。 */
	h.cancel()        /* 执行当前语句并推进处理流程。 */
	if h.tcp != nil { /* 判断条件并选择处理分支。 */
		_ = h.tcp.Close() /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	if h.udp != nil { /* 判断条件并选择处理分支。 */
		_ = h.udp.Close() /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Lock()                                              /* 执行当前语句并推进处理流程。 */
	sessions := make([]*listenerSession, 0, len(h.sessions)) /* 更新 sessions 的值。 */
	for _, s := range h.sessions {                           /* 循环处理当前数据。 */
		sessions = append(sessions, s) /* 更新 sessions 的值。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Unlock()                /* 执行当前语句并推进处理流程。 */
	for _, s := range sessions { /* 循环处理当前数据。 */
		s.close() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (h *protocolListener) newSession(remote string, conn net.Conn, addr net.Addr) *listenerSession { /* 定义 newSession 函数。 */
	return &listenerSession{host: h, remote: remote, conn: conn, addr: addr, done: make(chan struct{}), lastSeen: time.Now(), pending: make(map[string]chan commandResult)} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *protocolListener) accept() { /* 定义 accept 函数。 */
	for { /* 循环处理当前数据。 */
		conn, err := h.tcp.Accept() /* 更新 err 的值。 */
		if err != nil {             /* 判断条件并选择处理分支。 */
			if h.ctx.Err() == nil { /* 判断条件并选择处理分支。 */
				h.owner.warn("accept protocol connection", err) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		h.mu.Lock()                                                       /* 执行当前语句并推进处理流程。 */
		if len(h.sessions) >= listenerMaxSessions || h.ctx.Err() != nil { /* 判断条件并选择处理分支。 */
			h.mu.Unlock()    /* 执行当前语句并推进处理流程。 */
			_ = conn.Close() /* 更新 _ 的值。 */
			continue         /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		s := h.newSession(conn.RemoteAddr().String(), conn, nil) /* 更新 s 的值。 */
		h.sessions[s.remote] = s                                 /* 更新 h.sessions[s.remote] 的值。 */
		h.mu.Unlock()                                            /* 执行当前语句并推进处理流程。 */
		go s.receiveTCP()                                        /* 执行当前语句并推进处理流程。 */
		go s.poll()                                              /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *listenerSession) close() { /* 定义 close 函数。 */
	s.once.Do(func() { /* 执行当前语句并推进处理流程。 */
		close(s.done)      /* 执行当前语句并推进处理流程。 */
		if s.conn != nil { /* 判断条件并选择处理分支。 */
			_ = s.conn.Close() /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		s.mu.Lock()                     /* 执行当前语句并推进处理流程。 */
		s.closed = true                 /* 更新 s.closed 的值。 */
		deviceID := s.deviceID          /* 更新 deviceID 的值。 */
		for id, ch := range s.pending { /* 循环处理当前数据。 */
			ch <- commandResult{err: errors.New("device session closed")} /* 执行当前语句并推进处理流程。 */
			delete(s.pending, id)                                         /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		s.mu.Unlock()                         /* 执行当前语句并推进处理流程。 */
		s.host.mu.Lock()                      /* 执行当前语句并推进处理流程。 */
		if s.host.identified[deviceID] == s { /* 判断条件并选择处理分支。 */
			delete(s.host.identified, deviceID) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if s.host.sessions[s.remote] == s { /* 判断条件并选择处理分支。 */
			delete(s.host.sessions, s.remote) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		s.host.mu.Unlock()  /* 执行当前语句并推进处理流程。 */
		if deviceID != "" { /* 判断条件并选择处理分支。 */
			s.host.owner.reportConnection(s.host.snapshot(), deviceID, false) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *listenerSession) receiveTCP() { /* 定义 receiveTCP 函数。 */
	defer s.close()                  /* 安排函数结束时执行清理。 */
	buffer := make([]byte, 0, 16384) /* 更新 buffer 的值。 */
	chunk := make([]byte, 16384)     /* 更新 chunk 的值。 */
	for {                            /* 循环处理当前数据。 */
		s.mu.Lock()                                   /* 执行当前语句并推进处理流程。 */
		idle := listenerIdle                          /* 更新 idle 的值。 */
		for _, q := range s.host.snapshot().Queries { /* 循环处理当前数据。 */
			idle = max(idle, time.Duration(q.IntervalSec)*time.Second+listenerIdle) /* 更新 idle 的值。 */
		} /* 结束当前表达式或代码块。 */
		deadline := time.Now().Add(idle) /* 更新 deadline 的值。 */
		if s.partial {                   /* 判断条件并选择处理分支。 */
			deadline = s.frameStarted.Add(listenerFrameTimeout) /* 更新 deadline 的值。 */
		} /* 结束当前表达式或代码块。 */
		s.mu.Unlock()                                                                 /* 执行当前语句并推进处理流程。 */
		_ = s.conn.SetReadDeadline(deadline)                                          /* 更新 _ 的值。 */
		n, err := s.conn.Read(chunk[:min(len(chunk), listenerMaxBuffer-len(buffer))]) /* 更新 err 的值。 */
		if n > 0 {                                                                    /* 判断条件并选择处理分支。 */
			if len(buffer)+n > listenerMaxBuffer { /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			buffer = append(buffer, chunk[:n]...) /* 更新 buffer 的值。 */
			for len(buffer) > 0 {                 /* 循环处理当前数据。 */
				consumed, needMore, frameErr := s.frame(buffer, false) /* 更新 frameErr 的值。 */
				if frameErr != nil {                                   /* 判断条件并选择处理分支。 */
					s.host.mu.Lock()                                           /* 执行当前语句并推进处理流程。 */
					s.host.lastError = limitError(frameErr.Error(), 512)       /* 更新 s.host.lastError 的值。 */
					s.host.mu.Unlock()                                         /* 执行当前语句并推进处理流程。 */
					s.host.owner.warn("protocol TCP frame rejected", frameErr) /* 执行当前语句并推进处理流程。 */
					return                                                     /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				if needMore { /* 判断条件并选择处理分支。 */
					break /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				buffer = buffer[consumed:] /* 更新 buffer 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (h *protocolListener) receiveUDP() { /* 定义 receiveUDP 函数。 */
	buffer := make([]byte, 65536) /* 更新 buffer 的值。 */
	for {                         /* 循环处理当前数据。 */
		_ = h.udp.SetReadDeadline(time.Now().Add(time.Second)) /* 更新 _ 的值。 */
		n, addr, err := h.udp.ReadFrom(buffer)                 /* 更新 err 的值。 */
		if err != nil {                                        /* 判断条件并选择处理分支。 */
			if h.ctx.Err() != nil { /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() { /* 判断条件并选择处理分支。 */
				h.owner.warn("receive protocol datagram", err) /* 执行当前语句并推进处理流程。 */
				return                                         /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			h.expireUDP() /* 执行当前语句并推进处理流程。 */
			continue      /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if n == 0 { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		h.mu.Lock()                                            /* 执行当前语句并推进处理流程。 */
		s := h.sessions[addr.String()]                         /* 更新 s 的值。 */
		if s == nil && len(h.sessions) < listenerMaxSessions { /* 判断条件并选择处理分支。 */
			s = h.newSession(addr.String(), nil, addr) /* 更新 s 的值。 */
			s.packets = make(chan []byte, 16)          /* 更新 s.packets 的值。 */
			h.sessions[s.remote] = s                   /* 更新 h.sessions[s.remote] 的值。 */
			go s.processUDP()                          /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		h.mu.Unlock() /* 执行当前语句并推进处理流程。 */
		if s != nil { /* 判断条件并选择处理分支。 */
			select { /* 根据条件选择处理路径。 */
			case s.packets <- append([]byte(nil), buffer[:n]...): /* 处理当前分支。 */
			default: /* 处理当前分支。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		h.expireUDP() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (h *protocolListener) expireUDP() { /* 定义 expireUDP 函数。 */
	h.mu.Lock()                                              /* 执行当前语句并推进处理流程。 */
	sessions := make([]*listenerSession, 0, len(h.sessions)) /* 更新 sessions 的值。 */
	for _, s := range h.sessions {                           /* 循环处理当前数据。 */
		sessions = append(sessions, s) /* 更新 sessions 的值。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Unlock()                /* 执行当前语句并推进处理流程。 */
	for _, s := range sessions { /* 循环处理当前数据。 */
		s.mu.Lock()                                                             /* 执行当前语句并推进处理流程。 */
		expired := time.Since(s.lastSeen) > listenerIdle && len(s.pending) == 0 /* 更新 expired 的值。 */
		s.mu.Unlock()                                                           /* 执行当前语句并推进处理流程。 */
		if expired {                                                            /* 判断条件并选择处理分支。 */
			s.close() /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *listenerSession) processUDP() { /* 定义 processUDP 函数。 */
	defer s.close() /* 安排函数结束时执行清理。 */
	for {           /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case <-s.done: /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case <-s.host.ctx.Done(): /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case packet := <-s.packets: /* 处理当前分支。 */
			if _, _, err := s.frame(packet, true); err != nil { /* 判断条件并选择处理分支。 */
				s.host.owner.warn("protocol UDP frame rejected", err) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// selectRelease is called under the session lock. A partially received frame
// and outstanding commands keep their original wire semantics until complete.
func (s *listenerSession) selectRelease(ctx context.Context, p model.DeviceAccessProfile) error { /* 定义 selectRelease 函数。 */
	if s.release.Version != "" && (s.partial || len(s.pending) > 0) { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release, err := s.host.owner.release(ctx, p) /* 更新 err 的值。 */
	if err != nil {                              /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.release.ProtocolID != release.ProtocolID || s.release.Version != release.Version { /* 判断条件并选择处理分支。 */
		s.state = nil /* 更新 s.state 的值。 */
	} /* 结束当前表达式或代码块。 */
	s.release = release /* 更新 s.release 的值。 */
	return nil          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *listenerSession) invoke(ctx context.Context, p model.DeviceAccessProfile, request protocolworker.Request) (protocolworker.Response, error) { /* 定义 invoke 函数。 */
	r := s.host.owner                                        /* 更新 r 的值。 */
	timeout := time.Duration(p.TimeoutMs) * time.Millisecond /* 更新 timeout 的值。 */
	if timeout <= 0 {                                        /* 判断条件并选择处理分支。 */
		timeout = 5 * time.Second /* 更新 timeout 的值。 */
	} /* 结束当前表达式或代码块。 */
	if timeout > 30*time.Second { /* 判断条件并选择处理分支。 */
		timeout = 30 * time.Second /* 更新 timeout 的值。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(ctx, timeout) /* 更新 cancel 的值。 */
	defer cancel()                                   /* 安排函数结束时执行清理。 */
	select {                                         /* 根据条件选择处理路径。 */
	case r.workers <- struct{}{}: /* 处理当前分支。 */
	case <-ctx.Done(): /* 处理当前分支。 */
		return protocolworker.Response{}, ctx.Err() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer func() { <-r.workers }() /* 安排函数结束时执行清理。 */
	request.Version = 2            /* 更新 request.Version 的值。 */
	if request.DeviceID == "" {    /* 判断条件并选择处理分支。 */
		request.DeviceID = s.deviceID /* 更新 request.DeviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if request.DeviceID == "" { /* 判断条件并选择处理分支。 */
		request.DeviceID = p.DeviceID /* 更新 request.DeviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	request.State = append(json.RawMessage(nil), s.state...) /* 更新 request.State 的值。 */
	if request.Now == 0 {                                    /* 判断条件并选择处理分支。 */
		request.Now = time.Now().UnixMilli() /* 更新 request.Now 的值。 */
	} /* 结束当前表达式或代码块。 */
	response, err := r.call(ctx, r.root, s.release, request) /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		return response, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if response.Error != "" { /* 判断条件并选择处理分支。 */
		return response, errors.New(response.Error) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(response.State) > listenerMaxState || (len(response.State) > 0 && !json.Valid(response.State)) { /* 判断条件并选择处理分支。 */
		return response, errors.New("protocol state is invalid or too large") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(response.Reply) > listenerMaxBuffer*2 { /* 判断条件并选择处理分支。 */
		return response, errors.New("protocol reply is too large") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return response, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *listenerSession) frame(data []byte, datagram bool) (int, bool, error) { /* 定义 frame 函数。 */
	s.mu.Lock()                              /* 执行当前语句并推进处理流程。 */
	defer s.mu.Unlock()                      /* 安排函数结束时执行清理。 */
	if s.closed || s.host.ctx.Err() != nil { /* 判断条件并选择处理分支。 */
		return 0, false, errors.New("listener stopped") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	p := s.host.snapshot() /* 更新 p 的值。 */
	if s.deviceID != "" {  /* 判断条件并选择处理分支。 */
		s.host.mu.Lock()                         /* 执行当前语句并推进处理流程。 */
		current := s.host.identified[s.deviceID] /* 更新 current 的值。 */
		s.host.mu.Unlock()                       /* 执行当前语句并推进处理流程。 */
		if current != s {                        /* 判断条件并选择处理分支。 */
			return 0, false, errors.New("device reconnected on another session") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := s.selectRelease(s.host.ctx, p); err != nil { /* 判断条件并选择处理分支。 */
		return 0, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	receivedAt := time.Now().UnixMilli()                                                                                                    /* 更新 receivedAt 的值。 */
	response, err := s.invoke(s.host.ctx, p, protocolworker.Request{Operation: "ingress", Data: hex.EncodeToString(data), Now: receivedAt}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                         /* 判断条件并选择处理分支。 */
		return 0, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if response.NeedMore { /* 判断条件并选择处理分支。 */
		if datagram || response.Consumed != 0 || len(data) >= listenerMaxBuffer { /* 判断条件并选择处理分支。 */
			return 0, false, errors.New("invalid incomplete protocol frame") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if !s.partial { /* 判断条件并选择处理分支。 */
			s.frameStarted = time.Now() /* 更新 s.frameStarted 的值。 */
		} /* 结束当前表达式或代码块。 */
		s.partial = true    /* 更新 s.partial 的值。 */
		return 0, true, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if response.Consumed <= 0 || response.Consumed > len(data) || (datagram && response.Consumed != len(data)) { /* 判断条件并选择处理分支。 */
		return 0, false, errors.New("invalid protocol consumed length") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	reply, err := hex.DecodeString(response.Reply) /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		return 0, false, errors.New("invalid protocol reply hex") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	deviceID := strings.TrimSpace(response.DeviceID) /* 更新 deviceID 的值。 */
	if deviceID == "" {                              /* 判断条件并选择处理分支。 */
		deviceID = s.deviceID /* 更新 deviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if deviceID == "" { /* 判断条件并选择处理分支。 */
		deviceID = p.DeviceID /* 更新 deviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if s.deviceID != "" && deviceID != s.deviceID { /* 判断条件并选择处理分支。 */
		return 0, false, errors.New("protocol session device identity changed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(response.Children) > 0 && s.deviceID == "" { /* 判断条件并选择处理分支。 */
		return 0, false, errors.New("主设备注册完成后才能上报子设备") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	device, err := s.host.owner.device(s.host.ctx, p, deviceID, response.DeviceName) /* 更新 err 的值。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		return 0, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	payload, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(data[:response.Consumed])))                                                                                                                                                                                                                                                                                                                                                                                                                    /* 更新 _ 的值。 */
	raw := model.RawMessage{Source: "go-protocol-" + p.Network + "-listener", TenantID: p.TenantID, ProductID: p.ProductID, DeviceID: device.ID, DeviceName: device.Name, Protocol: s.release.ProtocolID, Transport: strings.ToUpper(p.Network), PayloadFormat: "hex", Payload: payload, RemoteAddress: s.remote, ProtocolID: s.release.ProtocolID, ProtocolVersion: s.release.Version, PointTableVersion: s.release.PointTableVersion, CollectorID: p.CollectorID, Metadata: map[string]any{"profileId": p.ID}} /* 更新 raw 的值。 */
	// Decode runs in a fresh process, possibly asynchronously or during replay.
	// Preserve the state from before this frame instead of consulting a live session.
	if len(s.state) > 0 { /* 判断条件并选择处理分支。 */
		raw.Metadata["protocolState"] = append(json.RawMessage(nil), s.state...) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if len(response.Children) > 0 { /* 判断条件并选择处理分支。 */
		raw.Metadata["children"] = response.Children /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	raw.ReceivedAt = receivedAt                  /* 更新 raw.ReceivedAt 的值。 */
	raw.Normalize(time.Now())                    /* 执行当前语句并推进处理流程。 */
	if c := s.host.owner.coordinator; c != nil { /* 判断条件并选择处理分支。 */
		if err := c.Validate(s.host.ctx, s.host.snapshot()); err != nil { /* 判断条件并选择处理分支。 */
			return 0, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := s.host.owner.ingest(s.host.ctx, raw); err != nil { /* 判断条件并选择处理分支。 */
		return 0, false, fmt.Errorf("ingest protocol frame: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := s.ingestChildren(p, device, raw, response.Children); err != nil { /* 判断条件并选择处理分支。 */
		return 0, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.partial = false     /* 更新 s.partial 的值。 */
	if s.deviceID == "" { /* 判断条件并选择处理分支。 */
		s.host.owner.reportConnection(p, device.ID, true) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	s.deviceID = device.ID                                                     /* 更新 s.deviceID 的值。 */
	s.lastSeen = time.Now()                                                    /* 更新 s.lastSeen 的值。 */
	s.host.mu.Lock()                                                           /* 执行当前语句并推进处理流程。 */
	previous := s.host.identified[device.ID]                                   /* 更新 previous 的值。 */
	s.host.identified[device.ID] = s                                           /* 更新 s.host.identified[device.ID] 的值。 */
	s.host.lastError = ""                                                      /* 更新 s.host.lastError 的值。 */
	s.host.lastAcceptedAt = max(s.host.lastAcceptedAt, s.lastSeen.UnixMilli()) /* 更新 s.host.lastAcceptedAt 的值。 */
	s.host.mu.Unlock()                                                         /* 执行当前语句并推进处理流程。 */
	if previous != nil && previous != s {                                      /* 判断条件并选择处理分支。 */
		go previous.close() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	s.state = append(json.RawMessage(nil), response.State...) /* 更新 s.state 的值。 */
	if len(reply) > 0 {                                       /* 判断条件并选择处理分支。 */
		if err := s.write(reply); err != nil { /* 判断条件并选择处理分支。 */
			return 0, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if ch := s.pending[response.CorrelationID]; response.CorrelationID != "" && ch != nil { /* 判断条件并选择处理分支。 */
		if s.commandChild != nil { /* 判断条件并选择处理分支。 */
			matched := false                      /* 更新 matched 的值。 */
			for _, c := range response.Children { /* 循环处理当前数据。 */
				if c.Address == s.commandChild.ChildAddress && c.Type == s.commandChild.ChildType { /* 判断条件并选择处理分支。 */
					matched = true /* 更新 matched 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if !matched { /* 判断条件并选择处理分支。 */
				return response.Consumed, false, nil /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		delete(s.pending, response.CorrelationID)                                                                                                                                                                              /* 执行当前语句并推进处理流程。 */
		ch <- commandResult{value: map[string]any{"status": "acknowledged", "correlationId": response.CorrelationID, "rawMessageId": raw.MessageID, "protocolId": s.release.ProtocolID, "protocolVersion": s.release.Version}} /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return response.Consumed, false, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Listeners) device(ctx context.Context, p model.DeviceAccessProfile, id, name string) (model.ManagedDevice, error) { /* 定义 device 函数。 */
	if !protocolworker.ValidDeviceID(id) { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, errors.New("protocol returned invalid device id") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.DeviceID != "" && p.DeviceID != id { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, errors.New("protocol device does not match access profile") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	current, e := r.repo.GetDeviceAccessProfile(ctx, p.TenantID, p.ID)                /* 更新 e 的值。 */
	if e != nil || !current.Enabled || current.Configuration() != p.Configuration() { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, errors.New("protocol access profile is disabled or changed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, err := r.repo.GetProduct(ctx, p.TenantID, p.ProductID) /* 更新 err 的值。 */
	if err != nil || product.Status != "ENABLED" {                  /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, errors.New("protocol product is disabled or unavailable") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.registerMu.Lock()                                         /* 执行当前语句并推进处理流程。 */
	defer r.registerMu.Unlock()                                 /* 安排函数结束时执行清理。 */
	device, err := r.repo.GetManagedDevice(ctx, p.TenantID, id) /* 更新 err 的值。 */
	if err == nil {                                             /* 判断条件并选择处理分支。 */
		if device.GatewayID != "" || device.TenantID != p.TenantID || device.ProductID != p.ProductID || strings.EqualFold(device.Status, "DISABLED") { /* 判断条件并选择处理分支。 */
			return device, errors.New("protocol device is disabled or belongs to another product") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return device, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !p.AutoRegister { /* 判断条件并选择处理分支。 */
		return device, fmt.Errorf("protocol device is not registered: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if r.registerDevice != nil { /* 判断条件并选择处理分支。 */
		registered, err := r.registerDevice(ctx, p, id, name) /* 更新 err 的值。 */
		if err != nil {                                       /* 判断条件并选择处理分支。 */
			return device, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if registered.ID != id || model.RegisteredProtocolDevice(registered, p) != nil { /* 判断条件并选择处理分支。 */
			return device, model.ErrProtocolRegistration /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		name = registered.Name /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	device, _, err = r.repo.RegisterProtocolDevice(ctx, p, id, name) /* 更新 err 的值。 */
	return device, err                                               /* 返回当前处理结果。 */

} /* 结束当前表达式或代码块。 */

// write is serialized by the session lock with ingress and command encoding.
func (s *listenerSession) write(data []byte) error { /* 定义 write 函数。 */
	if s.conn != nil { /* 判断条件并选择处理分支。 */
		_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)) /* 更新 _ 的值。 */
		for len(data) > 0 {                                          /* 循环处理当前数据。 */
			n, err := s.conn.Write(data) /* 更新 err 的值。 */
			if err != nil {              /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if n == 0 { /* 判断条件并选择处理分支。 */
				return io.ErrShortWrite /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			data = data[n:] /* 更新 data 的值。 */
		} /* 结束当前表达式或代码块。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	n, err := s.host.udp.WriteTo(data, s.addr) /* 更新 err 的值。 */
	if err == nil && n != len(data) {          /* 判断条件并选择处理分支。 */
		return io.ErrShortWrite /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Command dispatches through the package that owns the online session. A
// nonempty correlationId waits for matching, successfully ingested ingress;
// packages without correlation support return an explicit sent status.
func (r *Listeners) Command(ctx context.Context, tenant, profileID, deviceID string, command map[string]any) (map[string]any, error) { /* 定义 Command 函数。 */
	r.mu.Lock()                                  /* 执行当前语句并推进处理流程。 */
	h := r.hosts[listenerKey(tenant, profileID)] /* 更新 h 的值。 */
	r.mu.Unlock()                                /* 执行当前语句并推进处理流程。 */
	if h == nil {                                /* 判断条件并选择处理分支。 */
		return nil, errors.New("protocol listener is not running") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	requestedDevice := deviceID                                                                /* 更新 requestedDevice 的值。 */
	var child *model.ManagedDevice                                                             /* 声明 child。 */
	if d, e := r.repo.GetManagedDevice(ctx, tenant, deviceID); e == nil && d.GatewayID != "" { /* 判断条件并选择处理分支。 */
		child = &d             /* 更新 child 的值。 */
		deviceID = d.GatewayID /* 更新 deviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Lock()                                              /* 执行当前语句并推进处理流程。 */
	sessions := make([]*listenerSession, 0, len(h.sessions)) /* 更新 sessions 的值。 */
	if active := h.identified[deviceID]; active != nil {     /* 判断条件并选择处理分支。 */
		sessions = append(sessions, active) /* 更新 sessions 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		for _, s := range h.sessions { /* 循环处理当前数据。 */
			sessions = append(sessions, s) /* 更新 sessions 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Unlock()                /* 执行当前语句并推进处理流程。 */
	for _, s := range sessions { /* 循环处理当前数据。 */
		s.mu.Lock()                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
		if s.closed || (s.deviceID != deviceID && !(s.deviceID == "" && h.snapshot().ConnectionMode == "dial" && h.snapshot().DeviceID == deviceID)) || deviceID == "" { /* 判断条件并选择处理分支。 */
			s.mu.Unlock() /* 执行当前语句并推进处理流程。 */
			continue      /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if s.partial { /* 判断条件并选择处理分支。 */
			s.mu.Unlock()                                                                    /* 执行当前语句并推进处理流程。 */
			return nil, errors.New("device is receiving an incomplete frame; retry command") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		p := h.snapshot()                                         /* 更新 p 的值。 */
		if _, err := r.device(ctx, p, deviceID, ""); err != nil { /* 判断条件并选择处理分支。 */
			s.mu.Unlock()   /* 执行当前语句并推进处理流程。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := s.selectRelease(ctx, p); err != nil { /* 判断条件并选择处理分支。 */
			s.mu.Unlock()   /* 执行当前语句并推进处理流程。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if s.commandBusy || len(s.pending) > 0 { /* 判断条件并选择处理分支。 */
			s.mu.Unlock()                                                                /* 执行当前语句并推进处理流程。 */
			return nil, errors.New("device query is in progress; wait for its response") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if c := r.coordinator; c != nil { /* 判断条件并选择处理分支。 */
			if err := c.Validate(ctx, p); err != nil { /* 判断条件并选择处理分支。 */
				s.mu.Unlock()   /* 执行当前语句并推进处理流程。 */
				return nil, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		s.commandBusy = true /* 更新 s.commandBusy 的值。 */
		defer func() {       /* 安排函数结束时执行清理。 */
			s.mu.Lock()                              /* 执行当前语句并推进处理流程。 */
			s.commandBusy = false                    /* 更新 s.commandBusy 的值。 */
			s.commandChild = nil                     /* 更新 s.commandChild 的值。 */
			s.childRelease = model.ProtocolRelease{} /* 更新 s.childRelease 的值。 */
			s.mu.Unlock()                            /* 执行当前语句并推进处理流程。 */
		}() /* 结束当前表达式或代码块。 */
		response, err := s.encodeCommand(ctx, p, child, command) /* 更新 err 的值。 */
		if err != nil {                                          /* 判断条件并选择处理分支。 */
			s.mu.Unlock()   /* 执行当前语句并推进处理流程。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		wire, err := hex.DecodeString(response.Reply)                             /* 更新 err 的值。 */
		if err != nil || len(wire) == 0 || (s.conn == nil && len(wire) > 65507) { /* 判断条件并选择处理分支。 */
			s.mu.Unlock()                                                         /* 执行当前语句并推进处理流程。 */
			return nil, errors.New("protocol command bytes are empty or invalid") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		id := response.CorrelationID                   /* 更新 id 的值。 */
		if command["_scheduled"] == true && id == "" { /* 判断条件并选择处理分支。 */
			s.mu.Unlock()                                                           /* 执行当前语句并推进处理流程。 */
			return nil, errors.New("scheduled query requires response correlation") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(id) > 256 { /* 判断条件并选择处理分支。 */
			s.mu.Unlock()                                                         /* 执行当前语句并推进处理流程。 */
			return nil, errors.New("protocol command correlation id is too long") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, exists := s.pending[id]; id != "" && exists { /* 判断条件并选择处理分支。 */
			s.mu.Unlock()                                                       /* 执行当前语句并推进处理流程。 */
			return nil, errors.New("duplicate protocol command correlation id") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		ch := make(chan commandResult, 1) /* 更新 ch 的值。 */
		if id != "" {                     /* 判断条件并选择处理分支。 */
			s.pending[id] = ch /* 更新 s.pending[id] 的值。 */
		} /* 结束当前表达式或代码块。 */
		if err := s.write(wire); err != nil { /* 判断条件并选择处理分支。 */
			delete(s.pending, id) /* 执行当前语句并推进处理流程。 */
			s.mu.Unlock()         /* 执行当前语句并推进处理流程。 */
			s.close()             /* 执行当前语句并推进处理流程。 */
			return nil, err       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		s.state = append(json.RawMessage(nil), response.State...)      /* 更新 s.state 的值。 */
		version, protocolID := s.release.Version, s.release.ProtocolID /* 更新 protocolID 的值。 */
		s.mu.Unlock()                                                  /* 执行当前语句并推进处理流程。 */
		if id == "" {                                                  /* 判断条件并选择处理分支。 */
			return map[string]any{"status": "sent", "protocolId": protocolID, "protocolVersion": version, "deviceId": requestedDevice}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		timeout := time.Duration(p.TimeoutMs) * time.Millisecond /* 更新 timeout 的值。 */
		if timeout <= 0 {                                        /* 判断条件并选择处理分支。 */
			timeout = 5 * time.Second /* 更新 timeout 的值。 */
		} /* 结束当前表达式或代码块。 */
		if timeout > 30*time.Second { /* 判断条件并选择处理分支。 */
			timeout = 30 * time.Second /* 更新 timeout 的值。 */
		} /* 结束当前表达式或代码块。 */
		waitCtx, cancel := context.WithTimeout(ctx, timeout) /* 更新 cancel 的值。 */
		defer cancel()                                       /* 安排函数结束时执行清理。 */
		defer func() {                                       /* 安排函数结束时执行清理。 */
			s.mu.Lock()              /* 执行当前语句并推进处理流程。 */
			if s.pending[id] == ch { /* 判断条件并选择处理分支。 */
				delete(s.pending, id) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			s.mu.Unlock() /* 执行当前语句并推进处理流程。 */
		}() /* 结束当前表达式或代码块。 */
		select { /* 根据条件选择处理路径。 */
		case result := <-ch: /* 处理当前分支。 */
			return result.value, result.err /* 返回当前处理结果。 */
		case <-waitCtx.Done(): /* 处理当前分支。 */
			s.close()                                                                /* 执行当前语句并推进处理流程。 */ // Late responses must never satisfy a later query on this connection.
			return nil, fmt.Errorf("wait protocol command reply: %w", waitCtx.Err()) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil, errors.New("device has no online protocol session") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// SetDeviceRegistrar is configured before Start. Edge nodes must obtain real
// platform registration before acknowledging an unknown protocol identity.
func (r *Listeners) SetDeviceRegistrar(register func(context.Context, model.DeviceAccessProfile, string, string) (model.ManagedDevice, error)) { /* 定义 SetDeviceRegistrar 函数。 */
	r.registerDevice = register /* 更新 r.registerDevice 的值。 */
} /* 结束当前表达式或代码块。 */
