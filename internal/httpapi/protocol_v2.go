package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolruntime"
	"iot-platform/internal/protocolworker"

	"gopkg.in/yaml.v3"
)

var protocolSegmentV2 = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func (s *Server) protocolDefinitionsV2(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	definitions, err := s.engine.Repo.ListProtocolDefinitions(r.Context(), tenant)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	releases, err := s.engine.Repo.ListProtocolReleases(r.Context(), tenant, "")
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	byProtocol := map[string][]model.ProtocolRelease{}
	for _, release := range releases {
		byProtocol[release.ProtocolID] = append(byProtocol[release.ProtocolID], release)
	}
	items := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, map[string]any{"definition": definition, "releases": byProtocol[definition.ID]})
	}
	write(w, 200, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) saveProtocolDefinitionV2(w http.ResponseWriter, r *http.Request) {
	var v model.ProtocolDefinition
	if decode(w, r, &v) != nil {
		return
	}
	v.ID = strings.TrimSpace(v.ID)
	v.Name = strings.TrimSpace(v.Name)
	if !protocolSegmentV2.MatchString(v.ID) || v.Name == "" {
		problem(w, 422, "id and name are required; id may contain letters, numbers, dot, underscore and hyphen")
		return
	}
	v.TenantID = claims(r).TenantID
	now := time.Now().UnixMilli()
	if old, err := s.engine.Repo.GetProtocolDefinition(r.Context(), v.TenantID, v.ID); err == nil {
		v.CreatedAt = old.CreatedAt
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if err := s.engine.Repo.SaveProtocolDefinition(r.Context(), v); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "protocol.v2.definition.save", "protocol", v.ID, nil)
	write(w, 201, v)
}

