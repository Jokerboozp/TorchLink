package fieldprotocol

import (
	"context"
	"errors"
	"fmt"
	"github.com/gosnmp/gosnmp"
	"iot-platform/internal/model"
	"strings"
	"time"
)

func readSNMP(ctx context.Context, host string, p model.DeviceAccessProfile, points []model.PollPoint, credential Credential) (model.PollResponse, error) {
	out := model.PollResponse{}
	client := &gosnmp.GoSNMP{Target: host, Port: uint16(p.Port), Transport: "udp", Context: ctx, Timeout: time.Duration(p.TimeoutMs) * time.Millisecond, Retries: 0, MaxOids: 64}
	switch credential.SNMPVersion {
	case "2c":
		if credential.Community == "" {
			return out, errors.New("SNMP v2c requires an explicit community")
		}
		client.Version = gosnmp.Version2c
		client.Community = credential.Community
	case "3":
		if credential.Username == "" || len(credential.Password) < 8 || len(credential.PrivacyPassword) < 8 {
			return out, errors.New("SNMP v3 requires username and authentication/privacy passphrases")
		}
		client.Version = gosnmp.Version3
		client.SecurityModel = gosnmp.UserSecurityModel
		client.MsgFlags = gosnmp.AuthPriv
		client.SecurityParameters = &gosnmp.UsmSecurityParameters{UserName: credential.Username, AuthenticationProtocol: gosnmp.SHA256, AuthenticationPassphrase: credential.Password, PrivacyProtocol: gosnmp.AES, PrivacyPassphrase: credential.PrivacyPassword}
	default:
		return out, errors.New("explicit SNMP version 2c or 3 is required")
	}
	if err := client.Connect(); err != nil {
		return out, err
	}
	defer client.Close()
	// Capture the connection itself to avoid racing the client's Close mutation.
	conn := client.Conn
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	oids := make([]string, 0, len(points))
	for _, point := range points {
		if !strings.HasPrefix(point.Address, ".") {
			point.Address = "." + point.Address
		}
		oids = append(oids, point.Address)
	}
	response, err := client.Get(oids)
	if err != nil {
		return out, err
	}
	if response.Error != gosnmp.NoError {
		return out, fmt.Errorf("SNMP service error %s at %d", response.Error, response.ErrorIndex)
	}
	// Only protocol response fields are retained; security parameters contain credentials.
	out.Response = map[string]any{"requestId": response.RequestID, "variables": response.Variables, "error": response.Error, "errorIndex": response.ErrorIndex}
	for _, point := range points {
		found := false
		for _, variable := range response.Variables {
			if strings.TrimPrefix(variable.Name, ".") != strings.TrimPrefix(point.Address, ".") {
				continue
			}
			found = true
			quality := "GOOD"
			value := variable.Value
			if variable.Type == gosnmp.NoSuchObject || variable.Type == gosnmp.NoSuchInstance || variable.Type == gosnmp.EndOfMibView {
				quality = "MISSING"
			}
			if b, ok := value.([]byte); ok {
				value = string(b)
			}
			out.Values = append(out.Values, model.PollValue{Address: point.Address, Value: value, Quality: quality})
		}
		if !found {
			out.Values = append(out.Values, model.PollValue{Address: point.Address, Quality: "MISSING"})
		}
	}
	return out, nil
}
