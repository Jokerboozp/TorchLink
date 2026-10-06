package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/auth"
)

func (s *Server) mqttToken(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	// Only the built-in administrator reaches this handler (allowsRoute
	// refuses managed users), so the tenant-wide wildcard subscriptions
	// never reach an account limited to some devices.
	scope := []string{fmt.Sprintf("/iot/parsed/%s/#", c.TenantID), fmt.Sprintf("/iot/alarm/%s/#", c.TenantID), fmt.Sprintf("/iot/device/state/%s/#", c.TenantID), fmt.Sprintf("/iot/ui-action/%s", c.TenantID)}
	// Broker-only credentials: never usable as a console token.
	token, err := s.auth.IssueBrowserMQTT(c.Username, c.TenantID, scope, 15*time.Minute)
	if err != nil {
		s.fail(w, r, err, "创建消息令牌失败")
		return
	}
	write(w, 200, map[string]any{"username": auth.BrowserMQTTUsername(c.Username), "token": token, "expiresIn": 900, "subscriptions": scope, "websocketUrl": s.mqttWebSocketURL(r)})
}

// standardDeviceTokenTTL keeps standard device tokens short unless revoked
// credentials can be banned and kicked at the broker at once; the broker
// disconnects a session when its token expires, so short tokens force every
// device to reconnect that often.
func (s *Server) standardDeviceTokenTTL() time.Duration {
	if s.onboarding.RevokeUsername == nil {
		return 5 * time.Minute
	}
	if s.cfg.MQTTDeviceTokenTTL <= 0 {
		return 24 * time.Hour
	}
	return s.cfg.MQTTDeviceTokenTTL
}

func (s *Server) deviceMQTTToken(w http.ResponseWriter, r *http.Request) {
	accessKey, secret := r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret")
	v, err := s.onboarding.Authenticate(r.Context(), accessKey, secret)
	if deviceAuthUnavailable(w, err) {
		return
	}
	if err != nil {
		problem(w, 401, "invalid device credentials")
		return
	}
	product, productErr := s.engine.Repo.GetProduct(r.Context(), v.TenantID, v.ProductID)
	if productErr != nil || product.Status != "ENABLED" {
		problem(w, 401, "device product is disabled")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	topic := fmt.Sprintf("/external/raw/%s/%s/%s", v.TenantID, v.ProductID, v.ID)
	acl := []auth.ACLRule{{Permission: "allow", Action: "publish", Topic: topic}, {Permission: "allow", Action: "subscribe", Topic: fmt.Sprintf("/iot/device/command/%s/%s", v.TenantID, v.ID)}}
	ttl := 24 * time.Hour
	if v.Connector == "MQTT" || v.Connector == "HTTP" {
		acl = nil
		ttl = s.standardDeviceTokenTTL()
		topic = fmt.Sprintf("/iot/up/%s/%s/%s/property", v.TenantID, v.ProductID, v.ID)
	}
	for _, kind := range []string{"property", "event", "alarm", "state", "command-reply"} {
		acl = append(acl, auth.ACLRule{Permission: "allow", Action: "publish", Topic: fmt.Sprintf("/iot/up/%s/%s/%s/%s", v.TenantID, v.ProductID, v.ID, kind)})
	}
	acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: fmt.Sprintf("/iot/down/%s/%s/%s/command", v.TenantID, v.ProductID, v.ID)})

	receiptTopic := fmt.Sprintf("/iot/down/%s/%s/%s/receipt", v.TenantID, v.ProductID, v.ID)
	acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: receiptTopic})

	token, err := s.auth.IssueWithACL(v.AccessKey, v.TenantID, "device", nil, acl, ttl)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	response := map[string]any{"username": v.AccessKey, "token": token, "expiresIn": int(ttl.Seconds()), "publishTopic": topic, "receiptTopic": receiptTopic, "websocketUrl": s.mqttWebSocketURL(r)}
	write(w, 200, response)
}

func (s *Server) mqttLoadToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProductID string `json:"productId"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	if input.ProductID == "" {
		problem(w, 422, "productId is required")
		return
	}
	c := claims(r)
	topic := fmt.Sprintf("/external/raw/%s/%s/#", c.TenantID, input.ProductID)
	acl := []auth.ACLRule{{Permission: "allow", Action: "publish", Topic: topic}}
	token, err := s.auth.IssueWithACL("loadgen:"+c.Username, c.TenantID, "loadgen", nil, acl, time.Hour)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	s.audit(r, "mqtt.load-token.issue", "product", input.ProductID, map[string]any{"topic": topic, "expiresIn": 3600})
	write(w, 200, map[string]any{"username": "loadgen:" + c.Username, "token": token, "expiresIn": 3600, "publishTopicPrefix": strings.TrimSuffix(topic, "#")})
}

func (s *Server) mqttWebSocketURL(r *http.Request) string {
	if s.cfg.MQTTWebSocketURL != "" {
		return s.cfg.MQTTWebSocketURL
	}
	scheme := "ws"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "wss"
	}
	host := strings.Split(r.Host, ":")[0]
	return fmt.Sprintf("%s://%s:8083/mqtt", scheme, host)
}
