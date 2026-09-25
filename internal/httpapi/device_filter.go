package httpapi

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"iot-platform/internal/ports"
)

var (
	deviceRoles        = map[string]bool{"DIRECT": true, "GATEWAY": true, "CHILD": true}
	deviceEnableStates = map[string]bool{"ENABLED": true, "DISABLED": true}
	deviceRuntimeState = map[string]bool{"ONLINE": true, "ALARM": true, "SUSPECTED_OFFLINE": true, "OFFLINE": true, "NEVER_SEEN": true, "UNKNOWN": true}
)

// deviceFilter turns list query parameters into a repository filter. Product
// category is resolved to product IDs because categories belong to templates.
func (s *Server) deviceFilter(ctx context.Context, tenant string, query url.Values) (ports.DeviceFilter, error) {
	f := ports.DeviceFilter{
		TenantID: tenant,
		Role:     strings.ToUpper(strings.TrimSpace(query.Get("role"))),
		Query:    strings.TrimSpace(query.Get("q")),
		Status:   strings.ToUpper(strings.TrimSpace(query.Get("status"))),
		Runtime:  strings.ToUpper(strings.TrimSpace(query.Get("runtime"))),
	}
	if f.Role != "" && !deviceRoles[f.Role] {
		return f, errors.New("设备角色须为 DIRECT、GATEWAY 或 CHILD")
	}
	if f.Status != "" && !deviceEnableStates[f.Status] {
		return f, errors.New("设备启用状态须为 ENABLED 或 DISABLED")
	}
	if f.Runtime != "" && !deviceRuntimeState[f.Runtime] {
		return f, errors.New("不支持的设备运行状态")
	}
	if len([]rune(f.Query)) > 128 {
		return f, errors.New("关键字不能超过 128 个字符")
	}
	productID := strings.TrimSpace(query.Get("productId"))
	category := strings.TrimSpace(query.Get("category"))
	if f.Role == "" && productID == "" && category == "" {
		return f, nil
	}
	products, err := s.engine.Repo.ListProducts(ctx, tenant)
	if err != nil {
		return f, err
	}
	for _, p := range products {
		if p.Category == "gateway" {
			f.GatewayProductIDs = append(f.GatewayProductIDs, p.ID)
		}
	}
	if productID == "" && category == "" {
		return f, nil
	}
	f.RestrictProducts = true
	f.ProductIDs = []string{}
	for _, p := range products {
		productCategory := p.Category
		if productCategory == "" {
			productCategory = "other"
		}
		if (productID == "" || p.ID == productID) && (category == "" || productCategory == category) {
			f.ProductIDs = append(f.ProductIDs, p.ID)
		}
	}
	return f, nil
}
