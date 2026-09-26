package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// VideoConfig holds the deployment side of the optional camera live module.
// An empty MediaAPIURL means the media service is not deployed; the rest of
// the platform, including basic camera management, never depends on it.
type VideoConfig struct {
	MediaAPIURL      string
	MediaSecret      string
	MediaServerID    string
	HookSecret       string
	CredentialKey    []byte
	CredentialKeyID  string
	AllowedCIDRs     []*net.IPNet
	AllowedPorts     map[int]bool
	HLSPublicPath    string
	LeaseTTL         time.Duration
	IdleGrace        time.Duration
	StartTimeout     time.Duration
	MaxSessions      int
	MaxSourceStreams int
	Transcode        bool
	MaxTranscodes    int
	HWAccel          string
	loadErr          error
}

// Deployed reports whether the media service was configured for this API.
func (c VideoConfig) Deployed() bool { return c.MediaAPIURL != "" }

func loadVideo() VideoConfig {
	cfg := VideoConfig{
		MediaAPIURL:      trimURL(os.Getenv("IOT_VIDEO_MEDIA_API_URL")),
		MediaSecret:      strings.TrimSpace(os.Getenv("IOT_VIDEO_MEDIA_SECRET")),
		MediaServerID:    get("IOT_VIDEO_MEDIA_SERVER_ID", "torchlink-media-1"),
		HookSecret:       strings.TrimSpace(os.Getenv("IOT_VIDEO_HOOK_SECRET")),
		HLSPublicPath:    strings.TrimRight(get("IOT_VIDEO_HLS_PUBLIC_PATH", "/media/hls"), "/"),
		LeaseTTL:         duration("IOT_VIDEO_SESSION_LEASE", 45*time.Second),
		IdleGrace:        duration("IOT_VIDEO_IDLE_GRACE", 20*time.Second),
		StartTimeout:     duration("IOT_VIDEO_START_TIMEOUT", 15*time.Second),
		MaxSessions:      intValue("IOT_VIDEO_MAX_SESSIONS", 64),
		MaxSourceStreams: intValue("IOT_VIDEO_MAX_SOURCE_STREAMS", 32),
		Transcode:        boolValue("IOT_VIDEO_TRANSCODE_ENABLED", false),
		MaxTranscodes:    intValue("IOT_VIDEO_TRANSCODE_MAX", 2),
		HWAccel:          strings.ToLower(get("IOT_VIDEO_TRANSCODE_HWACCEL", "none")),
		AllowedPorts:     map[int]bool{},
	}
	if raw := strings.TrimSpace(os.Getenv("IOT_VIDEO_CREDENTIAL_KEY")); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			cfg.loadErr = fmt.Errorf("IOT_VIDEO_CREDENTIAL_KEY must be 32 random bytes encoded as standard base64")
		} else {
			sum := sha256.Sum256(key)
			cfg.CredentialKey, cfg.CredentialKeyID = key, hex.EncodeToString(sum[:4])
		}
	}
	// Camera networks must be named explicitly; there is no implicit allow-all.
	for _, value := range split(get("IOT_VIDEO_ALLOWED_CIDRS", "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16")) {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			cfg.loadErr = fmt.Errorf("IOT_VIDEO_ALLOWED_CIDRS contains an invalid CIDR: %s", value)
			break
		}
		cfg.AllowedCIDRs = append(cfg.AllowedCIDRs, network)
	}
	for _, value := range split(get("IOT_VIDEO_ALLOWED_PORTS", "80,443,554,8000,8080,8554,8899,10554,2020,5000,37777")) {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			cfg.loadErr = fmt.Errorf("IOT_VIDEO_ALLOWED_PORTS contains an invalid port: %s", value)
			break
		}
		cfg.AllowedPorts[port] = true
	}
	return cfg
}

// Problem reports a deployment configuration error. It is deliberately not
// part of Config.Validate: a broken optional module is reported as a module
// state and must not stop the API or other business interfaces.
func (c VideoConfig) Problem() error {
	if c.loadErr != nil {
		return c.loadErr
	}
	if !c.Deployed() {
		return nil
	}
	u, err := url.Parse(c.MediaAPIURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("IOT_VIDEO_MEDIA_API_URL must be an HTTP(S) origin without credentials")
	}
	var problems []string
	if len(c.MediaSecret) < 24 || insecurePlaceholder(c.MediaSecret) {
		problems = append(problems, "IOT_VIDEO_MEDIA_SECRET must be at least 24 random characters")
	}
	if len(c.HookSecret) < 24 || insecurePlaceholder(c.HookSecret) {
		problems = append(problems, "IOT_VIDEO_HOOK_SECRET must be at least 24 random characters")
	}
	if len(c.CredentialKey) != 32 {
		problems = append(problems, "IOT_VIDEO_CREDENTIAL_KEY is required when the video module is deployed")
	}
	if !strings.HasPrefix(c.HLSPublicPath, "/") || strings.ContainsAny(c.HLSPublicPath, "?#") {
		problems = append(problems, "IOT_VIDEO_HLS_PUBLIC_PATH must be an absolute path such as /media/hls")
	}
	if c.LeaseTTL < 15*time.Second || c.LeaseTTL > 5*time.Minute {
		problems = append(problems, "IOT_VIDEO_SESSION_LEASE must be between 15s and 5m")
	}
	if c.IdleGrace < 0 || c.IdleGrace > 10*time.Minute {
		problems = append(problems, "IOT_VIDEO_IDLE_GRACE must be between 0 and 10m")
	}
	if c.MaxTranscodes > 64 || c.MaxSessions > 10000 || c.MaxSourceStreams > 1000 {
		problems = append(problems, "video limits are out of range")
	}
	// Hardware encoders need device passthrough and an image with the matching
	// FFmpeg build; neither is part of the verified default deployment.
	if c.HWAccel != "none" {
		problems = append(problems, "IOT_VIDEO_TRANSCODE_HWACCEL currently only supports none")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid video module configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}
