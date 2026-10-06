package externaldata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/netguard"
	"strings"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/externaldata"
)

const testTenant = "external-test"

func runtimeService(t *testing.T, store externaldata.Store, deliver externaldata.Deliver) *externaldata.Service {
	t.Helper()
	if deliver == nil {
		deliver = func(_ context.Context, _ string, _ externaldata.Source, _ externaldata.Endpoint, e externaldata.Event, _ externaldata.Binding, previous externaldata.Result) (externaldata.Result, error) {
			return externaldata.Result{AlarmID: "alarm-" + e.ID}, nil
		}
	}
	s, err := externaldata.New(store, "external-data-test-encryption-key", func(ctx context.Context, _ string, _ externaldata.Source, _ externaldata.Endpoint) (context.Context, error) {
		return ctx, nil
	}, deliver)
	if err != nil {
		t.Fatal(err)
	}
	// Fixtures serve from httptest on the loopback address.
	s.SetOutbound(netguard.Loopback)
	return s
}

func runtimeFixture(t *testing.T, deliver externaldata.Deliver, hosts ...string) (*externaldata.Service, externaldata.Source) {
	t.Helper()
	s := runtimeService(t, memory.NewRepository().ExternalDataStore(), deliver)
	source, err := s.SaveSource(context.Background(), testTenant, externaldata.Source{Name: "视频分析平台", Username: "operator", Enabled: true, AllowedHosts: hosts})
	if err != nil {
		t.Fatal(err)
	}
	return s, source
}

func runtimeMapping() externaldata.Mapping {
	return externaldata.Mapping{Fields: []externaldata.Field{
		{Target: "id", Path: "eventId", Type: "string", Required: true},
		{Target: "timestamp", Path: "time", Type: "timestamp", TimeFormat: "milliseconds", Required: true},
		{Target: "version", Path: "version", Type: "number"},
		{Target: "status", Path: "status", Type: "string"},
		{Target: "content", Path: "text", Type: "string", Required: true},
		{Target: "objectId", Path: "camera", Type: "string"},
	}}
}

