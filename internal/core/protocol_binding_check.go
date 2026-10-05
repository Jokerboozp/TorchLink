package core

import (
	"context"
	"errors"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

// UnboundProduct is a device template whose raw messages have no protocol to
// parse them unless they arrive in the platform's standard format.
type UnboundProduct struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ProtocolPackageID string `json:"protocolPackageId,omitempty"`
	Reason            string `json:"reason"`
}

// UnboundProducts lists the tenant's templates that resolve no protocol the
// way the parser stage does: a usable protocol release binding, else a
// published protocol package. Templates of the standard protocol are bound by
// definition.
func (e *Engine) UnboundProducts(ctx context.Context, tenant string) ([]UnboundProduct, error) {
	products, err := e.Repo.ListProducts(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := []UnboundProduct{}
	for _, p := range products {
		if strings.HasPrefix(p.ProtocolPackageID, parser.StandardProtocolID+"@") {
			continue
		}
		binding, err := e.Repo.GetProductProtocolBinding(ctx, tenant, p.ID)
		switch {
		case err == nil:
			release, releaseErr := e.Repo.GetProtocolRelease(ctx, tenant, binding.ProtocolID, binding.Version)
			if releaseErr == nil && release.Status != "REVOKED" {
				continue
			}
			if releaseErr != nil && !errors.Is(releaseErr, model.ErrNotFound) {
				return nil, releaseErr
			}
		case !errors.Is(err, model.ErrNotFound):
			return nil, err
		}
		reason := "未绑定协议版本"
		if p.ProtocolPackageID != "" {
			pkg, pkgErr := e.Repo.GetProtocolPackage(ctx, tenant, p.ProtocolPackageID)
			if pkgErr == nil && pkg.Status == "PUBLISHED" {
				continue
			}
			if pkgErr != nil && !errors.Is(pkgErr, model.ErrNotFound) {
				return nil, pkgErr
			}
			reason = "协议包不存在或未发布"
		}
		out = append(out, UnboundProduct{ID: p.ID, Name: p.Name, ProtocolPackageID: p.ProtocolPackageID, Reason: reason})
	}
	return out, nil
}
