package edgeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/fieldprotocol"
	"iot-platform/internal/model"
	"time"
)

func (a *Agent) discoverONVIF(ctx context.Context, p model.DeviceAccessProfile, release model.ProtocolRelease) ([]model.RawMessage, error) {
	result, err := fieldprotocol.DiscoverONVIF(ctx, fieldprotocol.DiscoveryOptions{InterfaceAddress: p.Host, AllowedInterfaces: a.options.DiscoveryInterfaces, AllowedCIDRs: a.options.AllowedCIDRs, ProbeAddress: a.options.DiscoveryProbeAddress})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return []model.RawMessage{{MessageID: fmt.Sprintf("onvif-discovery-%d", now.UnixNano()), TenantID: p.TenantID, ProductID: p.ProductID, DeviceID: p.DeviceID, ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, Protocol: release.ProtocolID, Transport: release.Transport, ReceivedAt: now.UnixMilli(), PayloadFormat: "json", Payload: data, Source: "edge-discovery", CollectorID: a.options.NodeID, Metadata: map[string]any{"authentication": "none-ws-discovery", "representation": "discovery-candidates"}}}, nil
}
