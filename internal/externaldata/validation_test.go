package externaldata

import "testing"

func TestValidateConfiguration(t *testing.T) {
	s, e := fixtureHTTP("https://example.com:443/events")
	if err := ValidateSource(s); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEndpoint(e); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Endpoint){
		func(e *Endpoint) { e.URL = "https://user:secret@example.com/events" },
		func(e *Endpoint) { e.URL = "https://example.com/events?api_key=plaintext" },
		func(e *Endpoint) { e.Query = map[string]string{"access_token": "plaintext"} },
		func(e *Endpoint) { e.RequestBody = map[string]any{"login": map[string]any{"password": "plaintext"}} },
		func(e *Endpoint) { e.Headers = map[string]string{"Authorization": "secret"} },
		func(e *Endpoint) { e.Headers = map[string]string{"Cookie": "secret"} },
		func(e *Endpoint) { e.Headers = map[string]string{"X-Test": "ok\r\nInjected: bad"} },
		func(e *Endpoint) {
			e.Mapping.Fields = append(e.Mapping.Fields, Field{Target: "tenantId", Path: "tenant"})
		},
		func(e *Endpoint) { e.Pagination = Pagination{Mode: "cursor"} },
		func(e *Endpoint) { e.IntervalSeconds = 1 },
		func(e *Endpoint) { e.Mapping.Fields = append(e.Mapping.Fields, Field{Target: "id", Path: "other"}) },
		func(e *Endpoint) {
			e.Auth = &Auth{Type: "token", TokenURL: "https://example.com/login", TokenPath: "token", TokenBody: map[string]any{"password": "plaintext"}}
		},
	} {
		copy := e
		copy.Mapping.Fields = append([]Field(nil), e.Mapping.Fields...)
		mutate(&copy)
		if err := ValidateEndpoint(copy); err == nil {
			t.Fatalf("invalid endpoint accepted: %+v", copy)
		}
	}
	for _, host := range []string{"example.com", "*.example.com:443", "example.com:99999", "https://example.com:443", "example.com:0443"} {
		copy := s
		copy.AllowedHosts = []string{host}
		if err := ValidateSource(copy); err == nil {
			t.Fatalf("accepted host %s", host)
		}
	}
	for _, value := range []int{-1, 99, 3600001} {
		copy := s
		copy.RequestIntervalMillis = value
		if err := ValidateSource(copy); err == nil {
			t.Fatalf("source request interval %d accepted", value)
		}
	}
}
