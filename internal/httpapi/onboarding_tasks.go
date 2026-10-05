package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

func (s *Server) onboardingTaskRoutes() {
	s.router.GET("/api/v1/onboarding/drafts", s.authorize("operator"), s.endpoint(s.listOnboardingDrafts))
	s.router.GET("/api/v1/onboarding/drafts/:id", s.authorize("operator"), s.endpoint(s.getOnboardingDraft, "id"))
	s.router.PUT("/api/v1/onboarding/drafts/:id", s.authorize("operator"), s.endpoint(s.saveOnboardingDraft, "id"))
	s.router.POST("/api/v1/onboarding/batches/preflight", s.authorize("operator"), s.endpoint(s.preflightOnboardingBatch))
	s.router.POST("/api/v1/onboarding/batches", s.authorize("operator"), s.endpoint(s.createOnboardingBatch))
	s.router.GET("/api/v1/onboarding/batches", s.authorize("operator"), s.endpoint(s.listOnboardingBatches))
	s.router.GET("/api/v1/onboarding/batches/:id", s.authorize("operator"), s.endpoint(s.getOnboardingBatch, "id"))
	s.router.POST("/api/v1/onboarding/batches/:id/retry", s.authorize("operator"), s.endpoint(s.retryOnboardingBatch, "id"))
	s.router.POST("/api/v1/onboarding/batches/:id/credentials", s.authorize("operator"), s.endpoint(s.claimOnboardingBatchCredentials, "id"))
}

func taskOwner(r *http.Request) onboarding.TaskOwner {
	c := claims(r)
	return onboarding.TaskOwner{Username: c.Username, Managed: c.TokenUse == "user", SessionVersion: c.SessionVersion}
}

// authorizeOnboardingTask resolves the live account on each worker row and
// credential claim. No queued job inherits a stale browser authorization map.
func (s *Server) authorizeOnboardingTask(ctx context.Context, tenant string, owner onboarding.TaskOwner) (context.Context, error) {
	denied := &onboarding.EnrollError{Status: 403, Message: "当前账号不能登记设备或设备范围已发生变化"}
	if !owner.Managed {
		if owner.Username != s.cfg.AdminUser || !adminTenantAllowed(s.cfg.AdminTenants, tenant) {
			return ctx, denied
		}
		return context.WithValue(ctx, deviceScopeKey{}, deviceScope{Tenant: tenant, All: true}), nil
	}
	user, p, err := s.managedIdentity(ctx, auth.Claims{TenantID: tenant, Username: owner.Username, TokenUse: "user", SessionVersion: owner.SessionVersion})
	if err != nil || user.DeviceScope != "all" || !allowsRoute(p, "POST", "/api/v1/device-registry") {
		return ctx, denied
	}
	ctx = context.WithValue(ctx, deviceScopeKey{}, s.scopeFor(user, p, tenant))
	ctx = context.WithValue(ctx, permissionsKey{}, p)
	return ctx, nil
}

func (s *Server) onboardingTasks() *onboarding.TaskService {
	return &onboarding.TaskService{Service: s.onboarding, Key: s.cfg.JWTSecret, Authorize: s.authorizeOnboardingTask, TemplateDraftAuthorize: s.authorizeTemplateDraft, Fingerprint: func(ctx context.Context, tenant, product string) (string, error) {
		_, ready, err := s.onboarding.TemplateReadiness(ctx, tenant, product)
		if err != nil {
			return "", err
		}
		if !ready {
			return "", &onboarding.EnrollError{Status: 409, Message: "请先完成设备模板的首台验证，再批量登记"}
		}
		return s.onboarding.TemplateFingerprint(ctx, tenant, product)
	}}
}

