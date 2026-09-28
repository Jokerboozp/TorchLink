package platformapp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"iot-platform/internal/capacity"
	"iot-platform/internal/config"
)

// Local source runs share the existing controller implementation with the
// deployed module. No workload starts until an authorized user creates a run.
func startLocalCapacity(cfg *config.Config, log *slog.Logger) (func(), error) {
	noop := func() {}
	if !cfg.Ops.CapacityLocal || (cfg.ProcessRole != "" && cfg.ProcessRole != config.RoleCombined) || cfg.Ops.CapacityURL != "" || cfg.Ops.CapacityToken != "" {
		return noop, nil
	}
	if cfg.PostgresDSN == "" {
		return noop, errors.New("local capacity requires IOT_POSTGRES_DSN for reconciliation")
	}
	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return noop, fmt.Errorf("local capacity API address: %w", err)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	apiURL := "http://" + net.JoinHostPort(host, port)
	instance := cfg.InstanceID
	if instance == "" {
		instance = "local"
	}
	env := capacity.SelfEnvironment{
		API: apiURL, MQTT: cfg.MQTTBroker, Web: "http://127.0.0.1:5173",
		Metrics:     []capacity.MetricsTarget{{Role: config.RoleCombined, Instance: instance, URL: apiURL + "/metrics"}},
		PostgresDSN: cfg.PostgresDSN, ClickHouseURL: cfg.ClickHouseURL,
	}
	if _, err := env.Inventory(); err != nil {
		return noop, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return noop, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return noop, err
	}
	token := hex.EncodeToString(secret)
	results := filepath.Join(cfg.DataDir, "capacity-results")
	svc := capacity.NewService(capacity.ServeOptions{Self: &env, Token: token, ResultsDir: results, Log: os.Stdout})
	server := &http.Server{Handler: svc.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("local capacity controller stopped", "error", err)
		}
	}()
	cfg.Ops.CapacityURL, cfg.Ops.CapacityToken = "http://"+listener.Addr().String(), token
	log.Info("local capacity controller started", "results", results)
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = server.Close()
			svc.Shutdown(15 * time.Second)
		})
	}, nil
}
