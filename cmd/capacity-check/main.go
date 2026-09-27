// capacity-check is read-only: it reports measured prerequisites and explicit
// unknowns. It never creates topics, containers, replicas or a throughput claim.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
	"iot-platform/internal/adapters/mqtt"
	"iot-platform/internal/config"
	"iot-platform/internal/deploycheck"
	"iot-platform/internal/model"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	envFile := flag.String("env-file", ".env.local", "环境文件")
	replicas := flag.Int("replicas", 1, "同等连接池的 API / 网关进程总数（保守按均消费计算）")
	reserve := flag.Int("postgres-reserve", 32, "为其他进程及管理预留的连接")
	rpm := flag.Int("provider-rpm", 0, "提供方分配给自动研判的请求/分钟；0 表示未知")
	latency := flag.Duration("model-latency", 0, "实测模型平均延迟；0 表示未知")
	flag.Parse()
	if *replicas < 1 || *reserve < 0 || *rpm < 0 || *latency < 0 {
		fmt.Fprintln(os.Stderr, "容量参数必须非负且 replicas 至少为 1")
		os.Exit(2)
	}
	if err := config.LoadEnvFile(*envFile); err != nil {
		fmt.Fprintln(os.Stderr, "无法读取环境文件")
		os.Exit(2)
	}
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	checks := []deploycheck.CapacityCheck{}
	plan := deploycheck.CapacityPlan{Replicas: *replicas, ReservedConnections: *reserve, PoolPerProcess: int(cfg.PostgresMaxConns), AccessCoordinated: cfg.AccessCoordination && cfg.AccessNodeURL != "" && cfg.PostgresDSN != "", AIConcurrency: int(cfg.AIAnalysisConcurrency), AIRPM: int(cfg.AIAnalysisRPM), ProviderRPM: *rpm, ModelLatency: *latency}
	if pc, err := pgxpool.ParseConfig(cfg.PostgresDSN); err == nil && cfg.PostgresDSN != "" {
		if strings.Contains(cfg.PostgresDSN, "pool_max_conns") {
			plan.PoolPerProcess = int(pc.MaxConns)
		}
		pc.MaxConns, pc.MinConns = 1, 0
		if pool, err := pgxpool.NewWithConfig(ctx, pc); err == nil {
			_ = pool.QueryRow(ctx, "SELECT setting::int FROM pg_settings WHERE name='max_connections'").Scan(&plan.PostgresLimit)
			pool.Close()
		}
	}
	if len(cfg.KafkaBrokers) > 0 {
		dialer := kafka.Dialer{Timeout: 5 * time.Second}
		if conn, err := dialer.DialContext(ctx, "tcp", cfg.KafkaBrokers[0]); err == nil {
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			parts, err := conn.ReadPartitions()
			_ = conn.Close()
			if err == nil {
				counts := map[string]int{}
				repl := map[string]int{}
				for _, p := range parts {
					counts[p.Topic]++
					if repl[p.Topic] == 0 || len(p.Replicas) < repl[p.Topic] {
						repl[p.Topic] = len(p.Replicas)
					}
				}
				plan.MinPartitions, plan.MinReplication = 1<<30, 1<<30
				for _, topic := range []string{model.TopicRaw, model.TopicDeviceBusiness, model.TopicPropertyReport, model.TopicEventReport, model.TopicParsed, model.TopicAlarmRaised} {
					plan.MinPartitions = min(plan.MinPartitions, counts[topic])
					plan.MinReplication = min(plan.MinReplication, repl[topic])
				}
			}
		}
	}
	if cfg.EMQXAPIURL != "" && cfg.EMQXAPIKey != "" && cfg.EMQXAPISecret != "" {
		identity, err := os.ReadFile(filepath.Join(cfg.DataDir, "mqtt-inbox", cfg.ProcessRole, "client-id"))
		if err == nil {
			_, _, err = (&mqttadapter.Admin{URL: cfg.EMQXAPIURL, Key: cfg.EMQXAPIKey, Secret: cfg.EMQXAPISecret}).SessionQueue(ctx, strings.TrimSpace(string(identity)))
			plan.MQTTObserved = err == nil
		}
	}
	if u, err := url.Parse(cfg.ClickHouseURL); err == nil && u.Host != "" {
		q := u.Query()
		q.Set("query", "SELECT name,engine FROM system.tables WHERE database=currentDatabase() AND name IN ('iot_telemetry','iot_raw_message') FORMAT JSONEachRow")
		u.RawQuery = q.Encode()
		req, _ := http.NewRequestWithContext(ctx, "POST", u.String(), nil)
		client := http.Client{Timeout: 5 * time.Second}
		if res, err := client.Do(req); err == nil {
			defer res.Body.Close()
			if res.StatusCode == 200 {
				dec := json.NewDecoder(res.Body)
				for dec.More() {
					var row struct {
						Name   string `json:"name"`
						Engine string `json:"engine"`
					}
					if dec.Decode(&row) != nil {
						break
					}
					checks = append(checks, deploycheck.CapacityCheck{Name: "ClickHouse " + row.Name, Status: "observed", Detail: "当前表引擎：" + row.Engine})
				}
			}
		}
	}
	checks = append(checks, deploycheck.AssessCapacity(plan)...)
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	_ = out.Encode(map[string]any{"measuredAt": time.Now().Format(time.RFC3339), "checks": checks, "clusterCapacityVerified": false})
}
