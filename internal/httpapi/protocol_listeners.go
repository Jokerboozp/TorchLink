package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"errors"   /* 执行当前语句并推进处理流程。 */
	"net"      /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type protocolCommander interface { /* 定义 protocolCommander 类型。 */
	Command(context.Context, string, string, string, map[string]any) (map[string]any, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) SetProtocolListeners(listeners protocolCommander) { /* 定义 SetProtocolListeners 函数。 */
	s.protocolListeners = listeners         /* 更新 s.protocolListeners 的值。 */
	if status, ok := listeners.(interface { /* 判断条件并选择处理分支。 */
		Status(string, string) (string, string, int64) /* 执行当前语句并推进处理流程。 */
	}); ok { /* 结束当前表达式或代码块。 */
		s.onboarding.ListenerStatus = status.Status /* 更新 s.onboarding.ListenerStatus 的值。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) protocolDeviceCommand(w http.ResponseWriter, r *http.Request) { /* 定义 protocolDeviceCommand 函数。 */
	r.Body = http.MaxBytesReader(w, r.Body, 16384) /* 更新 r.Body 的值。 */
	var command map[string]any                     /* 声明 command。 */
	if decode(w, r, &command) != nil {             /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(firstNonBlankString(command["type"])) == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "请填写协议包支持的命令 type") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if command["confirmed"] != true { /* 判断条件并选择处理分支。 */
		problem(w, 422, "请人工确认设备命令") /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	delete(command, "confirmed")                                                                       /* 执行当前语句并推进处理流程。 */
	p, err := s.engine.Repo.GetDeviceAccessProfile(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                                    /* 判断条件并选择处理分支。 */
		problem(w, 404, "接入实例不存在") /* 执行当前语句并推进处理流程。 */
		return                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !p.Enabled { /* 判断条件并选择处理分支。 */
		problem(w, 422, "接入实例已停用") /* 执行当前语句并推进处理流程。 */
		return                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.EdgeNodeID != "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "边缘节点功能已移除，不能执行旧现场命令") /* 执行当前语句并推进处理流程。 */
		return                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.protocolListeners == nil { /* 判断条件并选择处理分支。 */
		problem(w, 503, "通用协议接入服务未启动") /* 执行当前语句并推进处理流程。 */
		return                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	delete(command, "requestId")                                                                                             /* 执行当前语句并推进处理流程。 */
	delete(command, "_scheduled")                                                                                            /* 执行当前语句并推进处理流程。 */
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)                                                          /* 更新 cancel 的值。 */
	defer cancel()                                                                                                           /* 安排函数结束时执行清理。 */
	result, err := s.protocolListeners.Command(ctx, claims(r).TenantID, r.PathValue("id"), r.PathValue("deviceId"), command) /* 更新 err 的值。 */
	if err != nil {                                                                                                          /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "protocol.command", "device", r.PathValue("deviceId"), map[string]any{"profileId": r.PathValue("id"), "commandType": command["type"]}) /* 执行当前语句并推进处理流程。 */
	write(w, 200, result)                                                                                                                             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func firstNonBlankString(value any) string { text, _ := value.(string); return text } /* 定义 firstNonBlankString 函数。 */

func validateListenerProfile(v model.DeviceAccessProfile) error { /* 定义 validateListenerProfile 函数。 */
	if !protocolSegmentV2.MatchString(v.ID) || v.ProductID == "" || v.ProtocolID == "" || v.ProtocolVersion == "" { /* 判断条件并选择处理分支。 */
		return errors.New("接入实例标识、产品和协议版本不能为空") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Network != "tcp" && v.Network != "udp" { /* 判断条件并选择处理分支。 */
		return errors.New("监听网络须为 tcp 或 udp") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.ConnectionMode != "dial" && net.ParseIP(v.Host) == nil { /* 判断条件并选择处理分支。 */
		return errors.New("监听地址须为本机 IP，例如 0.0.0.0 或 127.0.0.1") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Port < 1 || v.Port > 65535 { /* 判断条件并选择处理分支。 */
		return errors.New("监听端口须为 1 至 65535") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.TimeoutMs < 1 || v.TimeoutMs > 30000 { /* 判断条件并选择处理分支。 */
		return errors.New("命令超时须为 1 至 30000 毫秒") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func listenerSupports(release model.ProtocolRelease, network string) bool { /* 定义 listenerSupports 函数。 */
	transport := strings.ToUpper(release.Transport)                                                                      /* 更新 transport 的值。 */
	return release.ParserType == parser.GoProtocolParserName && release.Artifact["runtime"] == protocolworker.Runtime && /* 返回当前处理结果。 */
		protocolworker.HasCapability(release, "ingress") && (transport == strings.ToUpper(network) || transport == "TCP_UDP") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
