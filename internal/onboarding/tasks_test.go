package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func taskFixture(t *testing.T) (*TaskService, *memory.Repository, TaskOwner) {
	t.Helper()
	service, repo := enrollFixture(t)
	ctx := context.Background()
	fingerprint, err := service.TemplateFingerprint(ctx, "tenant", "product")
	if err != nil {
		t.Fatal(err)
	}
	rec, prep, err := service.TemplateRecord(ctx, "tenant", "product")
	if err != nil {
		t.Fatal(err)
	}
	prep.Status, prep.Fingerprint = "READY", fingerprint
	prep.Verification = &model.DeviceVerification{Status: "VERIFIED", Fingerprint: fingerprint}
	if _, err = service.SaveTemplateRecord(ctx, rec, prep); err != nil {
		t.Fatal(err)
	}
	owner := TaskOwner{Username: "alice", Managed: true, SessionVersion: 1}
	s := &TaskService{Service: service, Key: strings.Repeat("test-task-secret-", 3), Fingerprint: service.TemplateFingerprint, Authorize: func(ctx context.Context, tenant string, o TaskOwner) (context.Context, error) {
		if tenant != "tenant" || o.Username == "blocked" {
			return ctx, &EnrollError{Status: 403, Message: "denied"}
		}
		return ctx, nil
	}}
	return s, repo, owner
}
func batchRequest(n int) BatchRequest {
	q := BatchRequest{ID: "job", ProductID: "product", Connection: EnrollConnection{Mode: ModeStandard, Transport: "HTTP"}}
	for i := 0; i < n; i++ {
		q.Rows = append(q.Rows, BatchInputRow{Device: EnrollDevice{ID: fmt.Sprintf("d-%d", i), Name: fmt.Sprintf("设备 %d", i)}})
	}
	return q
}
func createTask(t *testing.T, s *TaskService, owner TaskOwner, q BatchRequest) BatchSummary {
	t.Helper()
	ctx := context.Background()
	check, err := s.PreflightBatch(ctx, "tenant", owner, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Fingerprint = check.Fingerprint
	summary, err := s.CreateBatch(ctx, "tenant", owner, q)
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func TestTaskDraftCASOwnerAndCredentialBoundary(t *testing.T) {
	s, repo, owner := taskFixture(t)
	ctx := context.Background()
	draft := DeviceDraft{Step: "connection", ProductID: "product", Request: enrollRequest("draft-device")}
	v, err := s.SaveDraft(ctx, "tenant", owner, "draft", draft)
	if err != nil || v.Revision != 1 {
		t.Fatal(v, err)
	}
	if _, err = s.SaveDraft(ctx, "tenant", owner, "draft", draft); !errors.Is(err, model.ErrOnboardingChanged) {
		t.Fatal("stale update", err)
	}
	other := owner
	other.Username = "bob"
	if _, err = s.OwnedRecord(ctx, "tenant", other, "draft", DraftKind); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("foreign draft", err)
	}
	draft.Revision = v.Revision
	draft.Step = "verify"
	if _, err = s.SaveDraft(ctx, "tenant", owner, "draft", draft); err != nil {
		t.Fatal(err)
	}
	draft.Request.NewProduct = &NewProduct{Metadata: map[string]any{"auth": map[string]any{"secret": "never-store"}}}
	draft.Revision = 0
	if _, err = s.SaveDraft(ctx, "tenant", owner, "unsafe", draft); statusOf(err) != 422 {
		t.Fatal("secret accepted", err)
	}
	if _, err = repo.GetOnboardingRecord(ctx, "tenant", "unsafe"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("unsafe saved", err)
	}
}

func TestTaskTemplateDraftPermissionCannotReclassifyExistingDraft(t *testing.T) {
	s, _, owner := taskFixture(t)
	ctx := context.Background()
	ordinary := DeviceDraft{Step: "connection", Request: enrollRequest("d")}
	v, err := s.SaveDraft(ctx, "tenant", owner, "normal", ordinary)
	if err != nil {
		t.Fatal(err)
	}
	base := s.Authorize
	s.TemplateDraftAuthorize = base
	s.Authorize = func(ctx context.Context, _ string, _ TaskOwner) (context.Context, error) {
		return ctx, &EnrollError{Status: 403, Message: "no registration permission"}
	}
	prep := DeviceDraft{Step: "preparation:0", Request: EnrollRequest{NewProduct: &NewProduct{Metadata: map[string]any{"preparationDraft": map[string]any{"product": map[string]any{"name": "试用模板"}}}}}}
	if _, err = s.SaveDraft(ctx, "tenant", owner, "preparation", prep); err != nil {
		t.Fatal(err)
	}
	prep.Revision = v.Revision
	if _, err = s.SaveDraft(ctx, "tenant", owner, "normal", prep); statusOf(err) != 403 {
		t.Fatal("changed ordinary draft through template permission", err)
	}
	items, total, err := s.ListDrafts(ctx, "tenant", owner, 20, 0, "")
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != "preparation" {
		t.Fatal(items, total, err)
	}
	if _, err = s.OwnedRecord(ctx, "tenant", owner, "normal", DraftKind); statusOf(err) != 403 {
		t.Fatal("ordinary draft read through template permission", err)
	}
}

func TestBatchPreflightIsReadOnlyAndFingerprintsInput(t *testing.T) {
	s, repo, owner := taskFixture(t)
	ctx := context.Background()
	q := batchRequest(2)
	q.Rows[1].Device.ID = q.Rows[0].Device.ID
	checked, err := s.PreflightBatch(ctx, "tenant", owner, q)
	if err != nil || checked.Rows[1].Valid {
		t.Fatal(checked, err)
	}
	devices, _ := repo.ListManagedDevices(ctx, "tenant")
	if len(devices) != 0 {
		t.Fatal("preflight mutated registry")
	}
	q = batchRequest(2)
	checked, err = s.PreflightBatch(ctx, "tenant", owner, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Fingerprint = checked.Fingerprint
	q.Rows[0].Device.Name = "changed"
	if _, err = s.CreateBatch(ctx, "tenant", owner, q); statusOf(err) != 409 {
		t.Fatal("stale input accepted", err)
	}
	q = batchRequest(1)
	q.Rows[0].Connection = &EnrollConnection{Mode: ModeListener, Listener: &EnrollListener{Network: "tcp", Port: 8888}}
	checked, err = s.PreflightBatch(ctx, "tenant", owner, q)
	if err != nil || checked.Rows[0].Valid {
		t.Fatal("batch created public listener", checked, err)
	}
}

func TestDraftPurposeFiltersBeforePagination(t *testing.T) {
	s, _, owner := taskFixture(t)
	s.TemplateDraftAuthorize = s.Authorize
	ctx := context.Background()
	prep := DeviceDraft{Step: "preparation:0", Request: EnrollRequest{NewProduct: &NewProduct{Metadata: map[string]any{"preparationDraft": map[string]any{"product": map[string]any{"name": "模板草稿"}}}}}}
	for i := range 2 {
		if _, err := s.SaveDraft(ctx, "tenant", owner, fmt.Sprintf("z-template-%d", i), prep); err != nil {
			t.Fatal(err)
		}
	}
	linked := prep
	linked.Step = "preparation:linked"
	if _, err := s.SaveDraft(ctx, "tenant", owner, "linked-template", linked); err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		if _, err := s.SaveDraft(ctx, "tenant", owner, fmt.Sprintf("device-%02d", i), DeviceDraft{Step: "connection", Request: enrollRequest(fmt.Sprintf("d-%d", i))}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := s.ListDrafts(ctx, "tenant", owner, 1, 1, "preparation")
	if err != nil || total != 2 || len(items) != 1 || !strings.HasPrefix(items[0].ID, "z-template-") {
		t.Fatal("device drafts displaced template drafts", items, total, err)
	}
	items, total, err = s.ListDrafts(ctx, "tenant", owner, 20, 0, "device")
	if err != nil || total != 25 || len(items) != 20 {
		t.Fatal(items, total, err)
	}
	for _, item := range items {
		if !strings.HasPrefix(item.ID, "device-") {
			t.Fatal("template draft leaked into device page", item.ID)
		}
	}
	if _, _, err = s.ListDrafts(ctx, "tenant", owner, 20, 0, "invalid"); statusOf(err) != 422 {
		t.Fatal("invalid purpose accepted", err)
	}
	s.TemplateDraftAuthorize = func(ctx context.Context, _ string, _ TaskOwner) (context.Context, error) {
		return ctx, &EnrollError{Status: 403, Message: "no template permission"}
	}
	items, total, err = s.ListDrafts(ctx, "tenant", owner, 20, 0, "preparation")
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatal("purpose bypassed draft authorization", items, total, err)
	}
}

func TestBatchDurableResumeEncryptedSingleDeliveryAndPaging(t *testing.T) {
	s, repo, owner := taskFixture(t)
	ctx := context.Background()
	createTask(t, s, owner, batchRequest(3))
	// Exercise the production decorator shape: new store methods must survive
	// ClickHouse and Redis embedding without requiring either external service.
	s.Service.Repo = redisadapter.New(&clickhouse.Repository{Repository: repo}, nil)
	if err := s.RunBatch(ctx, "tenant", "job", "worker-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.RunBatch(ctx, "tenant", "job", "worker-b"); err != nil {
		t.Fatal(err)
	}
	summary, rows, err := s.Batch(ctx, "tenant", owner, "job", 1, 1)
	if err != nil || summary.Succeeded != 3 || summary.Pending != 0 || len(rows) != 1 || rows[0].Index != 1 {
		t.Fatal(summary, rows, err)
	}
	var wg sync.WaitGroup
	claimed := make(chan BatchCredentials, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := s.ClaimCredentials(ctx, "tenant", owner, "job", []int{0})
			if e != nil {
				t.Error(e)
			}
			claimed <- got
		}()
	}
	wg.Wait()
	close(claimed)
	count := 0
	var secret string
	for got := range claimed {
		count += len(got.Items)
		if len(got.Items) > 0 {
			secret = got.Items[0].Credential.Secret
		}
	}
	if count != 1 || secret == "" {
		t.Fatal("credential disclosed more/less than once", count)
	}
	for i := 0; i < 3; i++ {
		row, e := repo.GetOnboardingRecord(ctx, "tenant", batchRowID("job", i))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(row.Body), secret) || strings.Contains(string(row.Body), `"secret":`) {
			t.Fatal("plaintext delivery persisted")
		}
	}
	d, err := repo.GetManagedDevice(ctx, "tenant", "d-0")
	if err != nil || d.SecretHash != Hash(secret) {
		t.Fatal("delivered credential does not authenticate", err)
	}
	other := owner
	other.Username = "bob"
	if _, err = s.ClaimCredentials(ctx, "tenant", other, "job", []int{1}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross-owner credential", err)
	}
	r, _ := repo.GetOnboardingRecord(ctx, "tenant", batchRowID("job", 1))
	var row BatchRow
	_ = json.Unmarshal(r.Body, &row)
	row.Delivery.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	r.Body, _ = json.Marshal(row)
	_, _ = repo.SaveOnboardingRecord(ctx, r, r.Revision)
	got, err := s.ClaimCredentials(ctx, "tenant", owner, "job", []int{1})
	if err != nil || len(got.Items) != 0 || len(got.Unavailable) != 1 {
		t.Fatal("expired disclosure", got, err)
	}
	// Invalid selection is rejected before consuming any delivery.
	if _, err = s.ClaimCredentials(ctx, "tenant", owner, "job", []int{2, 2}); statusOf(err) != 422 {
		t.Fatal("duplicate claim accepted", err)
	}
	wrongKey := *s
	wrongKey.Key = strings.Repeat("rotated-key", 4)
	if got, err = wrongKey.ClaimCredentials(ctx, "tenant", owner, "job", []int{2}); err != nil || len(got.Items) != 0 || len(got.Unavailable) != 1 {
		t.Fatal("wrong key disclosed credential", got, err)
	}
	if got, err = s.ClaimCredentials(ctx, "tenant", owner, "job", []int{2}); err != nil || len(got.Items) != 1 {
		t.Fatal("invalid requests consumed delivery", got, err)
	}
	markers, _ := repo.ListPendingOnboardingRecords(ctx, deliveryExpiryKind, 100)
	for _, marker := range markers {
		var expiry deliveryExpiry
		_ = json.Unmarshal(marker.Body, &expiry)
		if expiry.RowID == batchRowID("job", 1) {
			expiry.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
			marker.Body, _ = json.Marshal(expiry)
			_, _ = repo.SaveOnboardingRecord(ctx, marker, marker.Revision)
		}
	}
	if err = s.ExpireCredentialDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	expired, _ := repo.GetOnboardingRecord(ctx, "tenant", batchRowID("job", 1))
	row = BatchRow{}
	_ = json.Unmarshal(expired.Body, &row)
	if row.Delivery != nil || row.CredentialStatus != "EXPIRED" {
		t.Fatal("expired ciphertext retained")
	}
}

