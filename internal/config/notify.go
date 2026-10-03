package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// NotifyConfig configures fire alarm notifications.
type NotifyConfig struct {
	Enabled bool
	// AllowedCIDRs lists private networks notifications may reach, such as
	// an internal SMTP relay or IM gateway; public addresses are always
	// allowed.
	AllowedCIDRs []*net.IPNet
	// WebURL is the console address put into alarm links.
	WebURL  string
	loadErr error
}

func loadNotify() NotifyConfig {
	c := NotifyConfig{Enabled: boolValue("IOT_NOTIFY_ENABLED", true), WebURL: strings.TrimRight(strings.TrimSpace(get("IOT_PUBLIC_WEB_URL", "")), "/")}
	for _, item := range strings.Split(get("IOT_NOTIFY_ALLOWED_CIDRS", ""), ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			c.loadErr = fmt.Errorf("IOT_NOTIFY_ALLOWED_CIDRS contains an invalid CIDR %q", item)
			continue
		}
		c.AllowedCIDRs = append(c.AllowedCIDRs, network)
	}
	if c.WebURL != "" {
		if u, err := url.Parse(c.WebURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			c.loadErr = fmt.Errorf("IOT_PUBLIC_WEB_URL must be an HTTP(S) address")
		}
	}
	return c
}
