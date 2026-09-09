package fieldprotocol

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"iot-platform/internal/model"
	"net"
	"strings"
	"time"
)

func readOPCUA(ctx context.Context, host string, p model.DeviceAccessProfile, points []model.PollPoint, credential Credential) (model.PollResponse, error) {
	out := model.PollResponse{}
	if credential.Username == "" || credential.Password == "" || credential.CertificateFile == "" || credential.PrivateKeyFile == "" || len(credential.ServerSHA256) != 64 {
		return out, errors.New("OPC UA requires username, password, client certificate/key and pinned server SHA256")
	}
	if p.EndpointPath != "" && (!strings.HasPrefix(p.EndpointPath, "/") || strings.ContainsAny(p.EndpointPath, "?#")) {
		return out, errors.New("invalid OPC UA endpoint path")
	}
	endpoint := "opc.tcp://" + net.JoinHostPort(host, fmt.Sprint(p.Port)) + p.EndpointPath
	options := []opcua.Option{opcua.DialTimeout(time.Duration(p.TimeoutMs) * time.Millisecond), opcua.RequestTimeout(time.Duration(p.TimeoutMs) * time.Millisecond), opcua.AutoReconnect(false), opcua.MaxMessageSize(1 << 20)}
	endpoints, err := opcua.GetEndpoints(ctx, endpoint, options...)
	if err != nil {
		return out, err
	}
	var selected *ua.EndpointDescription
	for _, candidate := range endpoints {
		if candidate.SecurityPolicyURI != ua.SecurityPolicyURIBasic256Sha256 || candidate.SecurityMode != ua.MessageSecurityModeSignAndEncrypt {
			continue
		}
		hash := sha256.Sum256(candidate.ServerCertificate)
		if !strings.EqualFold(hex.EncodeToString(hash[:]), credential.ServerSHA256) {
			continue
		}
		cert, e := x509.ParseCertificate(candidate.ServerCertificate)
		if e != nil || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) || cert.VerifyHostname(p.Host) != nil {
			continue
		}
		for _, token := range candidate.UserIdentityTokens {
			if token.TokenType == ua.UserTokenTypeUserName {
				selected = candidate
				break
			}
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		return out, errors.New("no encrypted OPC UA username endpoint matches the trusted server certificate")
	}
	options = append(options, opcua.SecurityFromEndpoint(selected, ua.UserTokenTypeUserName), opcua.AuthUsername(credential.Username, credential.Password), opcua.CertificateFile(credential.CertificateFile), opcua.PrivateKeyFile(credential.PrivateKeyFile))
	client, err := opcua.NewClient(endpoint, options...)
	if err != nil {
		return out, err
	}
	if err = client.Connect(ctx); err != nil {
		return out, err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.Close(closeCtx)
	}()
	request := &ua.ReadRequest{TimestampsToReturn: ua.TimestampsToReturnBoth}
	for _, point := range points {
		node, err := ua.ParseNodeID(point.Address)
		if err != nil {
			return out, errors.New("invalid OPC UA node id")
		}
		request.NodesToRead = append(request.NodesToRead, &ua.ReadValueID{NodeID: node, AttributeID: ua.AttributeIDValue})
	}
	response, err := client.Read(ctx, request)
	if err != nil {
		return out, err
	}
	out.Response = response
	if len(response.Results) != len(points) {
		return out, errors.New("OPC UA response count mismatch")
	}
	for i, value := range response.Results {
		quality := "GOOD"
		var actual any
		var stamp int64
		if value == nil {
			quality = "MISSING"
		} else {
			if value.Status != ua.StatusOK {
				quality = value.Status.Error()
			}
			if value.Value != nil {
				actual = value.Value.Value()
			}
			if !value.SourceTimestamp.IsZero() {
				stamp = value.SourceTimestamp.UnixMilli()
			}
		}
		out.Values = append(out.Values, model.PollValue{Address: points[i].Address, Value: actual, Quality: quality, Timestamp: stamp})
	}
	return out, nil
}
