package protocolruntime

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/protocolworker"
	"strings"
)

func (s *listenerSession) ingestChildren(p model.DeviceAccessProfile, parent model.ManagedDevice, source model.RawMessage, children []protocolworker.ChildFrame) error {
	if len(children) > 256 {
		return errors.New("too many child observations")
	}
	for index, child := range children {
		var product string
		for _, v := range p.ChildProducts {
			if v.Type == child.Type {
				product = v.ProductID
			}
		}
		if product == "" {
			return fmt.Errorf("子设备类型 %q 未配置；发现信息已随主设备原文保留", child.Type)
		}
		repo := s.host.owner.repo
		binding, err := repo.GetProductProtocolBinding(s.host.ctx, p.TenantID, product)
		if err != nil {
			return err
		}
		release, err := repo.GetProtocolRelease(s.host.ctx, p.TenantID, binding.ProtocolID, binding.Version)
		if err != nil {
			return err
		}
		if len(s.pending) > 0 && s.commandChild != nil && child.Address == s.commandChild.Tags["childAddress"] && product == s.commandChild.ProductID {
			release = s.childRelease
		}
		if release.Status != "PUBLISHED" || !strings.EqualFold(release.PayloadFormat, "hex") {
			return errors.New("子设备需要已发布的 HEX 解析协议")
		}
		data, err := hex.DecodeString(child.Payload)
		if err != nil || len(data) > protocolworker.MaxFrameBytes {
			return errors.New("子设备报文无效")
		}
		device, _, err := repo.RegisterProtocolChild(s.host.ctx, p, parent.ID, child.ChildIdentity)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			continue
		} // Inventory-only registration is not telemetry.
		payload, _ := json.Marshal(strings.ToUpper(child.Payload))
		raw := model.RawMessage{MessageID: fmt.Sprintf("%s_child_%d", source.MessageID, index), TenantID: p.TenantID, ProductID: product, DeviceID: device.ID, DeviceName: device.Name, GatewayID: parent.ID, Protocol: release.ProtocolID, ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, PointTableVersion: release.PointTableVersion, Source: "go-protocol-child", Transport: source.Transport, PayloadFormat: "hex", Payload: payload, ReceivedAt: source.ReceivedAt, RemoteAddress: source.RemoteAddress, Metadata: map[string]any{"parentRawMessageId": source.MessageID, "parentProtocolId": source.ProtocolID, "parentProtocolVersion": source.ProtocolVersion, "childAddress": child.Address, "childType": child.Type, "profileId": p.ID}}
		if err = s.host.owner.ingest(s.host.ctx, raw); err != nil {
			return err
		}
	}
	return nil
}

// Child codecs encode the inner payload; the parent codec owns the envelope.
func (s *listenerSession) encodeCommand(ctx context.Context, p model.DeviceAccessProfile, child *model.ManagedDevice, command map[string]any) (protocolworker.Response, error) {
	if child == nil {
		return s.invoke(ctx, p, protocolworker.Request{Operation: "encode", Command: command})
	}
	empty := protocolworker.Response{}
	if child.Status != "ENABLED" || child.DeviceRole != "CHILD" || child.Tags["connectorProfileId"] != p.ID {
		return empty, errors.New("子设备不可控制或不属于接入实例")
	}
	allowed := false
	for _, mapping := range p.ChildProducts {
		if mapping.Type == child.Tags["childType"] && mapping.ProductID == child.ProductID {
			allowed = true
		}
	}
	if !allowed {
		return empty, errors.New("子设备产品映射已变更")
	}
	repo := s.host.owner.repo
	product, err := repo.GetProduct(ctx, p.TenantID, child.ProductID)
	if err != nil || product.Status != "ENABLED" {
		return empty, errors.New("子设备产品不可用")
	}
	binding, err := repo.GetProductProtocolBinding(ctx, p.TenantID, child.ProductID)
	if err != nil {
		return empty, err
	}
	release, err := repo.GetProtocolRelease(ctx, p.TenantID, binding.ProtocolID, binding.Version)
	if err != nil {
		return empty, err
	}
	if release.Status != "PUBLISHED" || !protocolworker.HasCapability(release, "encode") {
		return empty, errors.New("子设备协议不支持命令")
	}
	original, state := s.release, s.state
	s.release, s.state = release, nil
	inner, err := s.invoke(ctx, p, protocolworker.Request{Operation: "encode", Command: command, DeviceID: child.ID})
	s.release, s.state = original, state
	if err != nil {
		return empty, err
	}
	if len(inner.State) > 0 && string(inner.State) != "null" && string(inner.State) != "{}" {
		return empty, errors.New("子设备下行暂不支持独立会话状态，请在主协议维护会话")
	}
	if data, e := hex.DecodeString(inner.Reply); e != nil || len(data) == 0 {
		return empty, errors.New("子设备命令报文无效")
	}
	outer, err := s.invoke(ctx, p, protocolworker.Request{Operation: "encode", Command: map[string]any{"type": "child", "address": child.Tags["childAddress"], "childType": child.Tags["childType"], "payload": inner.Reply, "correlationId": inner.CorrelationID}})
	if err == nil {
		s.commandChild = child
		s.childRelease = release
	}
	return outer, err
}
