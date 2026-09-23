package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex" /* 执行当前语句并推进处理流程。 */
	"testing"      /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestLegacyCombinedPacketAndCRC(t *testing.T) { /* 定义 TestLegacyCombinedPacketAndCRC 函数。 */
	data, err := hex.DecodeString("38363838393230373432343334343601460000000D1A00000064003C0000000000160005000000000000000000010000503801460016000102000021B0") /* 更新 err 的值。 */
	if err != nil {                                                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	first, err := ingressSensor(data, Context{})                                   /* 更新 err 的值。 */
	if err != nil || first.Consumed != 50 || first.DeviceID != "868892074243446" { /* 判断条件并选择处理分支。 */
		t.Fatalf("first=%+v err=%v", first, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	msg, err := decodeSensor(data[:first.Consumed], Context{})                                         /* 更新 err 的值。 */
	if err != nil || msg.Properties["batteryLevel"] != 100 || msg.Properties["signalStrength"] != 22 { /* 判断条件并选择处理分支。 */
		t.Fatalf("message=%+v err=%v", msg, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second, err := ingressSensor(data[first.Consumed:], Context{DeviceID: first.DeviceID, State: first.State}) /* 更新 err 的值。 */
	if err != nil || second.Consumed != 11 || second.DeviceID != first.DeviceID {                              /* 判断条件并选择处理分支。 */
		t.Fatalf("second=%+v err=%v", second, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	alarm, err := decodeSensor(data[first.Consumed:], Context{DeviceID: first.DeviceID, State: first.State}) /* 更新 err 的值。 */
	if err != nil || alarm.Properties["alarm"] != false {                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("alarm=%+v err=%v", alarm, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	broken := append([]byte(nil), data[:first.Consumed]...)    /* 更新 broken 的值。 */
	broken[len(broken)-1] ^= 1                                 /* 执行当前语句并推进处理流程。 */
	if _, err = ingressSensor(broken, Context{}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("CRC corruption accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestCommandConfirmation(t *testing.T) { /* 定义 TestCommandConfirmation 函数。 */
	sent, err := encodeSensor(Command{Type: "setMultipleParams", Params: map[string]any{"collectionTime": 5, "alarmLowerLimit": -100, "alarmUpperLimit": 300}}, Context{State: map[string]any{"unit": 1}}) /* 更新 err 的值。 */
	if err != nil || len(sent.Reply) != 19 || sent.CorrelationID != "10-0006" {                                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("sent=%+v err=%v", sent, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ack := rtu([]byte{1, 0x10, 0, 6, 0, 5})                                                 /* 更新 ack 的值。 */
	got, err := ingressSensor(ack, Context{DeviceID: "868892074243446", State: sent.State}) /* 更新 err 的值。 */
	if err != nil || got.CorrelationID != sent.CorrelationID {                              /* 判断条件并选择处理分支。 */
		t.Fatalf("ack=%+v err=%v", got, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
