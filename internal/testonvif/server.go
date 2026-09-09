// Package testonvif provides a TLS camera simulator that verifies credentials.
package testonvif

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"
)

const Password = "onvif-fixture-password"

type Camera struct {
	Server *httptest.Server
	CAFile string
}

func Start(t *testing.T, digest bool, fault bool) Camera {
	t.Helper()
	var mu sync.Mutex
	used := map[string]bool{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/onvif/device_service" {
			w.WriteHeader(404)
			return
		}
		if digest {
			fields := map[string]string{}
			for _, pair := range regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([^, ]+))`).FindAllStringSubmatch(r.Header.Get("Authorization"), -1) {
				if pair[2] != "" {
					fields[pair[1]] = pair[2]
				} else {
					fields[pair[1]] = pair[3]
				}
			}
			md := func(s string) string { b := md5.Sum([]byte(s)); return hex.EncodeToString(b[:]) }
			want := md(md("operator:fixture:"+Password) + ":fixture-nonce:" + fields["nc"] + ":" + fields["cnonce"] + ":auth:" + md(r.Method+":"+r.URL.RequestURI()))
			if fields["username"] != "operator" || fields["realm"] != "fixture" || fields["nonce"] != "fixture-nonce" || fields["uri"] != r.URL.RequestURI() || fields["qop"] != "auth" || fields["nc"] != "00000001" || fields["cnonce"] == "" || subtle.ConstantTimeCompare([]byte(want), []byte(fields["response"])) != 1 {
				w.Header().Set("WWW-Authenticate", `Digest realm="fixture", nonce="fixture-nonce", qop="auth", algorithm=MD5`)
				w.WriteHeader(401)
				return
			}
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		if err != nil || len(data) > 8192 {
			w.WriteHeader(400)
			return
		}
		var request struct {
			Header struct {
				Security struct {
					Token struct{ Username, Password, Nonce, Created string } `xml:"UsernameToken"`
				} `xml:"Security"`
			} `xml:"Header"`
		}
		if xml.Unmarshal(data, &request) != nil {
			w.WriteHeader(400)
			return
		}
		token := request.Header.Security.Token
		nonce, nonceErr := base64.StdEncoding.DecodeString(token.Nonce)
		created, timeErr := time.Parse(time.RFC3339, token.Created)
		hash := sha1.New()
		hash.Write(nonce)
		hash.Write([]byte(token.Created))
		hash.Write([]byte(Password))
		want := base64.StdEncoding.EncodeToString(hash.Sum(nil))
		mu.Lock()
		replay := used[token.Nonce]
		used[token.Nonce] = true
		mu.Unlock()
		if nonceErr != nil || timeErr != nil || len(nonce) < 16 || time.Since(created).Abs() > time.Minute || token.Username != "operator" || replay || subtle.ConstantTimeCompare([]byte(want), []byte(token.Password)) != 1 {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/soap+xml")
		body := `<tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Manufacturer>Fixture</tds:Manufacturer><tds:Model>TLS Camera</tds:Model><tds:FirmwareVersion>1</tds:FirmwareVersion><tds:SerialNumber>CAM-42</tds:SerialNumber><tds:HardwareId>42</tds:HardwareId></tds:GetDeviceInformationResponse>`
		if fault {
			body = `<s:Fault><s:Reason><s:Text>NotAuthorized</s:Text></s:Reason></s:Fault>`
		}
		fmt.Fprintf(w, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>%s</s:Body></s:Envelope>`, body)
	})
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	ca := filepath.Join(t.TempDir(), "camera-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	return Camera{Server: s, CAFile: ca}
}
