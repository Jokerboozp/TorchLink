package gb26875 /* 声明 gb26875 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Request and Response implement the platform-neutral go-protocol-v2 contract.
// The host owns sockets, tenant authorization, registration and message storage.
type Request struct { /* 定义 Request 类型。 */
	Version   int             `json:"version"`           /* 执行当前语句并推进处理流程。 */
	Operation string          `json:"operation"`         /* 执行当前语句并推进处理流程。 */
	Raw       *RawMessage     `json:"raw,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Data      string          `json:"data,omitempty"`    /* 执行当前语句并推进处理流程。 */
	State     json.RawMessage `json:"state,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Command   json.RawMessage `json:"command,omitempty"` /* 执行当前语句并推进处理流程。 */
	Now       int64           `json:"now,omitempty"`     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Response struct { /* 定义 Response 类型。 */
	Error           string           `json:"error,omitempty"`           /* 执行当前语句并推进处理流程。 */
	Consumed        int              `json:"consumed,omitempty"`        /* 执行当前语句并推进处理流程。 */
	NeedMore        bool             `json:"needMore,omitempty"`        /* 执行当前语句并推进处理流程。 */
	DeviceID        string           `json:"deviceId,omitempty"`        /* 执行当前语句并推进处理流程。 */
	DeviceName      string           `json:"deviceName,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Reply           string           `json:"reply,omitempty"`           /* 执行当前语句并推进处理流程。 */
	State           *SessionState    `json:"state,omitempty"`           /* 执行当前语句并推进处理流程。 */
	CorrelationID   string           `json:"correlationId,omitempty"`   /* 执行当前语句并推进处理流程。 */
	StandardMessage *StandardMessage `json:"standardMessage,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// SessionState is returned to the host and passed into subsequent calls.
// It is serializable so no protocol process must stay alive between messages.
type SessionState struct { /* 定义 SessionState 类型。 */
	Source   string `json:"source"`   /* 执行当前语句并推进处理流程。 */
	Sequence uint16 `json:"sequence"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func Handle(request Request) Response { /* 定义 Handle 函数。 */
	if request.Version != 2 { /* 判断条件并选择处理分支。 */
		return Response{Error: "protocol request version must be 2"} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch request.Operation { /* 根据条件选择处理路径。 */
	case "decode": /* 处理当前分支。 */
		if request.Raw == nil { /* 判断条件并选择处理分支。 */
			return Response{Error: "decode requires raw"} /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		message, err := Decode(*request.Raw) /* 更新 err 的值。 */
		if err != nil {                      /* 判断条件并选择处理分支。 */
			return Response{Error: err.Error()} /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return Response{StandardMessage: message} /* 返回当前处理结果。 */
	case "ingress": /* 处理当前分支。 */
		return ingress(request) /* 返回当前处理结果。 */
	case "encode": /* 处理当前分支。 */
		return encode(request) /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return Response{Error: fmt.Sprintf("unsupported operation %q", request.Operation)} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func requestTime(now int64) time.Time { /* 定义 requestTime 函数。 */
	if now == 0 { /* 判断条件并选择处理分支。 */
		return time.Now().In(DeviceLocation) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return time.UnixMilli(now).In(DeviceLocation) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ingress(request Request) Response { /* 定义 ingress 函数。 */
	data, err := hex.DecodeString(strings.Join(strings.Fields(request.Data), "")) /* 更新 err 的值。 */
	if err != nil {                                                               /* 判断条件并选择处理分支。 */
		return Response{Error: fmt.Sprintf("invalid ingress hex: %v", err)} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) == 0 || data[0] != '@' || (len(data) >= 2 && data[1] != '@') { /* 判断条件并选择处理分支。 */
		return Response{Error: "invalid GB26875 frame prefix"} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < 27 { /* 判断条件并选择处理分支。 */
		return Response{NeedMore: true} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	start := 0                                                               /* 更新 start 的值。 */
	dataLength := int(binary.LittleEndian.Uint16(data[start+24 : start+26])) /* 更新 dataLength 的值。 */
	if dataLength > gb26875MaxDataLength {                                   /* 判断条件并选择处理分支。 */
		return Response{Consumed: start + 2, Error: fmt.Sprintf("GB26875 application data length %d exceeds %d", dataLength, gb26875MaxDataLength)} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	frameLength := 30 + dataLength     /* 更新 frameLength 的值。 */
	if len(data)-start < frameLength { /* 判断条件并选择处理分支。 */
		return Response{NeedMore: true} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	frameBytes := data[start : start+frameLength]       /* 更新 frameBytes 的值。 */
	response := Response{Consumed: start + frameLength} /* 更新 response 的值。 */
	frame, err := DecodeFrame(frameBytes)               /* 更新 err 的值。 */
	if err != nil {                                     /* 判断条件并选择处理分支。 */
		response.Error = err.Error() /* 更新 response.Error 的值。 */
		return response              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw := RawMessage{Payload: MarshalHexPayload(frameBytes), PayloadFormat: "hex", ReceivedAt: requestTime(request.Now).UnixMilli()} /* 更新 raw 的值。 */
	message, err := Decode(raw)                                                                                                       /* 更新 err 的值。 */
	if err != nil {                                                                                                                   /* 判断条件并选择处理分支。 */
		response.Error = err.Error() /* 更新 response.Error 的值。 */
		return response              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	response.DeviceID = "gb26875_" + strings.ToLower(frame.Source)                 /* 更新 response.DeviceID 的值。 */
	response.DeviceName = "GB26875 设备 " + frame.Source                             /* 更新 response.DeviceName 的值。 */
	response.State = &SessionState{Source: frame.Source, Sequence: frame.Sequence} /* 更新 response.State 的值。 */
	// Incoming report/ACK sequence numbers must not rewind the outbound counter.
	// Otherwise a delayed old ACK could complete a later command reusing its ID.
	var previous SessionState                                                               /* 声明 previous。 */
	if json.Unmarshal(request.State, &previous) == nil && previous.Source == frame.Source { /* 判断条件并选择处理分支。 */
		response.State.Sequence = previous.Sequence /* 更新 response.State.Sequence 的值。 */
	} /* 结束当前表达式或代码块。 */
	var destination [6]byte                 /* 声明 destination。 */
	copy(destination[:], frameBytes[12:18]) /* 执行当前语句并推进处理流程。 */
	switch message.Event["type"] {          /* 根据条件选择处理路径。 */
	case "ACK", "TIME_SYNC": /* 处理当前分支。 */
		response.CorrelationID = strconv.Itoa(int(frame.Sequence)) /* 更新 response.CorrelationID 的值。 */
	case "TIME_SYNC_REQUEST": /* 处理当前分支。 */
		response.Reply = strings.ToUpper(hex.EncodeToString(BuildGB26875TimeSyncFrame(frame.Sequence, destination, requestTime(request.Now)))) /* 更新 response.Reply 的值。 */
	default: /* 处理当前分支。 */
		response.Reply = strings.ToUpper(hex.EncodeToString(BuildGB26875AckFrame(frame.Sequence, destination, requestTime(request.Now)))) /* 更新 response.Reply 的值。 */
	} /* 结束当前表达式或代码块。 */
	return response /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func encode(request Request) Response { /* 定义 encode 函数。 */
	var state SessionState                                        /* 声明 state。 */
	if err := json.Unmarshal(request.State, &state); err != nil { /* 判断条件并选择处理分支。 */
		return Response{Error: "encode requires valid session state"} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	address, err := hex.DecodeString(state.Source) /* 更新 err 的值。 */
	if err != nil || len(address) != 6 {           /* 判断条件并选择处理分支。 */
		return Response{Error: "session state source must contain 12 hexadecimal digits"} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var command struct { /* 声明 command。 */
		Type      string `json:"type"`      /* 执行当前语句并推进处理流程。 */
		Timestamp int64  `json:"timestamp"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.Unmarshal(request.Command, &command); err != nil { /* 判断条件并选择处理分支。 */
		return Response{Error: "encode requires a valid command object"} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if command.Type != "time-sync" { /* 判断条件并选择处理分支。 */
		return Response{Error: fmt.Sprintf("unsupported command type %q", command.Type)} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	at := requestTime(request.Now) /* 更新 at 的值。 */
	if command.Timestamp != 0 {    /* 判断条件并选择处理分支。 */
		at = requestTime(command.Timestamp) /* 更新 at 的值。 */
	} /* 结束当前表达式或代码块。 */
	state.Sequence++         /* 执行当前语句并推进处理流程。 */
	if state.Sequence == 0 { /* 判断条件并选择处理分支。 */
		state.Sequence = 1 /* 更新 state.Sequence 的值。 */
	} /* 结束当前表达式或代码块。 */
	var destination [6]byte                                                                                                             /* 声明 destination。 */
	copy(destination[:], address)                                                                                                       /* 执行当前语句并推进处理流程。 */
	frame := BuildGB26875TimeSyncFrame(state.Sequence, destination, at)                                                                 /* 更新 frame 的值。 */
	return Response{Reply: strings.ToUpper(hex.EncodeToString(frame)), State: &state, CorrelationID: strconv.Itoa(int(state.Sequence))} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
