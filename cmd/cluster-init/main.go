// cluster-init prepares shared middleware once per deployment instead of
// letting every process race to do it: it creates or verifies the formal
// Kafka topic inventory (business, retry and dead-letter topics) with the
// required partitions and replication, applies the PostgreSQL schema, and
// verifies the ClickHouse cluster tables. Without -execute it only reports.
//
//	go run ./cmd/cluster-init -env-file .env.cluster -partitions 24 -replication 3
//	go run ./cmd/cluster-init -env-file .env.cluster -partitions 24 -replication 3 -execute
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/segmentio/kafka-go"

	kafkaadapter "iot-platform/internal/adapters/kafka"
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
)

type topicState struct {
	Partitions  int `json:"partitions"`
	Replication int `json:"replication"`
}

type topicAction struct {
	Topic   string     `json:"topic"`
	Action  string     `json:"action"` // ok, create, under_replicated, too_few_partitions
	Current topicState `json:"current"`
	Want    topicState `json:"want"`
}

// planTopics compares the inventory with the broker. Partitions are never
// reduced and replication is never changed automatically; both need an
// operator-run reassignment because they move data.
func planTopics(existing map[string]topicState, want []string, partitions, replication int) []topicAction {
	var out []topicAction
	for _, topic := range want {
		cur, ok := existing[topic]
		a := topicAction{Topic: topic, Current: cur, Want: topicState{partitions, replication}}
		switch {
		case !ok:
			a.Action = "create"
		case cur.Replication < replication:
			a.Action = "under_replicated"
		case cur.Partitions < partitions:
			a.Action = "too_few_partitions"
		default:
			a.Action = "ok"
		}
		out = append(out, a)
	}
	return out
}

func readTopics(ctx context.Context, broker string, dialer *kafka.Dialer) (map[string]topicState, error) {
	conn, err := dialer.DialContext(ctx, "tcp", broker)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	parts, err := conn.ReadPartitions()
	if err != nil {
		return nil, err
	}
	out := map[string]topicState{}
	for _, p := range parts {
		s := out[p.Topic]
		s.Partitions++
		if s.Replication == 0 || len(p.Replicas) < s.Replication {
			s.Replication = len(p.Replicas)
		}
		out[p.Topic] = s
	}
	return out, nil
}

func createTopics(ctx context.Context, broker string, topics []kafka.TopicConfig, dialer *kafka.Dialer) error {
	conn, err := dialer.DialContext(ctx, "tcp", broker)
	if err != nil {
		return err
	}
	defer conn.Close()
	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	cc, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return err
	}
	defer cc.Close()
	return cc.CreateTopics(topics...)
}

type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// clickhouseTables verifies the cluster layout: Distributed front tables and
// replicated *_local tables on every node of the cluster.
func clickhouseTables(ctx context.Context, base, cluster string) check {
	c := check{Name: "clickhouse cluster tables"}
	u, err := url.Parse(base)
	if err != nil {
		c.Detail = "invalid URL"
		return c
	}
	q := u.Query()
	q.Set("query", "SELECT hostName() AS host, name, engine FROM clusterAllReplicas('"+cluster+"', system.tables) WHERE database=currentDatabase() AND name LIKE 'iot_%' FORMAT JSONEachRow")
	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		c.Detail = err.Error()
		return c
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		c.Detail = strings.TrimSpace(string(body))
		return c
	}
	engines := map[string]map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var row struct{ Host, Name, Engine string }
		if json.Unmarshal([]byte(line), &row) == nil {
			if engines[row.Host] == nil {
				engines[row.Host] = map[string]string{}
			}
			engines[row.Host][row.Name] = row.Engine
		}
	}
	var problems []string
	for host, tables := range engines {
		for _, t := range []string{"iot_telemetry", "iot_raw_message"} {
			if tables[t] != "Distributed" || tables[t+"_local"] != "ReplicatedMergeTree" {
				problems = append(problems, fmt.Sprintf("%s: %s=%s %s_local=%s", host, t, tables[t], t, tables[t+"_local"]))
			}
		}
	}
	sort.Strings(problems)
	c.OK = len(engines) > 0 && len(problems) == 0
	c.Detail = fmt.Sprintf("%d nodes checked", len(engines))
	if len(problems) > 0 {
		c.Detail += "; " + strings.Join(problems, "; ")
	}
	return c
}

