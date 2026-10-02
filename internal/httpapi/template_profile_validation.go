package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

// Shared listeners are validated from the candidate itself. Device-specific
// connections remain live, so their wire requirements must also fit a switch.
func (s *Server) validateTemplateDeviceProfiles(r *http.Request, release *model.ProtocolRelease, productID string) error {
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), claims(r).TenantID)
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if !profile.Enabled {
			continue
		}
		for _, mapping := range profile.ChildProducts {
			if mapping.ProductID == productID && release.PayloadFormat != "hex" {
				return fmt.Errorf("接入点 %s 的子设备映射要求候选协议保留 HEX 解析能力", profile.ID)
			}
		}
		if profile.ProductID != productID || (profile.DeviceID == "" && profile.Mode == "listener" && profile.ConnectionMode != "dial") {
			continue
		}
		if err = validateAccessProfile(profile); err != nil {
			return fmt.Errorf("设备接入配置 %s 无效：%w", profile.ID, err)
		}
		if profile.Mode == "listener" {
			if !listenerSupports(*release, profile.Network) {
				return fmt.Errorf("候选协议不支持设备接入点 %s 的 %s 网络与 ingress 能力", profile.ID, strings.ToUpper(profile.Network))
			}
		} else {
			want := "MODBUS_TCP"
			if profile.WireFormat == "rtu_over_tcp" {
				want = "MODBUS_RTU"
			}
			if release.Transport != want {
				return fmt.Errorf("设备轮询配置 %s 要求 %s 协议，请保留对应报文格式", profile.ID, want)
			}
			if _, err = releaseBlocksForAPI(*release); err != nil {
				return fmt.Errorf("设备轮询配置 %s 缺少可执行点表：%w", profile.ID, err)
			}
		}
		if err = onboarding.ValidateChildProducts(r.Context(), s.engine.Repo, profile, *release); err != nil {
			return fmt.Errorf("设备接入配置 %s 不兼容：%w", profile.ID, err)
		}
	}
	return nil
}
