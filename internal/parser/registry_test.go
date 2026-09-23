package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex"                /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestPlatformRegistryRequiresExplicitLegacyBinding(t *testing.T) { /* 定义 TestPlatformRegistryRequiresExplicitLegacyBinding 函数。 */
	r := NewPlatformRegistry(t.TempDir())                                                                              /* 更新 r 的值。 */
	raw := model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: json.RawMessage(`"4040"`)} /* 更新 raw 的值。 */
	if _, err := r.Parse(raw); err == nil {                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal("automatically selected a specialized parser") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	frame := BuildGB26875RegistrationFrame(1, [6]byte{1, 2, 3, 4, 5, 6}, time.Now()) /* 更新 frame 的值。 */
	// Existing bindings and historical replay still resolve the old parser.
	raw.Payload, _ = json.Marshal(hex.EncodeToString(frame))              /* 更新 _ 的值。 */
	if _, err := r.ParseWith((GB26875Parser{}).Name(), raw); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, name := range []string{(GB26875Parser{}).Name(), ModbusTCPParserName, ModbusCoilParserName, JavaScriptParserName} { /* 循环处理当前数据。 */
		if ManagedParserType(name) { /* 判断条件并选择处理分支。 */
			t.Fatalf("legacy parser offered for new packages: %s", name) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
