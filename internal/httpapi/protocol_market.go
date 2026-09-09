package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/protocolbuild"
	"iot-platform/internal/protocolcatalog"
	"iot-platform/internal/protocolmarket"
)

func (s *Server) marketRoutes() {
	s.router.GET("/api/v2/protocol-market", s.authorize("viewer"), s.endpoint(s.marketInventory))
	s.router.POST("/api/v2/protocol-market/:id/:version", s.authorize("operator"), s.endpoint(s.marketSubmit, "id", "version"))
	s.router.POST("/api/v2/protocol-market/:id/:version/review", s.authorize("admin"), s.endpoint(s.marketReview, "id", "version"))
	s.router.GET("/api/v2/market-distribution/:tenant/catalog", s.endpoint(s.marketDistribution, "tenant"))
	s.router.GET("/api/v2/market-distribution/:tenant/protocols/:id/:version/source.zip", s.endpoint(s.marketDistribution, "tenant", "id", "version"))
}
func (s *Server) marketPolicy(w http.ResponseWriter, tenant string) (protocolmarket.Policy, protocolmarket.Organization, bool) {
	var empty protocolmarket.Organization
	if s.cfg.ProtocolMarketPolicy == "" {
		problem(w, 503, "private protocol market is not configured")
		return protocolmarket.Policy{}, empty, false
	}
	policy, err := protocolmarket.ReadPolicy(s.cfg.ProtocolMarketPolicy)
	if err != nil {
		problem(w, 503, err.Error())
		return policy, empty, false
	}
	org, ok := policy.Organizations[tenant]
	if !ok {
		problem(w, 404, "private market organization is unavailable")
		return policy, empty, false
	}
	return policy, org, true
}
func (s *Server) marketInventory(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ProtocolMarketPolicy == "" {
		write(w, 200, map[string]any{"enabled": false, "items": []any{}})
		return
	}
	policy, org, ok := s.marketPolicy(w, claims(r).TenantID)
	if !ok {
		return
	}
	items, err := s.engine.Repo.ListProtocolMarket(r.Context(), claims(r).TenantID)
	if err != nil {
		problem(w, 503, "read protocol submissions failed")
		return
	}
	write(w, 200, map[string]any{"enabled": true, "publisher": org.Publisher, "catalogUrl": policy.PublicOrigin + "/api/v2/market-distribution/" + url.PathEscape(claims(r).TenantID) + "/catalog", "items": items})
}
func (s *Server) marketSubmit(w http.ResponseWriter, r *http.Request) {
	policy, org, ok := s.marketPolicy(w, claims(r).TenantID)
	if !ok {
		return
	}
	if !claimCatalog(w) {
		return
	}
	defer func() { <-catalogSlots }()
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var input struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		License     string   `json:"license"`
		Tags        []string `json:"tags"`
		Confirmed   bool     `json:"confirmed"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	if !input.Confirmed || claims(r).Username == "" {
		problem(w, 422, "请确认提交此不可变版本进行独立审核")
		return
	}
	release, source, extension, ok := s.readProtocolSourceV2(w, r)
	if !ok {
		return
	}
	build, _ := release.Artifact["build"].(map[string]any)
	if extension != ".zip" || build["kind"] != "go-source" || (release.Status != "PUBLISHED" && release.Status != "VALIDATED") || artifactTestCountV2(release.Artifact) == 0 {
		problem(w, 422, "市场仅接受已实际校验且包含 protocol.json 的完整 Go 源码 ZIP")
		return
	}
	files, err := protocolbuild.Sources("source.zip", source)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	var metadata struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if json.Unmarshal(files["protocol.json"], &metadata) != nil || metadata.ID != release.ProtocolID || metadata.Version != release.Version {
		problem(w, 422, "源码 protocol.json 须与待提交的协议标识及版本一致")
		return
	}
	now := time.Now().UnixMilli()
	packageHash, _ := release.Artifact["packageSha256"].(string)
	entry := model.ProtocolMarketEntry{TenantID: claims(r).TenantID, ProtocolID: release.ProtocolID, Version: release.Version, Name: strings.TrimSpace(input.Name), Description: input.Description, License: strings.TrimSpace(input.License), Tags: input.Tags, SourceSHA256: onboarding.Hash(string(source)), PackageSHA256: packageHash, SourceSize: int64(len(source)), SubmittedBy: claims(r).Username, SubmittedAt: now, Generation: 1, Status: "SUBMITTED", Reviews: []model.ProtocolMarketReview{}}
	if entry.License == "" {
		problem(w, 422, "请填写组织许可说明")
		return
	}
	payload := protocolcatalog.Payload{IssuedAt: now, ExpiresAt: now + 3600000, Entries: []protocolcatalog.Entry{marketCatalogEntry(policy, org, entry)}}
	if err = protocolcatalog.Validate(payload, time.Now()); err != nil {
		problem(w, 422, err.Error())
		return
	}
	if err = s.engine.Repo.SubmitProtocolMarket(r.Context(), entry); err != nil {
		marketProblem(w, err)
		return
	}
	s.audit(r, "protocol.market.submit", "protocolRelease", entry.ProtocolID+"@"+entry.Version, map[string]any{"sourceSha256": entry.SourceSHA256})
	write(w, 201, entry)
}
func (s *Server) marketReview(w http.ResponseWriter, r *http.Request) {
	_, org, ok := s.marketPolicy(w, claims(r).TenantID)
	if !ok {
		return
	}
	if !claimCatalog(w) {
		return
	}
	defer func() { <-catalogSlots }()
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var input struct {
		Decision   string `json:"decision"`
		Reason     string `json:"reason"`
		Generation int64  `json:"generation"`
		Confirmed  bool   `json:"confirmed"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	if !input.Confirmed {
		problem(w, 422, "请确认审核或撤回决定")
		return
	}
	entry, err := s.engine.Repo.GetProtocolMarket(r.Context(), claims(r).TenantID, r.PathValue("id"), r.PathValue("version"))
	if err != nil {
		problem(w, 404, "protocol market submission not found")
		return
	}
	if _, err = entry.Review(claims(r).Username, input.Decision, strings.TrimSpace(input.Reason), input.Generation); err != nil {
		marketProblem(w, err)
		return
	}
	if input.Decision == "APPROVED" {
		release, data, extension, ok := s.readProtocolSourceV2(w, r)
		if !ok {
			return
		}
		if extension != ".zip" || !marketSourceMatches(entry, release, data) {
			problem(w, 409, "submitted source proof no longer matches release")
			return
		}
		now := time.Now().UnixMilli()
		if _, err = org.Sign(protocolcatalog.Payload{IssuedAt: now, ExpiresAt: now + 3600000, Entries: []protocolcatalog.Entry{}}); err != nil {
			problem(w, 503, err.Error())
			return
		}
	}
	entry, err = s.engine.Repo.ReviewProtocolMarket(r.Context(), entry.TenantID, entry.ProtocolID, entry.Version, claims(r).Username, input.Decision, strings.TrimSpace(input.Reason), input.Generation)
	if err != nil {
		marketProblem(w, err)
		return
	}
	s.audit(r, "protocol.market.review", "protocolRelease", entry.ProtocolID+"@"+entry.Version, map[string]any{"decision": entry.Status, "generation": entry.Generation})
	write(w, 200, entry)
}
func marketProblem(w http.ResponseWriter, err error) {
	code := 503
	if errors.Is(err, model.ErrMarketConflict) {
		code = 409
	} else if errors.Is(err, model.ErrMarketReview) {
		code = 422
	} else if errors.Is(err, model.ErrMarketLimit) {
		code = 429
	}
	problem(w, code, err.Error())
}
func marketCatalogEntry(policy protocolmarket.Policy, org protocolmarket.Organization, entry model.ProtocolMarketEntry) protocolcatalog.Entry {
	return protocolcatalog.Entry{Kind: "protocol-source", ID: entry.ProtocolID, Version: entry.Version, Name: entry.Name, Description: entry.Description, Publisher: org.Publisher, License: entry.License, Tags: entry.Tags, SourceURL: policy.PublicOrigin + "/api/v2/market-distribution/" + url.PathEscape(entry.TenantID) + "/protocols/" + url.PathEscape(entry.ProtocolID) + "/" + url.PathEscape(entry.Version) + "/source.zip", SHA256: entry.SourceSHA256, Size: entry.SourceSize}
}
func marketSourceMatches(entry model.ProtocolMarketEntry, release model.ProtocolRelease, data []byte) bool {
	return (release.Status == "PUBLISHED" || release.Status == "VALIDATED") && release.Artifact["packageSha256"] == entry.PackageSHA256 && int64(len(data)) == entry.SourceSize && onboarding.Hash(string(data)) == entry.SourceSHA256
}
func (s *Server) marketDistribution(w http.ResponseWriter, r *http.Request) {
	// Authenticate a separate organization reader credential, never a browser JWT
	// or a key supplied by the request. Local policy fixes the tenant and origin.
	if s.cfg.ProtocolMarketPolicy == "" {
		problem(w, 401, "invalid market reader credential")
		return
	}
	policy, err := protocolmarket.ReadPolicy(s.cfg.ProtocolMarketPolicy)
	if err != nil {
		problem(w, 503, "private market policy unavailable")
		return
	}
	tenant := r.PathValue("tenant")
	org, ok := policy.Organizations[tenant]
	header := r.Header.Get("Authorization")
	if !ok || !strings.HasPrefix(header, "Bearer ") || !org.Authenticate(strings.TrimPrefix(header, "Bearer ")) {
		problem(w, 401, "invalid market reader credential")
		return
	}
	if !claimCatalog(w) {
		return
	}
	defer func() { <-catalogSlots }()
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasSuffix(r.URL.Path, "/catalog") {
		items, err := s.engine.Repo.ListProtocolMarket(r.Context(), tenant)
		if err != nil {
			problem(w, 503, "read distribution catalog failed")
			return
		}
		// Stable within the issuance hour: the consumer re-fetches before installing
		// and compares the digest. Approvals/withdrawals change it immediately.
		issued := time.Now().Truncate(time.Hour).UnixMilli()
		payload := protocolcatalog.Payload{IssuedAt: issued, ExpiresAt: issued + 2*3600000, Entries: []protocolcatalog.Entry{}}
		for _, item := range items {
			if item.Status != "APPROVED" {
				continue
			}
			release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, item.ProtocolID, item.Version)
			if err != nil {
				problem(w, 503, "approved release unavailable")
				return
			}
			if (release.Status == "PUBLISHED" || release.Status == "VALIDATED") && release.Artifact["packageSha256"] == item.PackageSHA256 {
				payload.Entries = append(payload.Entries, marketCatalogEntry(policy, org, item))
			}
		}
		envelope, err := org.Sign(payload)
		if err != nil {
			problem(w, 503, err.Error())
			return
		}
		write(w, 200, envelope)
		return
	}
	entry, err := s.engine.Repo.GetProtocolMarket(r.Context(), tenant, r.PathValue("id"), r.PathValue("version"))
	if err != nil || entry.Status != "APPROVED" {
		problem(w, 404, "approved source is unavailable")
		return
	}
	// Supply only the tenant already authenticated above to the shared canonical
	// download reader. It still validates archive/source paths and both hashes.
	release, data, extension, ok := s.readProtocolSourceForTenantV2(w, r, tenant)
	if !ok {
		return
	}
	if extension != ".zip" || !marketSourceMatches(entry, release, data) {
		problem(w, 409, "approved source proof does not match release")
		return
	}
	writeProtocolDownloadV2(w, data, entry.ProtocolID+"-"+entry.Version+"-source.zip", "application/zip")
}
