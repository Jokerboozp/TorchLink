package gb28181

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestDigestVerification(t *testing.T) {
	n := nonceIssuer{key: []byte("0123456789abcdef0123"), now: time.Now}
	nonce := n.issue()
	if !n.valid(nonce, time.Minute) {
		t.Fatal("fresh nonce must be valid")
	}
	// Change the last character to a different one; a fixed replacement
	// would equal the original one time in sixteen.
	last := "0"
	if strings.HasSuffix(nonce, "0") {
		last = "1"
	}
	if n.valid(nonce[:len(nonce)-1]+last, time.Minute) || n.valid("0000000000000000"+nonce[16:], time.Minute) {
		t.Fatal("tampered nonce must be rejected")
	}
	later := nonceIssuer{key: n.key, now: func() time.Time { return time.Now().Add(10 * time.Minute) }}
	if later.valid(nonce, 5*time.Minute) {
		t.Fatal("expired nonce must be rejected")
	}
	other := nonceIssuer{key: []byte("another-key-0123456789"), now: time.Now}
	if other.valid(nonce, time.Minute) {
		t.Fatal("nonce of another key must be rejected")
	}

	user, realm, uri, password := "34020000001320000001", "3402000000", "sip:34020000002000000001@3402000000", "p@ss"
	ha1 := md5hex(user + ":" + realm + ":" + password)
	ha2 := md5hex("REGISTER:" + uri)
	plain := `Digest username="` + user + `", realm="` + realm + `", nonce="` + nonce + `", uri="` + uri + `", response="` + md5hex(ha1+":"+nonce+":"+ha2) + `", algorithm=MD5`
	if p := parseDigest(plain); !verifyDigest(p, "REGISTER", password) || verifyDigest(p, "REGISTER", "wrong") {
		t.Fatalf("digest without qop: %v", p)
	}
	withQop := `Digest username="` + user + `",realm="` + realm + `",nonce="` + nonce + `",uri="` + uri + `",qop=auth,nc=00000001,cnonce="abc",response="` + md5hex(ha1+":"+nonce+":00000001:abc:auth:"+ha2) + `"`
	if !verifyDigest(parseDigest(withQop), "REGISTER", password) {
		t.Fatal("digest with qop=auth must verify")
	}
	if verifyDigest(parseDigest(strings.Replace(plain, "algorithm=MD5", "algorithm=SHA-256", 1)), "REGISTER", password) {
		t.Fatal("unsupported algorithms must be rejected")
	}
}

func TestParseGBKCatalogAndSDP(t *testing.T) {
	name, _ := simplifiedchinese.GBK.NewEncoder().String("一号楼 & 大厅")
	body := "<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n<Response><CmdType>Catalog</CmdType><SN>7</SN><DeviceID>34020000001320000001</DeviceID><SumNum>2</SumNum><DeviceList Num=\"1\"><Item><DeviceID>34020000001310000001</DeviceID><Name>" + name + "</Name><Status>on</Status></Item></DeviceList></Response>"
	m, err := parseMessage([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if m.CmdType != "Catalog" || m.SN != 7 || m.SumNum != 2 || len(m.DeviceList.Items) != 1 {
		t.Fatalf("parsed %+v", m)
	}
	if item := m.DeviceList.Items[0]; item.Name != "一号楼 & 大厅" || item.Status != "ON" {
		t.Fatalf("GBK name and bare ampersand must decode: %+v", item)
	}
	utf8Body := "<?xml version=\"1.0\" encoding=\"GB2312\"?><Notify><CmdType>Keepalive</CmdType><SN>1</SN><DeviceID>34020000001320000001</DeviceID><Status>OK</Status></Notify>"
	if m, err := parseMessage([]byte(utf8Body)); err != nil || m.CmdType != "Keepalive" || m.XMLName.Local != "Notify" {
		t.Fatalf("keepalive: %+v %v", m, err)
	}

	offer := Offer{ServerID: "34020000002000000001", MediaIP: "10.0.0.50", Port: 30002, SSRC: "0200000001", TCP: true}
	sdp := string(offer.SDP())
	for _, want := range []string{"c=IN IP4 10.0.0.50", "m=video 30002 TCP/RTP/AVP 96 97 98", "a=setup:passive", "a=rtpmap:96 PS/90000", "y=0200000001", "s=Play"} {
		if !strings.Contains(sdp, want) {
			t.Fatalf("offer lacks %q:\n%s", want, sdp)
		}
	}
	a := ParseAnswer([]byte("v=0\no=x 0 0 IN IP4 10.0.0.9\nc=IN IP4 10.0.0.9\nm=video 15060 RTP/AVP 96\ny=0100000002\n"))
	if a.IP != "10.0.0.9" || a.Port != 15060 || a.SSRC != "0100000002" || a.Proto != "RTP/AVP" {
		t.Fatalf("answer %+v", a)
	}
	if !ValidSSRC("0100000002") || ValidSSRC("9999999999") || ValidSSRC("abc") {
		t.Fatal("SSRC validation")
	}
}
