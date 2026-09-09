// Package gbmetadata implements the registration and metadata subset of GB28181.
// It intentionally has no media, PTZ, or device configuration operations.
package gbmetadata

import (
	"bytes"
	"encoding/xml"
	"errors"
	"golang.org/x/text/encoding/simplifiedchinese"
	"io"
	"regexp"
	"strings"
)

var deviceIDPattern = regexp.MustCompile(`^[0-9]{20}$`)

type Channel struct {
	DeviceID     string `xml:"DeviceID" json:"deviceId"`
	Name         string `xml:"Name" json:"name"`
	Manufacturer string `xml:"Manufacturer" json:"manufacturer"`
	Model        string `xml:"Model" json:"model"`
	Owner        string `xml:"Owner" json:"owner,omitempty"`
	CivilCode    string `xml:"CivilCode" json:"civilCode,omitempty"`
	Address      string `xml:"Address" json:"address,omitempty"`
	Parental     int    `xml:"Parental" json:"parental"`
	ParentID     string `xml:"ParentID" json:"parentId,omitempty"`
	Status       string `xml:"Status" json:"status"`
}
type Message struct {
	XMLName      xml.Name
	CmdType      string `xml:"CmdType"`
	SN           int    `xml:"SN"`
	DeviceID     string `xml:"DeviceID"`
	Status       string `xml:"Status"`
	Result       string `xml:"Result"`
	DeviceName   string `xml:"DeviceName"`
	Manufacturer string `xml:"Manufacturer"`
	Model        string `xml:"Model"`
	Firmware     string `xml:"Firmware"`
	SumNum       int    `xml:"SumNum"`
	DeviceList   struct {
		Num   int       `xml:"Num,attr"`
		Items []Channel `xml:"Item"`
	} `xml:"DeviceList"`
}
type DeviceCatalog struct {
	DeviceID     string    `json:"deviceId"`
	Name         string    `json:"name"`
	Manufacturer string    `json:"manufacturer"`
	Model        string    `json:"model"`
	Firmware     string    `json:"firmware"`
	Registered   bool      `json:"registered"`
	LastSeenAt   int64     `json:"lastSeenAt"`
	CatalogAt    int64     `json:"catalogAt"`
	LastError    string    `json:"lastError,omitempty"`
	Channels     []Channel `json:"channels"`
}

func parseXML(data []byte) (Message, error) {
	var m Message
	if len(data) == 0 || len(data) > 64<<10 || bytes.Contains(bytes.ToUpper(data), []byte("<!DOCTYPE")) {
		return m, errors.New("invalid GB28181 XML size or declaration")
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = func(charset string, r io.Reader) (io.Reader, error) {
		switch strings.ToLower(charset) {
		case "gb2312", "gbk", "gb18030":
			return simplifiedchinese.GB18030.NewDecoder().Reader(r), nil
		default:
			return nil, errors.New("unsupported GB28181 XML encoding")
		}
	}
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, err
		}
		if text, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(text)) != "" {
			return m, errors.New("trailing GB28181 XML content")
		}
	}
	if (m.XMLName.Local != "Notify" && m.XMLName.Local != "Response") || !deviceIDPattern.MatchString(m.DeviceID) || m.SN < 1 || m.SumNum < 0 || m.SumNum > 1000 || len(m.DeviceList.Items) > 1000 || m.DeviceList.Num != len(m.DeviceList.Items) {
		return m, errors.New("invalid GB28181 response identity or catalog bounds")
	}
	for _, c := range m.DeviceList.Items {
		if !deviceIDPattern.MatchString(c.DeviceID) || len(c.Name) > 256 || len(c.Address) > 512 || len(c.Manufacturer) > 128 || len(c.Model) > 128 || len(c.Owner) > 256 || len(c.CivilCode) > 64 || len(c.Status) > 32 || (c.Parental != 0 && c.Parental != 1) || (c.ParentID != "" && !deviceIDPattern.MatchString(c.ParentID)) {
			return m, errors.New("invalid GB28181 catalog channel")
		}
	}
	return m, nil
}
