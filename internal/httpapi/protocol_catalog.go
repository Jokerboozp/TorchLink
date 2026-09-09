package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"

	"iot-platform/internal/protocolbuild"
	"iot-platform/internal/protocolcatalog"
)

type catalogProvenanceKey struct{}

var catalogSlots = make(chan struct{}, 4)

func claimCatalog(w http.ResponseWriter) bool {
	select {
	case catalogSlots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "5")
		problem(w, 429, "protocol catalog operations are busy")
		return false
	}
}

func (s *Server) protocolCatalog(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ProtocolCatalogPolicy == "" {
		write(w, 200, map[string]any{"enabled": false, "entries": []any{}})
		return
	}
	if !claimCatalog(w) {
		return
	}
	defer func() { <-catalogSlots }()
	client, err := protocolcatalog.ReadPolicy(s.cfg.ProtocolCatalogPolicy)
	if err != nil {
		problem(w, 503, err.Error())
		return
	}
	defer client.Close()
	catalog, err := client.Fetch(r.Context())
	if err != nil {
		problem(w, 502, err.Error())
		return
	}
	write(w, 200, map[string]any{"enabled": true, "catalog": catalog})
}
func (s *Server) installCatalogProtocol(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var request struct {
		ID        string `json:"id"`
		Version   string `json:"version"`
		Digest    string `json:"digest"`
		Confirmed bool   `json:"confirmed"`
	}
	if decode(w, r, &request) != nil {
		return
	}
	if !request.Confirmed {
		problem(w, 422, "请先确认安装并执行已签名协议包的样例校验")
		return
	}
	if s.cfg.ProtocolCatalogPolicy == "" {
		problem(w, 503, "protocol catalog is not configured")
		return
	}
	if !claimCatalog(w) {
		return
	}
	defer func() { <-catalogSlots }()
	client, err := protocolcatalog.ReadPolicy(s.cfg.ProtocolCatalogPolicy)
	if err != nil {
		problem(w, 503, err.Error())
		return
	}
	defer client.Close()
	catalog, err := client.Fetch(r.Context())
	if err != nil {
		problem(w, 502, err.Error())
		return
	}
	if request.Digest != catalog.Digest {
		problem(w, 409, "协议目录已更新，请刷新后重新确认")
		return
	}
	var entry *protocolcatalog.Entry
	for i := range catalog.Entries {
		v := &catalog.Entries[i]
		if v.ID == request.ID && v.Version == request.Version {
			entry = v
			break
		}
	}
	if entry == nil {
		problem(w, 404, "selected protocol version is absent from the signed catalog")
		return
	}
	if _, err := s.engine.Repo.GetProtocolRelease(r.Context(), claims(r).TenantID, entry.ID, entry.Version); err == nil {
		problem(w, 409, "该协议版本已安装，现有版本未修改")
		return
	}
	data, err := client.Source(r.Context(), *entry)
	if err != nil {
		problem(w, 502, err.Error())
		return
	}
	files, err := protocolbuild.Sources("source.zip", data)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	var metadata struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if json.Unmarshal(files["protocol.json"], &metadata) != nil || metadata.ID != entry.ID || metadata.Version != entry.Version {
		problem(w, 422, "源码 protocol.json 与签名目录的协议标识或版本不一致")
		return
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "source.zip")
	if err != nil {
		problem(w, 500, "prepare verified source failed")
		return
	}
	if _, err = file.Write(data); err != nil {
		problem(w, 500, "prepare verified source failed")
		return
	}
	_ = form.WriteField("publish", "false")
	_ = form.Close()
	// Reuse the complete existing source pipeline: bounded offline compiler,
	// required actual samples, immutable artifacts, and validated release only.
	provenance := map[string]any{"catalogSha256": catalog.Digest, "keyId": catalog.KeyID, "sourceSha256": entry.SHA256, "publisher": entry.Publisher}
	forwarded := r.Clone(context.WithValue(r.Context(), catalogProvenanceKey{}, provenance))
	forwarded.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	forwarded.ContentLength = int64(body.Len())
	forwarded.Header = r.Header.Clone()
	forwarded.Header.Set("Content-Type", form.FormDataContentType())
	forwarded.Form = nil
	forwarded.PostForm = nil
	forwarded.MultipartForm = nil
	forwarded.SetPathValue("id", entry.ID)
	s.audit(r, "protocol.catalog.install.request", "protocol", entry.ID, map[string]any{"version": entry.Version, "catalogSha256": catalog.Digest, "keyId": catalog.KeyID})
	s.uploadProtocolSource(w, forwarded)
}

// sourceCatalogProvenance contains verified provenance only; no remote headers,
// signing secrets or arbitrary request metadata are included in the artifact.
func sourceCatalogProvenance(ctx context.Context) map[string]any {
	value, _ := ctx.Value(catalogProvenanceKey{}).(map[string]any)
	return value
}
