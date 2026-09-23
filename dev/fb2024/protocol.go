package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var switchExample = hexBytes("40 40 00 00 01 01 0F 22 0E 19 07 13 01 00 00 00 00 00 FF FF 00 00 00 00 12 00 02 96 01 00 03 5B 01 00 00 00 02 00 00 0F 22 0E 19 07 13 F1 23 23") /* 声明 switchExample。 */

func hexBytes(s string) []byte { /* 定义 hexBytes 函数。 */
	b, e := hex.DecodeString(strings.Join(strings.Fields(s), "")) /* 更新 e 的值。 */
	if e != nil {                                                 /* 判断条件并选择处理分支。 */
		panic(e) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return b /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func Protocol() Definition { /* 定义 Protocol 函数。 */
	return Definition{ /* 返回当前处理结果。 */
		Name: "富贝 2024 消防传输装置", Version: "1.0.0", Transport: "TCP", Decode: decodeFB, Ingress: ingressFB, /* 执行当前语句并推进处理流程。 */
		Samples: []Sample{ /* 执行当前语句并推进处理流程。 */
			{Name: "开关量输入", Data: switchExample, Want: Message{ /* 执行当前语句并推进处理流程。 */
				MessageType: "PROPERTY_REPORT",                                 /* 执行当前语句并推进处理流程。 */
				Properties:  map[string]any{"typeFlag": 150, "objectCount": 1}, /* 执行当前语句并推进处理流程。 */
			}}, /* 结束当前表达式或代码块。 */
		}, /* 结束当前表达式或代码块。 */
		Operations: []OperationSample{ /* 执行当前语句并推进处理流程。 */
			{Name: "半帧", Operation: "ingress", Data: switchExample[:10], Want: Frame{NeedMore: true}},                              /* 执行当前语句并推进处理流程。 */
			{Name: "完整上报", Operation: "ingress", Data: switchExample, Want: Frame{Consumed: len(switchExample), DeviceID: "fb_1"}}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func fbLength(data []byte) (int, error) { /* 定义 fbLength 函数。 */
	if len(data) < 2 { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data[0] != 0x40 || data[1] != 0x40 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("FB2024 帧头错误") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < 30 { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	n := 30 + int(binary.LittleEndian.Uint16(data[24:26])) /* 更新 n 的值。 */
	if n > 65536 {                                         /* 判断条件并选择处理分支。 */
		return 0, errors.New("FB2024 帧过长") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < n { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data[n-2] != 0x23 || data[n-1] != 0x23 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("FB2024 帧尾错误") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var sum byte                      /* 声明 sum。 */
	for _, b := range data[2 : n-3] { /* 循环处理当前数据。 */
		sum += b /* 更新 sum 的值。 */
	} /* 结束当前表达式或代码块。 */
	if sum != data[n-3] { /* 判断条件并选择处理分支。 */
		return 0, errors.New("FB2024 校验和错误") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return n, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func fbDevice(data []byte, ctx Context) string { /* 定义 fbDevice 函数。 */
	var n uint64                /* 声明 n。 */
	for i := 17; i >= 12; i-- { /* 循环处理当前数据。 */
		n = n<<8 | uint64(data[i]) /* 更新 n 的值。 */
	} /* 结束当前表达式或代码块。 */
	if n == 0 && ctx.DeviceID != "" { /* 判断条件并选择处理分支。 */
		return ctx.DeviceID /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "fb_" + strconv.FormatUint(n, 10) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ingressFB(data []byte, ctx Context) (Frame, error) { /* 定义 ingressFB 函数。 */
	n, e := fbLength(data) /* 更新 e 的值。 */
	if e != nil {          /* 判断条件并选择处理分支。 */
		return Frame{}, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n == 0 { /* 判断条件并选择处理分支。 */
		return Frame{NeedMore: true}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return Frame{Consumed: n, DeviceID: fbDevice(data, ctx)}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func decodeFB(data []byte, ctx Context) (Message, error) { /* 定义 decodeFB 函数。 */
	n, e := fbLength(data) /* 更新 e 的值。 */
	if e != nil {          /* 判断条件并选择处理分支。 */
		return Message{}, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n == 0 || n != len(data) { /* 判断条件并选择处理分支。 */
		return Message{}, errors.New("需要单个完整 FB2024 帧") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	command := data[26]               /* 更新 command 的值。 */
	app := data[27 : n-3]             /* 更新 app 的值。 */
	if command != 2 || len(app) < 2 { /* 判断条件并选择处理分支。 */
		return Message{MessageType: "COMMAND_REPLY", Event: map[string]any{"type": "CONTROL", "command": int(command)}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	typ, count := app[0], int(app[1])                                                                                           /* 更新 count 的值。 */
	props := map[string]any{"typeFlag": int(typ), "objectCount": count, "serialNo": int(binary.LittleEndian.Uint16(data[2:4]))} /* 更新 props 的值。 */
	var size int                                                                                                                /* 声明 size。 */
	switch typ {                                                                                                                /* 根据条件选择处理路径。 */
	case 2, 3, 0x87, 0x96, 0x97: /* 处理当前分支。 */
		size = 16 /* 更新 size 的值。 */
	case 0x15, 0x88: /* 处理当前分支。 */
		size = 7 /* 更新 size 的值。 */
	default: /* 处理当前分支。 */
		props["payloadHex"] = hex.EncodeToString(app) /* 执行当前语句并推进处理流程。 */
		return properties(props), nil                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(app) != 2+count*size { /* 判断条件并选择处理分支。 */
		return Message{}, fmt.Errorf("FB2024 类型 %02x 对象长度不符", typ) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	objects := make([]map[string]any, 0, count)    /* 更新 objects 的值。 */
	components := make([]map[string]any, 0, count) /* 更新 components 的值。 */
	for i := 0; i < count; i++ {                   /* 循环处理当前数据。 */
		b := app[2+i*size : 2+(i+1)*size] /* 更新 b 的值。 */
		if typ == 0x15 || typ == 0x88 {   /* 判断条件并选择处理分支。 */
			status := b[0]   /* 更新 status 的值。 */
			if typ == 0x88 { /* 判断条件并选择处理分支。 */
				status = 0 /* 更新 status 的值。 */
			} /* 结束当前表达式或代码块。 */
			objects = append(objects, map[string]any{"transmissionStatus": int(status), "mainPowerFault": status&8 != 0, "backupPowerFault": status&16 != 0}) /* 更新 objects 的值。 */
			components = append(components, map[string]any{"id": "gateway", "name": "用户信息传输装置", "alarms": map[string]bool{"DEVICE_FAULT": status&0x18 != 0}}) /* 更新 components 的值。 */
			continue                                                                                                                                          /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		sys, dev, addr, port := int(b[1]), int(b[2]), int(b[3]), int(b[5])   /* 更新 port 的值。 */
		access, value := int(b[7]), int(binary.LittleEndian.Uint16(b[8:10])) /* 更新 value 的值。 */
		loop := port                                                         /* 更新 loop 的值。 */
		if typ == 0x96 || typ == 3 {                                         /* 判断条件并选择处理分支。 */
			if port == 0 { /* 判断条件并选择处理分支。 */
				loop = 1 /* 更新 loop 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if typ == 0x97 { /* 判断条件并选择处理分支。 */
			if port >= addr && port < addr+4 { /* 判断条件并选择处理分支。 */
				loop = port - addr + 1 /* 更新 loop 的值。 */
			} else if port == 0 { /* 结束当前表达式或代码块。 */
				loop = 1 /* 更新 loop 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if loop == 0 { /* 判断条件并选择处理分支。 */
			loop = addr /* 更新 loop 的值。 */
		} /* 结束当前表达式或代码块。 */
		obj := map[string]any{"systemAddress": sys, "systemType": int(b[0]), "deviceType": dev, "HLH": loop, "TDH": addr, "port": port, "accessType": access} /* 更新 obj 的值。 */
		switch typ {                                                                                                                                          /* 根据条件选择处理路径。 */
		case 3: /* 处理当前分支。 */
			obj["analogType"] = access                                                                       /* 执行当前语句并推进处理流程。 */
			obj["analogValue"] = value                                                                       /* 执行当前语句并推进处理流程。 */
			obj["analogue"] = map[string]any{"analogueTypeCode": analogCode(access), "analogueValue": value} /* 执行当前语句并推进处理流程。 */
		case 0x87: /* 处理当前分支。 */
			obj["switchState"] = value /* 执行当前语句并推进处理流程。 */
			obj["state"] = "恢复"        /* 执行当前语句并推进处理流程。 */
		default: /* 处理当前分支。 */
			obj["switchState"] = value               /* 执行当前语句并推进处理流程。 */
			obj["state"] = switchText(access, value) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		objects = append(objects, obj) /* 更新 objects 的值。 */
	} /* 结束当前表达式或代码块。 */
	props["objects"] = objects /* 执行当前语句并推进处理流程。 */
	if len(components) > 0 {   /* 判断条件并选择处理分支。 */
		return Message{MessageType: "STATE_CHANGE", Properties: props, Event: map[string]any{"components": components}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return properties(props), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func analogCode(t int) string { /* 定义 analogCode 函数。 */
	switch t { /* 根据条件选择处理路径。 */
	case 1: /* 处理当前分支。 */
		return "HUMIDITY" /* 返回当前处理结果。 */
	case 2: /* 处理当前分支。 */
		return "LEVEL" /* 返回当前处理结果。 */
	case 3: /* 处理当前分支。 */
		return "TEMPERATURE" /* 返回当前处理结果。 */
	case 4: /* 处理当前分支。 */
		return "PRESSURE_MPA" /* 返回当前处理结果。 */
	case 5: /* 处理当前分支。 */
		return "PRESSURE_KPA" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return "UNKNOWN" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func switchText(t, v int) string { /* 定义 switchText 函数。 */
	on := v != 0 /* 更新 on 的值。 */
	switch t {   /* 根据条件选择处理路径。 */
	case 0: /* 处理当前分支。 */
		if on { /* 判断条件并选择处理分支。 */
			return "手动" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "自动" /* 返回当前处理结果。 */
	case 1: /* 处理当前分支。 */
		if on { /* 判断条件并选择处理分支。 */
			return "断电" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "通电" /* 返回当前处理结果。 */
	case 2: /* 处理当前分支。 */
		if on { /* 判断条件并选择处理分支。 */
			return "启动" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "停止" /* 返回当前处理结果。 */
	case 3: /* 处理当前分支。 */
		if on { /* 判断条件并选择处理分支。 */
			return "故障" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "正常" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return strconv.Itoa(v) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