func (s *Server) authorizeTemplateDraft(ctx context.Context, tenant string, owner onboarding.TaskOwner) (context.Context, error) {
	if !owner.Managed {
		return s.authorizeOnboardingTask(ctx, tenant, owner)
	}
	u, p, err := s.managedIdentity(ctx, auth.Claims{TenantID: tenant, Username: owner.Username, TokenUse: "user", SessionVersion: owner.SessionVersion})
	if err != nil || u.DeviceScope != "all" || !p["menu:products"] || !(allowsRoute(p, "POST", "/api/v1/products") || allowsRoute(p, "PUT", "/api/v1/products/:id")) {
		return ctx, &onboarding.EnrollError{Status: 403, Message: "当前账号不能编辑设备模板或设备范围已变化"}
	}
	ctx = context.WithValue(ctx, deviceScopeKey{}, s.scopeFor(u, p, tenant))
	return context.WithValue(ctx, permissionsKey{}, p), nil
}

// RunOnboardingTasks is attached to the process lifetime, not an HTTP request.
func (s *Server) RunOnboardingTasks(ctx context.Context) { s.onboardingTasks().Run(ctx) }

func (s *Server) onboardingTaskProblem(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, model.ErrOnboardingChanged) {
		problem(w, 409, err.Error())
		return
	}
	if errors.Is(err, model.ErrNotFound) {
		problem(w, 404, "接入记录不存在或无权访问")
		return
	}
	var e *onboarding.EnrollError
	if errors.As(err, &e) {
		problem(w, e.Status, e.Message)
		return
	}
	s.failure(w, r, err, "接入任务暂时不可用，请稍后重试")
}
func taskPage(r *http.Request) (int, int) {
	page := parseListPagination(r)
	return page.PageSize, page.Offset
}
func (s *Server) listOnboardingDrafts(w http.ResponseWriter, r *http.Request) {
	limit, offset := taskPage(r)
	items, total, err := s.onboardingTasks().ListDrafts(r.Context(), claims(r).TenantID, taskOwner(r), limit, offset, r.URL.Query().Get("purpose"))
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) listOnboardingBatches(w http.ResponseWriter, r *http.Request) {
	owner := taskOwner(r)
	ctx, err := s.authorizeOnboardingTask(r.Context(), claims(r).TenantID, owner)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	limit, offset := taskPage(r)
	rows, total, err := s.engine.Repo.ListOnboardingRecords(ctx, claims(r).TenantID, owner.Username, onboarding.BatchKind, limit, offset)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	items := []onboarding.BatchSummary{}
	for _, v := range rows {
		item, e := onboarding.BatchRecordSummary(v)
		if e != nil {
			s.onboardingTaskProblem(w, r, e)
			return
		}
		items = append(items, item)
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) getOnboardingDraft(w http.ResponseWriter, r *http.Request) {
	v, err := s.onboardingTasks().OwnedRecord(r.Context(), claims(r).TenantID, taskOwner(r), r.PathValue("id"), onboarding.DraftKind)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, v)
}
func (s *Server) saveOnboardingDraft(w http.ResponseWriter, r *http.Request) {
	var d onboarding.DeviceDraft
	if decode(w, r, &d) != nil {
		return
	}
	v, err := s.onboardingTasks().SaveDraft(r.Context(), claims(r).TenantID, taskOwner(r), r.PathValue("id"), d)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, v)
}
func (s *Server) preflightOnboardingBatch(w http.ResponseWriter, r *http.Request) {
	var q onboarding.BatchRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.onboardingTasks().PreflightBatch(r.Context(), claims(r).TenantID, taskOwner(r), q)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, v)
}
func (s *Server) createOnboardingBatch(w http.ResponseWriter, r *http.Request) {
	var q onboarding.BatchRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.onboardingTasks().CreateBatch(r.Context(), claims(r).TenantID, taskOwner(r), q)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	s.audit(r, "device.batch.create", "onboarding", v.ID, map[string]any{"productId": v.ProductID, "count": v.Total})
	w.Header().Set("Cache-Control", "no-store")
	write(w, 202, v)
}
func (s *Server) getOnboardingBatch(w http.ResponseWriter, r *http.Request) {
	limit, offset := taskPage(r)
	if r.URL.Query().Get("limit") == "" || limit > 30 {
		limit = 30
	}
	v, rows, err := s.onboardingTasks().Batch(r.Context(), claims(r).TenantID, taskOwner(r), r.PathValue("id"), limit, offset)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	verification, err := s.enrichOnboardingRows(r.Context(), claims(r).TenantID, v.ProductID, rows)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, struct {
		onboarding.BatchSummary
		Rows                []onboarding.BatchPublicRow `json:"rows"`
		VerificationSummary batchVerificationSummary    `json:"verificationSummary"`
	}{v, rows, verification})
}
func (s *Server) retryOnboardingBatch(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Revision int64 `json:"revision"`
		Indices  []int `json:"indices"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.onboardingTasks().RetryBatch(r.Context(), claims(r).TenantID, taskOwner(r), r.PathValue("id"), q.Revision, q.Indices)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	s.audit(r, "device.batch.retry", "onboarding", v.ID, map[string]any{"count": len(q.Indices)})
	w.Header().Set("Cache-Control", "no-store")
	write(w, 202, v)
}
func (s *Server) claimOnboardingBatchCredentials(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Indices []int `json:"indices"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	v, err := s.onboardingTasks().ClaimCredentials(ctx, claims(r).TenantID, taskOwner(r), r.PathValue("id"), q.Indices)
	if err != nil {
		s.onboardingTaskProblem(w, r, err)
		return
	}
	for i := range v.Items {
		device, e := s.engine.Repo.GetManagedDevice(ctx, claims(r).TenantID, v.Items[i].DeviceID)
		if e != nil {
			continue
		}
		product, e := s.engine.Repo.GetProduct(ctx, claims(r).TenantID, device.ProductID)
		if e == nil {
			v.Items[i].AccessInfo = s.deviceAccessInfo(device, product)
		}
	}
	s.audit(r, "device.batch.credentials.claim", "onboarding", r.PathValue("id"), map[string]any{"count": len(v.Items)})
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, v)
}

