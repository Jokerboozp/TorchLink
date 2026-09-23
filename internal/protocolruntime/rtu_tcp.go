package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                           /* 执行当前语句并推进处理流程。 */
	"errors"                            /* 执行当前语句并推进处理流程。 */
	"io"                                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/modbusframe" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"       /* 执行当前语句并推进处理流程。 */
	"net"                               /* 执行当前语句并推进处理流程。 */
	"sync"                              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var modbusBuses = struct { /* 声明 modbusBuses。 */
	sync.Mutex                       /* 执行当前语句并推进处理流程。 */
	entries    map[string]*modbusBus /* 执行当前语句并推进处理流程。 */
}{entries: map[string]*modbusBus{}} /* 结束当前表达式或代码块。 */

type modbusBus struct { /* 定义 modbusBus 类型。 */
	token chan struct{} /* 执行当前语句并推进处理流程。 */
	refs  int           /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// One transaction sequence per physical TCP endpoint, including all unit IDs.
func lockModbusBus(ctx context.Context, key string) (func(), error) { /* 定义 lockModbusBus 函数。 */
	modbusBuses.Lock()            /* 执行当前语句并推进处理流程。 */
	b := modbusBuses.entries[key] /* 更新 b 的值。 */
	if b == nil {                 /* 判断条件并选择处理分支。 */
		b = &modbusBus{token: make(chan struct{}, 1)} /* 更新 b 的值。 */
		modbusBuses.entries[key] = b                  /* 更新 modbusBuses.entries[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	b.refs++               /* 执行当前语句并推进处理流程。 */
	modbusBuses.Unlock()   /* 执行当前语句并推进处理流程。 */
	releaseRef := func() { /* 更新 releaseRef 的值。 */
		modbusBuses.Lock() /* 执行当前语句并推进处理流程。 */
		b.refs--           /* 执行当前语句并推进处理流程。 */
		if b.refs == 0 {   /* 判断条件并选择处理分支。 */
			delete(modbusBuses.entries, key) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		modbusBuses.Unlock() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case b.token <- struct{}{}: /* 处理当前分支。 */
		return func() { <-b.token; releaseRef() }, nil /* 返回当前处理结果。 */
	case <-ctx.Done(): /* 处理当前分支。 */
		releaseRef()          /* 执行当前语句并推进处理流程。 */
		return nil, ctx.Err() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func readRTUResponse(conn net.Conn, unit, function byte) ([]byte, error) { /* 定义 readRTUResponse 函数。 */
	head := make([]byte, 3)                            /* 更新 head 的值。 */
	if _, err := io.ReadFull(conn, head); err != nil { /* 判断条件并选择处理分支。 */
		return head, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if head[0] != unit || (head[1] != function && head[1] != function|0x80) { /* 判断条件并选择处理分支。 */
		return head, errors.New("Modbus RTU address or function mismatch") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	size := int(head[2]) + 2 /* 更新 size 的值。 */
	if head[1]&0x80 != 0 {   /* 判断条件并选择处理分支。 */
		size = 2 /* 更新 size 的值。 */
	} /* 结束当前表达式或代码块。 */
	if size > 252 { /* 判断条件并选择处理分支。 */
		return head, errors.New("Modbus RTU response too large") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tail := make([]byte, size)         /* 更新 tail 的值。 */
	n, err := io.ReadFull(conn, tail)  /* 更新 err 的值。 */
	frame := append(head, tail[:n]...) /* 更新 frame 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return frame, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = modbusframe.Validate(frame); err != nil { /* 判断条件并选择处理分支。 */
		return frame, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if head[1]&0x80 != 0 { /* 判断条件并选择处理分支。 */
		return frame, &ModbusException{Code: head[2]} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return frame, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validateReadByteCount(frame []byte, block model.ModbusReadBlock, rtu bool) error { /* 定义 validateReadByteCount 函数。 */
	offset := 8 /* 更新 offset 的值。 */
	if rtu {    /* 判断条件并选择处理分支。 */
		offset = 2 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	expected := block.Quantity * 2 /* 更新 expected 的值。 */
	if block.FunctionCode <= 2 {   /* 判断条件并选择处理分支。 */
		expected = (block.Quantity + 7) / 8 /* 更新 expected 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(frame) <= offset || int(frame[offset]) != expected { /* 判断条件并选择处理分支。 */
		return errors.New("Modbus response byte count does not match query") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	size := offset + 1 + expected /* 更新 size 的值。 */
	if rtu {                      /* 判断条件并选择处理分支。 */
		size += 2 /* 更新 size 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(frame) != size { /* 判断条件并选择处理分支。 */
		return errors.New("Modbus response length does not match query") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
