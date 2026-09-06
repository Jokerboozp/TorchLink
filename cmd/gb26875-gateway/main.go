package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

type platformClient struct {
	baseURL, tenant, token string
	http                   *http.Client
	mu                     sync.Mutex
	devices                map[string]bool
}

type deviceSession struct {
	deviceID  string
	source    [6]byte
	tcp       net.Conn
	udp       *net.UDPConn
	udpRemote *net.UDPAddr
	writeMu   sync.Mutex
	mu        sync.Mutex
	sequence  uint16
	pending   map[uint16]chan []byte
}

type sessionRegistry struct {
	mu       sync.RWMutex
	byDevice map[string]*deviceSession
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{byDevice: map[string]*deviceSession{}}
}

func (r *sessionRegistry) registerTCP(deviceID string, source [6]byte, conn net.Conn) *deviceSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.byDevice[deviceID]
	if session == nil {
		session = &deviceSession{deviceID: deviceID, pending: map[uint16]chan []byte{}}
		r.byDevice[deviceID] = session
	}
	session.source, session.tcp, session.udp, session.udpRemote = source, conn, nil, nil
	return session
}

func (r *sessionRegistry) registerUDP(deviceID string, source [6]byte, conn *net.UDPConn, remote *net.UDPAddr) *deviceSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.byDevice[deviceID]
	if session == nil {
		session = &deviceSession{deviceID: deviceID, pending: map[uint16]chan []byte{}}
		r.byDevice[deviceID] = session
	}
	session.source, session.tcp, session.udp, session.udpRemote = source, nil, conn, remote
	return session
}

func (r *sessionRegistry) remove(deviceID string, session *deviceSession) {
	if deviceID == "" || session == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byDevice[deviceID] == session {
		delete(r.byDevice, deviceID)
	}
}

func (r *sessionRegistry) deliver(deviceID string, sequence uint16, frame []byte) {
	r.mu.RLock()
	session := r.byDevice[deviceID]
	r.mu.RUnlock()
	if session == nil {
		return
	}
	session.mu.Lock()
	waiter := session.pending[sequence]
	session.mu.Unlock()
	if waiter == nil {
		return
	}
	select {
	case waiter <- append([]byte(nil), frame...):
	default:
	}
}

func (s *deviceSession) send(frame []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.tcp != nil {
		_ = s.tcp.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := s.tcp.Write(frame)
		return err
	}
	if s.udp != nil && s.udpRemote != nil {
		_, err := s.udp.WriteToUDP(frame, s.udpRemote)
		return err
	}
	return errors.New("device connection is no longer available")
}

func (s *deviceSession) requestTimeSync(at time.Time) (uint16, []byte, error) {
	s.mu.Lock()
	s.sequence++
	if s.sequence == 0 {
		s.sequence = 1
	}
	sequence := s.sequence
	waiter := make(chan []byte, 1)
	s.pending[sequence] = waiter
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, sequence)
		s.mu.Unlock()
	}()
	request := parser.BuildGB26875TimeSyncFrame(sequence, s.source, at)
	if err := s.send(request); err != nil {
		return sequence, nil, err
	}
	select {
	case response := <-waiter:
		return sequence, response, nil
	case <-time.After(30 * time.Second):
		return sequence, nil, errors.New("device did not confirm time synchronization within 30 seconds")
	}
}

type controlHandler struct {
	sessions *sessionRegistry
	token    string
}

func (h controlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.token != "" {
		provided := r.Header.Get("X-Gateway-Token")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(h.token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "devices" || parts[4] != "time-sync" || parts[3] == "" {
		writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "route not found"})
		return
	}
	deviceID := parts[3]
	h.sessions.mu.RLock()
	session := h.sessions.byDevice[deviceID]
	h.sessions.mu.RUnlock()
	if session == nil {
		writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "device is not connected"})
		return
	}
	sequence, response, err := session.requestTimeSync(time.Now())
	if err != nil {
		writeGatewayJSON(w, http.StatusGatewayTimeout, map[string]any{"error": err.Error(), "deviceId": deviceID, "sequence": sequence})
		return
	}
	payload, _ := json.Marshal(hex.EncodeToString(response))
	message, parseErr := (parser.GB26875Parser{}).Parse(model.RawMessage{MessageID: "raw_control_response", Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload, ReceivedAt: time.Now().UnixMilli()})
	if parseErr != nil {
		writeGatewayJSON(w, http.StatusBadGateway, map[string]any{"error": parseErr.Error(), "deviceId": deviceID, "sequence": sequence})
		return
	}
	writeGatewayJSON(w, http.StatusOK, map[string]any{"deviceId": deviceID, "sequence": sequence, "request": "time-sync", "response": hex.EncodeToString(response), "event": message.Event})
}

func writeGatewayJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func main() {
	listen := flag.String("listen", env("GB26875_LISTEN", ":26875"), "TCP listen address")
	udpListen := flag.String("udp-listen", env("GB26875_UDP_LISTEN", ":26875"), "UDP listen address; empty disables UDP")
	platformURL := flag.String("platform", env("GB26875_PLATFORM_URL", "http://localhost:8081"), "platform API base URL")
	tenant := flag.String("tenant", env("GB26875_TENANT", "tenant_001"), "platform tenant")
	username := flag.String("username", env("GB26875_USERNAME", "admin"), "platform username")
	password := flag.String("password", env("GB26875_PASSWORD", ""), "platform password")
	controlListen := flag.String("control-listen", env("GB26875_CONTROL_LISTEN", ""), "optional local HTTP control address, for example 127.0.0.1:26876")
	controlToken := flag.String("control-token", env("GB26875_CONTROL_TOKEN", ""), "optional X-Gateway-Token for the control API")
	flag.Parse()

	ctx := context.Background()
	c := &platformClient{baseURL: strings.TrimRight(*platformURL, "/"), tenant: *tenant, http: &http.Client{Timeout: 10 * time.Second}, devices: map[string]bool{}}
	sessions := newSessionRegistry()
	if err := c.login(ctx, *username, *password); err != nil {
		log.Fatal(err)
	}
	if err := c.setup(ctx); err != nil {
		log.Fatal(err)
	}
	if *udpListen != "" {
		udpAddress, err := net.ResolveUDPAddr("udp", *udpListen)
		if err != nil {
			log.Fatal(err)
		}
		udpConn, err := net.ListenUDP("udp", udpAddress)
		if err != nil {
			log.Fatal(err)
		}
		defer udpConn.Close()
		go handleUDP(c, udpConn, sessions)
		log.Printf("GB26875 UDP gateway listening on %s", *udpListen)
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	if *controlListen != "" {
		controlServer := &http.Server{Addr: *controlListen, Handler: controlHandler{sessions: sessions, token: *controlToken}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second}
		go func() {
			if *controlToken == "" {
				log.Printf("WARNING: GB26875 control API %s has no token; bind it to localhost or set GB26875_CONTROL_TOKEN", *controlListen)
			}
			if err := controlServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("control API failed: %v", err)
			}
		}()
		defer controlServer.Close()
	}
	log.Printf("GB26875 gateway listening on %s, forwarding to %s", *listen, c.baseURL)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handleConnection(c, conn, sessions)
	}
}

func handleConnection(c *platformClient, conn net.Conn, sessions *sessionRegistry) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	var deviceID string
	var session *deviceSession
	defer func() { sessions.remove(deviceID, session) }()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		frame, err := readFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && !isTimeout(err) {
				log.Printf("%s invalid frame: %v", conn.RemoteAddr(), err)
			}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ack, deviceID, err := processFrame(ctx, c, frame, "TCP", conn.RemoteAddr().String())
		cancel()
		if err != nil {
			log.Printf("%s forward failed: %v", deviceID, err)
			continue
		}
		var source [6]byte
		if len(frame) >= 18 {
			copy(source[:], frame[12:18])
		}
		deviceID = "gb26875_" + strings.ToLower(hex.EncodeToString(source[:]))
		session = sessions.registerTCP(deviceID, source, conn)
		sessions.deliver(deviceID, binary.LittleEndian.Uint16(frame[2:4]), frame)
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if len(ack) == 0 {
			log.Printf("%s accepted %d-byte frame without a response", deviceID, len(frame))
			continue
		}
		if _, err := conn.Write(ack); err != nil {
			log.Printf("%s ack failed: %v", deviceID, err)
			return
		}
		log.Printf("%s accepted %d-byte frame and sent confirmation", deviceID, len(frame))
	}
}

func handleUDP(c *platformClient, conn *net.UDPConn, sessions *sessionRegistry) {
	buffer := make([]byte, 2+25+512+3)
	for {
		count, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("udp read: %v", err)
			return
		}
		frame := append([]byte(nil), buffer[:count]...)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ack, deviceID, err := processFrame(ctx, c, frame, "UDP", remote.String())
		cancel()
		if err != nil {
			log.Printf("%s UDP frame rejected: %v", deviceID, err)
			continue
		}
		var source [6]byte
		if len(frame) >= 18 {
			copy(source[:], frame[12:18])
		}
		deviceID = "gb26875_" + strings.ToLower(hex.EncodeToString(source[:]))
		sessions.registerUDP(deviceID, source, conn, remote)
		sessions.deliver(deviceID, binary.LittleEndian.Uint16(frame[2:4]), frame)
		if len(ack) == 0 {
			log.Printf("%s accepted %d-byte UDP frame without a response", deviceID, len(frame))
			continue
		}
		if _, err := conn.WriteToUDP(ack, remote); err != nil {
			log.Printf("%s UDP ack failed: %v", deviceID, err)
			continue
		}
		log.Printf("%s accepted %d-byte UDP frame and sent confirmation", deviceID, len(frame))
	}
}