type batchVerificationSummary struct {
	Scope                string `json:"scope"`
	Total                int    `json:"total"`
	Registered           int    `json:"registered"`
	WaitingConfiguration int    `json:"waitingConfiguration"`
	WaitingVerification  int    `json:"waitingVerification"`
	Verified             int    `json:"verified"`
	Errors               int    `json:"errors"`
	Pending              int    `json:"pending"`
}

// Batch pages join persisted acceptance snapshots. They never perform a raw
// history scan per device; a user starts/refetches evidence on the device page.
func (s *Server) enrichOnboardingRows(ctx context.Context, tenant, productID string, rows []onboarding.BatchPublicRow) (batchVerificationSummary, error) {
	out := batchVerificationSummary{Scope: "page", Total: len(rows)}
	product, err := s.engine.Repo.GetProduct(ctx, tenant, productID)
	configurationMissing := errors.Is(err, model.ErrNotFound)
	if err != nil && !configurationMissing {
		return out, err
	}
	fingerprint := ""
	var templateSince int64
	if !configurationMissing {
		fingerprint, err = s.onboarding.TemplateFingerprint(ctx, tenant, productID)
		if errors.Is(err, model.ErrNotFound) {
			configurationMissing = true
		} else if err != nil {
			return out, err
		}
		_, preparation, e := s.onboarding.TemplateRecord(ctx, tenant, productID)
		if e != nil {
			return out, e
		}
		templateSince = preparation.AppliedAt
		if binding, e := s.engine.Repo.GetProductProtocolBinding(ctx, tenant, productID); e == nil {
			templateSince = max(templateSince, binding.UpdatedAt)
		} else if !errors.Is(e, model.ErrNotFound) {
			return out, e
		}
	}
	profiles := map[string]model.DeviceAccessProfile{}
	for i := range rows {
		row := &rows[i]
		row.OnboardingStatus = "REGISTERED"
		if row.Status == "FAILED" {
			row.OnboardingStatus = "ERROR"
			out.Errors++
			continue
		}
		if row.Status != "SUCCEEDED" {
			row.OnboardingStatus = ""
			out.Pending++
			continue
		}
		d, e := s.engine.Repo.GetManagedDevice(ctx, tenant, row.DeviceID)
		if e != nil {
			if !errors.Is(e, model.ErrNotFound) {
				return out, e
			}
			row.OnboardingStatus = "ERROR"
			out.Errors++
			continue
		}
		row.OnboardingStatus = "WAITING_VERIFICATION"
		since := max(d.UpdatedAt, templateSince)
		configurationError := configurationMissing || d.ProductID != productID || product.Status != "ENABLED" || d.Status != "ENABLED"
		waitingConfiguration := false
		if d.UsesPlatformCredentials(product) {
			row.AccessInfo = s.deviceAccessInfo(d, product)
			address := "httpUrl"
			if d.Connector == "MQTT" {
				address = "mqttBroker"
			}
			waitingConfiguration = row.AccessInfo[address] == "" || row.CredentialStatus == "AVAILABLE" || row.CredentialStatus == "EXPIRED" || row.CredentialStatus == "REISSUE_REQUIRED"
		} else if d.ConnectorProfileID != "" {
			profile, found := profiles[d.ConnectorProfileID]
			if !found {
				profile, e = s.engine.Repo.GetDeviceAccessProfile(ctx, tenant, d.ConnectorProfileID)
				if e != nil {
					if !errors.Is(e, model.ErrNotFound) {
						return out, e
					}
					configurationError = true
				}
				profiles[d.ConnectorProfileID] = profile
			}
			since = max(since, profile.UpdatedAt)
			if profile.ID == "" {
				configurationError = true
			}
			if profile.ID != "" {
				configurationError = configurationError || !profile.Enabled || profile.ProductID != productID || profile.RuntimeStatus == "ERROR" || profile.RuntimeStatus == "UNSUPPORTED"
				host := profile.Host
				if profile.Mode == "listener" && profile.ConnectionMode != "dial" {
					host = profile.PublicHost
				}
				waitingConfiguration = host == ""
				row.AccessInfo = map[string]any{"kind": row.Mode, "network": profile.Network, "host": host, "port": profile.Port, "unitId": profile.UnitID, "deviceId": d.ID, "profileId": profile.ID}
			}
		} else {
			waitingConfiguration = true
		}
		if waitingConfiguration {
			row.OnboardingStatus = "WAITING_CONFIGURATION"
		}
		verification, e := s.engine.Repo.GetOnboardingRecord(ctx, tenant, "verification:"+d.ID)
		if e != nil && !errors.Is(e, model.ErrNotFound) {
			return out, e
		}
		if e == nil && verification.Kind == "device-verification" {
			var saved model.DeviceVerification
			if e = json.Unmarshal(verification.Body, &saved); e != nil {
				return out, e
			}
			profileMatches := d.ConnectorProfileID == "" || saved.ProfileID == d.ConnectorProfileID && saved.ProfileFingerprint == profiles[d.ConnectorProfileID].ConfigurationFingerprint()
			if saved.DeviceID == d.ID && saved.ProductID == productID && saved.Fingerprint == fingerprint && saved.CheckedAt >= since && profileMatches {
				if saved.Status == "VERIFIED" && saved.VerifiedAt > 0 {
					row.OnboardingStatus = "VERIFIED"
					row.VerificationAt = saved.VerifiedAt
				} else if saved.Status == "ERROR" || saved.Status == "FAILED" {
					row.OnboardingStatus = "ERROR"
				}
			}
		}
		if configurationError {
			row.OnboardingStatus = "ERROR"
		}
		switch row.OnboardingStatus {
		case "REGISTERED":
			out.Registered++
		case "WAITING_CONFIGURATION":
			out.WaitingConfiguration++
		case "WAITING_VERIFICATION":
			out.WaitingVerification++
		case "VERIFIED":
			out.Verified++
		case "ERROR":
			out.Errors++
		}
	}
	return out, nil
}