func (s *Server) protocolReleasesV2(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.Repo.ListProtocolReleases(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) createProtocolReleaseV2(w http.ResponseWriter, r *http.Request) {
	var v model.ProtocolRelease
	if decode(w, r, &v) != nil {
		return
	}
	v.TenantID = claims(r).TenantID
	v.ProtocolID = r.PathValue("id")
	if _, err := s.engine.Repo.GetProtocolDefinition(r.Context(), v.TenantID, v.ProtocolID); err != nil {
		problem(w, 404, "protocol definition not found")
		return
	}
	if !protocolSegmentV2.MatchString(v.Version) {
		problem(w, 422, "version is required and contains unsupported characters")
		return
	}
	if v.ParserType == "" {
		problem(w, 422, "parserType is required")
		return
	}
	if v.ParserType == parser.GoProtocolParserName {
		problem(w, 422, "custom Go releases must be uploaded as a versioned ZIP package")
		return
	}
	if v.ParserType != "configurable_json_parser" && v.ParserType != "configurable_hex_parser" {
		problem(w, 422, "unsupported protocol v2 parserType")
		return
	}
	if v.Status == "" {
		v.Status = "DRAFT"
	}
	if v.Status != "DRAFT" && v.Status != "VALIDATED" {
		problem(w, 422, "a release must be created as DRAFT or VALIDATED and published separately")
		return
	}
	if v.Transport == "" {
		v.Transport = "MQTT"
	}
	if v.PayloadFormat == "" {
		v.PayloadFormat = "json"
	}
	if err := validateProtocolReleaseV2(v); err != nil {
		problem(w, 422, err.Error())
		return
	}
	v.CreatedAt = time.Now().UnixMilli()
	if err := s.engine.Repo.CreateProtocolRelease(r.Context(), v); err != nil {
		problem(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "protocol.v2.release.create", "protocolRelease", v.ProtocolID+"@"+v.Version, map[string]any{"status": v.Status})
	write(w, 201, v)
}

type protocolPackageManifestV2 struct {
	SchemaVersion int               `yaml:"schemaVersion"`
	ID            string            `yaml:"id"`
	Name          string            `yaml:"name"`
	Version       string            `yaml:"version"`
	Transport     string            `yaml:"transport"`
	PayloadFormat string            `yaml:"payloadFormat"`
	Runtime       string            `yaml:"runtime"`
	Entrypoints   map[string]string `yaml:"entrypoints"`
	Capabilities  []string          `yaml:"capabilities"`
	Description   string            `yaml:"description"`
}

const maxProtocolPackageV2 = int64(64 << 20)
const maxProtocolPackageExpandedV2 = int64(128 << 20)

func (s *Server) uploadProtocolPackageV2(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxProtocolPackageV2+(1<<20))
	if err := r.ParseMultipartForm(maxProtocolPackageV2 + 1<<20); err != nil {
		problem(w, 400, "invalid protocol package upload")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("package")
	if err != nil {
		file, header, err = r.FormFile("file")
	}
	if err != nil {
		problem(w, 422, "package is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProtocolPackageV2+1))
	if err != nil || len(data) == 0 || int64(len(data)) > maxProtocolPackageV2 {
		problem(w, 422, "protocol package must be between 1 byte and 64 MiB")
		return
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		problem(w, 422, "protocol package must be a ZIP file")
		return
	}
	entries, err := inspectProtocolPackageV2(reader)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	manifestData, ok := entries["manifest.yaml"]
	if !ok {
		manifestData = entries["manifest.yml"]
	}
	if len(manifestData) == 0 {
		problem(w, 422, "manifest.yaml is required at the package root")
		return
	}
	var manifest protocolPackageManifestV2
	if err = yaml.Unmarshal(manifestData, &manifest); err != nil {
		problem(w, 422, "manifest.yaml is invalid: "+err.Error())
		return
	}
	protocolID := r.PathValue("id")
	if err = validateProtocolManifestV2(protocolID, manifest); err != nil {
		problem(w, 422, err.Error())
		return
	}
	s.installProtocolPackageV2(w, r, data, entries, manifest, header.Filename, nil)
}

// Source and prebuilt uploads share validation, immutable storage and binding.
func (s *Server) installProtocolPackageV2(w http.ResponseWriter, r *http.Request, data []byte, entries map[string][]byte, manifest protocolPackageManifestV2, filename string, buildInfo map[string]any) {
	protocolID := manifest.ID
	publish, err := formBoolStrict(r, "publish", true)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	productID := strings.TrimSpace(r.FormValue("productId"))
	if productID != "" {
		if !publish {
			problem(w, 422, "a product can only be bound when publish=true")
			return
		}
		if _, productErr := s.engine.Repo.GetProduct(r.Context(), claims(r).TenantID, productID); productErr != nil {
			problem(w, 422, "productId does not reference an existing product")
			return
		}
	}
	targetPlatform := runtime.GOOS + "-" + runtime.GOARCH
	entrypoint := filepath.ToSlash(strings.TrimSpace(manifest.Entrypoints[targetPlatform]))
	worker, ok := entries[entrypoint]
	if !ok || len(worker) == 0 {
		problem(w, 422, "package does not contain an entrypoint for "+targetPlatform)
		return
	}
	tenant, now := claims(r).TenantID, time.Now().UnixMilli()
	if _, getErr := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, protocolID, manifest.Version); getErr == nil {
		problem(w, 409, "protocol release already exists")
		return
	}
	root, err := filepath.Abs(s.cfg.DataDir)
	if err != nil {
		problem(w, 500, "resolve protocol data directory")
		return
	}
	directory := filepath.Join(root, "protocol-releases", tenant, protocolID, manifest.Version)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		problem(w, 500, "create protocol release directory")
		return
	}
	packagePath := filepath.Join(directory, "package.zip")
	workerName := "artifact"
	if runtime.GOOS == "windows" {
		workerName += ".exe"
	}
	workerPath := filepath.Join(directory, workerName)
	if err = writeExclusiveFile(packagePath, data, 0o600); err != nil {
		problem(w, 409, "protocol release artifact already exists")
		return
	}
	if err = writeExclusiveFile(workerPath, worker, 0o700); err != nil {
		_ = os.Remove(packagePath)
		problem(w, 409, "protocol release worker already exists")
		return
	}
	packageDigest, workerDigest := sha256.Sum256(data), sha256.Sum256(worker)
	relativeWorker, _ := filepath.Rel(root, workerPath)
	relativePackage, _ := filepath.Rel(root, packagePath)
	status, publishedAt := "VALIDATED", int64(0)
	if publish {
		status, publishedAt = "PUBLISHED", now
	}
	definition := model.ProtocolDefinition{ID: protocolID, TenantID: tenant, Name: firstNonBlank(manifest.Name, protocolID), Description: manifest.Description, CreatedAt: now, UpdatedAt: now}
	if old, getErr := s.engine.Repo.GetProtocolDefinition(r.Context(), tenant, protocolID); getErr == nil {
		definition.CreatedAt = old.CreatedAt
	}
	artifact := map[string]any{"path": filepath.ToSlash(relativeWorker), "packagePath": filepath.ToSlash(relativePackage), "filename": filename, "sha256": hex.EncodeToString(workerDigest[:]), "packageSha256": hex.EncodeToString(packageDigest[:]), "size": len(worker), "runtime": manifest.Runtime, "platform": targetPlatform, "uploadedAt": now}
	if buildInfo != nil {
		artifact["build"] = buildInfo
	}
	// Every custom release carries executable regression cases, even when it
	// is uploaded as VALIDATED and published in a later operation.
	testCount, testErr := validateProtocolPackageCasesContextV2(r.Context(), root, artifact, entries, manifest, true)
	if testErr != nil {
		_ = os.Remove(workerPath)
		_ = os.Remove(packagePath)
		problem(w, 422, testErr.Error())
		return
	}
	artifact["testCases"] = testCount
	if buildInfo != nil {
		if targets, ok := buildInfo["targets"].(map[string]string); ok {
			targets[targetPlatform] = "PASSED"
		}
	}
	created, variantErr := storeProtocolVariants(root, directory, artifact, entries, manifest)
	if variantErr != nil {
		_ = os.Remove(workerPath)
		_ = os.Remove(packagePath)
		problem(w, 422, variantErr.Error())
		return
	}
	retained := false
	defer func() {
		if !retained {
			for _, path := range created {
				_ = os.Remove(path)
			}
		}
	}()

	release := model.ProtocolRelease{TenantID: tenant, ProtocolID: protocolID, Version: manifest.Version, Transport: strings.ToUpper(manifest.Transport), PayloadFormat: strings.ToLower(manifest.PayloadFormat), ParserType: parser.GoProtocolParserName, Status: status, Capabilities: manifest.Capabilities, Config: map[string]any{"artifact": artifact, "timeoutMs": 5000}, Artifact: artifact, CreatedAt: now, PublishedAt: publishedAt}
	if err = s.engine.Repo.SaveProtocolDefinition(r.Context(), definition); err == nil {
		err = s.engine.Repo.CreateProtocolRelease(r.Context(), release)
	}
	if err != nil {
		_ = os.Remove(workerPath)
		_ = os.Remove(packagePath)
		problem(w, 500, err.Error())
		return
	}
	retained = true
	var binding any
	if productID != "" {
		bound, bindErr := s.bindProtocolRelease(r, protocolID, manifest.Version, productID)
		if bindErr != nil {
			problem(w, 500, "protocol was published but product binding failed: "+bindErr.Error())
			return
		}
		binding = bound
	}
	s.audit(r, "protocol.v2.package.upload", "protocolRelease", protocolID+"@"+manifest.Version, map[string]any{"sha256": artifact["packageSha256"], "published": publish, "platform": targetPlatform})
	write(w, 201, map[string]any{"definition": definition, "release": release, "manifest": manifest, "binding": binding, "testCases": testCount})
}

type protocolPackageCaseV2 = protocolworker.SampleCase

func validateProtocolPackageCasesV2(root string, artifact map[string]any, entries map[string][]byte, manifest protocolPackageManifestV2, required bool) (int, error) {
	return validateProtocolPackageCasesContextV2(context.Background(), root, artifact, entries, manifest, required)
}
func validateProtocolPackageCasesContextV2(parent context.Context, root string, artifact map[string]any, entries map[string][]byte, manifest protocolPackageManifestV2, required bool) (int, error) {
	return protocolworker.ValidateSamples(parent, root, artifact, entries, protocolworker.SampleManifest{ID: manifest.ID, Runtime: manifest.Runtime, Transport: manifest.Transport, PayloadFormat: manifest.PayloadFormat, Capabilities: manifest.Capabilities}, required)
}

func inspectProtocolPackageV2(reader *zip.Reader) (map[string][]byte, error) {
	if len(reader.File) == 0 || len(reader.File) > 100 {
		return nil, errors.New("protocol package must contain between 1 and 100 entries")
	}
	entries := make(map[string][]byte, len(reader.File))
	var expanded int64
	for _, entry := range reader.File {
		name := filepath.ToSlash(strings.TrimSpace(entry.Name))
		clean := filepath.ToSlash(filepath.Clean(name))
		if name == "" || strings.HasPrefix(clean, "../") || clean == ".." || filepath.IsAbs(name) || entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe ZIP entry %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if _, exists := entries[clean]; exists {
			return nil, fmt.Errorf("duplicate ZIP entry %q", clean)
		}
		if entry.UncompressedSize64 > uint64(maxProtocolPackageV2) {
			return nil, fmt.Errorf("ZIP entry %q is too large", name)
		}
		expanded += int64(entry.UncompressedSize64)
		if expanded > maxProtocolPackageExpandedV2 {
			return nil, errors.New("expanded protocol package exceeds 128 MiB")
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, maxProtocolPackageV2+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || int64(len(content)) > maxProtocolPackageV2 {
			return nil, fmt.Errorf("read ZIP entry %q", name)
		}
		entries[clean] = content
	}
	return entries, nil
}

func validateProtocolManifestV2(protocolID string, manifest protocolPackageManifestV2) error {
	if manifest.SchemaVersion != 1 {
		return errors.New("manifest schemaVersion must be 1")
	}
	if manifest.ID != protocolID || !protocolSegmentV2.MatchString(manifest.ID) {
		return errors.New("manifest id must match the protocol id in the URL")
	}
	if !protocolSegmentV2.MatchString(manifest.Version) {
		return errors.New("manifest version is required")
	}
	if manifest.Runtime != "go-json-lines-v1" && manifest.Runtime != protocolworker.Runtime {
		return errors.New("manifest runtime must be go-json-lines-v1 or go-protocol-v2")
	}
	seen := map[string]bool{}
	for _, capability := range manifest.Capabilities {
		if seen[capability] || (capability != "decode" && capability != "ingress" && capability != "encode") {
			return fmt.Errorf("unsupported or repeated protocol capability %q", capability)
		}
		if capability != "decode" && manifest.Runtime != protocolworker.Runtime {
			return errors.New("ingress/encode require go-protocol-v2")
		}
		seen[capability] = true
	}
	if manifest.Runtime == protocolworker.Runtime && !seen["decode"] {
		return errors.New("go-protocol-v2 requires decode capability")
	}
	if seen["ingress"] && strings.ToLower(manifest.PayloadFormat) != "hex" {
		return errors.New("ingress requires hex payloadFormat")
	}
	if len(manifest.Entrypoints) == 0 {
		return errors.New("manifest entrypoints are required")
	}
	if strings.TrimSpace(manifest.Transport) == "" || strings.TrimSpace(manifest.PayloadFormat) == "" {
		return errors.New("manifest transport and payloadFormat are required")
	}
	for platform, entrypoint := range manifest.Entrypoints {
		if !protocolSegmentV2.MatchString(platform) {
			return fmt.Errorf("invalid entrypoint platform %q", platform)
		}
		clean := filepath.ToSlash(filepath.Clean(entrypoint))
		if entrypoint == "" || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(entrypoint) {
			return fmt.Errorf("unsafe entrypoint for %s", platform)
		}
	}
	return nil
}

func writeExclusiveFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

func (s *Server) publishProtocolReleaseV2(w http.ResponseWriter, r *http.Request) {
	tenant, id, version := claims(r).TenantID, r.PathValue("id"), r.PathValue("version")
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, version)
	if err != nil {
		problem(w, 404, "protocol release not found")
		return
	}
	if release.Status == "REVOKED" {
		problem(w, 409, "revoked release cannot be published")
		return
	}
	if err = validateProtocolReleaseV2(release); err != nil {
		problem(w, 422, err.Error())
		return
	}
	if release.ParserType == parser.GoProtocolParserName && artifactTestCountV2(release.Artifact) == 0 {
		problem(w, 422, "custom protocol release has no passing package test cases")
		return
	}
	now := time.Now().UnixMilli()
	if err = s.engine.Repo.UpdateProtocolReleaseStatus(r.Context(), tenant, id, version, "PUBLISHED", now); err != nil {
		problem(w, 500, err.Error())
		return
	}
	release.Status = "PUBLISHED"
	release.PublishedAt = now
	s.audit(r, "protocol.v2.release.publish", "protocolRelease", id+"@"+version, nil)
	write(w, 200, release)
}

func (s *Server) bindProductProtocolV2(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProtocolID string `json:"protocolId"`
		Version    string `json:"version"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	binding, err := s.bindProtocolRelease(r, in.ProtocolID, in.Version, r.PathValue("id"))
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 200, binding)
}

func (s *Server) rollbackProductProtocolV2(w http.ResponseWriter, r *http.Request) {
	tenant, productID := claims(r).TenantID, r.PathValue("id")
	current, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, productID)
	if err != nil || current.PreviousVersion == "" {
		problem(w, 409, "there is no previous protocol release to roll back to")
		return
	}
	binding, err := s.bindProtocolRelease(r, firstNonBlank(current.PreviousProtocolID, current.ProtocolID), current.PreviousVersion, productID)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 200, binding)
}

func (s *Server) bindProtocolRelease(r *http.Request, protocolID, version, productID string) (model.ProductProtocolBinding, error) {
	tenant := claims(r).TenantID
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, protocolID, version)
	if err != nil {
		return model.ProductProtocolBinding{}, errors.New("protocol release not found")
	}
	if release.Status != "PUBLISHED" {
		return model.ProductProtocolBinding{}, errors.New("only a published protocol release can be bound")
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), tenant, productID)
	if err != nil {
		return model.ProductProtocolBinding{}, errors.New("product not found")
	}
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant)
	if err != nil {
		return model.ProductProtocolBinding{}, err
	}
	for _, profile := range profiles {
		if profile.Enabled && profile.ProductID == productID && len(profile.Queries) > 0 && !protocolworker.HasCapability(release, "encode") {
			return model.ProductProtocolBinding{}, errors.New("该产品已有定时查询，新版本必须保留 encode 能力")
		}
		for _, mapping := range profile.ChildProducts {
			if profile.Enabled && mapping.ProductID == productID && release.PayloadFormat != "hex" {
				return model.ProductProtocolBinding{}, errors.New("子设备接入映射要求 HEX 解析协议")
			}
		}
		if profile.Enabled && profile.Mode == "listener" && profile.ProductID == productID && !listenerSupports(release, profile.Network) {
			return model.ProductProtocolBinding{}, errors.New("新版本不支持该产品已启用的 TCP/UDP 接入实例")
		}
	}
	previous, previousProtocol := "", ""
	if old, getErr := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, productID); getErr == nil {
		if old.ProtocolID == protocolID && old.Version == version {
			return old, nil
		}
		previous = old.Version
		previousProtocol = old.ProtocolID
	}
	binding := model.ProductProtocolBinding{TenantID: tenant, ProductID: productID, ProtocolID: protocolID, Version: version, PreviousVersion: previous, PreviousProtocolID: previousProtocol, UpdatedAt: time.Now().UnixMilli()}
	shim := legacyProtocolShim(release)
	if err = s.engine.Repo.SaveProtocolPackage(r.Context(), shim); err != nil {
		return binding, err
	}
	product.ProtocolPackageID = shim.ID
	product.Transport = release.Transport
	product.PayloadFormat = release.PayloadFormat
	product.UpdatedAt = binding.UpdatedAt
	if err = s.engine.Repo.SaveProduct(r.Context(), product); err != nil {
		return binding, err
	}
	if err = s.engine.Repo.SaveProductProtocolBinding(r.Context(), binding); err != nil {
		return binding, err
	}
	s.audit(r, "protocol.v2.binding.switch", "product", productID, map[string]any{"protocolId": protocolID, "version": version, "previousVersion": previous})
	return binding, nil
}

// Existing Modbus releases remain readable; new specialized protocols use Go packages.
func (s *Server) importModbusTCPV2(w http.ResponseWriter, r *http.Request) {
	problem(w, 422, "新增 Modbus 协议请将点表和解析逻辑放入 Go 源码包上传；旧采集实例继续运行")
}

func (s *Server) deviceAccessProfilesV2(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), claims(r).TenantID)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	for i := range items {
		if items[i].Mode == "listener" {
			if !items[i].Enabled {
				items[i].RuntimeStatus = "DISABLED"
				items[i].LastError = ""
			} else if runtime, ok := s.protocolListeners.(interface {
				Status(string, string) (string, string, int64)
			}); ok {
				items[i].RuntimeStatus, items[i].LastError, items[i].LastSuccessAt = runtime.Status(items[i].TenantID, items[i].ID)
			}
			if binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), items[i].TenantID, items[i].ProductID); err == nil {
				items[i].ProtocolID, items[i].ProtocolVersion = binding.ProtocolID, binding.Version
			}
		}
	}
	write(w, 200, map[string]any{"items": items, "count": len(items)})
}
func (s *Server) saveDeviceAccessProfileV2(w http.ResponseWriter, r *http.Request) {
	var v model.DeviceAccessProfile
	if decode(w, r, &v) != nil {
		return
	}
	v.TenantID = claims(r).TenantID
	v.Mode = strings.ToLower(strings.TrimSpace(v.Mode))
	v.Network = strings.ToLower(strings.TrimSpace(v.Network))
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	if err := validateAccessProfile(v); err != nil {
		problem(w, 422, err.Error())
		return
	}
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), v.TenantID, v.ProtocolID, v.ProtocolVersion)
	if err != nil || release.Status != "PUBLISHED" {
		problem(w, 422, "a published protocol release is required")
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), v.TenantID, v.ProductID)
	if err != nil {
		problem(w, 422, "product not found")
		return
	}
	if v.Mode == "listener" {
		if !listenerSupports(release, v.Network) {
			problem(w, 422, "请选择支持该 TCP/UDP 网络与 ingress 能力的完整 Go 协议包")
			return
		}
		if v.ConnectionMode != "dial" {
			v.DeviceID = ""
		} else {
			device, e := s.engine.Repo.GetManagedDevice(r.Context(), v.TenantID, v.DeviceID)
			if e != nil || device.ProductID != v.ProductID || device.GatewayID != "" {
				problem(w, 422, "主动连接需要已配置的主设备")
				return
			}
		}
		profiles, listErr := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), "")
		if listErr != nil {
			problem(w, 500, "读取接入实例失败")
			return
		}
		for _, other := range profiles {
			if v.ConnectionMode != "dial" && other.ConnectionMode != "dial" && v.Enabled && other.Enabled && other.Mode == "listener" && other.Network == v.Network && other.Port == v.Port && (other.TenantID != v.TenantID || other.ID != v.ID) {
				problem(w, 409, "该监听端口已由另一个接入实例占用")
				return
			}
		}
	} else {
		if (v.WireFormat == "rtu_over_tcp") != (release.Transport == "MODBUS_RTU") {
			problem(w, 422, "serial profile and release transport must match")
			return
		}
		device, err := s.engine.Repo.GetManagedDevice(r.Context(), v.TenantID, v.DeviceID)
		if err != nil || device.ProductID != product.ID {
			problem(w, 422, "device not found or does not belong to product")
			return
		}
	}
	if err := onboarding.ValidateChildProducts(r.Context(), s.engine.Repo, v, release); err != nil {
		problem(w, 422, err.Error())
		return
	}
	binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), v.TenantID, v.ProductID)
	if err != nil || binding.ProtocolID != v.ProtocolID || binding.Version != v.ProtocolVersion {
		problem(w, 422, "access profile protocol must match the product's active protocol binding")
		return
	}
	now := time.Now().UnixMilli()
	if old, getErr := s.engine.Repo.GetDeviceAccessProfile(r.Context(), v.TenantID, v.ID); getErr == nil {
		v.CreatedAt = old.CreatedAt
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	v.RuntimeStatus, v.LastError = "PENDING", ""
	if !v.Enabled {
		v.RuntimeStatus = "DISABLED"
	}
	if err = s.engine.Repo.SaveDeviceAccessProfile(r.Context(), v); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "protocol.v2.access.save", "deviceAccessProfile", v.ID, map[string]any{"deviceId": v.DeviceID, "enabled": v.Enabled})
	write(w, 201, v)
}
func (s *Server) testDeviceAccessProfileV2(w http.ResponseWriter, r *http.Request) {
	profile, err := s.engine.Repo.GetDeviceAccessProfile(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "device access profile not found")
		return
	}
	if profile.Mode == "listener" {
		problem(w, 422, "TCP/UDP 监听请使用设备或协议模拟器发送报文验证")
		return
	}
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), profile.TenantID, profile.ProtocolID, profile.ProtocolVersion)
	if err != nil {
		problem(w, 422, "protocol release not found")
		return
	}
	if err := validateAccessProfile(profile); err != nil {
		problem(w, 422, err.Error())
		return
	}
	blocks, err := releaseBlocksForAPI(release)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	ctx, cancel := contextWithMaximum(r, 10*time.Second)
	defer cancel()
	raws, err := protocolruntime.ReadModbusTCPWithPolicy(ctx, profile, release, blocks[:1], s.cfg.ModbusAllowedCIDRs)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	message, err := s.engine.Parsers.ParseWithConfig(release.ParserType, release.Config, raws[0])
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 200, map[string]any{"request": raws[0].Metadata, "response": raws[0].Payload, "standardMessage": message})
}

func legacyProtocolShim(release model.ProtocolRelease) model.ProtocolPackage {
	return model.ProtocolPackage{ID: release.ProtocolID + "@" + release.Version, TenantID: release.TenantID, Name: release.ProtocolID + " " + release.Version, Version: release.Version, Protocol: release.ProtocolID, Transport: release.Transport, PayloadFormat: release.PayloadFormat, ParserType: release.ParserType, Status: "PUBLISHED", Description: "Protocol v2 compatibility binding", Config: release.Config, CreatedAt: release.CreatedAt, UpdatedAt: release.PublishedAt}
}
func validateAccessProfile(v model.DeviceAccessProfile) error {
	if err := model.ValidateProtocolAccess(v); err != nil {
		return err
	}
	if v.EdgeNodeID != "" {
		return errors.New("边缘节点功能已移除，请使用中心直接接入")
	}
	if v.Mode == "listener" {
		return validateListenerProfile(v)
	}
	if v.Mode != "" && v.Mode != "poll" {
		return errors.New("不支持的设备接入模式")
	}
	if v.Network != "" && v.Network != "tcp" {
		return errors.New("中心轮询仅支持 Modbus TCP")
	}
	if v.ID == "" || v.DeviceID == "" || v.ProductID == "" || v.ProtocolID == "" || v.ProtocolVersion == "" || v.Host == "" {
		return errors.New("id, deviceId, productId, protocolId, protocolVersion and host are required")
	}
	if v.Port <= 0 || v.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if v.UnitID < 0 || v.UnitID > 255 {
		return errors.New("unitId must be between 0 and 255")
	}
	if v.TimeoutMs <= 0 || v.TimeoutMs > 30000 {
		return errors.New("timeoutMs must be between 1 and 30000")
	}
	if v.Retries < 0 || v.Retries > 5 {
		return errors.New("retries must be between 0 and 5")
	}
	return nil
}
func releaseBlocksForAPI(release model.ProtocolRelease) ([]model.ModbusReadBlock, error) {
	b, err := json.Marshal(release.Config["blocks"])
	if err != nil {
		return nil, err
	}
	var blocks []model.ModbusReadBlock
	if err = json.Unmarshal(b, &blocks); err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, errors.New("release does not contain collection blocks")
	}
	return blocks, nil
}
func validateProtocolReleaseV2(release model.ProtocolRelease) error {
	if strings.TrimSpace(release.Transport) == "" || strings.TrimSpace(release.PayloadFormat) == "" {
		return errors.New("transport and payloadFormat are required")
	}
	switch release.ParserType {
	case parser.PollResponseParserName:
		_, err := parser.PollPoints(release.Config)
		return err
	case parser.ModbusTCPParserName, parser.ModbusRTUParserName:
		_, err := releaseBlocksForAPI(release)
		return err
	case parser.GoProtocolParserName:
		artifact, ok := release.Config["artifact"].(map[string]any)
		if !ok || strings.TrimSpace(fmt.Sprint(artifact["path"])) == "" {
			return errors.New("custom protocol release artifact is missing")
		}
	case "configurable_json_parser", "configurable_hex_parser":
		if len(release.Config) == 0 {
			return errors.New("configurable protocol release config is required")
		}
	default:
		return errors.New("unsupported protocol v2 parserType")
	}
	return nil
}
func jsonValue(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func artifactTestCountV2(artifact map[string]any) int {
	value := artifact["testCases"]
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}
func formIntStrict(r *http.Request, name string, fallback int) (int, error) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return n, nil
}
func formBoolStrict(r *http.Request, name string, fallback bool) (bool, error) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return n, nil
}
func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func contextWithMaximum(r *http.Request, maximum time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := r.Context().Deadline(); ok && time.Until(deadline) < maximum {
		return context.WithCancel(r.Context())
	}
	return context.WithTimeout(r.Context(), maximum)
}
