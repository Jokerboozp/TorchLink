package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                              /* 执行当前语句并推进处理流程。 */
	"encoding/hex"                         /* 执行当前语句并推进处理流程。 */
	"encoding/json"                        /* 执行当前语句并推进处理流程。 */
	"errors"                               /* 执行当前语句并推进处理流程。 */
	"fmt"                                  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker" /* 执行当前语句并推进处理流程。 */
	"strings"                              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (s *listenerSession) ingestChildren(p model.DeviceAccessProfile, parent model.ManagedDevice, source model.RawMessage, children []protocolworker.ChildFrame) error { /* 定义 ingestChildren 函数。 */
	if len(children) > 256 { /* 判断条件并选择处理分支。 */
		return errors.New("too many child observations") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for index, child := range children { /* 循环处理当前数据。 */
		var product string                  /* 声明 product。 */
		for _, v := range p.ChildProducts { /* 循环处理当前数据。 */
			if v.Type == child.Type { /* 判断条件并选择处理分支。 */
				product = v.ProductID /* 更新 product 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if product == "" { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("子设备类型 %q 未配置；发现信息已随主设备原文保留", child.Type) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		repo := s.host.owner.repo                                                       /* 更新 repo 的值。 */
		binding, err := repo.GetProductProtocolBinding(s.host.ctx, p.TenantID, product) /* 更新 err 的值。 */
		if err != nil {                                                                 /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		release, err := repo.GetProtocolRelease(s.host.ctx, p.TenantID, binding.ProtocolID, binding.Version) /* 更新 err 的值。 */
		if err != nil {                                                                                      /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(s.pending) > 0 && s.commandChild != nil && child.Address == s.commandChild.ChildAddress && product == s.commandChild.ProductID { /* 判断条件并选择处理分支。 */
			release = s.childRelease /* 更新 release 的值。 */
		} /* 结束当前表达式或代码块。 */
		if release.Status != "PUBLISHED" || !strings.EqualFold(release.PayloadFormat, "hex") { /* 判断条件并选择处理分支。 */
			return errors.New("子设备需要已发布的 HEX 解析协议") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		data, err := hex.DecodeString(child.Payload)                /* 更新 err 的值。 */
		if err != nil || len(data) > protocolworker.MaxFrameBytes { /* 判断条件并选择处理分支。 */
			return errors.New("子设备报文无效") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		device, _, err := repo.RegisterProtocolChild(s.host.ctx, p, parent.ID, child.ChildIdentity) /* 更新 err 的值。 */
		if err != nil {                                                                             /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(data) == 0 { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */ // Inventory-only registration is not telemetry.
		payload, _ := json.Marshal(strings.ToUpper(child.Payload))                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     /* 更新 _ 的值。 */
		raw := model.RawMessage{MessageID: fmt.Sprintf("%s_child_%d", source.MessageID, index), TenantID: p.TenantID, ProductID: product, DeviceID: device.ID, DeviceName: device.Name, GatewayID: parent.ID, Protocol: release.ProtocolID, ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, PointTableVersion: release.PointTableVersion, Source: "go-protocol-child", Transport: source.Transport, PayloadFormat: "hex", Payload: payload, ReceivedAt: source.ReceivedAt, RemoteAddress: source.RemoteAddress, Metadata: map[string]any{"parentRawMessageId": source.MessageID, "parentProtocolId": source.ProtocolID, "parentProtocolVersion": source.ProtocolVersion, "childAddress": child.Address, "childType": child.Type, "profileId": p.ID}} /* 更新 raw 的值。 */
		if err = s.host.owner.ingest(s.host.ctx, raw); err != nil {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Child codecs encode the inner payload; the parent codec owns the envelope.
func (s *listenerSession) encodeCommand(ctx context.Context, p model.DeviceAccessProfile, child *model.ManagedDevice, command map[string]any) (protocolworker.Response, error) { /* 定义 encodeCommand 函数。 */
	if child == nil { /* 判断条件并选择处理分支。 */
		return s.invoke(ctx, p, protocolworker.Request{Operation: "encode", Command: command}) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	empty := protocolworker.Response{}                                                                /* 更新 empty 的值。 */
	if child.Status != "ENABLED" || child.DeviceRole != "CHILD" || child.ConnectorProfileID != p.ID { /* 判断条件并选择处理分支。 */
		return empty, errors.New("子设备不可控制或不属于接入实例") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	allowed := false                          /* 更新 allowed 的值。 */
	for _, mapping := range p.ChildProducts { /* 循环处理当前数据。 */
		if mapping.Type == child.ChildType && mapping.ProductID == child.ProductID { /* 判断条件并选择处理分支。 */
			allowed = true /* 更新 allowed 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !allowed { /* 判断条件并选择处理分支。 */
		return empty, errors.New("子设备产品映射已变更") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	repo := s.host.owner.repo                                         /* 更新 repo 的值。 */
	product, err := repo.GetProduct(ctx, p.TenantID, child.ProductID) /* 更新 err 的值。 */
	if err != nil || product.Status != "ENABLED" {                    /* 判断条件并选择处理分支。 */
		return empty, errors.New("子设备产品不可用") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	binding, err := repo.GetProductProtocolBinding(ctx, p.TenantID, child.ProductID) /* 更新 err 的值。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		return empty, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release, err := repo.GetProtocolRelease(ctx, p.TenantID, binding.ProtocolID, binding.Version) /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		return empty, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.Status != "PUBLISHED" || !protocolworker.HasCapability(release, "encode") { /* 判断条件并选择处理分支。 */
		return empty, errors.New("子设备协议不支持命令") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	original, state := s.release, s.state                                                                             /* 更新 state 的值。 */
	s.release, s.state = release, nil                                                                                 /* 更新 s.state 的值。 */
	inner, err := s.invoke(ctx, p, protocolworker.Request{Operation: "encode", Command: command, DeviceID: child.ID}) /* 更新 err 的值。 */
	s.release, s.state = original, state                                                                              /* 更新 s.state 的值。 */
	if err != nil {                                                                                                   /* 判断条件并选择处理分支。 */
		return empty, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(inner.State) > 0 && string(inner.State) != "null" && string(inner.State) != "{}" { /* 判断条件并选择处理分支。 */
		return empty, errors.New("子设备下行暂不支持独立会话状态，请在主协议维护会话") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data, e := hex.DecodeString(inner.Reply); e != nil || len(data) == 0 { /* 判断条件并选择处理分支。 */
		return empty, errors.New("子设备命令报文无效") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	outer, err := s.invoke(ctx, p, protocolworker.Request{Operation: "encode", Command: map[string]any{"type": "child", "address": child.ChildAddress, "childType": child.ChildType, "payload": inner.Reply, "correlationId": inner.CorrelationID}}) /* 更新 err 的值。 */
	if err == nil {                                                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		s.commandChild = child   /* 更新 s.commandChild 的值。 */
		s.childRelease = release /* 更新 s.childRelease 的值。 */
	} /* 结束当前表达式或代码块。 */
	return outer, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
