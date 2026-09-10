package httpapi

import (
	"iot-platform/internal/protocolworker"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSourceManifestUsesPackageMetadataAndExplicitOverrides(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	files := map[string][]byte{"protocol.json": []byte(`{"id":"package","name":"Vendor","version":"1.0.0","runtime":"go-protocol-v2","transport":"TCP_UDP","payloadFormat":"hex","capabilities":["decode","ingress","encode"],"entrypoint":"cmd/worker"}`)}
	m, entry, err := sourceProtocolManifest(req, "package", files)
	if err != nil || m.Runtime != protocolworker.Runtime || m.Version != "1.0.0" || m.Transport != "TCP_UDP" || entry != "cmd/worker" {
		t.Fatalf("metadata %+v %s %v", m, entry, err)
	}
	req.Form = url.Values{"version": {"1.1.0"}, "transport": {"TCP"}}
	m, _, err = sourceProtocolManifest(req, "package", files)
	if err != nil || m.Version != "1.1.0" || m.Transport != "TCP" {
		t.Fatalf("override %+v %v", m, err)
	}
	if _, _, err = sourceProtocolManifest(req, "other", files); err == nil {
		t.Fatal("mismatched source id accepted")
	}
	req.Form.Set("runtime", "go-json-lines-v1")
	if _, _, err = sourceProtocolManifest(req, "package", files); err == nil {
		t.Fatal("v1 accepted ingress/encode")
	}
	req.Form.Set("capabilities", `["decode"]`)
	if _, _, err = sourceProtocolManifest(req, "package", files); err == nil {
		t.Fatal("decode-only v1 accepted")
	}
	req.Form = url.Values{"version": {"1.0.0"}}
	m, _, err = sourceProtocolManifest(req, "package", nil)
	if err != nil || m.Runtime != protocolworker.Runtime {
		t.Fatalf("current runtime default: %+v %v", m, err)
	}

}
