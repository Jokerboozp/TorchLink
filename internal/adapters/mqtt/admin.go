package mqttadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Admin uses dedicated EMQX API credentials. Errors never include response bodies
// or authentication material. Ban first, then kick, so stale JWTs cannot reconnect.
type Admin struct {
	URL, Key, Secret string
	Client           *http.Client
}

func (a *Admin) request(ctx context.Context, method, path string, body any, out any) (int, error) {
	base, e := url.Parse(a.URL)
	if e != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return 0, errors.New("invalid EMQX administration URL")
	}
	b, e := json.Marshal(body)
	if e != nil {
		return 0, e
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.URL, "/")+"/api/v5"+path, bytes.NewReader(b))
	if e != nil {
		return 0, e
	}
	req.SetBasicAuth(a.Key, a.Secret)
	req.Header.Set("Content-Type", "application/json")
	c := http.Client{Timeout: 10 * time.Second}
	if a.Client != nil {
		c = *a.Client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, e := c.Do(req)
	if e != nil {
		return 0, errors.New("EMQX administration request failed")
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if out != nil {
			e = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
		}
		return res.StatusCode, e
	}
	return res.StatusCode, fmt.Errorf("EMQX administration HTTP %d", res.StatusCode)
}
func (a *Admin) RevokeUsername(ctx context.Context, username string) error {
	if username == "" {
		return nil
	}
	if a.Key == "" || a.Secret == "" {
		return errors.New("EMQX API credentials unavailable")
	}
	code, e := a.request(ctx, "POST", "/banned", map[string]any{"as": "username", "who": username, "by": "iot-platform", "reason": "device credential revoked", "until": "infinity"}, nil)
	if e != nil {
		if code != 400 && code != 409 {
			return e
		}
		var existing struct {
			Data []struct {
				As    string `json:"as"`
				Who   string `json:"who"`
				Until string `json:"until"`
			} `json:"data"`
		}
		_, err := a.request(ctx, "GET", "/banned?username="+url.QueryEscape(username), nil, &existing)
		if err != nil {
			return err
		}
		found := false
		for _, v := range existing.Data {
			if v.As == "username" && v.Who == username && v.Until == "infinity" {
				found = true
			}
		}
		if !found {
			return e
		}
	}
	for page := 0; page < 100; page++ {
		var clients struct {
			Data []struct {
				ID       string `json:"clientid"`
				Username string `json:"username"`
			} `json:"data"`
		}
		_, e = a.request(ctx, "GET", "/clients?username="+url.QueryEscape(username)+"&page=1&limit=100", nil, &clients)
		if e != nil {
			return e
		}
		if len(clients.Data) == 0 {
			return nil
		}
		for _, c := range clients.Data {
			if c.Username != username || c.ID == "" {
				return errors.New("EMQX returned an unexpected client identity")
			}
			code, e = a.request(ctx, "DELETE", "/clients/"+url.PathEscape(c.ID), nil, nil)
			if e != nil && code != 404 {
				return e
			}
		}
	}
	return errors.New("EMQX client cleanup limit reached")
}

// SessionQueue returns the broker-side queue length of a client session and
// how many messages the broker dropped because that queue was full. For the
// platform's own inbox session a growing drop count means devices received
// PUBACK for messages the platform never got.
func (a *Admin) SessionQueue(ctx context.Context, clientID string) (queued, dropped int64, err error) {
	if a.Key == "" || a.Secret == "" {
		return 0, 0, errors.New("EMQX API credentials unavailable")
	}
	var info struct {
		MqueueLen     int64 `json:"mqueue_len"`
		MqueueDropped int64 `json:"mqueue_dropped"`
	}
	if _, err = a.request(ctx, "GET", "/clients/"+url.PathEscape(clientID), nil, &info); err != nil {
		return 0, 0, err
	}
	return info.MqueueLen, info.MqueueDropped, nil
}