func runtimeEndpoint(t *testing.T, s *externaldata.Service, source externaldata.Source, endpoint externaldata.Endpoint) externaldata.Endpoint {
	t.Helper()
	endpoint.SourceID = source.ID
	if endpoint.Name == "" {
		endpoint.Name = "告警接口"
	}
	if endpoint.Mode == "" {
		endpoint.Mode = "push"
	}
	if endpoint.Kind == "" {
		endpoint.Kind = "event"
	}
	if len(endpoint.Mapping.Fields) == 0 {
		endpoint.Mapping = runtimeMapping()
	}
	endpoint.Enabled = true
	saved, err := s.SaveEndpoint(context.Background(), testTenant, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func runtimeEntries(t *testing.T, s *externaldata.Service, kind string) []externaldata.Entry {
	t.Helper()
	entries, _, err := s.Store.List(context.Background(), externaldata.Query{TenantID: testTenant, Kind: kind, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func runtimeBody[T any](t *testing.T, e externaldata.Entry) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(e.Body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func runtimeReceive(t *testing.T, s *externaldata.Service, ep externaldata.Endpoint, payload string) {
	t.Helper()
	if _, err := s.Receive(context.Background(), testTenant, ep, []byte(payload), ""); err != nil {
		t.Fatal(err)
	}
}

func runtimeStep(t *testing.T, s *externaldata.Service, kind string) {
	t.Helper()
	if kind == "job" {
		runtimeExpireRequestWindow(t, s)
	}
	worked, err := s.Step(context.Background(), kind, "test-worker")
	if err != nil || !worked {
		t.Fatalf("step %s worked=%v err=%v", kind, worked, err)
	}
}

// Business lifecycle tests explicitly advance the persisted outgoing window;
// dedicated throttle tests use Step/FetchPreview directly and never call this.
func runtimeExpireRequestWindow(t *testing.T, s *externaldata.Service) {
	t.Helper()
	for _, entry := range runtimeEntries(t, s, "rate_limit") {
		entry.Body = json.RawMessage(`{"nextSourceAt":0,"nextEndpoints":{}}`)
		if _, err := s.Store.Put(context.Background(), entry, entry.Revision); err != nil {
			t.Fatal(err)
		}
	}
}

func runtimeDue(t *testing.T, s *externaldata.Service, kind, id string) externaldata.Entry {
	t.Helper()
	e, err := s.Store.Get(context.Background(), testTenant, kind, id)
	if err != nil {
		t.Fatal(err)
	}
	e.DueAt = 0
	e, err = s.Store.Put(context.Background(), e, e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestServiceCredentialPreservationAndIsolation(t *testing.T) {
	ctx := context.Background()
	s, source := runtimeFixture(t, nil)
	source.Auth = externaldata.Auth{Type: "bearer", Secret: "source-private-token"}
	source, err := s.SaveSource(ctx, testTenant, source)
	if err != nil || source.Auth.Secret != "" || !source.Auth.SecretSet {
		t.Fatalf("public source: %+v %v", source.Auth, err)
	}
	stored, err := s.Store.Get(ctx, testTenant, "source", source.ID)
	if err != nil || bytes.Contains(stored.Body, []byte("source-private-token")) {
		t.Fatal("credential stored in plaintext", err)
	}
	source.Name = "改名后保留凭据"
	source, err = s.SaveSource(ctx, testTenant, source)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Source(ctx, testTenant, source.ID)
	if err != nil || loaded.Auth.Secret != "source-private-token" {
		t.Fatal("blank update did not preserve secret", err)
	}
	if _, err = s.Source(ctx, "another-tenant", source.ID); !errors.Is(err, externaldata.ErrNotFound) {
		t.Fatalf("cross-tenant read: %v", err)
	}
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Auth: &externaldata.Auth{Type: "bearer", Secret: "endpoint-private-token"}})
	if ep.Auth.Secret != "" || !ep.Auth.SecretSet {
		t.Fatal("endpoint credential leaked")
	}
	stored, err = s.Store.Get(ctx, testTenant, "endpoint", ep.ID)
	if err != nil || bytes.Contains(stored.Body, []byte("endpoint-private-token")) {
		t.Fatal("endpoint credential stored plaintext", err)
	}
	key, err := s.RotateKey(ctx, testTenant, ep.ID)
	if err != nil || len(key) == 0 {
		t.Fatal("key rotation failed", err)
	}
	stored, err = s.Store.Get(ctx, testTenant, "endpoint", ep.ID)
	if err != nil || bytes.Contains(stored.Body, []byte(key)) {
		t.Fatal("push credential stored plaintext", err)
	}
	loadedEP, err := s.Endpoint(ctx, testTenant, ep.ID)
	if err != nil || loadedEP.Auth.Secret != "endpoint-private-token" {
		t.Fatal("key rotation corrupted endpoint auth", err)
	}
	list, _, err := s.List(ctx, externaldata.Query{TenantID: testTenant, Kind: "endpoint"})
	encoded, _ := json.Marshal(list)
	if err != nil || strings.Contains(string(encoded), "endpoint-private-token") || strings.Contains(string(encoded), key) {
		t.Fatal("list leaks credentials", err)
	}
	source.Auth.ClearSecret = true
	if _, err = s.SaveSource(ctx, testTenant, source); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.Source(ctx, testTenant, source.ID)
	if err != nil || loaded.Auth.Secret != "" {
		t.Fatal("explicit clear did not clear", err)
	}
}

func TestServiceSourceCredentialsCanBeReenteredAfterEncryptionKeyChanges(t *testing.T) {
	ctx := context.Background()
	old, source := runtimeFixture(t, nil)
	source.Auth = externaldata.Auth{Type: "bearer", Secret: "old-source-secret"}
	source, err := old.SaveSource(ctx, testTenant, source)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := externaldata.New(old.Store, "replacement-encryption-key", old.Authorize, old.Deliver)
	if fresh != nil {
		fresh.SetOutbound(netguard.Loopback)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.Source(ctx, testTenant, source.ID); err == nil {
		t.Fatal("new encryption key unexpectedly decrypted the old credential")
	}
	public, err := fresh.SourceInfo(ctx, testTenant, source.ID)
	if err != nil || public.Name != source.Name || public.Auth.Secret != "" || !public.Auth.SecretSet {
		t.Fatalf("public configuration must remain readable: %+v %v", public, err)
	}
	if _, _, err = fresh.List(ctx, externaldata.Query{TenantID: testTenant, Kind: "source"}); err != nil {
		t.Fatalf("source list must remain readable: %v", err)
	}
	before, _ := old.Store.Get(ctx, testTenant, "source", source.ID)
	public.Name = "沿用不可解密凭据必须失败"
	if _, err = fresh.SaveSource(ctx, testTenant, public); err == nil {
		t.Fatal("silently reused credential encrypted by an unavailable key")
	}
	after, _ := old.Store.Get(ctx, testTenant, "source", source.ID)
	if before.Revision != after.Revision || !bytes.Equal(before.Body, after.Body) {
		t.Fatal("failed credential preservation mutated the source")
	}
	public.Auth.Secret = "replacement-source-secret"
	if _, err = fresh.SaveSource(ctx, testTenant, public); err != nil {
		t.Fatalf("reenter secret: %v", err)
	}
	loaded, err := fresh.Source(ctx, testTenant, source.ID)
	if err != nil || loaded.Auth.Secret != "replacement-source-secret" {
		t.Fatalf("replacement credential unusable: %v", err)
	}
	clearSource := source
	clearSource.ID, clearSource.Revision = "", 0
	clearSource.Auth.Secret = "another-old-secret"
	clearSource, err = old.SaveSource(ctx, testTenant, clearSource)
	if err != nil {
		t.Fatal(err)
	}
	clearSource.Auth.ClearSecret = true
	if _, err = fresh.SaveSource(ctx, testTenant, clearSource); err != nil {
		t.Fatalf("explicit credential clearing: %v", err)
	}
	loaded, err = fresh.Source(ctx, testTenant, clearSource.ID)
	if err != nil || loaded.Auth.Secret != "" {
		t.Fatalf("cleared source remains unavailable: %v", err)
	}
}

func TestExtractionErrorRetryReplacesRecordWithoutDeliveringOriginal(t *testing.T) {
	ctx := context.Background()
	deliveries := 0
	s, source := runtimeFixture(t, func(_ context.Context, _ string, _ externaldata.Source, _ externaldata.Endpoint, _ externaldata.Event, _ externaldata.Binding, _ externaldata.Result) (externaldata.Result, error) {
		deliveries++
		return externaldata.Result{AlarmID: "corrected-alarm"}, nil
	})
	mapping := runtimeMapping()
	mapping.ItemsPath = "missing"
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mapping: mapping})
	runtimeReceive(t, s, ep, `{"eventId":"extract-error","time":1,"text":"valid item but wrong collection path"}`)
	old := runtimeEntries(t, s, "record")[0]
	if old.Status != "FAILED" || !runtimeBody[externaldata.Record](t, old).ExtractionError {
		t.Fatalf("extraction diagnosis missing: %+v", old)
	}
	// Transform could accept this raw object; the failed extraction must not be
	// treated as a successfully extracted business item by a stored-rule retry.
	if _, err := s.Retry(ctx, testTenant, "record", old.ID, old.Revision, false); err == nil {
		t.Fatal("extraction error accepted a retry without correcting extraction")
	}
	if _, err := s.Retry(ctx, testTenant, "record", old.ID, old.Revision, true); err == nil {
		t.Fatal("unchanged mapping accepted an extraction retry")
	}
	ep.Mapping.ItemsPath = ""
	if _, err := s.SaveEndpoint(ctx, testTenant, ep); err != nil {
		t.Fatal(err)
	}
	replaced, err := s.Retry(ctx, testTenant, "record", old.ID, old.Revision, true)
	if err != nil || replaced.Status != "REPLACED" {
		t.Fatalf("old extraction record was not replaced: %+v %v", replaced, err)
	}
	runtimeStep(t, s, "record")
	if deliveries != 1 {
		t.Fatalf("corrected extraction delivered %d times", deliveries)
	}
	if _, err = s.Retry(ctx, testTenant, "record", old.ID, replaced.Revision, false); err == nil {
		t.Fatal("replaced extraction record reentered the business queue")
	}
	if worked, err := s.Step(ctx, "record", "after-replaced"); err != nil || worked || deliveries != 1 {
		t.Fatalf("original failed extraction was reprocessed: worked=%v deliveries=%d err=%v", worked, deliveries, err)
	}
}

func TestReceivePersistsRawAndMappingFailuresWithoutPreviewWrites(t *testing.T) {
	ctx := context.Background()
	s, source := runtimeFixture(t, nil)
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{})
	payload := `{"eventId":"bad","time":1,"unexpected":"missing content"}`
	preview, err := s.Preview(ep, []byte(payload))
	if err != nil || len(preview) != 1 {
		t.Fatalf("preview: %v %v", preview, err)
	}
	if len(runtimeEntries(t, s, "record")) != 0 || len(runtimeEntries(t, s, "receipt")) != 0 {
		t.Fatal("preview wrote business records")
	}
	runtimeReceive(t, s, ep, payload)
	if len(runtimeEntries(t, s, "receipt")) != 1 || len(runtimeEntries(t, s, "record")) != 1 {
		t.Fatal("receipt or pending record missing")
	}
	record := runtimeEntries(t, s, "record")[0]
	if record.Status != "PENDING" {
		t.Fatal(record.Status)
	}
	runtimeStep(t, s, "record")
	record = runtimeEntries(t, s, "record")[0]
	r := runtimeBody[externaldata.Record](t, record)
	if record.Status != "FAILED" || r.Error == "" || !json.Valid(r.Raw) || r.ConfigRevision != ep.Revision {
		t.Fatalf("field error lost raw or configuration: %+v %+v", record, r)
	}
	ep.Mapping.Fields[4].Path = "unexpected"
	ep, err = s.SaveEndpoint(ctx, testTenant, ep)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Retry(ctx, testTenant, "record", record.ID, record.Revision, true); err != nil {
		t.Fatal(err)
	}
	runtimeStep(t, s, "record")
	record = runtimeEntries(t, s, "record")[0]
	if record.Status != "PROCESSED" {
		t.Fatalf("current mapping retry: %+v", record)
	}
	if err = s.Delete(ctx, testTenant, "endpoint", ep.ID, ep.Revision); !errors.Is(err, externaldata.ErrInvalid) {
		t.Fatal("deleted endpoint with history", err)
	}
	if err = s.Delete(ctx, testTenant, "source", source.ID, source.Revision); !errors.Is(err, externaldata.ErrInvalid) {
		t.Fatal("deleted source with history", err)
	}
}

type failRecordStore struct {
	externaldata.Store
	fail bool
}

func (s *failRecordStore) Put(ctx context.Context, e externaldata.Entry, revision int64) (externaldata.Entry, error) {
	if s.fail && e.Kind == "record" {
		s.fail = false
		return externaldata.Entry{}, errors.New("injected record write failure")
	}
	return s.Store.Put(ctx, e, revision)
}

func TestReceiveResumesDurableReceiptAfterRecordWriteFailure(t *testing.T) {
	s, source := runtimeFixture(t, nil)
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{})
	store := &failRecordStore{Store: s.Store, fail: true}
	s.Store = store
	_, err := s.Receive(context.Background(), testTenant, ep, []byte(`{"eventId":"saved-before-crash","time":1,"text":"保留接收原文"}`), "")
	if err == nil {
		t.Fatal("injected storage failure was ignored")
	}
	if len(runtimeEntries(t, s, "receipt")) != 1 {
		t.Fatal("durable receipt missing")
	}
	restarted := runtimeService(t, store.Store, nil)
	for n := 0; n < 3; n++ {
		if _, err := restarted.Step(context.Background(), "receipt", "recovery-worker"); err != nil {
			t.Fatal(err)
		}
	}
	if len(runtimeEntries(t, restarted, "record")) != 1 {
		t.Fatal("persisted receipt was not resumed after restart")
	}
	runtimeStep(t, restarted, "record")
	if runtimeEntries(t, restarted, "record")[0].Status != "PROCESSED" {
		t.Fatal("resumed receipt did not deliver")
	}
}

func TestReceiveFailedResponseIdentityDoesNotBlockSuccessfulRetry(t *testing.T) {
	ctx := context.Background()
	delivered := 0
	s, source := runtimeFixture(t, func(context.Context, string, externaldata.Source, externaldata.Endpoint, externaldata.Event, externaldata.Binding, externaldata.Result) (externaldata.Result, error) {
		delivered++
		return externaldata.Result{}, nil
	})
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{})
	payload, _ := json.Marshal(map[string]any{"eventId": "same-body", "time": 1, "text": "同一条告警"})
	for _, detail := range []string{"HTTP 502, request trace one", "HTTP 503, request trace two"} {
		if err := s.ReceiveFailed(ctx, testTenant, ep, payload, "same-job", detail); err != nil {
			t.Fatal(err)
		}
	}
	if len(runtimeEntries(t, s, "record")) != 1 {
		t.Fatal("changing error text created duplicate failed records")
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Receive(ctx, testTenant, ep, payload, "same-job"); err != nil {
			t.Fatal(err)
		}
	}
	if len(runtimeEntries(t, s, "record")) != 2 {
		t.Fatal("successful retry collided with failed response identity or lost deduplication")
	}
	runtimeStep(t, s, "record")
	counts := map[string]int{}
	for _, e := range runtimeEntries(t, s, "record") {
		counts[e.Status]++
	}
	if counts["FAILED"] != 1 || counts["PROCESSED"] != 1 || delivered != 1 {
		t.Fatalf("response history/delivery=%v/%d", counts, delivered)
	}
	ep.Name = "修改映射配置后仍不能接纳失败响应"
	ep, err := s.SaveEndpoint(ctx, testTenant, ep)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range runtimeEntries(t, s, "record") {
		if entry.Status == "FAILED" {
			if _, err = s.Retry(ctx, testTenant, "record", entry.ID, entry.Revision, true); err == nil {
				t.Fatal("HTTP failure body became valid business data after changing configuration")
			}
		}
	}
	if worked, err := s.Step(ctx, "record", "after-response-error"); err != nil || worked || delivered != 1 {
		t.Fatalf("failed response was delivered: worked=%v delivered=%d err=%v", worked, delivered, err)
	}
}
