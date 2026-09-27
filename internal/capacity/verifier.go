package capacity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RawRecord is the archive index of one raw message.
type RawRecord struct {
	PayloadHash string
	Published   bool
	ParseError  string
}

// StandardRecord is one parsed message derived from a raw message.
type StandardRecord struct {
	MessageID     string
	Type          string
	HasProperties bool
	ProcessedAt   int64 // Unix ms in the database clock; 0 = not processed
}

// Store answers fixed, parameterised reconciliation questions. It never
// accepts caller-supplied SQL.
type Store interface {
	RawIndexes(ctx context.Context, tenant string, ids []string) (map[string]RawRecord, error)
	RawBodies(ctx context.Context, tenant string, ids []string) (map[string]int, error)
	Standards(ctx context.Context, tenant string, rawIDs []string) (map[string][]StandardRecord, error)
	TelemetryConfigured() bool
	Telemetry(ctx context.Context, tenant string, messageIDs []string) (map[string]int, error)
	// ClockOffset is database clock minus local clock.
	ClockOffset(ctx context.Context) (offset, uncertainty time.Duration, err error)
	Close()
}

// Message verification states. confirmed/conflict/parse_failed are final;
// the others may still change until the drain ends.
const (
	StatePending    = "pending"
	StateConfirmed  = "confirmed"
	StateMissing    = "missing"
	StateUnknown    = "unknown"
	StateRejected   = "rejected"
	StateConflict   = "conflict"
	StateParseFail  = "parse_failed"
	StateNoBody     = "body_missing"
	StateNoTelemetr = "telemetry_missing"
)

type trackedMessage struct {
	phase       string
	measured    bool
	stream      string
	device      string
	hash        string
	ingressOK   bool
	result      string
	scheduledUS int64 // controller clock
	state       string
	processedAt int64
	copies      int
}

// Verifier reconciles every ledger message by raw ID. Each pass only queries
// messages that have not reached a final state.
type Verifier struct {
	store      Store
	tenant     string
	batch      int
	mu         sync.Mutex
	msgs       map[string]*trackedMessage
	duplicates map[string]int
	ackOnly    map[string]uint64 // phase -> TCP messages with protocol-ACK evidence only
	dbOffset   time.Duration
	dbUncert   time.Duration
	clockErr   error
}

func NewVerifier(store Store, tenant string) *Verifier {
	return &Verifier{store: store, tenant: tenant, batch: 1000, msgs: map[string]*trackedMessage{}, duplicates: map[string]int{}, ackOnly: map[string]uint64{}}
}

// Track adds a phase ledger. agentOffset is agent clock minus controller clock.
func (v *Verifier) Track(path, phase string, agentOffset time.Duration) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return ReadLedger(path, func(_ LedgerHeader, e LedgerEntry) error {
		if e.Result == "not_sent" {
			return nil
		}
		if e.Stream == "tcp" {
			if e.OK {
				v.ackOnly[phase]++
			}
			return nil
		}
		if e.RawID == "" {
			return nil
		}
		if _, dup := v.msgs[e.RawID]; dup {
			v.duplicates[phase]++
			return nil
		}
		v.msgs[e.RawID] = &trackedMessage{phase: phase, measured: e.Measured, stream: e.Stream, device: e.Device, hash: e.Hash, ingressOK: e.OK, result: e.Result, scheduledUS: e.Scheduled - agentOffset.Microseconds(), state: StatePending}
		return nil
	})
}

// Pass queries unresolved messages. With final=true, anything still absent
// is classified as missing (ingress confirmed), rejected, or unknown.
func (v *Verifier) Pass(ctx context.Context, final bool) error {
	if off, unc, err := v.store.ClockOffset(ctx); err == nil {
		v.mu.Lock()
		v.dbOffset, v.dbUncert, v.clockErr = off, unc, nil
		v.mu.Unlock()
	} else {
		v.mu.Lock()
		v.clockErr = err
		v.mu.Unlock()
	}
	v.mu.Lock()
	var open []string
	for id, m := range v.msgs {
		if m.state != StateConfirmed && m.state != StateConflict && m.state != StateParseFail {
			open = append(open, id)
		}
	}
	v.mu.Unlock()
	sort.Strings(open)
	for i := 0; i < len(open); i += v.batch {
		ids := open[i:min(i+v.batch, len(open))]
		if err := v.passBatch(ctx, ids, final); err != nil {
			return err
		}
	}
	return nil
}