// CheckTopicAuthorization verifies the running broker rather than inferring
// authorization from environment settings. It never returns broker secrets.
// Consumer tokens must also end their exact allow-list with explicit deny rules.
func (a *Admin) CheckTopicAuthorization(ctx context.Context) error {
	if a.Key == "" || a.Secret == "" {
		return errors.New("EMQX API credentials unavailable")
	}
	var chain []topicJWTAuthenticator
	if _, err := a.request(ctx, "GET", "/authentication", nil, &chain); err != nil {
		return err
	}
	if !validTopicJWTChain(chain) {
		return errors.New("MQTT topic authorization requires JWT password authentication, username verification, ACL claims and expiration disconnect")
	}
	var settings struct {
		NoMatch string `json:"no_match"`
	}
	if _, err := a.request(ctx, "GET", "/authorization/settings", nil, &settings); err != nil {
		return err
	}
	if settings.NoMatch != "deny" {
		return errors.New("MQTT topic authorization requires deny-by-default authorization")
	}
	var sources struct {
		Sources *[]struct {
			Type   string `json:"type"`
			Enable bool   `json:"enable"`
		} `json:"sources"`
	}
	if _, err := a.request(ctx, "GET", "/authorization/sources", nil, &sources); err != nil {
		return err
	}
	if sources.Sources == nil {
		return errors.New("EMQX returned an invalid authorization source configuration")
	}
	for _, source := range *sources.Sources {
		if !source.Enable {
			continue
		}
		// File rules may retain dashboard/loopback allowances for the broker.
		// Our JWT has explicit final deny rules and never falls through to them.
		// Dynamic sources have additional failure semantics and are not verified.
		if source.Type != "file" {
			return errors.New("MQTT topic authorization cannot verify the configured fallback authorization source")
		}
	}
	var listeners []struct {
		ID     string `json:"id"`
		Enable bool   `json:"enable"`
	}
	if _, err := a.request(ctx, "GET", "/listeners", nil, &listeners); err != nil {
		return err
	}
	enabled := false
	for _, listener := range listeners {
		if !listener.Enable {
			continue
		}
		enabled = true
		if listener.ID == "" {
			return errors.New("EMQX returned an invalid listener")
		}
		var config struct {
			EnableAuthn    json.RawMessage         `json:"enable_authn"`
			Authentication []topicJWTAuthenticator `json:"authentication"`
		}
		if _, err := a.request(ctx, "GET", "/listeners/"+url.PathEscape(listener.ID), nil, &config); err != nil {
			return err
		}
		mode := strings.TrimSpace(string(config.EnableAuthn))
		if mode != "true" && mode != `"quick_deny_anonymous"` {
			return errors.New("MQTT topic authorization requires authentication on every enabled listener")
		}
		if len(config.Authentication) > 0 && !validTopicJWTChain(config.Authentication) {
			return errors.New("MQTT listener overrides the required JWT authentication")
		}
	}
	if !enabled {
		return errors.New("MQTT topic authorization requires an enabled authenticated listener")
	}
	return nil
}

type topicJWTAuthenticator struct {
	Enable                bool   `json:"enable"`
	Mechanism             string `json:"mechanism"`
	From                  string `json:"from"`
	Algorithm             string `json:"algorithm"`
	UseJWKS               bool   `json:"use_jwks"`
	ACLClaim              string `json:"acl_claim_name"`
	DisconnectAfterExpire bool   `json:"disconnect_after_expire"`
	Precondition          string `json:"precondition"`
	VerifyClaims          []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"verify_claims"`
}

func validTopicJWTChain(chain []topicJWTAuthenticator) bool {
	enabled := 0
	for _, authn := range chain {
		if !authn.Enable {
			continue
		}
		enabled++
		if authn.Mechanism != "jwt" || authn.From != "password" || authn.Algorithm != "hmac-based" || authn.UseJWKS || authn.ACLClaim != "acl" || !authn.DisconnectAfterExpire || (authn.Precondition != "" && authn.Precondition != "true") {
			return false
		}
		username := false
		for _, claim := range authn.VerifyClaims {
			if claim.Name == "username" && claim.Value == "${username}" {
				username = true
			}
		}
		if !username {
			return false
		}
	}
	return enabled == 1
}
