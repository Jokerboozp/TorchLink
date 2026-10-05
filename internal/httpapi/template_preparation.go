package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
)

func (s *Server) templatePreparationRoutes() {
	s.router.GET("/api/v1/products/:id/preparation", s.authorize("viewer"), s.endpoint(s.templatePreparation, "id"))
	s.router.PUT("/api/v1/products/:id/preparation", s.authorize("operator"), s.endpoint(s.saveTemplatePreparation, "id"))
	s.router.POST("/api/v1/products/:id/preparation/apply", s.authorize("operator"), s.endpoint(s.applyTemplatePreparation, "id"))
	s.router.POST("/api/v1/products/:id/preparation/trial", s.authorize("operator"), s.endpoint(s.trialTemplatePreparation, "id"))
	s.router.POST("/api/v1/products/:id/preparation/rollback", s.authorize("operator"), s.endpoint(s.rollbackTemplatePreparation, "id"))
	s.router.POST("/api/v1/products/:id/verification", s.authorize("operator"), s.endpoint(s.verifyTemplate, "id"))
	s.router.GET("/api/v1/device-registry/:id/verification", s.authorize("viewer"), s.endpoint(s.verifyRegisteredDevice, "id"))
	s.router.POST("/api/v1/device-registry/:id/verification", s.authorize("operator"), s.endpoint(s.verifyRegisteredDevice, "id"))
}

func (s *Server) preparationProblem(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, model.ErrOnboardingChanged) || errors.Is(err, model.ErrBindingChanged) {
		problem(w, 409, "模板或流程已被其他操作修改，请刷新后继续")
		return
	}
	if errors.Is(err, model.ErrNotFound) {
		problem(w, 404, "设备模板或关联记录不存在")
		return
	}
	s.enrollProblem(w, r, err)
}

func (s *Server) templatePreparation(w http.ResponseWriter, r *http.Request) {
	tenant, id := claims(r).TenantID, r.PathValue("id")
	current, err := s.onboarding.CurrentCandidate(r.Context(), tenant, id)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	rec, prep, err := s.onboarding.TemplateRecord(r.Context(), tenant, id)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	status, ready, err := s.onboarding.TemplateReadiness(r.Context(), tenant, id)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	if rec.Revision == 0 {
		prep.Candidate = current
	}
	_, total, err := s.engine.Repo.ListManagedDevicesFiltered(r.Context(), ports.DeviceFilter{TenantID: tenant, RestrictProducts: true, ProductIDs: []string{id}}, 1, 0)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	runtime := "PENDING"
	if prep.Verification != nil && prep.Verification.Fingerprint == s.onboarding.CandidateFingerprint(current) && prep.Verification.Status == "VERIFIED" {
		runtime = "FIELD_CONFIRMED"
	}
	write(w, 200, map[string]any{"revision": rec.Revision, "product": current.Product, "candidate": prep.Candidate, "candidateFingerprint": s.onboarding.CandidateFingerprint(prep.Candidate), "applied": map[string]any{"fingerprint": s.onboarding.CandidateFingerprint(current), "appliedAt": prep.AppliedAt, "runtimeStatus": runtime, "trialVerification": prep.AppliedTrialVerification}, "status": status, "reusable": ready, "verification": prep.Verification, "trialProductId": prep.TrialProductID, "history": prep.History, "affectedDevices": total})
}

