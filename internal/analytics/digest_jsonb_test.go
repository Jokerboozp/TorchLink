package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestAnalysisLegacyJSONBExactDigest(t *testing.T) {
	for _, test := range []struct {
		name, original, stored string
		matches                bool
	}{
		{"ordinary", `{"n":3,"s":"1e-7"}`, `{"s":"1e-7","n":3}`, true},
		{"mixed-typed-and-raw", `{"typed":1e-7,"raw":{"n":0.0000001},"integer":9007199254740993}`, `{"typed":0.0000001,"raw":{"n":0.0000001},"integer":9007199254740993}`, true},
		{"nested-array", `{"v":[1e-7,{"a":0.0000001,"b":1e-9}]}`, `{"v":[0.0000001,{"a":0.0000001,"b":0.000000001}]}`, true},
		{"scalar", `1e-7`, `0.0000001`, true},
		{"changed-value", `{"n":1e-7}`, `{"n":0.0000002}`, false},
		{"changed-large-integer", `{"n":1e-7,"i":9007199254740993}`, `{"n":0.0000001,"i":9007199254740992}`, false},
		{"unrecoverable-raw-spelling", `{"i":9.007199254740993e15}`, `{"i":9007199254740993}`, false},
		{"strings-are-not-numbers", `{"n":1e-7,"s":"1e-7"}`, `{"n":0.0000001,"s":"0.0000001"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected, err := AnalysisHash(json.RawMessage(test.original))
			if err != nil {
				t.Fatal(err)
			}
			if got := AnalysisHashMatchesJSONB(expected, json.RawMessage(test.stored)); got != test.matches {
				t.Fatalf("matched=%t, want %t", got, test.matches)
			}
		})
	}
}

func TestAnalysisLegacyJSONBDigestEnclosingSnapshot(t *testing.T) {
	type manifest struct {
		Statistics json.RawMessage
		Facts      []json.RawMessage
	}
	facts := []json.RawMessage{json.RawMessage(`{"sourceValue":0.0000001}`)}
	original := json.RawMessage(`{"typed":1e-7,"source":{"value":0.0000001}}`)
	stored := json.RawMessage(`{"typed":0.0000001,"source":{"value":0.0000001}}`)
	expected, err := AnalysisHash(manifest{original, facts})
	if err != nil {
		t.Fatal(err)
	}
	if !AnalysisLegacyJSONBDigestMatches(expected, stored, func(candidate json.RawMessage) (string, error) {
		return AnalysisHash(manifest{candidate, facts})
	}) {
		t.Fatal("mixed statistics could not verify the exact old snapshot digest")
	}
	if AnalysisLegacyJSONBDigestMatches(expected, stored, func(candidate json.RawMessage) (string, error) {
		return AnalysisHash(manifest{candidate, []json.RawMessage{json.RawMessage(`{"sourceValue":0.0000002}`)}})
	}) {
		t.Fatal("changed facts matched a frozen snapshot digest")
	}
}

func TestAnalysisLegacyJSONBDigestWorkLimits(t *testing.T) {
	digest := func(raw json.RawMessage) (string, error) { return AnalysisHash(raw) }
	for _, count := range []int{6, 7} {
		original := "[" + strings.TrimSuffix(strings.Repeat("1e-7,", count), ",") + "]"
		stored := "[" + strings.TrimSuffix(strings.Repeat("0.0000001,", count), ",") + "]"
		expected, _ := AnalysisHash(json.RawMessage(original))
		calls := 0
		got := AnalysisLegacyJSONBDigestMatches(expected, json.RawMessage(stored), func(raw json.RawMessage) (string, error) {
			calls++
			return digest(raw)
		})
		if got != (count == 6) || calls > 64 || count == 7 && calls != 1 {
			t.Fatal("ambiguity limit", count, got, calls)
		}
	}
	// Ordinary bodies take the direct SHA path, even if many numbers would
	// otherwise be ambiguous. They must not be rejected for an unused fallback.
	body := json.RawMessage("[" + strings.TrimSuffix(strings.Repeat("0.0000001,", 100), ",") + "]")
	expected, _ := AnalysisHash(body)
	calls := 0
	if !AnalysisLegacyJSONBDigestMatches(expected, body, func(raw json.RawMessage) (string, error) {
		calls++
		return digest(raw)
	}) || calls != 1 {
		t.Fatal("ordinary path was not direct", calls)
	}
	tooLarge := map[string]any{"padding": strings.Repeat("x", legacyJSONBDigestInputBytes)}
	expected, _ = AnalysisHash(tooLarge)
	calls = 0
	if !AnalysisLegacyJSONBDigestMatches(expected, tooLarge, func(raw json.RawMessage) (string, error) {
		calls++
		return digest(raw)
	}) || calls != 1 {
		t.Fatal("large ordinary record did not verify its original SHA", calls)
	}
	calls = 0
	if AnalysisLegacyJSONBDigestMatches("unverifiable", tooLarge, func(raw json.RawMessage) (string, error) {
		calls++
		return digest(raw)
	}) || calls != 1 {
		t.Fatal("large mismatched record entered numeric compatibility fallback", calls)
	}
	pad := strings.Repeat("x", 2<<20)
	original := map[string]any{"pad": pad, "numbers": json.RawMessage(`[1e-7,1e-7,1e-7,1e-7,1e-7,1e-7]`)}
	stored := map[string]any{"pad": pad, "numbers": json.RawMessage(`[0.0000001,0.0000001,0.0000001,0.0000001,0.0000001,0.0000001]`)}
	expected, _ = AnalysisHash(original)
	calls = 0
	if AnalysisLegacyJSONBDigestMatches(expected, stored, func(raw json.RawMessage) (string, error) {
		calls++
		return digest(raw)
	}) || calls >= 64 {
		t.Fatal("cumulative byte limit did not fail closed", calls)
	}
	if AnalysisLegacyJSONBDigestMatches("unused", json.RawMessage(`{"n":0.0000001}`), func(json.RawMessage) (string, error) {
		return "", errors.New("enclosing digest work limit")
	}) {
		t.Fatal("digest callback failure was accepted")
	}
	for _, number := range []json.Number{"1e-1000000000", "0e+1000000000", "1e+1000000000", "9007199254740993"} {
		if _, ok := legacyJSONBNumberAlternative(number); ok {
			t.Fatal("unsafe or inexact numeric candidate", number)
		}
	}
}

func TestAnalysisLegacyJSONBPostgresReadOnly(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("connect to the configured test database", err)
	}
	defer conn.Close(context.Background())
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	original, err := json.Marshal(struct {
		Typed float64         `json:"typed"`
		Raw   json.RawMessage `json:"raw"`
	}{1e-7, json.RawMessage(`{"epsilon":0.0000001,"integer":9007199254740993}`)})
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := AnalysisHash(json.RawMessage(original))
	var stored string
	if err = tx.QueryRow(ctx, `SELECT $1::jsonb::text`, string(original)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	current, _ := AnalysisHash(json.RawMessage(stored))
	if expected == current || !AnalysisHashMatchesJSONB(expected, json.RawMessage(stored)) {
		t.Fatal("real JSONB numeric expansion did not preserve the verifiable legacy digest")
	}
	changed := strings.Replace(stored, "9007199254740993", "9007199254740992", 1)
	if AnalysisHashMatchesJSONB(expected, json.RawMessage(changed)) {
		t.Fatal("changed large integer matched the legacy digest")
	}
	t.Log("SELECT-only JSONB round trip: mixed numeric spelling verified; changed value rejected")
}
