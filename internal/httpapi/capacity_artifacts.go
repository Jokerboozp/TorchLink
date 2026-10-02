package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Recheck the complete product before allowing a product-wide broker sweep or
// removing its private protocol files. Prefixes alone never establish ownership.
func (s *Server) capacityFinalFixture(ctx context.Context, tenant, product string) (model.CapacityFixtureProduct, error) {
	lister, ok := s.unscopedRepo().(ports.CapacityFixtureLister)
	if !ok {
		return model.CapacityFixtureProduct{}, errors.New("存储不支持核对专用测试产品")
	}
	for after := ""; ; {
		page, err := lister.ListCapacityFixtureProducts(ctx, tenant, after, 100)
		if err != nil {
			return model.CapacityFixtureProduct{}, err
		}
		for _, p := range page {
			if p.ProductID == product {
				if p.DeviceCount != 0 || p.BlockedReason != "" {
					return p, model.ErrResourceInUse
				}
				return p, nil
			}
		}
		if len(page) < 100 || page[len(page)-1].ProductID >= product {
			return model.CapacityFixtureProduct{}, model.ErrResourceInUse
		}
		next := page[len(page)-1].ProductID
		if next <= after {
			return model.CapacityFixtureProduct{}, errors.New("历史产品分页没有前进")
		}
		after = next
	}
}

// Only the exact private protocol directory may be removed. An unexpected
// path or link keeps the protocol row and surfaces a partial-cleanup warning.
func (s *Server) removeCapacityProtocolArtifacts(tenant, id string) error {
	target, err := s.capacityProtocolArtifactPath(tenant, id)
	if err != nil || target == "" {
		return err
	}
	return os.RemoveAll(target)
}

func (s *Server) capacityProtocolArtifactPath(tenant, id string) (string, error) {
	if s.cfg.DataDir == "" || !protocolSegmentV2.MatchString(id) || tenant == "" || tenant == "." || tenant == ".." || filepath.Base(tenant) != tenant || strings.ContainsAny(tenant, `/\`) {
		return "", errors.New("无法定位测试协议制品")
	}
	root, err := filepath.Abs(s.cfg.DataDir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, "protocol-releases", tenant, id)
	current := root
	for _, part := range []string{"", "protocol-releases", tenant, id} {
		current = filepath.Join(current, part)
		st, e := os.Lstat(current)
		if os.IsNotExist(e) {
			return "", nil
		}
		if e != nil {
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", errors.New("测试协议制品路径包含链接或非目录")
		}
	}
	if err = filepath.WalkDir(target, func(_ string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return errors.New("测试协议制品包含链接或特殊文件")
		}
		return nil
	}); err != nil {
		return "", err
	}
	return target, nil
}