func (s *Server) normalizeCandidate(r *http.Request, id string, v model.TemplateCandidate) (model.TemplateCandidate, model.ProtocolRelease, error) {
	tenant := claims(r).TenantID
	current, err := s.engine.Repo.GetProduct(r.Context(), tenant, id)
	if err != nil {
		return v, model.ProtocolRelease{}, err
	}
	v.Product.ID = id
	v.Product.TenantID = tenant
	v.Product.CreatedAt = current.CreatedAt
	v.Product.PreparationStatus = ""
	v.Product.Reusable = false
	if strings.TrimSpace(v.Product.Name) == "" {
		return v, model.ProtocolRelease{}, &onboarding.EnrollError{Status: 422, Message: "请填写设备模板名称"}
	}
	if v.ProtocolID != "" && v.Version != "" {
		v.Product.ProtocolPackageID = v.ProtocolID + "@" + v.Version
	}
	if err = onboarding.ValidateThingModel(v.Product.ThingModel); err != nil {
		return v, model.ProtocolRelease{}, &onboarding.EnrollError{Status: 422, Message: err.Error()}
	}
	if err = model.ValidateDeviceTiming(v.Product.ReportIntervalSec, v.Product.OfflineToleranceSec); err != nil {
		return v, model.ProtocolRelease{}, &onboarding.EnrollError{Status: 422, Message: err.Error()}
	}
	v.VerificationRules, err = onboarding.NormalizeVerificationRules(v.VerificationRules)
	if err != nil {
		return v, model.ProtocolRelease{}, err
	}
	v.Product.VerificationRules = &v.VerificationRules
	plan, err := s.onboarding.DraftPlan(r.Context(), tenant, v.Product)
	if err != nil {
		return v, model.ProtocolRelease{}, err
	}
	if plan.Release == nil || plan.Mode == onboarding.ModeUnsupported {
		return v, model.ProtocolRelease{}, &onboarding.EnrollError{Status: 422, Message: plan.Reason}
	}
	release := *plan.Release
	if err = s.validateTemplateDeviceProfiles(r, &release, id); err != nil {
		return v, release, &onboarding.EnrollError{Status: 422, Message: err.Error()}
	}
	v.ProtocolID = release.ProtocolID
	v.Version = release.Version
	if v.Product.Transport == "" {
		v.Product.Transport = release.Transport
	}
	v.Product.PayloadFormat = release.PayloadFormat
	if v.Product.Status == "" {
		v.Product.Status = "ENABLED"
	}
	if v.Product.Status != "ENABLED" && v.Product.Status != "DISABLED" {
		return v, release, &onboarding.EnrollError{Status: 422, Message: "模板启用状态无效"}
	}
	if len(v.Profiles) > 16 {
		return v, release, &onboarding.EnrollError{Status: 422, Message: "一个模板最多配置 16 个共享接入点"}
	}
	seen, portsSeen := map[string]bool{}, map[string]bool{}
	for i := range v.Profiles {
		p := &v.Profiles[i]
		p.TenantID = tenant
		p.ProductID = id
		p.ProtocolID = release.ProtocolID
		p.ProtocolVersion = release.Version
		if p.DeviceID != "" || p.ConnectionMode == "dial" || p.Mode != "listener" {
			return v, release, &onboarding.EnrollError{Status: 422, Message: "模板只保存共享监听；主动连接地址和 Modbus 站号在设备登记时填写"}
		}
		if seen[p.ID] {
			return v, release, &onboarding.EnrollError{Status: 422, Message: "接入点标识重复"}
		}
		seen[p.ID] = true
		address := fmt.Sprintf("%s:%d", p.Network, p.Port)
		if p.Enabled && portsSeen[address] {
			return v, release, &onboarding.EnrollError{Status: 422, Message: "共享监听端口重复"}
		}
		portsSeen[address] = p.Enabled
		if err = validateAccessProfile(*p); err != nil {
			return v, release, &onboarding.EnrollError{Status: 422, Message: err.Error()}
		}
		if !listenerSupports(release, p.Network) {
			return v, release, &onboarding.EnrollError{Status: 422, Message: "协议版本不支持所选 TCP/UDP 接入方式"}
		}
		if len(p.Queries) > 0 && !protocolworker.HasCapability(release, "encode") {
			return v, release, &onboarding.EnrollError{Status: 422, Message: "定时查询需要协议具备 encode 能力"}
		}
		if err = onboarding.ValidateChildProducts(r.Context(), s.engine.Repo, *p, release); err != nil {
			return v, release, &onboarding.EnrollError{Status: 422, Message: err.Error()}
		}
		p.RuntimeStatus = ""
		p.LastError = ""
		p.LastSuccessAt = 0
		p.LastErrorAt = 0
	}
	return v, release, nil
}

