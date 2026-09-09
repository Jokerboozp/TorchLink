package fieldprotocol

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// digestAuthorization implements the auth quality-of-protection from RFC 7616.
// MD5 is retained only for cameras implementing the older ONVIF HTTP profile.
func digestAuthorization(challenge, username, password, method, uri string) (string, error) {
	if len(challenge) > 8192 || !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return "", errors.New("unsupported ONVIF authentication challenge")
	}
	fields := map[string]string{}
	s := strings.TrimSpace(challenge[7:])
	for s != "" {
		key, rest, ok := strings.Cut(s, "=")
		if !ok {
			return "", errors.New("invalid digest challenge")
		}
		key, rest = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(rest)
		var value string
		if strings.HasPrefix(rest, "\"") {
			end := 1
			for end < len(rest) {
				if rest[end] == '\\' {
					end += 2
					continue
				}
				if rest[end] == '"' {
					break
				}
				end++
			}
			if end >= len(rest) {
				return "", errors.New("unterminated digest challenge")
			}
			var err error
			value, err = strconv.Unquote(rest[:end+1])
			if err != nil {
				return "", err
			}
			s = strings.TrimSpace(rest[end+1:])
			if s != "" {
				if s[0] != ',' {
					return "", errors.New("invalid digest delimiter")
				}
				s = strings.TrimSpace(s[1:])
			}
		} else {
			value, s, _ = strings.Cut(rest, ",")
			value, s = strings.TrimSpace(value), strings.TrimSpace(s)
		}
		if key == "" {
			return "", errors.New("empty digest field")
		}
		if _, exists := fields[key]; exists {
			return "", errors.New("duplicate digest field")
		}
		fields[key] = value
	}
	if fields["realm"] == "" || fields["nonce"] == "" {
		return "", errors.New("missing digest realm or nonce")
	}
	algorithm := strings.ToUpper(fields["algorithm"])
	if algorithm == "" {
		algorithm = "MD5"
	}
	hash := func(s string) string { v := md5.Sum([]byte(s)); return hex.EncodeToString(v[:]) }
	if algorithm == "SHA-256" || algorithm == "SHA-256-SESS" {
		hash = func(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
	} else if algorithm != "MD5" && algorithm != "MD5-SESS" {
		return "", errors.New("unsupported digest algorithm")
	}
	qop := ""
	if fields["qop"] != "" {
		for _, item := range strings.Split(fields["qop"], ",") {
			if strings.TrimSpace(item) == "auth" {
				qop = "auth"
			}
		}
		if qop == "" {
			return "", errors.New("unsupported digest qop")
		}
	}
	var nonce [20]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	cnonce := hex.EncodeToString(nonce[:])
	ha1 := hash(username + ":" + fields["realm"] + ":" + password)
	if strings.HasSuffix(algorithm, "-SESS") {
		ha1 = hash(ha1 + ":" + fields["nonce"] + ":" + cnonce)
	}
	input := ha1 + ":" + fields["nonce"] + ":"
	if qop != "" {
		input += "00000001:" + cnonce + ":auth:"
	}
	response := hash(input + hash(method+":"+uri))
	quote := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	for _, value := range []string{username, fields["realm"], fields["nonce"], fields["opaque"], uri} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return "", errors.New("invalid digest value")
		}
	}
	result := fmt.Sprintf("Digest username=%s, realm=%s, nonce=%s, uri=%s, response=%s, algorithm=%s", quote(username), quote(fields["realm"]), quote(fields["nonce"]), quote(uri), quote(response), algorithm)
	if qop != "" {
		result += ", qop=auth, nc=00000001, cnonce=" + quote(cnonce)
	} else if strings.HasSuffix(algorithm, "-SESS") {
		result += ", cnonce=" + quote(cnonce)
	}
	if fields["opaque"] != "" {
		result += ", opaque=" + quote(fields["opaque"])
	}
	return result, nil
}