type batchFailureRepository struct {
	ports.Repository
	failDevice  string
	failResult  bool
	enrollCalls int
}

func (r *batchFailureRepository) SaveOnboarding(ctx context.Context, b model.OnboardingBundle) error {
	r.enrollCalls++
	if b.Device.ID == r.failDevice {
		return errors.New("temporary storage outage")
	}
	return r.Repository.SaveOnboarding(ctx, b)
}

func TestBatchRecoversCommittedRegistrationAfterTemplateUpgrade(t *testing.T) {
	for _, test := range []struct {
		name              string
		total             int
		mismatchedRequest bool
	}{
		{name: "pause-remaining-rows", total: 2},
		{name: "complete-already-registered-task", total: 1},
		{name: "do-not-reuse-another-request", total: 2, mismatchedRequest: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, repo, owner := taskFixture(t)
			ctx := context.Background()
			failure := &batchFailureRepository{Repository: repo, failResult: true}
			s.Service.Repo = failure
			createTask(t, s, owner, batchRequest(test.total))
			if err := s.RunBatch(ctx, "tenant", "job", "worker"); err == nil {
				t.Fatal("injected result persistence failure did not occur")
			}
			device, err := repo.GetManagedDevice(ctx, "tenant", "d-0")
			if err != nil || device.OnboardingRequestHash == "" || device.SecretHash == "" {
				t.Fatal("atomic enrollment did not commit before the crash", err)
			}
			if test.mismatchedRequest {
				device.OnboardingRequestHash = Hash("another request owns this device")
				if err = repo.SaveManagedDevice(ctx, device); err != nil {
					t.Fatal(err)
				}
			}
			// Install a different published version and READY preparation while
			// the original task is interrupted. It must never enroll its remaining
			// frozen rows against this new configuration.
			product, err := repo.GetProduct(ctx, "tenant", "product")
			if err != nil {
				t.Fatal(err)
			}
			release, err := repo.GetProtocolRelease(ctx, "tenant", "iot-standard", "1.0.0")
			if err != nil {
				t.Fatal(err)
			}
			release.Version = "2.0.0"
			if err = repo.CreateProtocolRelease(ctx, release); err != nil {
				t.Fatal(err)
			}
			product.ProtocolPackageID = release.ProtocolID + "@" + release.Version
			if err = repo.SaveProduct(ctx, product); err != nil {
				t.Fatal(err)
			}
			if err = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: product.ID, ProtocolID: release.ProtocolID, Version: release.Version, UpdatedAt: time.Now().UnixMilli()}); err != nil {
				t.Fatal(err)
			}
			fingerprint, err := s.Service.TemplateFingerprint(ctx, "tenant", product.ID)
			if err != nil {
				t.Fatal(err)
			}
			record, prep, err := s.Service.TemplateRecord(ctx, "tenant", product.ID)
			if err != nil {
				t.Fatal(err)
			}
			prep.Fingerprint, prep.Verification.Fingerprint = fingerprint, fingerprint
			if _, err = s.Service.SaveTemplateRecord(ctx, record, prep); err != nil {
				t.Fatal(err)
			}
			if err = s.RunBatch(ctx, "tenant", "job", "restart"); err != nil {
				t.Fatal(err)
			}
			summary, rows, err := s.Batch(ctx, "tenant", owner, "job", 20, 0)
			if err != nil || len(rows) != test.total {
				t.Fatal(summary, rows, err)
			}
			if test.mismatchedRequest {
				if summary.Status != "PAUSED" || summary.Succeeded != 0 || rows[0].Status != "RUNNING" || rows[0].CredentialStatus != "NONE" {
					t.Fatal("recovered a device owned by another request", summary, rows)
				}
			} else {
				wantStatus := "PAUSED"
				if test.total == 1 {
					wantStatus = "COMPLETED"
				}
				if summary.Status != wantStatus || summary.Succeeded != 1 || summary.Pending != test.total-1 || rows[0].Status != "SUCCEEDED" || rows[0].CredentialStatus != "REISSUE_REQUIRED" {
					t.Fatal("committed registration was not recovered", summary, rows)
				}
			}
			after, err := repo.GetManagedDevice(ctx, "tenant", "d-0")
			if err != nil || failure.enrollCalls != 1 || after.AccessKey != device.AccessKey || after.SecretHash != device.SecretHash || after.OnboardingRequestHash != device.OnboardingRequestHash {
				t.Fatal("recovery reenrolled or mutated the existing device", failure.enrollCalls, err)
			}
			if _, err = repo.GetManagedDevice(ctx, "tenant", "d-1"); !errors.Is(err, model.ErrNotFound) {
				t.Fatal("old task enrolled remaining rows against the upgraded template", err)
			}
			credentials, err := s.ClaimCredentials(ctx, "tenant", owner, "job", []int{0})
			if err != nil || len(credentials.Items) != 0 || len(credentials.Unavailable) != 1 {
				t.Fatal("recovery generated or recovered a lost plaintext secret", err)
			}
		})
	}
}
func (r *batchFailureRepository) SaveOnboardingRecord(ctx context.Context, v model.OnboardingRecord, expected int64) (model.OnboardingRecord, error) {
	if r.failResult && strings.HasPrefix(v.Kind, "device-batch-row:") && v.Status == "SUCCEEDED" {
		r.failResult = false
		return model.OnboardingRecord{}, errors.New("crash after enrollment")
	}
	return r.Repository.SaveOnboardingRecord(ctx, v, expected)
}

