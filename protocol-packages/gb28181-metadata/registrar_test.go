package gbmetadata

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

const testDevice = "34020000001320000001"
const testChannel = "34020000001310000001"
const testPassword = "gb28181-test-password"

func TestSIPRegistrationCatalogAndKeepalive(t *testing.T) {
	for _, network := range []string{"udp", "tcp"} {
		t.Run(network, func(t *testing.T) {
			free, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := free.Addr().String()
			free.Close()
			r, err := New(Config{ServerID: "34020000002000000001", Realm: "3402000000", Listen: address, AllowedCIDRs: []string{"127.0.0.0/8"}, Devices: map[string]string{testDevice: testPassword}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.Serve(ctx) }()
			defer func() { cancel(); <-done }()
			// TCP readiness also proves the UDP socket was opened first.
			for {
				c, e := net.DialTimeout("tcp", address, 50*time.Millisecond)
				if e == nil {
					c.Close()
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("SIP listener not ready")
				case <-time.After(10 * time.Millisecond):
				}
			}
			conn, err := net.Dial(network, address)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			reader := bufio.NewReader(conn)
			sequence := 1
			request := func(method, body, authorization string, expires int) []byte {
				sequence++
				uri := "sip:3402000000"
				header := ""
				if authorization != "" {
					header = "Authorization: " + authorization + "\r\n"
				}
				return []byte(fmt.Sprintf("%s %s SIP/2.0\r\nVia: SIP/2.0/%s %s;branch=z9hG4bK%s;rport\r\nFrom: <sip:%s@3402000000>;tag=test\r\nTo: <sip:%s@3402000000>\r\nCall-ID: fixture-%d\r\nCSeq: %d %s\r\nContact: <sip:%s@%s>\r\nExpires: %d\r\n%sContent-Type: Application/MANSCDP+xml\r\nContent-Length: %d\r\n\r\n%s", method, uri, strings.ToUpper(network), conn.LocalAddr(), randomToken(), testDevice, testDevice, sequence, sequence, method, testDevice, conn.LocalAddr(), expires, header, len([]byte(body)), body))
			}
			read := func() sip.Message {
				t.Helper()
				conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				var data []byte
				var err error
				if network == "tcp" {
					data, err = readSIPFrame(reader)
				} else {
					buf := make([]byte, 64<<10)
					var n int
					n, err = conn.Read(buf)
					data = buf[:n]
				}
				if err != nil {
					t.Fatal(err)
				}
				m, err := sip.ParseMessage(data)
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			status := func(expected int) *sip.Response {
				t.Helper()
				m := read()
				response, ok := m.(*sip.Response)
				if !ok || response.StatusCode != expected {
					t.Fatalf("expected %d got %s", expected, m.String())
				}
				return response
			}
			challenge := func() *digest.Challenge {
				conn.Write(request("REGISTER", "", "", 3600))
				response := status(401)
				challenge, err := digest.ParseChallenge(response.GetHeader("WWW-Authenticate").Value())
				if err != nil {
					t.Fatal(err)
				}
				return challenge
			}
			authorization := func(c *digest.Challenge, password string) string {
				md := func(value string) string { sum := md5.Sum([]byte(value)); return hex.EncodeToString(sum[:]) }
				response := md(md(testDevice+":"+c.Realm+":"+password) + ":" + c.Nonce + ":00000001:fixture-client:auth:" + md("REGISTER:sip:3402000000"))
				return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="sip:3402000000", response="%s", algorithm=MD5, qop=auth, nc=00000001, cnonce="fixture-client"`, testDevice, c.Realm, c.Nonce, response)
			}
			bad := authorization(challenge(), "wrong-password")
			conn.Write(request("REGISTER", "", bad, 3600))
			status(403)
			if len(r.Snapshot()) != 0 {
				t.Fatal("bad password registered device")
			}
			good := authorization(challenge(), testPassword)
			registered := request("REGISTER", "", good, 3600)
			conn.Write(registered)
			status(200)
			conn.Write(registered)
			status(200) // exact UDP/TCP retransmission is idempotent
			conn.Write(request("REGISTER", "", good, 3600))
			status(403) // a new transaction cannot replay a used nonce
			if snapshot := r.Snapshot(); len(snapshot) != 1 || !snapshot[0].Registered {
				t.Fatal("actual registration missing", snapshot)
			}
			keepalive := fmt.Sprintf(`<Notify><CmdType>Keepalive</CmdType><SN>1</SN><DeviceID>%s</DeviceID><Status>OK</Status></Notify>`, testDevice)
			conn.Write(request("MESSAGE", keepalive, "", 3600))
			status(200)
			queriesHandled := make(map[int]bool)
			acknowledgments := 0
			for len(queriesHandled) < 2 || acknowledgments < 2 {
				switch message := read().(type) {
				case *sip.Request:
					var metadata Message
					if err := xml.Unmarshal(message.Body(), &metadata); err != nil {
						t.Fatal(err)
					}
					conn.Write([]byte(sip.NewResponseFromRequest(message, 200, "OK", nil).String()))
					if queriesHandled[metadata.SN] {
						continue
					}
					var body string
					switch metadata.CmdType {
					case "Catalog":
						body = fmt.Sprintf(`<Response><CmdType>Catalog</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><SumNum>1</SumNum><DeviceList Num="1"><Item><DeviceID>%s</DeviceID><Name>Test camera</Name><Manufacturer>Fixture</Manufacturer><Status>ON</Status></Item></DeviceList></Response>`, metadata.SN, testDevice, testChannel)
					case "DeviceInfo":
						body = fmt.Sprintf(`<Response><CmdType>DeviceInfo</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>OK</Result><DeviceName>Test recorder</DeviceName><Manufacturer>Fixture</Manufacturer><Model>DVR</Model><Firmware>1</Firmware></Response>`, metadata.SN, testDevice)
					default:
						t.Fatalf("unexpected query type %q", metadata.CmdType)
					}
					queriesHandled[metadata.SN] = true
					conn.Write(request("MESSAGE", body, "", 3600))
				case *sip.Response:
					if message.StatusCode != 200 {
						t.Fatalf("unexpected metadata response %d", message.StatusCode)
					}
					acknowledgments++
				default:
					t.Fatal("unexpected SIP message")
				}
			}
			snapshot := r.Snapshot()
			if len(snapshot) != 1 || snapshot[0].CatalogAt == 0 || len(snapshot[0].Channels) != 1 || snapshot[0].Channels[0].DeviceID != testChannel || snapshot[0].Model != "DVR" {
				t.Fatalf("catalog: %+v", snapshot)
			}
			if platform := os.Getenv("IOT_TEST_GB_PLATFORM_URL"); platform != "" {
				u := Uploader{URL: platform, TenantID: "t", NodeID: "video-node", Secret: os.Getenv("IOT_TEST_GB_NODE_SECRET"), DataDir: t.TempDir(), AllowHTTP: true}
				if err := u.Init(ctx); err != nil {
					t.Fatal(err)
				}
				if err := u.Upload(ctx, snapshot); err != nil {
					t.Fatal(err)
				}
			}
			good = authorization(challenge(), testPassword)
			conn.Write(request("REGISTER", "", good, 0))
			status(200)
			conn.Write(request("MESSAGE", keepalive, "", 3600))
			status(403)
		})
	}
}

func FuzzSIPFrame(f *testing.F) {
	f.Add("MESSAGE sip:3402000000 SIP/2.0\r\nContent-Length: 0\r\n\r\n")
	f.Fuzz(func(t *testing.T, input string) { _, _ = readSIPFrame(bufio.NewReader(strings.NewReader(input))) })
}
func FuzzCatalogXML(f *testing.F) {
	f.Add(`<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000001320000001</DeviceID><SumNum>0</SumNum></Response>`)
	f.Fuzz(func(t *testing.T, input string) { _, _ = parseXML([]byte(input)) })
}

func TestCatalogCorrelationCompletenessAndTimeout(t *testing.T) {
	r, err := New(Config{ServerID: "34020000002000000001", Realm: "3402000000", Listen: "127.0.0.1:5060", AllowedCIDRs: []string{"127.0.0.0/8"}, Devices: map[string]string{testDevice: testPassword}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s := &session{peer: "127.0.0.1:7000", network: "udp", expires: now.Add(time.Hour), lastQuery: now, send: func([]byte) error { return nil }, catalog: DeviceCatalog{DeviceID: testDevice, Registered: true, LastSeenAt: now.UnixMilli(), CatalogAt: now.Add(-time.Minute).UnixMilli(), Channels: []Channel{{DeviceID: testChannel, Name: "Last complete"}}}}
	r.sessions[testDevice] = s
	r.query(s, "Catalog", now)
	q := r.queries[1]
	message, err := sip.ParseMessage(q.wire)
	if err != nil {
		t.Fatal(err)
	}
	response := sip.NewResponseFromRequest(message.(*sip.Request), 200, "OK", nil)
	response.CSeq().SeqNo++
	r.response(response, s.peer, "udp")
	if q.acked {
		t.Fatal("wrong sequence acknowledged query")
	}
	response.CSeq().SeqNo--
	r.response(response, s.peer, "tcp")
	if q.acked {
		t.Fatal("wrong transport acknowledged query")
	}
	r.response(response, "127.0.0.1:7001", "udp")
	if q.acked {
		t.Fatal("wrong peer acknowledged query")
	}
	r.response(response, s.peer, "udp")
	if !q.acked || len(s.catalog.Channels) != 1 || s.catalog.Channels[0].Name != "Last complete" {
		t.Fatal("SIP acknowledgment replaced metadata")
	}
	body := fmt.Sprintf(`<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>%s</DeviceID><SumNum>2</SumNum><DeviceList Num="1"><Item><DeviceID>%s</DeviceID><Name>New first</Name></Item></DeviceList></Response>`, testDevice, testChannel)
	partial, err := parseXML([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.metadata(testDevice, partial); err != nil {
		t.Fatal(err)
	}
	if s.catalog.Channels[0].Name != "Last complete" {
		t.Fatal("partial catalog overwrote complete catalog")
	}
	if err := r.metadata(testDevice, partial); err != nil {
		t.Fatal("duplicate partial rejected", err)
	}
	conflict := partial
	conflict.SumNum = 3
	if r.metadata(testDevice, conflict) == nil {
		t.Fatal("conflicting total accepted")
	}
	r.Tick(now.Add(11 * time.Second))
	if s.catalog.LastError == "" || s.catalog.Channels[0].Name != "Last complete" {
		t.Fatal("timeout lost last complete catalog or error")
	}
	if r.metadata(testDevice, partial) == nil {
		t.Fatal("late metadata accepted after timeout")
	}
}
