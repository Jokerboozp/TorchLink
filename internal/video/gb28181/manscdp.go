// Package gb28181 implements the signalling side of GB/T 28181 needed for
// live view: device registration with digest authentication, keepalive,
// catalog and device information queries, and INVITE/ACK/BYE for real-time
// streams. Media is received by the media server; this package never touches
// RTP.
package gb28181

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// CatalogItem is one entry of a Catalog response. Only fields the platform
// shows or uses are kept.
type CatalogItem struct {
	DeviceID     string `xml:"DeviceID"`
	Name         string `xml:"Name"`
	Manufacturer string `xml:"Manufacturer"`
	Model        string `xml:"Model"`
	ParentID     string `xml:"ParentID"`
	Status       string `xml:"Status"`
}

// DeviceInfo is the DeviceInfo query response.
type DeviceInfo struct {
	DeviceName   string
	Manufacturer string
	Model        string
	Firmware     string
	Channels     int
}

// message is the union of MANSCDP bodies the server reads.
type message struct {
	XMLName      xml.Name
	CmdType      string `xml:"CmdType"`
	SN           int    `xml:"SN"`
	DeviceID     string `xml:"DeviceID"`
	Status       string `xml:"Status"`
	Result       string `xml:"Result"`
	SumNum       int    `xml:"SumNum"`
	DeviceName   string `xml:"DeviceName"`
	Manufacturer string `xml:"Manufacturer"`
	Model        string `xml:"Model"`
	Firmware     string `xml:"Firmware"`
	Channel      int    `xml:"Channel"`
	DeviceList   struct {
		Items []CatalogItem `xml:"Item"`
	} `xml:"DeviceList"`
}

var (
	xmlDecl     = regexp.MustCompile(`^\s*<\?xml[^>]*\?>`)
	bareAmp     = regexp.MustCompile(`&([^a-zA-Z#]|$)`)
	maxBodySize = 256 << 10
)

// decodeBody returns the body as UTF-8. Devices usually declare GB2312 and
// send GBK bytes, but some declare GB2312 and send UTF-8; valid multi-byte
// UTF-8 is almost never valid GBK text, so it is taken as UTF-8.
func decodeBody(body []byte) ([]byte, error) {
	if len(body) > maxBodySize {
		return nil, fmt.Errorf("MANSCDP body too large")
	}
	if utf8.Valid(body) {
		return body, nil
	}
	out, err := io.ReadAll(simplifiedchinese.GB18030.NewDecoder().Reader(bytes.NewReader(body)))
	if err != nil {
		return nil, fmt.Errorf("decode MANSCDP body: %w", err)
	}
	return out, nil
}

func parseMessage(body []byte) (message, error) {
	var m message
	text, err := decodeBody(body)
	if err != nil {
		return m, err
	}
	// The declaration names the original charset; the text is UTF-8 now.
	text = xmlDecl.ReplaceAll(text, nil)
	if err := xml.Unmarshal(text, &m); err != nil {
		// Some devices leave '&' unescaped in names; retry once escaped.
		if err2 := xml.Unmarshal(bareAmp.ReplaceAll(text, []byte("&amp;$1")), &m); err2 != nil {
			return m, fmt.Errorf("parse MANSCDP body: %w", err)
		}
	}
	m.CmdType = strings.TrimSpace(m.CmdType)
	m.DeviceID = strings.TrimSpace(m.DeviceID)
	for i := range m.DeviceList.Items {
		item := &m.DeviceList.Items[i]
		item.DeviceID, item.Name, item.Status = strings.TrimSpace(item.DeviceID), strings.TrimSpace(item.Name), strings.ToUpper(strings.TrimSpace(item.Status))
	}
	return m, nil
}

// queryBody builds a Query request. The body only contains ASCII, so the
// declared GB2312 charset (expected by many devices) is also valid UTF-8.
func queryBody(cmd string, sn int, deviceID string) []byte {
	return []byte(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n<Query>\r\n<CmdType>%s</CmdType>\r\n<SN>%d</SN>\r\n<DeviceID>%s</DeviceID>\r\n</Query>\r\n", cmd, sn, deviceID))
}
