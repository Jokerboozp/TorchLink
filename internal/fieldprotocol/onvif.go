package fieldprotocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/model"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const onvifDeviceNS = "http://www.onvif.org/ver10/device/wsdl"

func readONVIF(ctx context.Context, host string, p model.DeviceAccessProfile, points []model.PollPoint, credential Credential) (model.PollResponse, error) {
	out := model.PollResponse{}
	if credential.Username == "" || credential.Password == "" {
		return out, errors.New("ONVIF username and password are required")
	}
	if len(points) != 1 || points[0].Address != "device-information" {
		return out, errors.New("ONVIF metadata reader supports device-information")
	}
	path := p.EndpointPath
	if path == "" {
		path = "/onvif/device_service"
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return out, errors.New("invalid ONVIF service path")
	}
	var nonce [20]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return out, err
	}
	created := time.Now().UTC().Format(time.RFC3339)
	digest := sha1.New()
	_, _ = digest.Write(nonce[:])
	_, _ = digest.Write([]byte(created))
	_, _ = digest.Write([]byte(credential.Password))
	var username bytes.Buffer
	_ = xml.EscapeText(&username, []byte(credential.Username))
	body := fmt.Sprintf(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"><s:Header><wsse:Security s:mustUnderstand="1"><wsse:UsernameToken><wsse:Username>%s</wsse:Username><wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password><wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce><wsu:Created>%s</wsu:Created></wsse:UsernameToken></wsse:Security></s:Header><s:Body><tds:GetDeviceInformation/></s:Body></s:Envelope>`, username.String(), base64.StdEncoding.EncodeToString(digest.Sum(nil)), base64.StdEncoding.EncodeToString(nonce[:]), created)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if credential.TLSCAFile != "" {
		data, err := os.ReadFile(credential.TLSCAFile)
		if err != nil {
			return out, errors.New("read local ONVIF CA file")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(data) {
			return out, errors.New("invalid ONVIF CA file")
		}
		tlsConfig.RootCAs = roots
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(p.Port)))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	scheme := "https"
	if credential.AllowInsecureHTTP {
		scheme = "http"
	}
	request, err := http.NewRequestWithContext(ctx, "POST", scheme+"://"+net.JoinHostPort(p.Host, fmt.Sprint(p.Port))+path, strings.NewReader(body))
	if err != nil {
		return out, err
	}
	request.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation"`)
	response, err := client.Do(request)
	if err != nil {
		return out, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		challenge := response.Header.Get("WWW-Authenticate")
		response.Body.Close()
		authorization, err := digestAuthorization(challenge, credential.Username, credential.Password, request.Method, request.URL.RequestURI())
		if err != nil {
			return out, err
		}
		request.Body = io.NopCloser(strings.NewReader(body))
		request.Header.Set("Authorization", authorization)
		response, err = client.Do(request)
		if err != nil {
			return out, err
		}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (512<<10)+1))
	if err != nil {
		return out, err
	}
	if len(data) > 512<<10 {
		return out, errors.New("ONVIF response exceeds 512 KiB")
	}
	if response.StatusCode != 200 {
		return out, fmt.Errorf("ONVIF device returned HTTP %d", response.StatusCode)
	}
	var envelope struct {
		XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
		Body    struct {
			Fault *struct{ XMLName xml.Name } `xml:"http://www.w3.org/2003/05/soap-envelope Fault"`
			Info  *struct {
				Manufacturer    string
				Model           string
				FirmwareVersion string
				SerialNumber    string
				HardwareId      string
			} `xml:"http://www.onvif.org/ver10/device/wsdl GetDeviceInformationResponse"`
		} `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
	}
	if xml.Unmarshal(data, &envelope) != nil || envelope.Body.Fault != nil || envelope.Body.Info == nil {
		return out, errors.New("ONVIF SOAP response is a fault or invalid device-information result")
	}
	info := envelope.Body.Info
	metadata := map[string]any{"manufacturer": info.Manufacturer, "model": info.Model, "firmwareVersion": info.FirmwareVersion, "serialNumber": info.SerialNumber, "hardwareId": info.HardwareId}
	out.Values = []model.PollValue{{Address: points[0].Address, Value: metadata, Quality: "GOOD"}}
	out.Response = map[string]any{"responseXML": string(data)}
	return out, nil
}