func (v *Verifier) passBatch(ctx context.Context, ids []string, final bool) error {
	raws, err := v.store.RawIndexes(ctx, v.tenant, ids)
	if err != nil {
		return fmt.Errorf("raw index: %w", err)
	}
	var present []string
	for _, id := range ids {
		if _, ok := raws[id]; ok {
			present = append(present, id)
		}
	}
	bodies, stds := map[string]int{}, map[string][]StandardRecord{}
	if len(present) > 0 {
		if bodies, err = v.store.RawBodies(ctx, v.tenant, present); err != nil {
			return fmt.Errorf("raw body: %w", err)
		}
		if stds, err = v.store.Standards(ctx, v.tenant, present); err != nil {
			return fmt.Errorf("standard message: %w", err)
		}
	}
	telemetry := map[string]int{}
	if v.store.TelemetryConfigured() {
		var want []string
		for _, list := range stds {
			for _, s := range list {
				if s.ProcessedAt > 0 && s.HasProperties {
					want = append(want, s.MessageID)
				}
			}
		}
		if len(want) > 0 {
			if telemetry, err = v.store.Telemetry(ctx, v.tenant, want); err != nil {
				return fmt.Errorf("telemetry: %w", err)
			}
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, id := range ids {
		m := v.msgs[id]
		raw, ok := raws[id]
		m.copies = bodies[id]
		switch {
		case !ok:
			m.state = StatePending
			if final {
				m.state = absentState(m)
			}
		case raw.PayloadHash != m.hash:
			m.state = StateConflict
		case raw.ParseError != "":
			m.state = StateParseFail
		default:
			m.state = settledState(raw, stds[id], bodies[id], telemetry, v.store.TelemetryConfigured(), final)
			for _, s := range stds[id] {
				if s.ProcessedAt > m.processedAt {
					m.processedAt = s.ProcessedAt
				}
			}
		}
	}
	return nil
}

func absentState(m *trackedMessage) string {
	if m.ingressOK {
		return StateMissing
	}
	// An explicit HTTP 4xx rejection is a known outcome; timeouts, resets and
	// 5xx may have been written, so they stay unknown instead of lost.
	if len(m.result) == 3 && m.result[0] == '4' {
		return StateRejected
	}
	return StateUnknown
}

func settledState(raw RawRecord, stds []StandardRecord, copies int, telemetry map[string]int, telemetryOn, final bool) string {
	processed := len(stds) > 0
	for _, s := range stds {
		if s.ProcessedAt == 0 {
			processed = false
		}
	}
	if !raw.Published || !processed {
		return StatePending
	}
	if copies == 0 {
		if final {
			return StateNoBody
		}
		return StatePending
	}
	if telemetryOn {
		for _, s := range stds {
			if s.HasProperties && telemetry[s.MessageID] == 0 {
				if final {
					return StateNoTelemetr
				}
				return StatePending
			}
		}
	}
	return StateConfirmed
}

// Integrity is the reconciliation result for one phase.
type Integrity struct {
	VerificationMode       string            `json:"verificationMode"`
	UniqueSent             uint64            `json:"uniqueSent"`
	IngressConfirmed       uint64            `json:"ingressConfirmed"`
	UniqueArchiveConfirmed uint64            `json:"uniqueArchiveConfirmed"`
	UniqueBusinessDone     uint64            `json:"uniqueBusinessCompleted"`
	States                 map[string]uint64 `json:"states"`
	Missing                uint64            `json:"missing"`
	Unknown                uint64            `json:"unknown"`
	Pending                uint64            `json:"pending"`
	Unaccounted            uint64            `json:"unaccounted"`
	DuplicateLedgerIDs     uint64            `json:"duplicateLedgerIds"`
	PhysicalDuplicateRows  uint64            `json:"physicalDuplicateRows"`
	TCPAckOnly             uint64            `json:"tcpAckOnly"`
	Samples                map[string]string `json:"samples,omitempty"`
	BusinessLatency        Histogram         `json:"businessLatency"`
	BusinessLatencyValid   bool              `json:"businessLatencyValid"`
	ClockUncertaintyMS     float64           `json:"clockUncertaintyMs"`
	Note                   string            `json:"note,omitempty"`
}

// PhaseIntegrity summarises one phase. Business latency is the database
// completion time minus the scheduled send time, both mapped to the
// controller clock; it covers measured-window messages only.
func (v *Verifier) PhaseIntegrity(phase string, agentUncertainty time.Duration) Integrity {
	v.mu.Lock()
	defer v.mu.Unlock()
	in := Integrity{VerificationMode: "full_id", States: map[string]uint64{}, Samples: map[string]string{}, TCPAckOnly: v.ackOnly[phase], DuplicateLedgerIDs: uint64(v.duplicates[phase])}
	for id, m := range v.msgs {
		if m.phase != phase {
			continue
		}
		in.UniqueSent++
		if m.ingressOK {
			in.IngressConfirmed++
		}
		in.States[m.state]++
		if m.copies > 1 {
			in.PhysicalDuplicateRows += uint64(m.copies - 1)
		}
		switch m.state {
		case StateConfirmed:
			in.UniqueArchiveConfirmed++
			in.UniqueBusinessDone++
			if m.measured && m.processedAt > 0 {
				done := m.processedAt*1000 - v.dbOffset.Microseconds()
				in.BusinessLatency.Observe(float64(done-m.scheduledUS) / 1000)
			}
		case StatePending, StateNoTelemetr, StateNoBody:
			in.UniqueArchiveConfirmed++
		}
		switch m.state {
		case StateMissing, StateConflict, StateParseFail, StateNoBody, StateNoTelemetr:
			in.Missing++
		case StateUnknown:
			in.Unknown++
		case StatePending:
			in.Pending++
		}
		if m.state != StateConfirmed && m.state != StateRejected {
			if _, ok := in.Samples[m.state]; !ok {
				in.Samples[m.state] = redactID(id) + " device=" + m.device + " result=" + m.result
			}
		}
	}
	in.Unaccounted = in.Missing
	in.ClockUncertaintyMS = float64((v.dbUncert + agentUncertainty).Microseconds()) / 1000
	in.BusinessLatencyValid = v.clockErr == nil && in.BusinessLatency.N > 0
	if v.clockErr != nil {
		in.Note = "database clock offset unavailable; business latency not computed"
	}
	if in.UniqueSent == 0 && in.TCPAckOnly > 0 {
		in.VerificationMode = "ack_only"
	}
	return in
}

// redactID keeps enough of an ID to locate it without printing it in full.
func redactID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:12] + "…" + id[len(id)-4:]
}

