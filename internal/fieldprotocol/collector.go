// Package fieldprotocol performs read-only protocol operations at the assigned node.
package fieldprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolruntime"
	"os"
	"runtime"
	"strings"
	"time"
)

// Credentials are loaded from a local file and are never included in profiles,
// synchronized configurations, protocol responses, or node heartbeats.
type Credential struct {
	TLSCAFile         string `json:"tlsCaFile"`
	AllowInsecureHTTP bool   `json:"allowInsecureHttp"`
	BACnetMode        string `json:"bacnetMode"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	CertificateFile   string `json:"certificateFile"`
	PrivateKeyFile    string `json:"privateKeyFile"`
	ServerSHA256      string `json:"serverSha256"`
	SNMPVersion       string `json:"snmpVersion"`
	Community         string `json:"community"`
	PrivacyPassword   string `json:"privacyPassword"`
}

func LoadCredentials(path string) (map[string]Credential, error) {
	if path == "" {
		return map[string]Credential{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.New("read node credential file metadata")
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("credential file must be regular, at most 1 MiB, and owner-only readable")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read node credential file")
	}
	var out map[string]Credential
	if json.Unmarshal(b, &out) != nil {
		return nil, errors.New("invalid node credential file JSON")
	}
	return out, nil
}

type Collector struct {
	AllowedCIDRs []string
	Credentials  map[string]Credential
}

func (c *Collector) Read(ctx context.Context, p model.DeviceAccessProfile, release model.ProtocolRelease) (raws []model.RawMessage, err error) {
	if p.EdgeNodeID != "" {
		return nil, errors.New("read must execute on assigned node")
	}
	credential, ok := c.Credentials[p.CredentialRef]
	if !ok {
		return nil, errors.New("local connection credential reference was not found")
	}
	defer func() {
		if err != nil {
			message := err.Error()
			for _, secret := range []string{credential.Password, credential.PrivacyPassword, credential.Community} {
				if secret != "" {
					message = strings.ReplaceAll(message, secret, "[redacted]")
				}
			}
			err = errors.New(message)
		}
	}()
	if p.TimeoutMs < 1 || p.TimeoutMs > 10000 || p.Port < 1 || p.Port > 65535 {
		return nil, errors.New("invalid protocol endpoint timeout or port")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMs)*time.Millisecond)
	defer cancel()
	points, err := parser.PollPoints(release.Config)
	if err != nil {
		return nil, err
	}
	host, err := protocolruntime.ResolveAllowedTarget(ctx, p.Host, c.AllowedCIDRs)
	if err != nil {
		return nil, err
	}
	var response model.PollResponse
	switch release.Transport {
	case "ONVIF":
		response, err = readONVIF(ctx, host, p, points, credential)
	case "BACNET":
		response, err = readBACnet(ctx, host, p, points, credential)
	case "SNMP":
		response, err = readSNMP(ctx, host, p, points, credential)
	case "OPC_UA":
		response, err = readOPCUA(ctx, host, p, points, credential)
	default:
		return nil, errors.New("unsupported field protocol")
	}
	if err != nil {
		return nil, err
	}
	response.Transport = release.Transport
	payload, err := json.Marshal(response)
	if err != nil {
		return nil, errors.New("protocol response cannot be encoded")
	}
	if len(payload) > 512<<10 {
		return nil, errors.New("protocol response exceeds 512 KiB")
	}
	now := time.Now()
	// This is the actual authenticated service response, represented as JSON.
	// It is not described as a capture of encrypted transport bytes.
	return []model.RawMessage{{MessageID: fmt.Sprintf("raw_read_%d", now.UnixNano()), TenantID: p.TenantID, ProductID: p.ProductID, DeviceID: p.DeviceID, Source: "edge-protocol-response", Protocol: release.ProtocolID, ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, Transport: release.Transport, PayloadFormat: "json", ReceivedAt: now.UnixMilli(), Payload: payload, CollectorID: p.CollectorID, RemoteAddress: fmt.Sprintf("%s:%d", p.Host, p.Port), Metadata: map[string]any{"profileId": p.ID, "representation": "protocol-service-response", "authentication": responseAuthentication(release.Transport)}}}, nil
}

func responseAuthentication(transport string) string {
	if transport == "BACNET" {
		return "none-native-bacnet-ip"
	}
	return "verified"
}
