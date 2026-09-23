package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bufio"           /* 执行当前语句并推进处理流程。 */
	"bytes"           /* 执行当前语句并推进处理流程。 */
	"context"         /* 执行当前语句并推进处理流程。 */
	"crypto/subtle"   /* 执行当前语句并推进处理流程。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"flag"            /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"io"              /* 执行当前语句并推进处理流程。 */
	"log"             /* 执行当前语句并推进处理流程。 */
	"net"             /* 执行当前语句并推进处理流程。 */
	"net/http"        /* 执行当前语句并推进处理流程。 */
	"os"              /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
	"sync"            /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type platformClient struct { /* 定义 platformClient 类型。 */
	baseURL, tenant, token string          /* 执行当前语句并推进处理流程。 */
	http                   *http.Client    /* 执行当前语句并推进处理流程。 */
	mu                     sync.Mutex      /* 执行当前语句并推进处理流程。 */
	devices                map[string]bool /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type deviceSession struct { /* 定义 deviceSession 类型。 */
	deviceID  string                 /* 执行当前语句并推进处理流程。 */
	source    [6]byte                /* 执行当前语句并推进处理流程。 */
	tcp       net.Conn               /* 执行当前语句并推进处理流程。 */
	udp       *net.UDPConn           /* 执行当前语句并推进处理流程。 */
	udpRemote *net.UDPAddr           /* 执行当前语句并推进处理流程。 */
	writeMu   sync.Mutex             /* 执行当前语句并推进处理流程。 */
	mu        sync.Mutex             /* 执行当前语句并推进处理流程。 */
	sequence  uint16                 /* 执行当前语句并推进处理流程。 */
	pending   map[uint16]chan []byte /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type sessionRegistry struct { /* 定义 sessionRegistry 类型。 */
	mu       sync.RWMutex              /* 执行当前语句并推进处理流程。 */
	byDevice map[string]*deviceSession /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func newSessionRegistry() *sessionRegistry { /* 定义 newSessionRegistry 函数。 */
	return &sessionRegistry{byDevice: map[string]*deviceSession{}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *sessionRegistry) registerTCP(deviceID string, source [6]byte, conn net.Conn) *deviceSession { /* 定义 registerTCP 函数。 */
	r.mu.Lock()                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()             /* 安排函数结束时执行清理。 */
	session := r.byDevice[deviceID] /* 更新 session 的值。 */
	if session == nil {             /* 判断条件并选择处理分支。 */
		session = &deviceSession{deviceID: deviceID, pending: map[uint16]chan []byte{}} /* 更新 session 的值。 */
		r.byDevice[deviceID] = session                                                  /* 更新 r.byDevice[deviceID] 的值。 */
	} /* 结束当前表达式或代码块。 */
	session.source, session.tcp, session.udp, session.udpRemote = source, conn, nil, nil /* 更新 session.udpRemote 的值。 */
	return session                                                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *sessionRegistry) registerUDP(deviceID string, source [6]byte, conn *net.UDPConn, remote *net.UDPAddr) *deviceSession { /* 定义 registerUDP 函数。 */
	r.mu.Lock()                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()             /* 安排函数结束时执行清理。 */
	session := r.byDevice[deviceID] /* 更新 session 的值。 */
	if session == nil {             /* 判断条件并选择处理分支。 */
		session = &deviceSession{deviceID: deviceID, pending: map[uint16]chan []byte{}} /* 更新 session 的值。 */
		r.byDevice[deviceID] = session                                                  /* 更新 r.byDevice[deviceID] 的值。 */
	} /* 结束当前表达式或代码块。 */
	session.source, session.tcp, session.udp, session.udpRemote = source, nil, conn, remote /* 更新 session.udpRemote 的值。 */
	return session                                                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *sessionRegistry) remove(deviceID string, session *deviceSession) { /* 定义 remove 函数。 */
	if deviceID == "" || session == nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.Lock()                          /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                  /* 安排函数结束时执行清理。 */
	if r.byDevice[deviceID] == session { /* 判断条件并选择处理分支。 */
		delete(r.byDevice, deviceID) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (r *sessionRegistry) deliver(deviceID string, sequence uint16, frame []byte) { /* 定义 deliver 函数。 */
	r.mu.RLock()                    /* 执行当前语句并推进处理流程。 */
	session := r.byDevice[deviceID] /* 更新 session 的值。 */
	r.mu.RUnlock()                  /* 执行当前语句并推进处理流程。 */
	if session == nil {             /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	session.mu.Lock()                   /* 执行当前语句并推进处理流程。 */
	waiter := session.pending[sequence] /* 更新 waiter 的值。 */
	session.mu.Unlock()                 /* 执行当前语句并推进处理流程。 */
	if waiter == nil {                  /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case waiter <- append([]byte(nil), frame...): /* 处理当前分支。 */
	default: /* 处理当前分支。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *deviceSession) send(frame []byte) error { /* 定义 send 函数。 */
	s.writeMu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer s.writeMu.Unlock() /* 安排函数结束时执行清理。 */
	if s.tcp != nil {        /* 判断条件并选择处理分支。 */
		_ = s.tcp.SetWriteDeadline(time.Now().Add(5 * time.Second)) /* 更新 _ 的值。 */
		_, err := s.tcp.Write(frame)                                /* 更新 err 的值。 */
		return err                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.udp != nil && s.udpRemote != nil { /* 判断条件并选择处理分支。 */
		_, err := s.udp.WriteToUDP(frame, s.udpRemote) /* 更新 err 的值。 */
		return err                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return errors.New("device connection is no longer available") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *deviceSession) requestTimeSync(at time.Time) (uint16, []byte, error) { /* 定义 requestTimeSync 函数。 */
	s.mu.Lock()          /* 执行当前语句并推进处理流程。 */
	s.sequence++         /* 执行当前语句并推进处理流程。 */
	if s.sequence == 0 { /* 判断条件并选择处理分支。 */
		s.sequence = 1 /* 更新 s.sequence 的值。 */
	} /* 结束当前表达式或代码块。 */
	sequence := s.sequence         /* 更新 sequence 的值。 */
	waiter := make(chan []byte, 1) /* 更新 waiter 的值。 */
	s.pending[sequence] = waiter   /* 更新 s.pending[sequence] 的值。 */
	s.mu.Unlock()                  /* 执行当前语句并推进处理流程。 */
	defer func() {                 /* 安排函数结束时执行清理。 */
		s.mu.Lock()                 /* 执行当前语句并推进处理流程。 */
		delete(s.pending, sequence) /* 执行当前语句并推进处理流程。 */
		s.mu.Unlock()               /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
	request := parser.BuildGB26875TimeSyncFrame(sequence, s.source, at) /* 更新 request 的值。 */
	if err := s.send(request); err != nil {                             /* 判断条件并选择处理分支。 */
		return sequence, nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case response := <-waiter: /* 处理当前分支。 */
		return sequence, response, nil /* 返回当前处理结果。 */
	case <-time.After(30 * time.Second): /* 处理当前分支。 */
		return sequence, nil, errors.New("device did not confirm time synchronization within 30 seconds") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

type controlHandler struct { /* 定义 controlHandler 类型。 */
	sessions *sessionRegistry /* 执行当前语句并推进处理流程。 */
	token    string           /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (h controlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { /* 定义 ServeHTTP 函数。 */
	if r.Method != http.MethodPost { /* 判断条件并选择处理分支。 */
		w.Header().Set("Allow", http.MethodPost)   /* 执行当前语句并推进处理流程。 */
		w.WriteHeader(http.StatusMethodNotAllowed) /* 执行当前语句并推进处理流程。 */
		return                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if h.token != "" { /* 判断条件并选择处理分支。 */
		provided := r.Header.Get("X-Gateway-Token")                             /* 更新 provided 的值。 */
		if subtle.ConstantTimeCompare([]byte(provided), []byte(h.token)) != 1 { /* 判断条件并选择处理分支。 */
			w.WriteHeader(http.StatusUnauthorized) /* 执行当前语句并推进处理流程。 */
			return                                 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")                                                                          /* 更新 parts 的值。 */
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "devices" || parts[4] != "time-sync" || parts[3] == "" { /* 判断条件并选择处理分支。 */
		writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "route not found"}) /* 执行当前语句并推进处理流程。 */
		return                                                                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	deviceID := parts[3]                     /* 更新 deviceID 的值。 */
	h.sessions.mu.RLock()                    /* 执行当前语句并推进处理流程。 */
	session := h.sessions.byDevice[deviceID] /* 更新 session 的值。 */
	h.sessions.mu.RUnlock()                  /* 执行当前语句并推进处理流程。 */
	if session == nil {                      /* 判断条件并选择处理分支。 */
		writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "device is not connected"}) /* 执行当前语句并推进处理流程。 */
		return                                                                                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	sequence, response, err := session.requestTimeSync(time.Now()) /* 更新 err 的值。 */
	if err != nil {                                                /* 判断条件并选择处理分支。 */
		writeGatewayJSON(w, http.StatusGatewayTimeout, map[string]any{"error": err.Error(), "deviceId": deviceID, "sequence": sequence}) /* 执行当前语句并推进处理流程。 */
		return                                                                                                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	payload, _ := json.Marshal(hex.EncodeToString(response))                                                                                                                                                              /* 更新 _ 的值。 */
	message, parseErr := (parser.GB26875Parser{}).Parse(model.RawMessage{MessageID: "raw_control_response", Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload, ReceivedAt: time.Now().UnixMilli()}) /* 更新 parseErr 的值。 */
	if parseErr != nil {                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		writeGatewayJSON(w, http.StatusBadGateway, map[string]any{"error": parseErr.Error(), "deviceId": deviceID, "sequence": sequence}) /* 执行当前语句并推进处理流程。 */
		return                                                                                                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	writeGatewayJSON(w, http.StatusOK, map[string]any{"deviceId": deviceID, "sequence": sequence, "request": "time-sync", "response": hex.EncodeToString(response), "event": message.Event}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func writeGatewayJSON(w http.ResponseWriter, status int, value any) { /* 定义 writeGatewayJSON 函数。 */
	w.Header().Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(status)                              /* 执行当前语句并推进处理流程。 */
	_ = json.NewEncoder(w).Encode(value)               /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */

func main() { /* 定义 main 函数。 */
	listen := flag.String("listen", env("GB26875_LISTEN", ":26875"), "TCP listen address")                                                                /* 更新 listen 的值。 */
	udpListen := flag.String("udp-listen", env("GB26875_UDP_LISTEN", ":26875"), "UDP listen address; empty disables UDP")                                 /* 更新 udpListen 的值。 */
	platformURL := flag.String("platform", env("GB26875_PLATFORM_URL", "http://localhost:8081"), "platform API base URL")                                 /* 更新 platformURL 的值。 */
	tenant := flag.String("tenant", env("GB26875_TENANT", "tenant_001"), "platform tenant")                                                               /* 更新 tenant 的值。 */
	username := flag.String("username", env("GB26875_USERNAME", "admin"), "platform username")                                                            /* 更新 username 的值。 */
	password := flag.String("password", env("GB26875_PASSWORD", ""), "platform password")                                                                 /* 更新 password 的值。 */
	controlListen := flag.String("control-listen", env("GB26875_CONTROL_LISTEN", ""), "optional local HTTP control address, for example 127.0.0.1:26876") /* 更新 controlListen 的值。 */
	controlToken := flag.String("control-token", env("GB26875_CONTROL_TOKEN", ""), "optional X-Gateway-Token for the control API")                        /* 更新 controlToken 的值。 */
	flag.Parse()                                                                                                                                          /* 执行当前语句并推进处理流程。 */

	ctx := context.Background()                                                                                                                                     /* 更新 ctx 的值。 */
	c := &platformClient{baseURL: strings.TrimRight(*platformURL, "/"), tenant: *tenant, http: &http.Client{Timeout: 10 * time.Second}, devices: map[string]bool{}} /* 更新 c 的值。 */
	sessions := newSessionRegistry()                                                                                                                                /* 更新 sessions 的值。 */
	if err := c.login(ctx, *username, *password); err != nil {                                                                                                      /* 判断条件并选择处理分支。 */
		log.Fatal(err) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := c.setup(ctx); err != nil { /* 判断条件并选择处理分支。 */
		log.Fatal(err) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if *udpListen != "" { /* 判断条件并选择处理分支。 */
		udpAddress, err := net.ResolveUDPAddr("udp", *udpListen) /* 更新 err 的值。 */
		if err != nil {                                          /* 判断条件并选择处理分支。 */
			log.Fatal(err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		udpConn, err := net.ListenUDP("udp", udpAddress) /* 更新 err 的值。 */
		if err != nil {                                  /* 判断条件并选择处理分支。 */
			log.Fatal(err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		defer udpConn.Close()                                         /* 安排函数结束时执行清理。 */
		go handleUDP(c, udpConn, sessions)                            /* 执行当前语句并推进处理流程。 */
		log.Printf("GB26875 UDP gateway listening on %s", *udpListen) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	listener, err := net.Listen("tcp", *listen) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		log.Fatal(err) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	defer listener.Close()    /* 安排函数结束时执行清理。 */
	if *controlListen != "" { /* 判断条件并选择处理分支。 */
		controlServer := &http.Server{Addr: *controlListen, Handler: controlHandler{sessions: sessions, token: *controlToken}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second} /* 更新 controlServer 的值。 */
		go func() {                                                                                                                                                                                                               /* 执行当前语句并推进处理流程。 */
			if *controlToken == "" { /* 判断条件并选择处理分支。 */
				log.Printf("WARNING: GB26875 control API %s has no token; bind it to localhost or set GB26875_CONTROL_TOKEN", *controlListen) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if err := controlServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { /* 判断条件并选择处理分支。 */
				log.Printf("control API failed: %v", err) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
		defer controlServer.Close() /* 安排函数结束时执行清理。 */
	} /* 结束当前表达式或代码块。 */
	log.Printf("GB26875 gateway listening on %s, forwarding to %s", *listen, c.baseURL) /* 执行当前语句并推进处理流程。 */
	for {                                                                               /* 循环处理当前数据。 */
		conn, err := listener.Accept() /* 更新 err 的值。 */
		if err != nil {                /* 判断条件并选择处理分支。 */
			log.Printf("accept: %v", err) /* 执行当前语句并推进处理流程。 */
			continue                      /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		go handleConnection(c, conn, sessions) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func handleConnection(c *platformClient, conn net.Conn, sessions *sessionRegistry) { /* 定义 handleConnection 函数。 */
	defer conn.Close()                                    /* 安排函数结束时执行清理。 */
	reader := bufio.NewReader(conn)                       /* 更新 reader 的值。 */
	var deviceID string                                   /* 声明 deviceID。 */
	var session *deviceSession                            /* 声明 session。 */
	defer func() { sessions.remove(deviceID, session) }() /* 安排函数结束时执行清理。 */
	for {                                                 /* 循环处理当前数据。 */
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second)) /* 更新 _ 的值。 */
		frame, err := readFrame(reader)                            /* 更新 err 的值。 */
		if err != nil {                                            /* 判断条件并选择处理分支。 */
			if !errors.Is(err, io.EOF) && !isTimeout(err) { /* 判断条件并选择处理分支。 */
				log.Printf("%s invalid frame: %v", conn.RemoteAddr(), err) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)             /* 更新 cancel 的值。 */
		ack, deviceID, err := processFrame(ctx, c, frame, "TCP", conn.RemoteAddr().String()) /* 更新 err 的值。 */
		cancel()                                                                             /* 执行当前语句并推进处理流程。 */
		if err != nil {                                                                      /* 判断条件并选择处理分支。 */
			log.Printf("%s forward failed: %v", deviceID, err) /* 执行当前语句并推进处理流程。 */
			continue                                           /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var source [6]byte    /* 声明 source。 */
		if len(frame) >= 18 { /* 判断条件并选择处理分支。 */
			copy(source[:], frame[12:18]) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		deviceID = "gb26875_" + strings.ToLower(hex.EncodeToString(source[:]))    /* 更新 deviceID 的值。 */
		session = sessions.registerTCP(deviceID, source, conn)                    /* 更新 session 的值。 */
		sessions.deliver(deviceID, binary.LittleEndian.Uint16(frame[2:4]), frame) /* 执行当前语句并推进处理流程。 */
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))                /* 更新 _ 的值。 */
		if len(ack) == 0 {                                                        /* 判断条件并选择处理分支。 */
			log.Printf("%s accepted %d-byte frame without a response", deviceID, len(frame)) /* 执行当前语句并推进处理流程。 */
			continue                                                                         /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, err := conn.Write(ack); err != nil { /* 判断条件并选择处理分支。 */
			log.Printf("%s ack failed: %v", deviceID, err) /* 执行当前语句并推进处理流程。 */
			return                                         /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		log.Printf("%s accepted %d-byte frame and sent confirmation", deviceID, len(frame)) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func handleUDP(c *platformClient, conn *net.UDPConn, sessions *sessionRegistry) { /* 定义 handleUDP 函数。 */
	buffer := make([]byte, 2+25+512+3) /* 更新 buffer 的值。 */
	for {                              /* 循环处理当前数据。 */
		count, remote, err := conn.ReadFromUDP(buffer) /* 更新 err 的值。 */
		if err != nil {                                /* 判断条件并选择处理分支。 */
			log.Printf("udp read: %v", err) /* 执行当前语句并推进处理流程。 */
			return                          /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		frame := append([]byte(nil), buffer[:count]...)                           /* 更新 frame 的值。 */
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)  /* 更新 cancel 的值。 */
		ack, deviceID, err := processFrame(ctx, c, frame, "UDP", remote.String()) /* 更新 err 的值。 */
		cancel()                                                                  /* 执行当前语句并推进处理流程。 */
		if err != nil {                                                           /* 判断条件并选择处理分支。 */
			log.Printf("%s UDP frame rejected: %v", deviceID, err) /* 执行当前语句并推进处理流程。 */
			continue                                               /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var source [6]byte    /* 声明 source。 */
		if len(frame) >= 18 { /* 判断条件并选择处理分支。 */
			copy(source[:], frame[12:18]) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		deviceID = "gb26875_" + strings.ToLower(hex.EncodeToString(source[:]))    /* 更新 deviceID 的值。 */
		sessions.registerUDP(deviceID, source, conn, remote)                      /* 执行当前语句并推进处理流程。 */
		sessions.deliver(deviceID, binary.LittleEndian.Uint16(frame[2:4]), frame) /* 执行当前语句并推进处理流程。 */
		if len(ack) == 0 {                                                        /* 判断条件并选择处理分支。 */
			log.Printf("%s accepted %d-byte UDP frame without a response", deviceID, len(frame)) /* 执行当前语句并推进处理流程。 */
			continue                                                                             /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, err := conn.WriteToUDP(ack, remote); err != nil { /* 判断条件并选择处理分支。 */
			log.Printf("%s UDP ack failed: %v", deviceID, err) /* 执行当前语句并推进处理流程。 */
			continue                                           /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		log.Printf("%s accepted %d-byte UDP frame and sent confirmation", deviceID, len(frame)) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func processFrame(ctx context.Context, c *platformClient, frame []byte, transport, remote string) ([]byte, string, error) { /* 定义 processFrame 函数。 */
	if len(frame) < 18 { /* 判断条件并选择处理分支。 */
		return nil, "unknown", errors.New("frame is too short") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	source := strings.ToUpper(hex.EncodeToString(frame[12:18]))                                                                                                                                                                                                                                                                                                                                                                  /* 更新 source 的值。 */
	deviceID := "gb26875_" + strings.ToLower(source)                                                                                                                                                                                                                                                                                                                                                                             /* 更新 deviceID 的值。 */
	payload, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(frame)))                                                                                                                                                                                                                                                                                                                                                       /* 更新 _ 的值。 */
	raw := model.RawMessage{MessageID: fmt.Sprintf("raw_gb26875_%s_%d", strings.ToLower(source), time.Now().UnixNano()), TenantID: c.tenant, ProductID: "product_gb26875_lora_fire", DeviceID: deviceID, Protocol: "gb26875-dahua-v1.03", Transport: transport, PayloadFormat: "hex", Payload: payload, ReceivedAt: time.Now().UnixMilli(), Source: "gb26875-" + strings.ToLower(transport) + "-gateway", RemoteAddress: remote} /* 更新 raw 的值。 */
	message, err := (parser.GB26875Parser{}).Parse(raw)                                                                                                                                                                                                                                                                                                                                                                          /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                                                                                                              /* 判断条件并选择处理分支。 */
		return nil, deviceID, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := c.forward(ctx, raw, source); err != nil { /* 判断条件并选择处理分支。 */
		return nil, deviceID, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var destination [6]byte                                                               /* 声明 destination。 */
	copy(destination[:], frame[12:18])                                                    /* 执行当前语句并推进处理流程。 */
	if eventType, _ := message.Event["type"].(string); eventType == "TIME_SYNC_REQUEST" { /* 判断条件并选择处理分支。 */
		return parser.BuildGB26875TimeSyncFrame(binary.LittleEndian.Uint16(frame[2:4]), destination, time.Now()), deviceID, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if eventType, _ := message.Event["type"].(string); eventType == "ACK" { /* 判断条件并选择处理分支。 */
		// The v1.03 time-sync flow explicitly allows the platform to finish
		// after receiving the device confirmation; do not ACK an ACK.
		return nil, deviceID, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return parser.BuildGB26875AckFrame(binary.LittleEndian.Uint16(frame[2:4]), destination, time.Now()), deviceID, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func readFrame(reader *bufio.Reader) ([]byte, error) { /* 定义 readFrame 函数。 */
	for { /* 循环处理当前数据。 */
		first, err := reader.ReadByte() /* 更新 err 的值。 */
		if err != nil {                 /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if first != '@' { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		second, err := reader.ReadByte() /* 更新 err 的值。 */
		if err != nil {                  /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if second != '@' { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		break /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	header := make([]byte, 25)                             /* 更新 header 的值。 */
	if _, err := io.ReadFull(reader, header); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	applicationLength := int(binary.LittleEndian.Uint16(header[22:24])) /* 更新 applicationLength 的值。 */
	if applicationLength > 512 {                                        /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("application length %d exceeds 512", applicationLength) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tail := make([]byte, applicationLength+3)            /* 更新 tail 的值。 */
	if _, err := io.ReadFull(reader, tail); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	frame := append([]byte{'@', '@'}, header...) /* 更新 frame 的值。 */
	return append(frame, tail...), nil           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (c *platformClient) login(ctx context.Context, username, password string) error { /* 定义 login 函数。 */
	var out struct { /* 声明 out。 */
		AccessToken string `json:"accessToken"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": username, "password": password, "tenantId": c.tenant}, &out); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("platform login: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c.token = out.AccessToken /* 更新 c.token 的值。 */
	return nil                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Deprecated gateway: use a go-protocol-v2 package and generic listener for new devices.
// Startup must never overwrite an operator's protocol or product binding.
func (c *platformClient) setup(ctx context.Context) error { /* 定义 setup 函数。 */
	var product model.Product                                                                                      /* 声明 product。 */
	if err := c.do(ctx, http.MethodGet, "/api/v1/products/product_gb26875_lora_fire", nil, &product); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("已有 GB 产品未配置；请使用 Go 协议包及通用监听器: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if product.ProtocolPackageID == "" { /* 判断条件并选择处理分支。 */
		return errors.New("GB 产品未绑定协议，请先在平台配置") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (c *platformClient) forward(ctx context.Context, raw model.RawMessage, source string) error { /* 定义 forward 函数。 */
	c.mu.Lock()                      /* 执行当前语句并推进处理流程。 */
	known := c.devices[raw.DeviceID] /* 更新 known 的值。 */
	c.mu.Unlock()                    /* 执行当前语句并推进处理流程。 */
	if !known {                      /* 判断条件并选择处理分支。 */
		if err := c.do(ctx, http.MethodPost, "/api/v1/device-registry", map[string]any{"id": raw.DeviceID, "productId": raw.ProductID, "name": "GB26875 设备 " + source, "status": "ENABLED", "deviceRole": "DIRECT", "registrationSource": "GB26875_TCP", "tags": map[string]string{"sourceAddress": source}}, nil); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		c.mu.Lock()                    /* 执行当前语句并推进处理流程。 */
		c.devices[raw.DeviceID] = true /* 更新 c.devices[raw.DeviceID] 的值。 */
		c.mu.Unlock()                  /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return c.do(ctx, http.MethodPost, "/api/v1/raw-messages", raw, nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (c *platformClient) do(ctx context.Context, method, path string, body, out any) error { /* 定义 do 函数。 */
	payload, err := json.Marshal(body) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload)) /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	if c.token != "" {                                 /* 判断条件并选择处理分支。 */
		req.Header.Set("Authorization", "Bearer "+c.token) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := c.http.Do(req) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                           /* 安排函数结束时执行清理。 */
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) /* 更新 err 的值。 */
	if err != nil {                                                   /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("%s %s: %s", resp.Status, path, strings.TrimSpace(string(responseBody))) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if out != nil && len(responseBody) > 0 { /* 判断条件并选择处理分支。 */
		return json.Unmarshal(responseBody, out) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func env(name, fallback string) string { /* 定义 env 函数。 */
	if value := os.Getenv(name); value != "" { /* 判断条件并选择处理分支。 */
		return value /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fallback /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func isTimeout(err error) bool { /* 定义 isTimeout 函数。 */
	var netErr net.Error                               /* 声明 netErr。 */
	return errors.As(err, &netErr) && netErr.Timeout() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
