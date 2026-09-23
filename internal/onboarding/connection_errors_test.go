package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                         /* 执行当前语句并推进处理流程。 */
	"errors"                          /* 执行当前语句并推进处理流程。 */
	"fmt"                             /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"    /* 执行当前语句并推进处理流程。 */
	"testing"                         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestConnectionErrorResults(t *testing.T) { /* 定义 TestConnectionErrorResults 函数。 */
	for _, tc := range []struct { /* 循环处理当前数据。 */
		err  error  /* 执行当前语句并推进处理流程。 */
		code string /* 执行当前语句并推进处理流程。 */
	}{{fmt.Errorf("broker: %w", connector.ErrAuthentication), "AUTH_FAILED"}, {fmt.Errorf("dial: %w", context.DeadlineExceeded), "TIMEOUT"}, {errors.New("unreachable"), "NETWORK_ERROR"}} { /* 结束当前表达式或代码块。 */
		t.Run(tc.code, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			s, _, q := fixture(t)                                                                                                                                                                         /* 更新 q 的值。 */
			q.Type = connector.MQTT                                                                                                                                                                       /* 更新 q.Type 的值。 */
			s.MQTTHealth = func(context.Context) error { return tc.err }                                                                                                                                  /* 更新 s.MQTTHealth 的值。 */
			r, err := s.Test(context.Background(), "tenant", q)                                                                                                                                           /* 更新 err 的值。 */
			if err != nil || r.Success || r.ErrorCode != tc.code || r.Stage != "broker" || r.DeviceID != q.DeviceID || r.ProtocolID != parser.StandardProtocolID || r.Parser == "" || r.TestToken != "" { /* 判断条件并选择处理分支。 */
				t.Fatalf("%+v %v", r, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	s, _, q := fixture(t)                                                                                               /* 更新 q 的值。 */
	q.ProductID = "missing"                                                                                             /* 更新 q.ProductID 的值。 */
	r, err := s.Test(context.Background(), "tenant", q)                                                                 /* 更新 err 的值。 */
	if err == nil || r == nil || r.ErrorCode != "PROTOCOL_ERROR" || r.Stage != "validate" || r.DeviceID != q.DeviceID { /* 判断条件并选择处理分支。 */
		t.Fatalf("%+v %v", r, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
