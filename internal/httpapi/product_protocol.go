package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

// Products can be created before any devices or access instances exist.
// Resolve only immutable, published releases; uploaded drafts remain unusable.
func (s *Server) productProtocol(ctx context.Context, tenant, id string) (model.ProtocolPackage, error) {
	if id == parser.StandardProtocolID+"@1.0.0" {
		release, err := s.engine.Repo.GetProtocolRelease(ctx, tenant, parser.StandardProtocolID, "1.0.0")
		if errors.Is(err, model.ErrNotFound) {
			now := time.Now().UnixMilli()
			release = model.ProtocolRelease{TenantID: tenant, ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Transport: "MQTT_HTTP", PayloadFormat: "json", ParserType: parser.StandardParserName, Status: "PUBLISHED", CreatedAt: now, PublishedAt: now}
			if err = s.engine.Repo.CreateProtocolRelease(ctx, release); err != nil {
				// Another product may have initialized the same tenant's release.
				release, err = s.engine.Repo.GetProtocolRelease(ctx, tenant, parser.StandardProtocolID, "1.0.0")
			}
		}
		if err != nil {
			return model.ProtocolPackage{}, err
		}
		if release.Status != "PUBLISHED" || release.ParserType != parser.StandardParserName {
			return model.ProtocolPackage{}, fmt.Errorf("标准协议当前不可用")
		}
		pkg := legacyProtocolShim(release)
		pkg.Name = "标准设备上报"
		return pkg, s.engine.Repo.SaveProtocolPackage(ctx, pkg)
	}
	pkg, err := s.engine.Repo.GetProtocolPackage(ctx, tenant, id)
	if err == nil {
		if pkg.ParserType == parser.GoProtocolParserName {
			release, err := s.engine.Repo.GetProtocolRelease(ctx, tenant, pkg.Protocol, pkg.Version)
			if err != nil {
				return pkg, err
			}
			if release.Status != "PUBLISHED" {
				return pkg, fmt.Errorf("请选择已发布的协议版本")
			}
		}
		return pkg, nil
	}
	if !errors.Is(err, model.ErrNotFound) {
		return pkg, err
	}
	index := strings.LastIndex(id, "@")
	if index <= 0 {
		return pkg, err
	}
	release, err := s.engine.Repo.GetProtocolRelease(ctx, tenant, id[:index], id[index+1:])
	if err != nil {
		return pkg, err
	}
	if release.Status != "PUBLISHED" {
		return pkg, fmt.Errorf("请选择已发布的协议版本")
	}
	pkg = legacyProtocolShim(release)
	return pkg, s.engine.Repo.SaveProtocolPackage(ctx, pkg)
}
