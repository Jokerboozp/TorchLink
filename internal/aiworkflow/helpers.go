package aiworkflow

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

func id(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func truncateRunes(text string, limit int) string {
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}