func (s *Server) saveTemplatePreparation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision  int64                   `json:"revision"`
		Candidate model.TemplateCandidate `json:"candidate"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	tenant, id := claims(r).TenantID, r.PathValue("id")
	rec, prep, err := s.onboarding.TemplateRecord(r.Context(), tenant, id)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	if rec.Revision != in.Revision {
		s.preparationProblem(w, r, model.ErrOnboardingChanged)
		return
	}
	candidate := in.Candidate
	if _, err = s.engine.Repo.GetProduct(r.Context(), tenant, id); err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	candidate.Product.ID = id
	candidate.Product.TenantID = tenant
	candidate.Product.PreparationStatus = ""
	candidate.Product.Reusable = false
	if err = onboarding.ValidatePreparationDraft(candidate); err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	prep.Candidate = candidate
	if prep.Fingerprint == "" {
		prep.Fingerprint, err = s.onboarding.TemplateFingerprint(r.Context(), tenant, id)
		if err != nil {
			s.preparationProblem(w, r, err)
			return
		}
	}
	if prep.TrialFingerprint != s.onboarding.CandidateFingerprint(candidate) {
		prep.TrialProductID = ""
		prep.TrialFingerprint = ""
		prep.TrialConfigFingerprint = ""
	}
	_, err = s.onboarding.SaveTemplateRecord(r.Context(), rec, prep)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	s.templatePreparation(w, r)
}

func (s *Server) applyTemplatePreparation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision int64 `json:"revision"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if err := s.applyPreparedCandidate(r, in.Revision, false); err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	s.templatePreparation(w, r)
}

