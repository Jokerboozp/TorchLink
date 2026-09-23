package connector /* 声明 connector 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"errors"  /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestCapabilitiesAndUnsupported(t *testing.T) { /* 定义 TestCapabilitiesAndUnsupported 函数。 */
	if len(Types()) != 6 || !Describe(ModbusTCP).Capabilities.ReadOnce || Describe(HTTP).Capabilities.Command || Describe(Edge).Supported { /* 判断条件并选择处理分支。 */
		t.Fatal("incorrect capabilities") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	a := Adapter{Kind: Edge, Probe: func(context.Context, Request) (*Result, error) { t.Fatal("unsupported probe called"); return nil, nil }} /* 检查错误并决定后续处理。 */
	if r, e := a.Test(context.Background(), Request{}); !errors.Is(e, ErrUnsupported) || r.ErrorCode != "UNSUPPORTED" {                       /* 判断条件并选择处理分支。 */
		t.Fatal(r, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithCancel(context.Background())              /* 更新 cancel 的值。 */
	cancel()                                                             /* 执行当前语句并推进处理流程。 */
	if _, e := a.Test(ctx, Request{}); !errors.Is(e, context.Canceled) { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestRemovedConnectorsNeverInvokeProbe(t *testing.T) { /* 定义 TestRemovedConnectorsNeverInvokeProbe 函数。 */
	for _, kind := range []Type{Edge, ModbusRTU, OPCUA, SNMP, BACnet} { /* 循环处理当前数据。 */
		a := Adapter{Kind: kind, Probe: func(context.Context, Request) (*Result, error) { /* 更新 a 的值。 */
			t.Fatal("removed connector executed") /* 验证实际结果符合预期。 */
			return nil, nil                       /* 返回当前处理结果。 */
		}} /* 结束当前表达式或代码块。 */
		r, err := a.Test(context.Background(), Request{}) /* 更新 err 的值。 */
		if !errors.Is(err, ErrUnsupported) || r.Success { /* 判断条件并选择处理分支。 */
			t.Fatalf("%s was not rejected: %v", kind, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