func main() {
	envFile := flag.String("env-file", "", "环境文件（使用其中的 Kafka、PostgreSQL、ClickHouse 地址）")
	partitions := flag.Int("partitions", 12, "每个主题的分区数（不少于业务消费者实例数×通道）")
	replication := flag.Int("replication", 3, "主题复制因子；集群为 3")
	execute := flag.Bool("execute", false, "创建缺失主题并执行 PostgreSQL 结构迁移；默认只检查")
	bootstrap := flag.Bool("bootstrap-postgres", false, "用 IOT_POSTGRES_ADMIN_DSN 创建应用角色 iot 与数据库 iot（密码取 IOT_POSTGRES_APP_PASSWORD），已存在则跳过")
	flag.Parse()
	if *envFile != "" {
		if err := config.LoadEnvFile(*envFile); err != nil {
			fmt.Fprintln(os.Stderr, "cannot read environment file")
			os.Exit(2)
		}
	}
	// The rendered init.env carries the inventory's topic layout.
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for name, target := range map[string]*int{"partitions": partitions, "replication": replication} {
		env := map[string]string{"partitions": "IOT_CLUSTER_KAFKA_PARTITIONS", "replication": "IOT_CLUSTER_KAFKA_REPLICATION"}[name]
		if v, err := strconv.Atoi(os.Getenv(env)); !set[name] && err == nil && v > 0 {
			*target = v
		}
	}
	cfg := config.Load()
	dialer, err := kafkaadapter.NewDialer(kafkaadapter.SecurityConfig{
		Username: cfg.KafkaSASLUsername, Password: cfg.KafkaSASLPassword,
		Mechanism: cfg.KafkaSASLMechanism, TLS: cfg.KafkaTLS, CAFile: cfg.KafkaTLSCAFile,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Kafka security configuration invalid:", err)
		os.Exit(2)
	}
	dialer.Timeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report := map[string]any{"execute": *execute}
	var checks []check
	ok := true
	if len(cfg.KafkaBrokers) == 0 {
		checks = append(checks, check{Name: "kafka", Detail: "IOT_KAFKA_BROKERS not set"})
		ok = false
	} else if existing, err := readTopics(ctx, cfg.KafkaBrokers[0], dialer); err != nil {
		checks = append(checks, check{Name: "kafka", Detail: err.Error()})
		ok = false
	} else {
		plan := planTopics(existing, model.AllTopics(), *partitions, *replication)
		var create []kafka.TopicConfig
		for _, a := range plan {
			switch a.Action {
			case "create":
				create = append(create, kafka.TopicConfig{Topic: a.Topic, NumPartitions: *partitions, ReplicationFactor: *replication})
			case "ok":
			default:
				ok = false
			}
		}
		if len(create) > 0 {
			if *execute {
				if err = createTopics(ctx, cfg.KafkaBrokers[0], create, dialer); err != nil {
					checks = append(checks, check{Name: "kafka create topics", Detail: err.Error()})
					ok = false
				} else {
					for i := range plan {
						if plan[i].Action == "create" {
							plan[i].Action = "created"
						}
					}
				}
			} else {
				ok = false
			}
		}
		report["topics"] = plan
	}
	if *bootstrap && *execute {
		c := check{Name: "postgresql application database", OK: true, Detail: "role and database present"}
		if err := bootstrapPostgres(ctx, os.Getenv("IOT_POSTGRES_ADMIN_DSN"), os.Getenv("IOT_POSTGRES_APP_PASSWORD")); err != nil {
			c.OK, c.Detail, ok = false, err.Error(), false
		}
		checks = append(checks, c)
	}
	if cfg.PostgresDSN == "" {
		checks = append(checks, check{Name: "postgresql schema", Detail: "IOT_POSTGRES_DSN not set"})
		ok = false
	} else if *execute {
		// New runs the idempotent migration under an advisory lock.
		repo, err := postgres.NewWithOptions(ctx, cfg.PostgresDSN, postgres.PoolOptions{MaxConns: 2})
		c := check{Name: "postgresql schema", OK: err == nil, Detail: "migrated"}
		if err != nil {
			c.Detail = err.Error()
			ok = false
		} else {
			_ = repo.Close()
		}
		checks = append(checks, c)
	}
	if cfg.ClickHouseCluster != "" {
		c := clickhouseTables(ctx, cfg.ClickHouseURL, cfg.ClickHouseCluster)
		if !c.OK && !*execute {
			c.Detail += " (tables are created by the first platform start or cmd/clickhouse-migrate)"
		}
		ok = ok && c.OK
		checks = append(checks, c)
	}
	report["checks"], report["ready"] = checks, ok
	_ = json.NewEncoder(os.Stdout).Encode(report)
	if !ok {
		os.Exit(1)
	}
}

var appPasswordPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,}$`)

// bootstrapPostgres creates the application role and database on a fresh
// Patroni cluster. The password charset is restricted, so quoting it into
// the utility statement (which takes no bind parameters) is safe.
func bootstrapPostgres(ctx context.Context, adminDSN, password string) error {
	if adminDSN == "" || !appPasswordPattern.MatchString(password) {
		return errors.New("IOT_POSTGRES_ADMIN_DSN and an IOT_POSTGRES_APP_PASSWORD of letters, digits and . _ ~ - (8+) are required")
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var exists bool
	if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='iot')`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err = conn.Exec(ctx, `CREATE ROLE iot LOGIN PASSWORD '`+password+`'`); err != nil {
			return err
		}
	}
	if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='iot')`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		_, err = conn.Exec(ctx, `CREATE DATABASE iot OWNER iot`)
	}
	return err
}
