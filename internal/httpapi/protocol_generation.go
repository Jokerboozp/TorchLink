package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/core"   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func generatedMapping(kind string) bool { /* 定义 generatedMapping 函数。 */
	return kind == "configurable_json_parser" || kind == "configurable_hex_parser" || kind == parser.ModbusTCPParserName || kind == parser.ModbusRTUParserName /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) saveGeneratedProtocol(w http.ResponseWriter, r *http.Request, id, version string, draft model.ProtocolAssistantDraft, payload json.RawMessage, format string) { /* 定义 saveGeneratedProtocol 函数。 */
	id, version = strings.TrimSpace(id), strings.TrimSpace(version)                                                           /* 更新 version 的值。 */
	if !protocolSegmentV2.MatchString(id) || !protocolSegmentV2.MatchString(version) || strings.TrimSpace(draft.Name) == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "请填写协议名称、标识和版本") /* 执行当前语句并推进处理流程。 */
		return                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if format != "" { /* 判断条件并选择处理分支。 */
		draft.PayloadFormat = format /* 更新 draft.PayloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                                                                                                                                                                                                                                 /* 更新 now 的值。 */
	tenant := claims(r).TenantID                                                                                                                                                                                                                                                                  /* 更新 tenant 的值。 */
	release := model.ProtocolRelease{TenantID: tenant, ProtocolID: id, Version: version, ParserType: draft.ParserType, Transport: draft.Transport, PayloadFormat: draft.PayloadFormat, Config: draft.Config, Status: "DRAFT", CreatedAt: now, Artifact: map[string]any{"generatedMapping": true}} /* 更新 release 的值。 */
	if err := validateGeneratedMapping(release); err != nil {                                                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, version); err == nil { /* 判断条件并选择处理分支。 */
		problem(w, 409, "该版本已存在，请使用新的版本号") /* 执行当前语句并推进处理流程。 */
		return                             /* 返回当前处理结果。 */
	} else if !errors.Is(err, model.ErrNotFound) { /* 结束当前表达式或代码块。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var preview *model.StandardMessage                 /* 声明 preview。 */
	if len(payload) > 0 && string(payload) != "null" { /* 判断条件并选择处理分支。 */
		text, err := assistantPayloadText(payload, draft.PayloadFormat) /* 更新 err 的值。 */
		if err != nil {                                                 /* 判断条件并选择处理分支。 */
			problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		preview, err = core.PreviewProtocolAssistant(draft, tenant, text) /* 更新 err 的值。 */
		if err != nil {                                                   /* 判断条件并选择处理分支。 */
			problem(w, 422, "样本解析失败："+err.Error()) /* 执行当前语句并推进处理流程。 */
			return                                 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		release.Status = "VALIDATED" /* 更新 release.Status 的值。 */
	} /* 结束当前表达式或代码块。 */
	definition, err := s.engine.Repo.GetProtocolDefinition(r.Context(), tenant, id) /* 更新 err 的值。 */
	if err != nil && !errors.Is(err, model.ErrNotFound) {                           /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if errors.Is(err, model.ErrNotFound) { /* 判断条件并选择处理分支。 */
		definition = model.ProtocolDefinition{TenantID: tenant, ID: id, Name: draft.Name, Description: draft.Description, CreatedAt: now, UpdatedAt: now} /* 更新 definition 的值。 */
		if err = s.engine.Repo.SaveProtocolDefinition(r.Context(), definition); err != nil {                                                              /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err = s.engine.Repo.CreateProtocolRelease(r.Context(), release); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 409, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "protocol.generated.save", "protocolRelease", id+"@"+version, map[string]any{"status": release.Status}) /* 执行当前语句并推进处理流程。 */
	write(w, 201, map[string]any{"release": release, "definition": definition, "standardMessage": preview})            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func validateGeneratedMapping(release model.ProtocolRelease) error { /* 定义 validateGeneratedMapping 函数。 */
	if !generatedMapping(release.ParserType) { /* 判断条件并选择处理分支。 */
		return errors.New("该协议需要 Go 源码") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.ParserType == parser.ModbusTCPParserName || release.ParserType == parser.ModbusRTUParserName { /* 判断条件并选择处理分支。 */
		want := "MODBUS_TCP"                                  /* 更新 want 的值。 */
		if release.ParserType == parser.ModbusRTUParserName { /* 判断条件并选择处理分支。 */
			want = "MODBUS_RTU" /* 更新 want 的值。 */
		} /* 结束当前表达式或代码块。 */
		if release.Transport != want || release.PayloadFormat != "hex" { /* 判断条件并选择处理分支。 */
			return errors.New("点表协议的传输方式与格式不一致") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := core.NormalizeGeneratedModbusConfig(release.Config); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		if release.Transport != "MQTT" && release.Transport != "HTTP" { /* 判断条件并选择处理分支。 */
			return errors.New("报文映射支持 MQTT / HTTP；TCP / UDP 接入请使用含拆帧能力的 Go 源码") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		want := "json"                                       /* 更新 want 的值。 */
		if release.ParserType == "configurable_hex_parser" { /* 判断条件并选择处理分支。 */
			want = "hex" /* 更新 want 的值。 */
		} /* 结束当前表达式或代码块。 */
		if release.PayloadFormat != want { /* 判断条件并选择处理分支。 */
			return errors.New("报文格式与解析器不一致") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return validateProtocolReleaseV2(release) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) previewGeneratedRelease(w http.ResponseWriter, r *http.Request) { /* 定义 previewGeneratedRelease 函数。 */
	var input struct { /* 声明 input。 */
		Payload      json.RawMessage `json:"payload"`      /* 执行当前语句并推进处理流程。 */
		StartAddress *int            `json:"startAddress"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &input) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), claims(r).TenantID, r.PathValue("id"), r.PathValue("version")) /* 更新 err 的值。 */
	if err != nil {                                                                                                              /* 判断条件并选择处理分支。 */
		problem(w, 404, "protocol release not found") /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.Status == "REVOKED" { /* 判断条件并选择处理分支。 */
		problem(w, 409, "已撤销的版本不能校验") /* 执行当前语句并推进处理流程。 */
		return                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !generatedMapping(release.ParserType) { /* 判断条件并选择处理分支。 */
		problem(w, 422, "此入口仅用于报文与点表生成的协议") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	config, ok := jsonValue(release.Config).(map[string]any) /* 更新 ok 的值。 */
	if !ok {                                                 /* 判断条件并选择处理分支。 */
		problem(w, 422, "protocol mapping is missing") /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if input.StartAddress != nil { /* 判断条件并选择处理分支。 */
		config["startAddress"] = *input.StartAddress /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	payload, err := assistantPayloadText(input.Payload, release.PayloadFormat) /* 更新 err 的值。 */
	if err != nil {                                                            /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	draft := model.ProtocolAssistantDraft{Protocol: release.ProtocolID, ParserType: release.ParserType, Transport: release.Transport, PayloadFormat: release.PayloadFormat, Config: config} /* 更新 draft 的值。 */
	message, err := core.PreviewProtocolAssistant(draft, claims(r).TenantID, payload)                                                                                                       /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		problem(w, 422, "样本解析失败："+err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.Status == "DRAFT" { /* 判断条件并选择处理分支。 */
		if err = s.engine.Repo.UpdateProtocolReleaseStatus(r.Context(), release.TenantID, release.ProtocolID, release.Version, "VALIDATED", 0); err != nil { /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		release.Status = "VALIDATED" /* 更新 release.Status 的值。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "protocol.generated.preview", "protocolRelease", release.ProtocolID+"@"+release.Version, map[string]any{"status": release.Status}) /* 执行当前语句并推进处理流程。 */
	write(w, 200, map[string]any{"release": release, "standardMessage": message})                                                                 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