func processFrame(ctx context.Context, c *platformClient, frame []byte, transport, remote string) ([]byte, string, error) {
	if len(frame) < 18 {
		return nil, "unknown", errors.New("frame is too short")
	}
	source := strings.ToUpper(hex.EncodeToString(frame[12:18]))
	deviceID := "gb26875_" + strings.ToLower(source)
	payload, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(frame)))
	raw := model.RawMessage{MessageID: fmt.Sprintf("raw_gb26875_%s_%d", strings.ToLower(source), time.Now().UnixNano()), TenantID: c.tenant, ProductID: "product_gb26875_lora_fire", DeviceID: deviceID, Protocol: "gb26875-dahua-v1.03", Transport: transport, PayloadFormat: "hex", Payload: payload, ReceivedAt: time.Now().UnixMilli(), Source: "gb26875-" + strings.ToLower(transport) + "-gateway", RemoteAddress: remote}
	message, err := (parser.GB26875Parser{}).Parse(raw)
	if err != nil {
		return nil, deviceID, err
	}
	if err := c.forward(ctx, raw, source); err != nil {
		return nil, deviceID, err
	}
	var destination [6]byte
	copy(destination[:], frame[12:18])
	if eventType, _ := message.Event["type"].(string); eventType == "TIME_SYNC_REQUEST" {
		return parser.BuildGB26875TimeSyncFrame(binary.LittleEndian.Uint16(frame[2:4]), destination, time.Now()), deviceID, nil
	}
	if eventType, _ := message.Event["type"].(string); eventType == "ACK" {
		// The v1.03 time-sync flow explicitly allows the platform to finish
		// after receiving the device confirmation; do not ACK an ACK.
		return nil, deviceID, nil
	}
	return parser.BuildGB26875AckFrame(binary.LittleEndian.Uint16(frame[2:4]), destination, time.Now()), deviceID, nil
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	for {
		first, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if first != '@' {
			continue
		}
		second, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if second != '@' {
			continue
		}
		break
	}
	header := make([]byte, 25)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	applicationLength := int(binary.LittleEndian.Uint16(header[22:24]))
	if applicationLength > 512 {
		return nil, fmt.Errorf("application length %d exceeds 512", applicationLength)
	}
	tail := make([]byte, applicationLength+3)
	if _, err := io.ReadFull(reader, tail); err != nil {
		return nil, err
	}
	frame := append([]byte{'@', '@'}, header...)
	return append(frame, tail...), nil
}

func (c *platformClient) login(ctx context.Context, username, password string) error {
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": username, "password": password, "tenantId": c.tenant}, &out); err != nil {
		return fmt.Errorf("platform login: %w", err)
	}
	c.token = out.AccessToken
	return nil
}

// Deprecated gateway: use a go-protocol-v2 package and generic listener for new devices.
// Startup must never overwrite an operator's protocol or product binding.
func (c *platformClient) setup(ctx context.Context) error {
	var product model.Product
	if err := c.do(ctx, http.MethodGet, "/api/v1/products/product_gb26875_lora_fire", nil, &product); err != nil {
		return fmt.Errorf("已有 GB 产品未配置；请使用 Go 协议包及通用监听器: %w", err)
	}
	if product.ProtocolPackageID == "" {
		return errors.New("GB 产品未绑定协议，请先在平台配置")
	}
	return nil
}

func (c *platformClient) forward(ctx context.Context, raw model.RawMessage, source string) error {
	c.mu.Lock()
	known := c.devices[raw.DeviceID]
	c.mu.Unlock()
	if !known {
		if err := c.do(ctx, http.MethodPost, "/api/v1/device-registry", map[string]any{"id": raw.DeviceID, "productId": raw.ProductID, "name": "GB26875 设备 " + source, "status": "ENABLED", "deviceRole": "DIRECT", "registrationSource": "GB26875_TCP", "tags": map[string]string{"sourceAddress": source}}, nil); err != nil {
			return err
		}
		c.mu.Lock()
		c.devices[raw.DeviceID] = true
		c.mu.Unlock()
	}
	return c.do(ctx, http.MethodPost, "/api/v1/raw-messages", raw, nil)
}

func (c *platformClient) do(ctx context.Context, method, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s", resp.Status, path, strings.TrimSpace(string(responseBody)))
	}
	if out != nil && len(responseBody) > 0 {
		return json.Unmarshal(responseBody, out)
	}
	return nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