func (s *Server) applyPreparedCandidate(r *http.Request, revision int64, rollback bool) error {
	tenant, id := claims(r).TenantID, r.PathValue("id")
	rec, prep, err := s.onboarding.TemplateRecord(r.Context(), tenant, id)
	if err != nil {
		return err
	}
	if rec.Revision != revision || revision == 0 {
		return model.ErrOnboardingChanged
	}
	current, err := s.onboarding.CurrentCandidate(r.Context(), tenant, id)
	if err != nil {
		return err
	}
	candidate, release, err := s.normalizeCandidate(r, id, prep.Candidate)
	if err != nil {
		return err
	}
	oldFP, newFP := s.onboarding.CandidateFingerprint(current), s.onboarding.CandidateFingerprint(candidate)
	var trialVerification *model.DeviceVerification
	if len(candidate.Profiles) > 0 || len(current.Profiles) > 0 {
		if !requestAllows(r, "POST", "/api/v2/device-access-profiles") || !requestAllows(r, "PUT", "/api/v2/device-access-profiles/:id") {
			return &onboarding.EnrollError{Status: 403, Message: "当前账号没有配置共享接入点的权限"}
		}
	}
	if oldFP != newFP && !rollback {
		_, count, e := s.engine.Repo.ListManagedDevicesFiltered(r.Context(), ports.DeviceFilter{TenantID: tenant, RestrictProducts: true, ProductIDs: []string{id}}, 1, 0)
		if e != nil {
			return e
		}
		if count > 0 {
			if prep.TrialProductID == "" || prep.TrialFingerprint != newFP {
				return &onboarding.EnrollError{Status: 409, Message: "已有设备正在使用此模板，请先在隔离试验模板上完成候选配置的实机验证"}
			}
			trialFP, e := s.onboarding.TemplateFingerprint(r.Context(), tenant, prep.TrialProductID)
			if e != nil {
				return e
			}
			if trialFP != prep.TrialConfigFingerprint || prep.TrialConfigFingerprint == "" {
				return &onboarding.EnrollError{Status: 409, Message: "试验模板配置已改变，请重新创建隔离试验并验证本次候选配置"}
			}
			_, ready, e := s.onboarding.TemplateReadiness(r.Context(), tenant, prep.TrialProductID)
			if e != nil {
				return e
			}
			if !ready {
				return &onboarding.EnrollError{Status: 409, Message: "隔离试验模板尚未通过实机验收"}
			}
			_, trialPrep, e := s.onboarding.TemplateRecord(r.Context(), tenant, prep.TrialProductID)
			if e != nil {
				return e
			}
			if trialPrep.Verification == nil || trialPrep.Verification.Status != "VERIFIED" || trialPrep.Verification.Fingerprint != trialFP {
				return model.ErrOnboardingChanged
			}
			trialVerification = trialPrep.Verification
		}
	}
	operationalChange := oldFP != newFP || prep.AppliedAt == 0
	now := time.Now().UnixMilli()
	candidate.Product.UpdatedAt = now
	oldProfiles := map[string]model.DeviceAccessProfile{}
	for _, p := range current.Profiles {
		oldProfiles[p.ID] = p
	}
	for i := range candidate.Profiles {
		p := &candidate.Profiles[i]
		p.CreatedAt = oldProfiles[p.ID].CreatedAt
		if p.CreatedAt == 0 {
			p.CreatedAt = now
		}
		p.UpdatedAt = now
		p.RuntimeStatus = "PENDING"
	}
	if !operationalChange {
		candidate.Profiles = current.Profiles
	}
	if release.ProtocolID == parser.StandardProtocolID {
		if _, e := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, release.ProtocolID, release.Version); errors.Is(e, model.ErrNotFound) {
			if e = s.engine.Repo.CreateProtocolRelease(r.Context(), release); e != nil {
				return e
			}
		}
	}
	var expected *model.ProductProtocolBinding
	if b, e := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, id); e == nil {
		expected = &b
	} else if !errors.Is(e, model.ErrNotFound) {
		return e
	}
	binding := model.ProductProtocolBinding{TenantID: tenant, ProductID: id, ProtocolID: release.ProtocolID, Version: release.Version, UpdatedAt: now}
	if expected != nil {
		binding.PreviousProtocolID = expected.ProtocolID
		binding.PreviousVersion = expected.Version
	}
	if operationalChange {
		prep.History = append(prep.History, model.TemplateRevision{Revision: rec.Revision, Fingerprint: oldFP, AppliedAt: prep.AppliedAt, Candidate: current, Verification: prep.Verification, AppliedTrialVerification: prep.AppliedTrialVerification})
		prep.AppliedAt = now
		prep.Status = "AWAITING_VALIDATION"
		prep.Verification = nil
		prep.AppliedTrialVerification = trialVerification
	} else if expected != nil {
		binding = *expected
	}
	prep.Candidate = candidate
	prep.Fingerprint = newFP
	rec.Body, err = json.Marshal(prep)
	if err != nil {
		return err
	}
	rec.Status = prep.Status
	err = s.engine.Repo.SwitchProductProtocol(r.Context(), model.ProtocolSwitch{Product: candidate.Product, Package: legacyProtocolShim(release), Binding: binding, Expected: expected, Preparation: &model.TemplateSwitch{ExpectedProduct: current.Product, ExpectedProfiles: current.Profiles, Profiles: candidate.Profiles, Record: rec, ExpectedRevision: rec.Revision}})
	if err == nil {
		s.engine.ProtocolsChanged(tenant)
		if current.Product.ReportIntervalSec != candidate.Product.ReportIntervalSec || current.Product.OfflineToleranceSec != candidate.Product.OfflineToleranceSec {
			s.applyTemplateTiming(tenant, id)
		}
		s.audit(r, "template.apply", "product", id, map[string]any{"fingerprint": newFP, "previousFingerprint": oldFP, "rollback": rollback})
	}
	return err
}

