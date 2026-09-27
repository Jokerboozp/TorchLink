// clickhouse-migrate copies the single-node telemetry and raw payload tables
// into a ClickHouse cluster (replicated *_local tables behind Distributed
// tables) one monthly partition at a time. Without -execute it only reports
// the plan. Every copied partition is verified by row count and distinct
// message IDs; a partially filled target partition is never merged into.
//
//	go run ./cmd/clickhouse-migrate -source <单节点 URL> -target <集群 URL> -cluster iot_cluster
//	go run ./cmd/clickhouse-migrate ... -execute
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	clickhouseadapter "iot-platform/internal/adapters/clickhouse"
)

type table struct {
	name      string
	partition string // expression equal to system.parts partition values
}

var tables = []table{
	{"iot_telemetry", "toYYYYMM(ts)"},
	{"iot_raw_message", "toYYYYMM(fromUnixTimestamp64Milli(received_at))"},
}

var partitionPattern = regexp.MustCompile(`^[0-9]{6}$`)

type client struct {
	base string
	http *http.Client
}

func (c client) query(ctx context.Context, q string, body io.Reader) ([]byte, error) {
	u, err := url.Parse(c.base)
	if err != nil {
		return nil, err
	}
	params := u.Query()
	params.Set("query", q)
	u.RawQuery = params.Encode()
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("clickhouse %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	return out, err
}

// stream sends the source query result straight into the target insert.
func stream(ctx context.Context, src, dst client, selectSQL, insertSQL string) error {
	u, err := url.Parse(src.base)
	if err != nil {
		return err
	}
	params := u.Query()
	params.Set("query", selectSQL)
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := src.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("source %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	_, err = dst.query(ctx, insertSQL, resp.Body)
	return err
}

func number(body []byte, field string) (int64, error) {
	var row map[string]json.Number
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(body))), &row); err != nil {
		return 0, err
	}
	return row[field].Int64()
}

// stats identify a partition's content: counts plus an order-independent
// checksum of message IDs, so equal counts of different messages differ.
type stats struct {
	Rows     int64  `json:"rows"`
	Distinct int64  `json:"distinctMessages"`
	Checksum string `json:"idChecksum"`
}

func (a stats) same(b stats) bool {
	return a.Rows == b.Rows && a.Distinct == b.Distinct && a.Checksum == b.Checksum
}

func count(ctx context.Context, c client, t table, partition string) (stats, error) {
	body, err := c.query(ctx, fmt.Sprintf("SELECT count() AS rows, uniqExact(message_id) AS ids, toString(sum(cityHash64(message_id))) AS checksum FROM %s WHERE %s = %s FORMAT JSONEachRow", t.name, t.partition, partition), nil)
	if err != nil {
		return stats{}, err
	}
	var s stats
	if s.Rows, err = number(body, "rows"); err == nil {
		s.Distinct, err = number(body, "ids")
	}
	var row struct {
		Checksum string `json:"checksum"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(strings.TrimSpace(string(body))), &row)
		s.Checksum = row.Checksum
	}
	return s, err
}

type step struct {
	Table     string `json:"table"`
	Partition string `json:"partition"`
	Source    stats  `json:"source"`
	Target    stats  `json:"targetBefore"`
	Action    string `json:"action"`
	After     *stats `json:"targetAfter,omitempty"`
	Verified  bool   `json:"verified"`
	Error     string `json:"error,omitempty"`
}

func partitions(ctx context.Context, c client, t table) ([]string, error) {
	body, err := c.query(ctx, "SELECT DISTINCT partition FROM system.parts WHERE database=currentDatabase() AND table='"+t.name+"' AND active ORDER BY partition FORMAT JSONEachRow", nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var row struct{ Partition string }
		if line == "" || json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		if !partitionPattern.MatchString(row.Partition) {
			return nil, fmt.Errorf("unexpected partition %q in %s", row.Partition, t.name)
		}
		out = append(out, row.Partition)
	}
	return out, nil
}

func run(ctx context.Context, src, dst client, cluster string, execute bool, only map[string]bool) ([]step, error) {
	if execute {
		for _, statement := range clickhouseadapter.SchemaStatements(cluster) {
			if _, err := dst.query(ctx, statement, nil); err != nil {
				return nil, fmt.Errorf("prepare target schema: %w", err)
			}
		}
	}
	var steps []step
	for _, t := range tables {
		parts, err := partitions(ctx, src, t)
		if err != nil {
			return steps, err
		}
		for _, p := range parts {
			if len(only) > 0 && !only[p] {
				continue
			}
			st := step{Table: t.name, Partition: p}
			if st.Source, err = count(ctx, src, t, p); err != nil {
				return steps, err
			}
			if st.Target, err = count(ctx, dst, t, p); err != nil && execute {
				return steps, err
			}
			switch {
			case st.Target.same(st.Source):
				st.Action, st.Verified = "already_copied", true
			case st.Target.Rows > 0:
				// Merging into a partly filled partition could duplicate rows;
				// an operator must inspect and drop it first.
				st.Action, st.Error = "blocked_partial_target", "target partition is partly filled; drop it on the cluster after inspection and rerun"
			case !execute:
				st.Action = "copy"
			default:
				st.Action = "copied"
				insert := "INSERT INTO " + t.name + " SETTINGS insert_distributed_sync=1 FORMAT JSONEachRow"
				if err = stream(ctx, src, dst, fmt.Sprintf("SELECT * FROM %s WHERE %s = %s FORMAT JSONEachRow", t.name, t.partition, p), insert); err != nil {
					st.Error = err.Error()
					steps = append(steps, st)
					return steps, fmt.Errorf("copy %s %s: %w", t.name, p, err)
				}
				after, err := count(ctx, dst, t, p)
				if err != nil {
					return steps, err
				}
				st.After = &after
				st.Verified = after.same(st.Source)
				if !st.Verified {
					st.Error = "row count, distinct messages or ID checksum differ after copy"
				}
			}
			steps = append(steps, st)
		}
	}
	return steps, nil
}

func main() {
	source := flag.String("source", "", "单节点 ClickHouse URL（含 ?database=）")
	target := flag.String("target", "", "集群任一节点 ClickHouse URL（含 ?database=）")
	cluster := flag.String("cluster", "", "目标集群名，与 IOT_CLICKHOUSE_CLUSTER 一致")
	execute := flag.Bool("execute", false, "实际建表并复制；默认只输出计划")
	only := flag.String("partitions", "", "只处理这些月份分区（逗号分隔，如 202609,202610）")
	timeout := flag.Duration("timeout", 6*time.Hour, "整体时限")
	flag.Parse()
	if *source == "" || *target == "" || *cluster == "" {
		fmt.Fprintln(os.Stderr, "source, target and cluster are required")
		os.Exit(2)
	}
	filter := map[string]bool{}
	for _, p := range strings.Split(*only, ",") {
		if p = strings.TrimSpace(p); p != "" {
			if !partitionPattern.MatchString(p) {
				fmt.Fprintln(os.Stderr, "partitions must be YYYYMM")
				os.Exit(2)
			}
			filter[p] = true
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	hc := &http.Client{Timeout: 0}
	steps, err := run(ctx, client{*source, hc}, client{*target, hc}, *cluster, *execute, filter)
	ok := err == nil
	for _, s := range steps {
		ok = ok && s.Error == "" && (s.Verified || !*execute)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"execute": *execute, "cluster": *cluster, "steps": steps, "complete": ok, "error": errString(err)})
	if !ok {
		os.Exit(1)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
