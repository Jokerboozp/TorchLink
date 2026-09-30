package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// AnalyticsConfig bounds only the five analysis applications. These limits are
// resource protections, not capacity or statistical acceptance claims.
type AnalyticsConfig struct {
	Workers    int
	MaxDevices int
	QueueLimit int
	BatchSize  int
	MaxRange   time.Duration
	RunTimeout time.Duration
	Lease      time.Duration
	Poll       time.Duration
	loadErr    error
}

func loadAnalytics() AnalyticsConfig {
	c := AnalyticsConfig{Workers: 2, MaxDevices: 1000, QueueLimit: 100, BatchSize: 1000, MaxRange: 31 * 24 * time.Hour, RunTimeout: 30 * time.Minute, Lease: 30 * time.Second, Poll: time.Second}
	for key, target := range map[string]*int{"IOT_ANALYTICS_WORKERS": &c.Workers, "IOT_ANALYTICS_MAX_DEVICES": &c.MaxDevices, "IOT_ANALYTICS_QUEUE_LIMIT": &c.QueueLimit, "IOT_ANALYTICS_BATCH_SIZE": &c.BatchSize} {
		if raw := os.Getenv(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				c.loadErr = fmt.Errorf("%s must be a positive integer", key)
			} else {
				*target = n
			}
		}
	}
	for key, target := range map[string]*time.Duration{"IOT_ANALYTICS_MAX_RANGE": &c.MaxRange, "IOT_ANALYTICS_RUN_TIMEOUT": &c.RunTimeout, "IOT_ANALYTICS_LEASE": &c.Lease, "IOT_ANALYTICS_POLL": &c.Poll} {
		if raw := os.Getenv(key); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil || d <= 0 {
				c.loadErr = fmt.Errorf("%s must be a positive duration", key)
			} else {
				*target = d
			}
		}
	}
	return c
}

func (c AnalyticsConfig) WithDefaults() AnalyticsConfig {
	d := AnalyticsConfig{Workers: 2, MaxDevices: 1000, QueueLimit: 100, BatchSize: 1000, MaxRange: 31 * 24 * time.Hour, RunTimeout: 30 * time.Minute, Lease: 30 * time.Second, Poll: time.Second}
	if c.Workers == 0 {
		c.Workers = d.Workers
	}
	if c.MaxDevices == 0 {
		c.MaxDevices = d.MaxDevices
	}
	if c.QueueLimit == 0 {
		c.QueueLimit = d.QueueLimit
	}
	if c.BatchSize == 0 {
		c.BatchSize = d.BatchSize
	}
	if c.MaxRange == 0 {
		c.MaxRange = d.MaxRange
	}
	if c.RunTimeout == 0 {
		c.RunTimeout = d.RunTimeout
	}
	if c.Lease == 0 {
		c.Lease = d.Lease
	}
	if c.Poll == 0 {
		c.Poll = d.Poll
	}
	return c
}

func (c AnalyticsConfig) Validate() error {
	if c.loadErr != nil {
		return c.loadErr
	}
	c = c.WithDefaults()
	if c.Workers < 1 || c.Workers > 64 || c.MaxDevices < 1 || c.QueueLimit < 1 || c.BatchSize < 1 || c.MaxRange <= 0 || c.RunTimeout <= 0 || c.Poll <= 0 || c.Lease < 300*time.Millisecond {
		return fmt.Errorf("invalid analytics resource limits")
	}
	return nil
}
