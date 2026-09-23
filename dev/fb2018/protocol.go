package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var componentExample = mustHex("40 40 03 00 01 01 0D 10 16 15 0A 13 71 11 01 00 00 00 38 5B 01 00 00 00  30 00 02 02 01 01 01 17 05 00 65 00 02 00 20 20 20 20 20 20 20 20 20  20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 00  0C 10 16 15 0A 13 5F 23 23") /* 声明 componentExample。 */

func mustHex(s string) []byte { /* 定义 mustHex 函数。 */
	b, e := hex.DecodeString(strings.Join(strings.Fields(s), "")) /* 更新 e 的值。 */
	if e != nil {                                                 /* 判断条件并选择处理分支。 */
		panic(e) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return b /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func Protocol() Definition { /* 定义 Protocol 函数。 */
	return Definition{ /* 返回当前处理结果。 */
		Name: "富贝 2018 消防传输装置", Version: "1.0.0", Transport: "TCP", Decode: decodeFB, Ingress: ingressFB, /* 执行当前语句并推进处理流程。 */
		Samples: []Sample{{Name: "手报火警", Data: componentExample, Context: Context{Now: 1789000000000}, Want: Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"typeFlag": 2, "objectCount": 1, "fireAlarm": true}}}}, /* 执行当前语句并推进处理流程。 */
		Operations: []OperationSample{ /* 执行当前语句并推进处理流程。 */
			{Name: "半帧", Operation: "ingress", Data: componentExample[:10], Want: Frame{NeedMore: true}},                                         /* 执行当前语句并推进处理流程。 */
			{Name: "完整报文", Operation: "ingress", Data: componentExample, Want: Frame{Consumed: len(componentExample), DeviceID: "fb2018_70001"}}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func fbLength(data []byte) (int, error) { /* 定义 fbLength 函数。 */
	if len(data) < 2 { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data[0] != 0x40 || data[1] != 0x40 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("FB2018 帧头错误") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < 30 { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	n := 30 + int(binary.LittleEndian.Uint16(data[24:26])) /* 更新 n 的值。 */
	if n > 65536 {                                         /* 判断条件并选择处理分支。 */
		return 0, errors.New("FB2018 帧过长") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < n { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data[n-2] != 0x23 || data[n-1] != 0x23 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("FB2018 帧尾错误") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var sum byte                      /* 声明 sum。 */
	for _, b := range data[2 : n-3] { /* 循环处理当前数据。 */
		sum += b /* 更新 sum 的值。 */
	} /* 结束当前表达式或代码块。 */
	if sum != data[n-3] { /* 判断条件并选择处理分支。 */
		return 0, errors.New("FB2018 校验和错误") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return n, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func fbDevice(data []byte, ctx Context) string { /* 定义 fbDevice 函数。 */
	// Source is six-byte little-endian. An all-zero source needs a dedicated
	// listener/profile or explicit device mapping to avoid identity collision.
	var n uint64                /* 声明 n。 */
	for i := 17; i >= 12; i-- { /* 循环处理当前数据。 */
		n = n<<8 | uint64(data[i]) /* 更新 n 的值。 */
	} /* 结束当前表达式或代码块。 */
	if n == 0 && ctx.DeviceID != "" { /* 判断条件并选择处理分支。 */
		return ctx.DeviceID /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "fb2018_" + strconv.FormatUint(n, 10) /* 返回当前处理结果。 */
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
		return Message{}, errors.New("需要单个完整 FB2018 帧") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	command := data[26]               /* 更新 command 的值。 */
	app := data[27 : n-3]             /* 更新 app 的值。 */
	if command != 2 || len(app) < 2 { /* 判断条件并选择处理分支。 */
		return Message{MessageType: "COMMAND_REPLY", Event: map[string]any{"type": "CONTROL", "command": int(command)}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	typ, count := app[0], int(app[1]) /* 更新 count 的值。 */
	if count > 256 {                  /* 判断条件并选择处理分支。 */
		return Message{}, errors.New("对象数过多") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	props := map[string]any{"typeFlag": int(typ), "objectCount": count, "serialNo": int(binary.LittleEndian.Uint16(data[2:4]))} /* 更新 props 的值。 */
	var objects []map[string]any                                                                                                /* 声明 objects。 */
	var components []map[string]any                                                                                             /* 声明 components。 */
	var fire, fault bool                                                                                                        /* 声明 fire。 */
	var bodySize int                                                                                                            /* 声明 bodySize。 */
	switch typ {                                                                                                                /* 根据条件选择处理路径。 */
	case 1, 0x86: /* 处理当前分支。 */
		bodySize = 10 /* 更新 bodySize 的值。 */
	case 2, 0x87: /* 处理当前分支。 */
		if len(app) == 2+count*46 { /* 判断条件并选择处理分支。 */
			bodySize = 46 /* 更新 bodySize 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			bodySize = 15 /* 更新 bodySize 的值。 */
		} /* 结束当前表达式或代码块。 */
	case 0x15, 0x88: /* 处理当前分支。 */
		bodySize = 7 /* 更新 bodySize 的值。 */
	case 0x18: /* 处理当前分支。 */
		bodySize = 8 /* 更新 bodySize 的值。 */
	case 0x19: /* 处理当前分支。 */
		if len(app) < 4 { /* 判断条件并选择处理分支。 */
			return Message{}, errors.New("软件版本报文过短") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		props["version"] = fmt.Sprintf("%d.%d", app[2], app[3]) /* 执行当前语句并推进处理流程。 */
		return properties(props), nil                           /* 返回当前处理结果。 */
	case 0x1c: /* 处理当前分支。 */
		if len(app) < 8 { /* 判断条件并选择处理分支。 */
			return Message{}, errors.New("系统时间报文过短") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		props["systemTime"] = fbTime(app[2:8]) /* 执行当前语句并推进处理流程。 */
		return properties(props), nil          /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		props["payloadHex"] = hex.EncodeToString(app) /* 执行当前语句并推进处理流程。 */
		return properties(props), nil                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(app) != 2+count*bodySize { /* 判断条件并选择处理分支。 */
		return Message{}, fmt.Errorf("FB2018 类型 %02x 对象长度不符", typ) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i := 0; i < count; i++ { /* 循环处理当前数据。 */
		b := app[2+i*bodySize : 2+(i+1)*bodySize] /* 更新 b 的值。 */
		var obj map[string]any                    /* 声明 obj。 */
		var component map[string]any              /* 声明 component。 */
		switch typ {                              /* 根据条件选择处理路径。 */
		case 1, 0x86: /* 处理当前分支。 */
			status := binary.LittleEndian.Uint16(b[2:4]) /* 更新 status 的值。 */
			if typ == 0x86 {                             /* 判断条件并选择处理分支。 */
				status = 0 /* 更新 status 的值。 */
			} /* 结束当前表达式或代码块。 */
			id := fmt.Sprintf("system/%d", b[1])                                                                       /* 更新 id 的值。 */
			alarms := map[string]bool{"FIRE": status&2 != 0, "DEVICE_FAULT": status&4 != 0 || status&0x700 != 0}       /* 更新 alarms 的值。 */
			obj = map[string]any{"systemType": int(b[0]), "systemAddress": int(b[1]), "status": int(status), "id": id} /* 更新 obj 的值。 */
			component = map[string]any{"id": id, "name": fmt.Sprintf("系统 %d", b[1]), "alarms": alarms}                 /* 更新 component 的值。 */
		case 2, 0x87: /* 处理当前分支。 */
			point, zone, status := binary.LittleEndian.Uint16(b[3:5]), binary.LittleEndian.Uint16(b[5:7]), binary.LittleEndian.Uint16(b[7:9]) /* 更新 status 的值。 */
			if typ == 0x87 {                                                                                                                  /* 判断条件并选择处理分支。 */
				status = 0 /* 更新 status 的值。 */
			} /* 结束当前表达式或代码块。 */
			id := fmt.Sprintf("system/%d/zone/%d/point/%d", b[1], zone, point)                                                                                                       /* 更新 id 的值。 */
			alarms := map[string]bool{"FIRE": status&2 != 0, "DEVICE_FAULT": status&4 != 0 || status&0x100 != 0}                                                                     /* 更新 alarms 的值。 */
			obj = map[string]any{"systemAddress": int(b[1]), "systemType": int(b[0]), "deviceType": int(b[2]), "HLH": int(zone), "TDH": int(point), "status": int(status), "id": id} /* 更新 obj 的值。 */
			component = map[string]any{"id": id, "name": fbTypeName(b[2]), "alarms": alarms}                                                                                         /* 更新 component 的值。 */
		case 0x15, 0x88: /* 处理当前分支。 */
			status := uint16(b[0]) /* 更新 status 的值。 */
			if typ == 0x88 {       /* 判断条件并选择处理分支。 */
				status = 0 /* 更新 status 的值。 */
			} /* 结束当前表达式或代码块。 */
			alarms := map[string]bool{"FIRE": status&2 != 0, "DEVICE_FAULT": status&0x7c != 0} /* 更新 alarms 的值。 */
			obj = map[string]any{"status": int(status), "id": "gateway"}                       /* 更新 obj 的值。 */
			component = map[string]any{"id": "gateway", "name": "用户信息传输装置", "alarms": alarms}  /* 更新 component 的值。 */
		case 0x18: /* 处理当前分支。 */
			obj = map[string]any{"operation": int(b[0]), "operator": int(b[1]), "eventTime": fbTime(b[2:8])} /* 更新 obj 的值。 */
		} /* 结束当前表达式或代码块。 */
		objects = append(objects, obj) /* 更新 objects 的值。 */
		if component != nil {          /* 判断条件并选择处理分支。 */
			components = append(components, component)      /* 更新 components 的值。 */
			alarms := component["alarms"].(map[string]bool) /* 更新 alarms 的值。 */
			fire = fire || alarms["FIRE"]                   /* 更新 fire 的值。 */
			fault = fault || alarms["DEVICE_FAULT"]         /* 更新 fault 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	props["objects"] = objects /* 执行当前语句并推进处理流程。 */
	if len(components) > 0 {   /* 判断条件并选择处理分支。 */
		props["fireAlarm"] = fire                                                                                            /* 执行当前语句并推进处理流程。 */
		props["fault"] = fault                                                                                               /* 执行当前语句并推进处理流程。 */
		return Message{MessageType: "STATE_CHANGE", Properties: props, Event: map[string]any{"components": components}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return properties(props), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func fbTypeName(code byte) string { /* 定义 fbTypeName 函数。 */
	if code == 23 { /* 判断条件并选择处理分支。 */
		return "手动火灾报警按钮" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fmt.Sprintf("部件类型 %d", code) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func fbTime(data []byte) string { /* 定义 fbTime 函数。 */
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", 2000+int(data[5]), data[4], data[3], data[2], data[1], data[0]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