// PGCHStore reads PostgreSQL and (optionally) ClickHouse with fixed queries.
type PGCHStore struct {
	pool   *pgxpool.Pool
	chURL  string
	client *http.Client
}

func NewPGCHStore(ctx context.Context, pgDSN, chURL string) (*PGCHStore, error) {
	cfg, err := pgxpool.ParseConfig(pgDSN)
	if err != nil {
		return nil, errors.New("observer PostgreSQL DSN is invalid")
	}
	cfg.MaxConns, cfg.MinConns = 2, 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("observer PostgreSQL unreachable: %w", err)
	}
	return &PGCHStore{pool: pool, chURL: chURL, client: &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{Proxy: nil}}}, nil
}

func (s *PGCHStore) Close() { s.pool.Close() }

func (s *PGCHStore) RawIndexes(ctx context.Context, tenant string, ids []string) (map[string]RawRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT message_id,payload_hash,published_at>0,parse_error FROM raw_archive_index WHERE tenant_id=$1 AND message_id=ANY($2)`, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]RawRecord{}
	for rows.Next() {
		var id string
		var r RawRecord
		if err = rows.Scan(&id, &r.PayloadHash, &r.Published, &r.ParseError); err != nil {
			return nil, err
		}
		out[id] = r
	}
	return out, rows.Err()
}

// RawBodies counts readable raw payload copies in PostgreSQL and ClickHouse;
// more than one copy is a physical duplicate, not a business duplicate.
func (s *PGCHStore) RawBodies(ctx context.Context, tenant string, ids []string) (map[string]int, error) {
	out := map[string]int{}
	rows, err := s.pool.Query(ctx, `SELECT message_id FROM raw_message_log WHERE tenant_id=$1 AND message_id=ANY($2)`, tenant, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		out[id]++
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if s.chURL != "" {
		counts, err := s.chCounts(ctx, "iot_raw_message", tenant, ids)
		if err != nil {
			return nil, err
		}
		for id, n := range counts {
			out[id] += n
		}
	}
	return out, nil
}

func (s *PGCHStore) Standards(ctx context.Context, tenant string, rawIDs []string) (map[string][]StandardRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT raw_message_id,message_id,message_type,properties<>'{}'::jsonb,processed_at FROM standard_message WHERE tenant_id=$1 AND raw_message_id=ANY($2)`, tenant, rawIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]StandardRecord{}
	for rows.Next() {
		var raw string
		var r StandardRecord
		if err = rows.Scan(&raw, &r.MessageID, &r.Type, &r.HasProperties, &r.ProcessedAt); err != nil {
			return nil, err
		}
		out[raw] = append(out[raw], r)
	}
	return out, rows.Err()
}

