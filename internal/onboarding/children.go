package onboarding

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
)

func ValidateChildProducts(ctx context.Context, repo ports.Repository, p model.DeviceAccessProfile, parent model.ProtocolRelease) error {
	if len(p.Queries) > 0 && !protocolworker.HasCapability(parent, "encode") {
		return errors.New("定时查询需要协议 encode 能力")
	}
	for _, mapping := range p.ChildProducts {
		product, err := repo.GetProduct(ctx, p.TenantID, mapping.ProductID)
		if err != nil || product.Status != "ENABLED" {
			return errors.New("子设备产品不存在或未启用")
		}
		binding, err := repo.GetProductProtocolBinding(ctx, p.TenantID, product.ID)
		if err != nil {
			return errors.New("请先为每种子设备产品绑定协议")
		}
		release, err := repo.GetProtocolRelease(ctx, p.TenantID, binding.ProtocolID, binding.Version)
		if err != nil || release.Status != "PUBLISHED" || release.PayloadFormat != "hex" {
			return errors.New("子设备产品需要已发布的 HEX 报文解析协议")
		}
	}
	return nil
}
