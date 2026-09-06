package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
)

type protocolCommander interface {
	Command(context.Context, string, string, string, map[string]any) (map[string]any, error)
}

func (s *Server) SetProtocolListeners(listeners protocolCommander) { s.protocolListeners = listeners }

func (s *Server) protocolDeviceCommand(w http.ResponseWriter, r *http.Request) {
	if s.protocolListeners == nil {
		problem(w, 503, "通用协议接入服务未启动")
		return
	}
	var command map[string]any
	if decode(w, r, &command) != nil {
		return
	}
	if strings.TrimSpace(firstNonBlankString(command["type"])) == "" {
		problem(w, 422, "请填写协议包支持的命令 type")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.protocolListeners.Command(ctx, claims(r).TenantID, r.PathValue("id"), r.PathValue("deviceId"), command)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	s.audit(r, "protocol.command", "device", r.PathValue("deviceId"), map[string]any{"profileId": r.PathValue("id"), "commandType": command["type"]})
	write(w, 200, result)
}

func firstNonBlankString(value any) string { text, _ := value.(string); return text }

func validateListenerProfile(v model.DeviceAccessProfile) error {
	if !protocolSegmentV2.MatchString(v.ID) || v.ProductID == "" || v.ProtocolID == "" || v.ProtocolVersion == "" {
		return errors.New("接入实例标识、产品和协议版本不能为空")
	}
	if v.Network != "tcp" && v.Network != "udp" {
		return errors.New("监听网络须为 tcp 或 udp")
	}
	if net.ParseIP(v.Host) == nil {
		return errors.New("监听地址须为本机 IP，例如 0.0.0.0 或 127.0.0.1")
	}
	if v.Port < 1 || v.Port > 65535 {
		return errors.New("监听端口须为 1 至 65535")
	}
	if v.TimeoutMs < 1 || v.TimeoutMs > 30000 {
		return errors.New("命令超时须为 1 至 30000 毫秒")
	}
	return nil
}

func listenerSupports(release model.ProtocolRelease, network string) bool {
	transport := strings.ToUpper(release.Transport)
	return release.ParserType == parser.GoProtocolParserName && release.Artifact["runtime"] == protocolworker.Runtime &&
		protocolworker.HasCapability(release, "ingress") && (transport == strings.ToUpper(network) || transport == "TCP_UDP")
}