func (s *PGCHStore) TelemetryConfigured() bool { return s.chURL != "" }

func (s *PGCHStore) Telemetry(ctx context.Context, tenant string, ids []string) (map[string]int, error) {
	return s.chCounts(ctx, "iot_telemetry", tenant, ids)
}

func (s *PGCHStore) ClockOffset(ctx context.Context) (time.Duration, time.Duration, error) {
	best, bestUnc := time.Duration(0), time.Duration(math.MaxInt64)
	for i := 0; i < 3; i++ {
		t0 := time.Now()
		var dbMS int64
		if err := s.pool.QueryRow(ctx, `SELECT (extract(epoch FROM clock_timestamp())*1000)::bigint`).Scan(&dbMS); err != nil {
			return 0, 0, err
		}
		t1 := time.Now()
		mid := t0.Add(t1.Sub(t0) / 2)
		if unc := t1.Sub(t0)/2 + time.Millisecond; unc < bestUnc {
			best, bestUnc = time.UnixMilli(dbMS).Sub(mid), unc
		}
	}
	return best, bestUnc, nil
}

// chCounts uses ClickHouse query parameters so IDs are never spliced into SQL.
func (s *PGCHStore) chCounts(ctx context.Context, table, tenant string, ids []string) (map[string]int, error) {
	if table != "iot_raw_message" && table != "iot_telemetry" {
		return nil, errors.New("table not allowed")
	}
	out := map[string]int{}
	for i := 0; i < len(ids); i += 500 {
		chunk := ids[i:min(i+500, len(ids))]
		quoted := make([]string, len(chunk))
		for j, id := range chunk {
			quoted[j] = "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(id) + "'"
		}
		u, err := url.Parse(s.chURL)
		if err != nil {
			return nil, errors.New("observer ClickHouse URL is invalid")
		}
		user := u.User
		u.User = nil
		q := u.Query()
		q.Set("query", "SELECT message_id, count() AS n FROM "+table+" WHERE tenant_id={tenant:String} AND message_id IN {ids:Array(String)} GROUP BY message_id FORMAT JSONEachRow")
		q.Set("param_tenant", tenant)
		q.Set("param_ids", "["+strings.Join(quoted, ",")+"]")
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
		if err != nil {
			return nil, err
		}
		if user != nil {
			pw, _ := user.Password()
			req.SetBasicAuth(user.Username(), pw)
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			return nil, fmt.Errorf("clickhouse %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		}
		dec := json.NewDecoder(resp.Body)
		for {
			var row struct {
				ID string `json:"message_id"`
				N  json.Number
			}
			if err = dec.Decode(&row); err != nil {
				break
			}
			n, _ := row.N.Int64()
			out[row.ID] += int(n)
		}
		resp.Body.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}
	return out, nil
}