func (s *Server) verifyRegisteredDevice(w http.ResponseWriter, r *http.Request) {
	result, err := s.onboarding.VerifyDevice(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	if r.Method == "POST" {
		rec, e := s.engine.Repo.GetOnboardingRecord(r.Context(), claims(r).TenantID, "verification:"+result.DeviceID)
		if errors.Is(e, model.ErrNotFound) {
			rec = model.OnboardingRecord{TenantID: claims(r).TenantID, ID: "verification:" + result.DeviceID, OwnerID: "device", Kind: "device-verification"}
		} else if e != nil {
			s.preparationProblem(w, r, e)
			return
		}
		rec.Status = result.Status
		rec.Body, _ = json.Marshal(result)
		if _, e = s.engine.Repo.SaveOnboardingRecord(r.Context(), rec, rec.Revision); e != nil {
			s.preparationProblem(w, r, e)
			return
		}
	}
	write(w, 200, result)
}

func (s *Server) verifyTemplate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID string `json:"deviceId"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	tenant, id := claims(r).TenantID, r.PathValue("id")
	result, err := s.onboarding.VerifyDevice(r.Context(), tenant, in.DeviceID)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	if result.ProductID != id {
		problem(w, 422, "验证设备不属于此模板")
		return
	}
	rec, prep, err := s.onboarding.TemplateRecord(r.Context(), tenant, id)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	current, err := s.onboarding.CurrentCandidate(r.Context(), tenant, id)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	if s.onboarding.CandidateFingerprint(current) != result.Fingerprint {
		s.preparationProblem(w, r, model.ErrOnboardingChanged)
		return
	}
	if rec.Revision == 0 {
		prep.Candidate = current
	}
	prep.Fingerprint = result.Fingerprint
	prep.Verification = &result
	prep.Status = "AWAITING_VALIDATION"
	if result.Status == "VERIFIED" {
		prep.Status = "READY"
	}
	if _, err = s.onboarding.SaveTemplateRecord(r.Context(), rec, prep); err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	s.audit(r, "template.verify", "product", id, map[string]any{"deviceId": in.DeviceID, "status": result.Status, "fingerprint": result.Fingerprint})
	write(w, 200, result)
}

func (s *Server) rollbackTemplatePreparation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision       int64 `json:"revision"`
		TargetRevision int64 `json:"targetRevision"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	rec, prep, err := s.onboarding.TemplateRecord(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	if rec.Revision != in.Revision {
		s.preparationProblem(w, r, model.ErrOnboardingChanged)
		return
	}
	found := false
	for _, previous := range prep.History {
		if previous.Revision == in.TargetRevision {
			prep.Candidate = previous.Candidate
			found = true
			break
		}
	}
	if !found {
		problem(w, 422, "历史配置修订不存在")
		return
	}
	saved, err := s.onboarding.SaveTemplateRecord(r.Context(), rec, prep)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	if err = s.applyPreparedCandidate(r, saved.Revision, true); err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	s.templatePreparation(w, r)
}

// A trial has its own product identity and listener ports. It cannot change the
// version serving an existing product's live sessions.
func (s *Server) trialTemplatePreparation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision int64                       `json:"revision"`
		Profiles []model.DeviceAccessProfile `json:"profiles,omitempty"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if !requestAllows(r, "POST", "/api/v1/products") {
		problem(w, 403, "当前账号不能创建隔离试验模板")
		return
	}
	tenant, id := claims(r).TenantID, r.PathValue("id")
	rec, prep, err := s.onboarding.TemplateRecord(r.Context(), tenant, id)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	if rec.Revision != in.Revision {
		s.preparationProblem(w, r, model.ErrOnboardingChanged)
		return
	}
	candidate, release, err := s.normalizeCandidate(r, id, prep.Candidate)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	sourceFingerprint := s.onboarding.CandidateFingerprint(candidate)
	trialID := "trial_" + randomHex(8)
	if prep.TrialProductID != "" && prep.TrialFingerprint == sourceFingerprint {
		trialRecord, trialPrep, e := s.onboarding.TemplateRecord(r.Context(), tenant, prep.TrialProductID)
		if e != nil {
			s.preparationProblem(w, r, e)
			return
		}
		actual, e := s.onboarding.TemplateFingerprint(r.Context(), tenant, prep.TrialProductID)
		if e == nil && trialRecord.Revision > 0 && actual == prep.TrialConfigFingerprint && trialPrep.Fingerprint == actual {
			write(w, 200, map[string]any{"trialProductId": prep.TrialProductID, "revision": rec.Revision})
			return
		}
		if errors.Is(e, model.ErrNotFound) {
			trialID = prep.TrialProductID
		}
	}
	if len(in.Profiles) > 0 {
		// An isolated endpoint may differ only in its address, port and identity.
		if len(in.Profiles) != len(candidate.Profiles) {
			problem(w, 422, "请为每个共享接入点指定隔离监听地址和端口")
			return
		}
		for i, p := range in.Profiles {
			candidate.Profiles[i].Host = p.Host
			candidate.Profiles[i].PublicHost = p.PublicHost
			candidate.Profiles[i].Port = p.Port
		}
	}
	if len(candidate.Profiles) > 0 && !requestAllows(r, "POST", "/api/v2/device-access-profiles") {
		problem(w, 403, "当前账号不能创建试验接入点")
		return
	}
	all, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), "")
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	for _, p := range candidate.Profiles {
		for _, other := range all {
			if p.Enabled && other.Enabled && p.Network == other.Network && p.Port == other.Port && other.Mode == "listener" && other.ConnectionMode != "dial" {
				problem(w, 409, "隔离试验必须使用独立监听端口，请提供未占用的试验端口")
				return
			}
		}
	}
	now := time.Now().UnixMilli()
	candidate.Product.ID = trialID
	candidate.Product.Name += " · 隔离试验"
	candidate.Product.CreatedAt = now
	candidate.Product.UpdatedAt = now
	candidate.Product.Status = "ENABLED"
	candidate.Product.Metadata = copyMetadata(candidate.Product.Metadata)
	candidate.Product.Metadata["trialOf"] = id
	candidate.Product.VerificationRules = &candidate.VerificationRules
	for i := range candidate.Profiles {
		p := &candidate.Profiles[i]
		p.ID = fmt.Sprintf("%s_%d", trialID, i+1)
		p.ProductID = trialID
		p.CreatedAt = now
		p.UpdatedAt = now
		p.Enabled = true
	}
	prep.TrialProductID = trialID
	prep.TrialFingerprint = sourceFingerprint
	prep.TrialConfigFingerprint = s.onboarding.CandidateFingerprint(candidate)
	saved, err := s.onboarding.SaveTemplateRecord(r.Context(), rec, prep)
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	trialPrep := model.TemplatePreparation{Candidate: candidate, Status: "AWAITING_VALIDATION", Fingerprint: s.onboarding.CandidateFingerprint(candidate), AppliedAt: now, History: []model.TemplateRevision{}}
	body, _ := json.Marshal(trialPrep)
	record := model.OnboardingRecord{TenantID: tenant, ID: onboarding.TemplateRecordID(trialID), OwnerID: "template", Kind: "template-preparation", Status: trialPrep.Status, Body: body}
	err = s.engine.Repo.SwitchProductProtocol(r.Context(), model.ProtocolSwitch{Product: candidate.Product, Package: legacyProtocolShim(release), Binding: model.ProductProtocolBinding{TenantID: tenant, ProductID: trialID, ProtocolID: release.ProtocolID, Version: release.Version, UpdatedAt: now}, Preparation: &model.TemplateSwitch{CreateProduct: true, ExpectedProduct: candidate.Product, Profiles: candidate.Profiles, Record: record}})
	if err != nil {
		s.preparationProblem(w, r, err)
		return
	}
	s.engine.ProtocolsChanged(tenant)
	s.audit(r, "template.trial", "product", id, map[string]any{"trialProductId": trialID})
	write(w, 201, map[string]any{"trialProductId": trialID, "revision": saved.Revision})
}

func copyMetadata(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Server) allowDirectTemplateProtocolChange(w http.ResponseWriter, r *http.Request) bool {
	_, count, err := s.engine.Repo.ListManagedDevicesFiltered(r.Context(), ports.DeviceFilter{TenantID: claims(r).TenantID, RestrictProducts: true, ProductIDs: []string{r.PathValue("id")}}, 1, 0)
	if err != nil {
		s.failure(w, r, err, "读取模板使用情况失败")
		return false
	}
	if count > 0 {
		problem(w, 409, "已有设备使用此模板，请在模板准备流程中应用或回滚完整配置")
		return false
	}
	return true
}
