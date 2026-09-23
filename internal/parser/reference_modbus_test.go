package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Synthetic read responses use the addresses in the supplied BPW, 2XP,
// dual-power and inspection-cabinet documents. No control writes are sent.
func TestReferenceCabinetModbusReads(t *testing.T) { /* 定义 TestReferenceCabinetModbusReads 函数。 */
	for _, tc := range []struct { /* 循环处理当前数据。 */
		name    string   /* 执行当前语句并推进处理流程。 */
		address int      /* 执行当前语句并推进处理流程。 */
		fields  []string /* 执行当前语句并推进处理流程。 */
		values  []byte   /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{"BPW", 0x1100, []string{"pump1State", "pump2State"}, []byte{0, 1, 0, 2}},                                                                                                                                /* 执行当前语句并推进处理流程。 */
		{"2XP", 0x2000, []string{"pump1State", "pump2State"}, []byte{0, 1, 0, 2}},                                                                                                                                /* 执行当前语句并推进处理流程。 */
		{"dual-power", 0x3000, []string{"mainVoltage", "mainCurrent", "backupVoltage", "backupCurrent"}, []byte{0, 220, 0, 10, 0, 219, 0, 0}},                                                                    /* 执行当前语句并推进处理流程。 */
		{"inspection", 0x2000, []string{"pump1State", "pump2State", "pump3State", "pump4State", "pump5State", "pump6State", "pump7State", "pump8State"}, []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0}}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		t.Run(tc.name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			points := []model.ModbusPoint{}   /* 更新 points 的值。 */
			for i, field := range tc.fields { /* 循环处理当前数据。 */
				points = append(points, model.ModbusPoint{Identifier: field, FunctionCode: 3, Address: tc.address + i, DataType: "uint16", RegisterCount: 1, Scale: 1}) /* 更新 points 的值。 */
			} /* 结束当前表达式或代码块。 */
			frame := referenceCRC(append([]byte{1, 3, byte(len(tc.values))}, tc.values...))                                                                                                                   /* 更新 frame 的值。 */
			payload, _ := json.Marshal(hex.EncodeToString(frame))                                                                                                                                             /* 更新 _ 的值。 */
			raw := model.RawMessage{MessageID: "reference", TenantID: "test", ProductID: "cabinet", DeviceID: tc.name, ReceivedAt: 1, Payload: payload, Metadata: map[string]any{"startAddress": tc.address}} /* 更新 raw 的值。 */
			message, err := (ModbusRTUParser{}).ParseWithConfig(raw, map[string]any{"points": points})                                                                                                        /* 更新 err 的值。 */
			if err != nil {                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			for i, field := range tc.fields { /* 循环处理当前数据。 */
				if message.Properties[field] != float64(int(tc.values[i*2])<<8|int(tc.values[i*2+1])) { /* 判断条件并选择处理分支。 */
					t.Fatalf("%s decoded as %v", field, message.Properties[field]) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			frame[len(frame)-1] ^= 1                                                                              /* 执行当前语句并推进处理流程。 */
			raw.Payload, _ = json.Marshal(hex.EncodeToString(frame))                                              /* 更新 _ 的值。 */
			if _, err := (ModbusRTUParser{}).ParseWithConfig(raw, map[string]any{"points": points}); err == nil { /* 判断条件并选择处理分支。 */
				t.Fatal("corrupted CRC was accepted") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func referenceCRC(data []byte) []byte { /* 定义 referenceCRC 函数。 */
	crc := uint16(0xffff)    /* 更新 crc 的值。 */
	for _, b := range data { /* 循环处理当前数据。 */
		crc ^= uint16(b)               /* 执行当前语句并推进处理流程。 */
		for bit := 0; bit < 8; bit++ { /* 循环处理当前数据。 */
			if crc&1 != 0 { /* 判断条件并选择处理分支。 */
				crc = crc>>1 ^ 0xa001 /* 更新 crc 的值。 */
			} else { /* 结束当前表达式或代码块。 */
				crc >>= 1 /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return append(data, byte(crc), byte(crc>>8)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestReferenceCabinetInputBits(t *testing.T) { /* 定义 TestReferenceCabinetInputBits 函数。 */
	bit := 3                                                                                    /* 更新 bit 的值。 */
	payload, _ := json.Marshal(hex.EncodeToString(referenceCRC([]byte{1, 3, 2, 0, 8})))         /* 更新 _ 的值。 */
	raw := model.RawMessage{Payload: payload, Metadata: map[string]any{"startAddress": 0x2003}} /* 更新 raw 的值。 */
	for _, dataType := range []string{"uint16", "bits"} {                                       /* 循环处理当前数据。 */
		t.Run(dataType, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			m, err := (ModbusRTUParser{}).ParseWithConfig(raw, map[string]any{"points": []model.ModbusPoint{{Identifier: "input4", FunctionCode: 3, Address: 0x2003, DataType: dataType, RegisterCount: 1, Bit: &bit}}}) /* 更新 err 的值。 */
			if err != nil {                                                                                                                                                                                              /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if m.Properties["input4"] != true { /* 判断条件并选择处理分支。 */
				t.Fatalf("input 4 must be true, got %v", m.Properties["input4"]) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
