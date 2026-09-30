package analytics

import (
	"bytes"
	"encoding/json"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

const (
	legacyJSONBDigestAmbiguities = 6
	legacyJSONBDigestInputBytes  = 8 << 20
	legacyJSONBDigestTotalBytes  = 64 << 20
	legacyJSONBNumberLimit       = 4096
)

// AnalysisLegacyJSONBDigestMatches verifies an existing digest without changing
// it. PostgreSQL JSONB may expand a Go float's exponent spelling. Each eligible
// number therefore has two possible legacy spellings, and mixed typed/RawMessage
// bodies require trying their combinations. A replacement must have exactly the
// same decimal rational value, and only a matching original SHA is accepted.
//
// A direct original-SHA match is checked first. The numeric compatibility
// fallback is limited to 8 MiB of input, six ambiguous numbers (64 combinations),
// and 64 MiB of candidate JSON. An error or an unverifiable digest fails closed.
// A callback hashing a larger enclosing object must also bound fallback work.
func AnalysisLegacyJSONBDigestMatches(expected string, v any, digest func(json.RawMessage) (string, error)) bool {
	if expected == "" || digest == nil {
		return false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return false
	}
	hash, err := digest(raw)
	if err != nil {
		return false
	}
	if hash == expected {
		return true
	}
	if len(raw) > legacyJSONBDigestInputBytes {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var tree any
	if err = decoder.Decode(&tree); err != nil {
		return false
	}
	type alternative struct {
		set       func(any)
		original  json.Number
		candidate json.Number
	}
	choices := []alternative{}
	var walk func(any, func(any)) bool
	walk = func(value any, set func(any)) bool {
		switch x := value.(type) {
		case json.Number:
			candidate, ok := legacyJSONBNumberAlternative(x)
			if !ok {
				return true
			}
			choices = append(choices, alternative{set, x, candidate})
			return len(choices) <= legacyJSONBDigestAmbiguities
		case []any:
			for i, child := range x {
				if !walk(child, func(v any) { x[i] = v }) {
					return false
				}
			}
		case map[string]any:
			for _, key := range slices.Sorted(maps.Keys(x)) {
				if !walk(x[key], func(v any) { x[key] = v }) {
					return false
				}
			}
		}
		return true
	}
	if !walk(tree, func(v any) { tree = v }) || len(choices) == 0 {
		return false
	}
	processed := len(raw)
	for mask := 1; mask < 1<<len(choices); mask++ {
		for i, choice := range choices {
			if mask&(1<<i) == 0 {
				choice.set(choice.original)
			} else {
				choice.set(choice.candidate)
			}
		}
		candidate, err := json.Marshal(tree)
		if err != nil || len(candidate) > legacyJSONBDigestTotalBytes-processed {
			return false
		}
		processed += len(candidate)
		hash, err := digest(candidate)
		if err != nil {
			return false
		}
		if hash == expected {
			return true
		}
	}
	return false
}

func legacyJSONBNumberAlternative(number json.Number) (json.Number, bool) {
	text := string(number)
	if len(text) > legacyJSONBNumberLimit {
		return "", false
	}
	if at := strings.IndexAny(text, "eE"); at >= 0 {
		exponent, err := strconv.ParseInt(text[at+1:], 10, 64)
		if err != nil || exponent < -legacyJSONBNumberLimit || exponent > legacyJSONBNumberLimit {
			return "", false
		}
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return "", false
	}
	candidate, err := json.Marshal(f)
	if err != nil || string(candidate) == text {
		return "", false
	}
	before, ok := new(big.Rat).SetString(text)
	if !ok {
		return "", false
	}
	after, ok := new(big.Rat).SetString(string(candidate))
	if !ok || before.Cmp(after) != 0 {
		return "", false
	}
	return json.Number(candidate), true
}

func AnalysisHashMatchesJSONB(expected string, v any) bool {
	return AnalysisLegacyJSONBDigestMatches(expected, v, func(raw json.RawMessage) (string, error) {
		return AnalysisHash(raw)
	})
}