func TestBatchFailedSubsetRetryAndCrashAfterEnrollment(t *testing.T) {
	s, repo, owner := taskFixture(t)
	ctx := context.Background()
	failure := &batchFailureRepository{Repository: repo, failDevice: "d-1"}
	s.Service.Repo = failure
	createTask(t, s, owner, batchRequest(3))
	if err := s.RunBatch(ctx, "tenant", "job", "worker"); err != nil {
		t.Fatal(err)
	}
	summary, _, _ := s.Batch(ctx, "tenant", owner, "job", 20, 0)
	if summary.Status != "PARTIAL_FAILED" || summary.Failed != 1 || summary.Succeeded != 2 {
		t.Fatal(summary)
	}
	if _, err := s.RetryBatch(ctx, "tenant", owner, "job", summary.Revision, []int{0}); statusOf(err) != 422 {
		t.Fatal("success retried", err)
	}
	failure.failDevice = ""
	failure.failResult = true
	if _, err := s.RetryBatch(ctx, "tenant", owner, "job", summary.Revision, []int{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunBatch(ctx, "tenant", "job", "worker"); err == nil {
		t.Fatal("injected crash did not occur")
	}
	if err := s.RunBatch(ctx, "tenant", "job", "restart"); err != nil {
		t.Fatal(err)
	}
	summary, rows, err := s.Batch(ctx, "tenant", owner, "job", 20, 0)
	if err != nil || summary.Succeeded != 3 || summary.Failed != 0 || rows[1].CredentialStatus != "REISSUE_REQUIRED" {
		t.Fatal(summary, rows, err)
	}
	devices, _ := repo.ListManagedDevices(ctx, "tenant")
	if len(devices) != 3 {
		t.Fatal("retry duplicated devices", len(devices))
	}
}

func TestBatchReauthorizesEveryRowAndPinsTemplate(t *testing.T) {
	s, repo, owner := taskFixture(t)
	ctx := context.Background()
	createTask(t, s, owner, batchRequest(2))
	baseAuth := s.Authorize
	s.Authorize = func(ctx context.Context, tenant string, o TaskOwner) (context.Context, error) {
		if _, err := repo.GetManagedDevice(ctx, tenant, "d-0"); err == nil {
			return ctx, errors.New("permission revoked")
		}
		return baseAuth(ctx, tenant, o)
	}
	if err := s.RunBatch(ctx, "tenant", "job", "worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetManagedDevice(ctx, "tenant", "d-1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("revoked user enrolled second device", err)
	}
	s.Authorize = baseAuth
	summary, _, _ := s.Batch(ctx, "tenant", owner, "job", 20, 0)
	if summary.Status != "PAUSED" || summary.Succeeded != 1 || summary.Pending != 1 {
		t.Fatal(summary)
	}
	s.Fingerprint = func(context.Context, string, string) (string, error) { return "new-template", nil }
	if _, err := s.RetryBatch(ctx, "tenant", owner, "job", summary.Revision, []int{1}); statusOf(err) != 409 {
		t.Fatal("old task accepted changed template", err)
	}
}

func TestBatchPinsTemplateAcrossEnrollmentRead(t *testing.T) {
	for _, change := range []string{"template", "public-address"} {
		t.Run(change, func(t *testing.T) {
			s, repo, owner := taskFixture(t)
			ctx := context.Background()
			createTask(t, s, owner, batchRequest(1))
			checks := 0
			s.Fingerprint = func(ctx context.Context, tenant, product string) (string, error) {
				fp, err := s.Service.TemplateFingerprint(ctx, tenant, product)
				checks++
				if err != nil || checks != 2 {
					return fp, err
				}
				// Change a READY template after the worker's last fingerprint read,
				// before Enroll assembles its atomic repository snapshot.
				if change == "public-address" {
					s.Service.PublicHTTP = "https://changed.example.test"
				} else {
					p, e := repo.GetProduct(ctx, tenant, product)
					if e != nil {
						return "", e
					}
					p.Metadata = map[string]any{"identity": map[string]any{"field": "serial"}}
					if e = repo.SaveProduct(ctx, p); e != nil {
						return "", e
					}
				}
				updated, e := s.Service.TemplateFingerprint(ctx, tenant, product)
				if e != nil {
					return "", e
				}
				rec, prep, e := s.Service.TemplateRecord(ctx, tenant, product)
				if e != nil {
					return "", e
				}
				prep.Fingerprint = updated
				prep.Verification.Fingerprint = updated
				_, e = s.Service.SaveTemplateRecord(ctx, rec, prep)
				return fp, e
			}
			if err := s.RunBatch(ctx, "tenant", "job", "worker"); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.GetManagedDevice(ctx, "tenant", "d-0"); !errors.Is(err, model.ErrNotFound) {
				t.Fatal("old task enrolled against a changed template", err)
			}
			summary, rows, err := s.Batch(ctx, "tenant", owner, "job", 20, 0)
			if err != nil || summary.Failed != 1 || len(rows) != 1 || rows[0].Status != "FAILED" {
				t.Fatal(summary, rows, err)
			}
		})
	}
}

func TestBatchCanResumeSelectedRowsAfterPausingBeforeInitialization(t *testing.T) {
	s, repo, owner := taskFixture(t)
	ctx := context.Background()
	createTask(t, s, owner, batchRequest(3))
	baseAuth := s.Authorize
	s.Authorize = func(ctx context.Context, _ string, _ TaskOwner) (context.Context, error) {
		return ctx, errors.New("permission revoked")
	}
	if err := s.RunBatch(ctx, "tenant", "job", "worker"); err != nil {
		t.Fatal(err)
	}
	s.Authorize = baseAuth
	summary, rows, err := s.Batch(ctx, "tenant", owner, "job", 1, 1)
	if err != nil || summary.Status != "PAUSED" || len(rows) != 1 || rows[0].Index != 1 || rows[0].Status != "PENDING" {
		t.Fatal("uninitialized task lost its pending inputs", summary, rows, err)
	}
	if _, err = s.RetryBatch(ctx, "tenant", owner, "job", summary.Revision, []int{1}); err != nil {
		t.Fatal(err)
	}
	if err = s.RunBatch(ctx, "tenant", "job", "restart"); err != nil {
		t.Fatal(err)
	}
	summary, rows, err = s.Batch(ctx, "tenant", owner, "job", 20, 0)
	if err != nil || summary.Status != "PAUSED" || summary.Succeeded != 1 || summary.Pending != 2 || rows[0].Status != "PENDING" || rows[1].Status != "SUCCEEDED" || rows[2].Status != "PENDING" {
		t.Fatal("resume executed rows outside the selected page", summary, rows, err)
	}
	devices, err := repo.ListManagedDevices(ctx, "tenant")
	if err != nil || len(devices) != 1 || devices[0].ID != "d-1" {
		t.Fatal("unexpected enrolled subset", devices, err)
	}
	if _, err = s.RetryBatch(ctx, "tenant", owner, "job", summary.Revision, []int{0, 2}); err != nil {
		t.Fatal(err)
	}
	if err = s.RunBatch(ctx, "tenant", "job", "restart"); err != nil {
		t.Fatal(err)
	}
	summary, _, err = s.Batch(ctx, "tenant", owner, "job", 20, 0)
	if err != nil || summary.Status != "COMPLETED" || summary.Succeeded != 3 || summary.Pending != 0 {
		t.Fatal(summary, err)
	}
}
