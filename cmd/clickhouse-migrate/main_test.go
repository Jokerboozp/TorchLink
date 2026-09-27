package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCH stores JSONEachRow rows per table and answers the queries the tool issues.
type fakeCH struct {
	mu   sync.Mutex
	rows map[string][]map[string]any
	ddl  []string
}

var wherePartition = regexp.MustCompile(`FROM (\w+) WHERE .* = (\d{6})`)

func partitionOf(table string, row map[string]any) string {
	if table == "iot_telemetry" {
		ts := row["ts"].(string)
		return ts[0:4] + ts[5:7]
	}
	ms := int64(row["received_at"].(float64))
	return time.UnixMilli(ms).UTC().Format("200601")
}

func (f *fakeCH) handler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("query")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasPrefix(q, "CREATE") || strings.HasPrefix(q, "ALTER"):
		f.ddl = append(f.ddl, q)
	case strings.Contains(q, "FROM system.parts"):
		table := regexp.MustCompile(`table='(\w+)'`).FindStringSubmatch(q)[1]
		seen := map[string]bool{}
		for _, row := range f.rows[table] {
			if p := partitionOf(table, row); !seen[p] {
				seen[p] = true
				fmt.Fprintf(w, `{"partition":"%s"}`+"\n", p)
			}
		}
	case strings.HasPrefix(q, "SELECT count()"):
		m := wherePartition.FindStringSubmatch(q)
		n, ids, sum := 0, map[any]bool{}, uint64(0)
		for _, row := range f.rows[m[1]] {
			if partitionOf(m[1], row) == m[2] {
				n++
				ids[row["message_id"]] = true
				h := fnv.New64a()
				_, _ = h.Write([]byte(row["message_id"].(string)))
				sum += h.Sum64()
			}
		}
		fmt.Fprintf(w, `{"rows":"%d","ids":"%d","checksum":"%d"}`+"\n", n, len(ids), sum)
	case strings.HasPrefix(q, "SELECT *"):
		m := wherePartition.FindStringSubmatch(q)
		for _, row := range f.rows[m[1]] {
			if partitionOf(m[1], row) == m[2] {
				b, _ := json.Marshal(row)
				_, _ = w.Write(append(b, '\n'))
			}
		}
	case strings.HasPrefix(q, "INSERT INTO"):
		if !strings.Contains(q, "insert_distributed_sync=1") {
			http.Error(w, "asynchronous distributed insert", 400)
			return
		}
		table := strings.Fields(q)[2]
		body, _ := io.ReadAll(r.Body)
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) == nil {
				f.rows[table] = append(f.rows[table], row)
			}
		}
	default:
		http.Error(w, "unexpected "+q, 400)
	}
}

func TestMigrationPlansCopiesVerifiesAndRefusesPartialTargets(t *testing.T) {
	source := &fakeCH{rows: map[string][]map[string]any{
		"iot_telemetry": {
			{"message_id": "a", "ts": "2026-09-01 00:00:00.000"}, {"message_id": "b", "ts": "2026-09-02 00:00:00.000"},
			{"message_id": "c", "ts": "2026-10-01 00:00:00.000"},
		},
		"iot_raw_message": {{"message_id": "r1", "received_at": float64(time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC).UnixMilli())}},
	}}
	target := &fakeCH{rows: map[string][]map[string]any{}}
	src, dst := httptest.NewServer(http.HandlerFunc(source.handler)), httptest.NewServer(http.HandlerFunc(target.handler))
	defer src.Close()
	defer dst.Close()
	ctx := context.Background()
	s, d := client{src.URL, http.DefaultClient}, client{dst.URL, http.DefaultClient}
	plan, err := run(ctx, s, d, "iot_cluster", false, nil)
	if err != nil || len(plan) != 3 || len(target.ddl) != 0 || len(target.rows) != 0 {
		t.Fatalf("dry run must not write: %+v %v ddl=%d", plan, err, len(target.ddl))
	}
	for _, st := range plan {
		if st.Action != "copy" {
			t.Fatal(st)
		}
	}
	done, err := run(ctx, s, d, "iot_cluster", true, map[string]bool{"202609": true})
	if err != nil || len(done) != 2 {
		t.Fatal(done, err)
	}
	for _, st := range done {
		if st.Action != "copied" || !st.Verified || st.After.Rows != st.Source.Rows {
			t.Fatal("copy not verified", st)
		}
	}
	if !strings.Contains(strings.Join(target.ddl, "\n"), "ReplicatedMergeTree") {
		t.Fatal("target schema not prepared")
	}
	// Rerunning skips verified partitions; a partial target blocks.
	target.rows["iot_telemetry"] = append(target.rows["iot_telemetry"], map[string]any{"message_id": "x", "ts": "2026-10-05 00:00:00.000"})
	again, err := run(ctx, s, d, "iot_cluster", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, st := range again {
		actions[st.Table+"/"+st.Partition] = st.Action
	}
	if actions["iot_telemetry/202609"] != "already_copied" || actions["iot_telemetry/202610"] != "blocked_partial_target" {
		t.Fatal(actions)
	}
}
