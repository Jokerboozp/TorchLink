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
