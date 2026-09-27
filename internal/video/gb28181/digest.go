package gb28181

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"time"
)

// nonceIssuer creates stateless, self-verifying nonces: a timestamp and its
// HMAC. The server keeps no per-device challenge state, and a nonce older
// than the allowed age is rejected.
type nonceIssuer struct {
	key []byte
	now func() time.Time
}

func (n nonceIssuer) issue() string {
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(n.now().Unix()))
	return hex.EncodeToString(ts) + n.mac(ts)
}

func (n nonceIssuer) mac(ts []byte) string {
	m := hmac.New(sha256.New, n.key)
	m.Write([]byte("gb28181-nonce\x00"))
	m.Write(ts)
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func (n nonceIssuer) valid(nonce string, maxAge time.Duration) bool {
	if len(nonce) != 48 {
		return false
	}
	ts, err := hex.DecodeString(nonce[:16])
	if err != nil || subtle.ConstantTimeCompare([]byte(nonce[16:]), []byte(n.mac(ts))) != 1 {
		return false
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(ts)), 0)
	age := n.now().Sub(issued)
	return age >= -time.Minute && age <= maxAge
}

// parseDigest parses an Authorization header value: Digest k="v", k=v, ...
func parseDigest(value string) map[string]string {
	out := map[string]string{}
	rest, ok := strings.CutPrefix(strings.TrimSpace(value), "Digest")
	if !ok {
		return out
	}
	for rest = strings.TrimSpace(rest); rest != ""; {
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(rest[:eq]))
		rest = strings.TrimSpace(rest[eq+1:])
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				break
			}
			val, rest = rest[1:end+1], rest[end+2:]
		} else {
			end := strings.IndexByte(rest, ',')
			if end < 0 {
				end = len(rest)
			}
			val, rest = strings.TrimSpace(rest[:end]), rest[end:]
		}
		out[key] = val
		rest = strings.TrimLeft(strings.TrimSpace(rest), ",")
		rest = strings.TrimSpace(rest)
	}
	return out
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// verifyDigest checks an MD5 digest response (RFC 2617), with or without
// qop=auth. MD5 is what GB/T 28181 devices implement; the nonce is bound to
// this server and time-limited.
func verifyDigest(p map[string]string, method, password string) bool {
	if alg := strings.ToUpper(p["algorithm"]); alg != "" && alg != "MD5" {
		return false
	}
	ha1 := md5hex(p["username"] + ":" + p["realm"] + ":" + password)
	ha2 := md5hex(method + ":" + p["uri"])
	var expected string
	switch p["qop"] {
	case "":
		expected = md5hex(ha1 + ":" + p["nonce"] + ":" + ha2)
	case "auth":
		expected = md5hex(ha1 + ":" + p["nonce"] + ":" + p["nc"] + ":" + p["cnonce"] + ":auth:" + ha2)
	default:
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(p["response"])), []byte(expected)) == 1
}
