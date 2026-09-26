package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
)

func TestBuiltinAdminCannotMintTokenForUnconfiguredTenant(t *testing.T) {
	cfg := config.Config{AdminUser: "admin", AdminPassword: "admin123", JWTSecret: "test-secret-at-least-32-characters"}
	s := &Server{cfg: cfg, auth: auth.New(cfg.JWTSecret), engine: &core.Engine{}}
	body, err := json.Marshal(map[string]string{"username": "admin", "password": "admin123", "tenantId": "tenant-attacker"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	s.login(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("unexpected status for unconfigured admin tenant: got=%d body=%s", resp.Code, resp.Body.String())
	}
	if bytes.Contains(resp.Body.Bytes(), []byte(`"accessToken"`)) {
		t.Fatal("unconfigured admin tenant response contains an access token")
	}
}

func TestBuiltinAdminCanUseConfiguredTenant(t *testing.T) {
	cfg := config.Config{AdminUser: "admin", AdminPassword: "admin123", AdminTenants: []string{"tenant-allowed"}, JWTSecret: "test-secret-at-least-32-characters"}
	s := &Server{cfg: cfg, auth: auth.New(cfg.JWTSecret), engine: &core.Engine{}}
	body, err := json.Marshal(map[string]string{"username": "admin", "password": "admin123", "tenantId": "tenant-allowed"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	s.login(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("configured admin tenant was rejected: status=%d body=%s", resp.Code, resp.Body.String())
	}
	var result struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	claims, err := s.auth.Parse(result.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.TenantID != "tenant-allowed" || claims.Role != "admin" {
		t.Fatalf("unexpected configured admin claims: tenant=%q role=%q", claims.TenantID, claims.Role)
	}
}
