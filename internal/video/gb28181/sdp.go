package gb28181

import (
	"fmt"
	"strconv"
	"strings"
)

// Offer describes where the device should send a live stream.
type Offer struct {
	ServerID string
	MediaIP  string
	Port     int
	SSRC     string
	// TCP asks the device to connect to the media port (TCP passive on the
	// receiving side); otherwise RTP is sent over UDP.
	TCP bool
}

// SDP renders the offer. Payload types follow GB/T 28181: 96 PS, 97 MPEG-4,
// 98 H.264. The y= line carries the SSRC used to match the incoming stream.
func (o Offer) SDP() []byte {
	proto := "RTP/AVP"
	if o.TCP {
		proto = "TCP/RTP/AVP"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "v=0\r\no=%s 0 0 IN IP4 %s\r\ns=Play\r\nc=IN IP4 %s\r\nt=0 0\r\n", o.ServerID, o.MediaIP, o.MediaIP)
	fmt.Fprintf(&b, "m=video %d %s 96 97 98\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\na=rtpmap:97 MPEG4/90000\r\na=rtpmap:98 H264/90000\r\n", o.Port, proto)
	if o.TCP {
		b.WriteString("a=setup:passive\r\na=connection:new\r\n")
	}
	fmt.Fprintf(&b, "y=%s\r\n", o.SSRC)
	return []byte(b.String())
}

// Answer is the part of the device's SDP answer the platform uses.
type Answer struct {
	IP    string
	Port  int
	SSRC  string
	Proto string
}

// ParseAnswer reads the device's SDP answer leniently: devices differ in line
// endings and optional attributes, and only the SSRC is acted upon.
func ParseAnswer(body []byte) Answer {
	var a Answer
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "c="):
			if f := strings.Fields(line[2:]); len(f) == 3 {
				a.IP = f[2]
			}
		case strings.HasPrefix(line, "m=video "):
			if f := strings.Fields(line[2:]); len(f) >= 3 {
				a.Port, _ = strconv.Atoi(f[1])
				a.Proto = f[2]
			}
		case strings.HasPrefix(line, "y="):
			a.SSRC = strings.TrimSpace(line[2:])
		}
	}
	return a
}

// ValidSSRC reports whether s is a decimal SSRC that fits in 32 bits.
func ValidSSRC(s string) bool {
	if s == "" || len(s) > 10 {
		return false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return err == nil && v <= 0xFFFFFFFF
}
