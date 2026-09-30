package config

import (
	"testing"
	"time"
)

func TestAnalyticsResourceLimits(t *testing.T) {
	for _, key := range []string{"IOT_ANALYTICS_WORKERS", "IOT_ANALYTICS_MAX_DEVICES", "IOT_ANALYTICS_QUEUE_LIMIT", "IOT_ANALYTICS_BATCH_SIZE", "IOT_ANALYTICS_MAX_RANGE", "IOT_ANALYTICS_RUN_TIMEOUT", "IOT_ANALYTICS_LEASE", "IOT_ANALYTICS_POLL"} {
		t.Setenv(key, "")
	}
	if c := loadAnalytics(); c.Workers != 2 || c.Validate() != nil {
		t.Fatal(c)
	}
	t.Setenv("IOT_ANALYTICS_WORKERS", "0")
	if loadAnalytics().Validate() == nil {
		t.Fatal("zero workers accepted")
	}
	t.Setenv("IOT_ANALYTICS_WORKERS", "2")
	t.Setenv("IOT_ANALYTICS_MAX_RANGE", "invalid")
	if loadAnalytics().Validate() == nil {
		t.Fatal("invalid max range accepted")
	}
	t.Setenv("IOT_ANALYTICS_MAX_RANGE", "24h")
	t.Setenv("IOT_ANALYTICS_LEASE", "1ms")
	if loadAnalytics().Validate() == nil {
		t.Fatal("unusable heartbeat lease accepted")
	}
	t.Setenv("IOT_ANALYTICS_LEASE", "30s")
	c := loadAnalytics()
	if c.MaxRange != 24*time.Hour || c.Validate() != nil {
		t.Fatal(c)
	}
}
