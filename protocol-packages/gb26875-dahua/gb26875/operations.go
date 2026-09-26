package gb26875

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Request and Response implement the platform-neutral go-protocol-v2 contract.
// The host owns sockets, tenant authorization, registration and message storage.
type Request struct {
	Version   int             `json:"version"`
	Operation string          `json:"operation"`
	Raw       *RawMessage     `json:"raw,omitempty"`
	Data      string          `json:"data,omitempty"`
	State     json.RawMessage `json:"state,omitempty"`
	Command   json.RawMessage `json:"command,omitempty"`
	Now       int64           `json:"now,omitempty"`
}

type Response struct {
	Error           string           `json:"error,omitempty"`
	Consumed        int              `json:"consumed,omitempty"`
	NeedMore        bool             `json:"needMore,omitempty"`
	DeviceID        string           `json:"deviceId,omitempty"`
	DeviceName      string           `json:"deviceName,omitempty"`
	Reply           string           `json:"reply,omitempty"`
	State           *SessionState    `json:"state,omitempty"`
	CorrelationID   string           `json:"correlationId,omitempty"`
	StandardMessage *StandardMessage `json:"standardMessage,omitempty"`
}

// SessionState is returned to the host and passed into subsequent calls.
// It is serializable so no protocol process must stay alive between messages.
type SessionState struct {
	Source   string `json:"source"`
	Sequence uint16 `json:"sequence"`
}

func Handle(request Request) Response {
	if request.Version != 2 {
		return Response{Error: "protocol request version must be 2"}
	}
	switch request.Operation {
	case "decode":
		if request.Raw == nil {
			return Response{Error: "decode requires raw"}
		}
		message, err := Decode(*request.Raw)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{StandardMessage: message}
	case "ingress":
		return ingress(request)
	case "encode":
		return encode(request)
	default:
		return Response{Error: fmt.Sprintf("unsupported operation %q", request.Operation)}
	}
}

func requestTime(now int64) time.Time {
	if now == 0 {
		return time.Now().In(DeviceLocation)
	}
	return time.UnixMilli(now).In(DeviceLocation)
}

func ingress(request Request) Response {
	data, err := hex.DecodeString(strings.Join(strings.Fields(request.Data), ""))
	if err != nil {
		return Response{Error: fmt.Sprintf("invalid ingress hex: %v", err)}
	}
	if len(data) == 0 || data[0] != '@' || (len(data) >= 2 && data[1] != '@') {
		return Response{Error: "invalid GB26875 frame prefix"}
	}
	if len(data) < 27 {
		return Response{NeedMore: true}
	}
	start := 0
	dataLength := int(binary.LittleEndian.Uint16(data[start+24 : start+26]))
	if dataLength > gb26875MaxDataLength {
		return Response{Consumed: start + 2, Error: fmt.Sprintf("GB26875 application data length %d exceeds %d", dataLength, gb26875MaxDataLength)}
	}
	frameLength := 30 + dataLength
	if len(data)-start < frameLength {
		return Response{NeedMore: true}
	}
	frameBytes := data[start : start+frameLength]
	response := Response{Consumed: start + frameLength}
	frame, err := DecodeFrame(frameBytes)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	raw := RawMessage{Payload: MarshalHexPayload(frameBytes), PayloadFormat: "hex", ReceivedAt: requestTime(request.Now).UnixMilli()}
	message, err := Decode(raw)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	response.DeviceID = "gb26875_" + strings.ToLower(frame.Source)
	response.DeviceName = "GB26875 设备 " + frame.Source
	response.State = &SessionState{Source: frame.Source, Sequence: frame.Sequence}
	// Incoming report/ACK sequence numbers must not rewind the outbound counter.
	// Otherwise a delayed old ACK could complete a later command reusing its ID.
	var previous SessionState
	if json.Unmarshal(request.State, &previous) == nil && previous.Source == frame.Source {
		response.State.Sequence = previous.Sequence
	}
	var destination [6]byte
	copy(destination[:], frameBytes[12:18])
	switch message.Event["type"] {
	case "ACK", "TIME_SYNC":
		response.CorrelationID = strconv.Itoa(int(frame.Sequence))
	case "TIME_SYNC_REQUEST":
		response.Reply = strings.ToUpper(hex.EncodeToString(BuildGB26875TimeSyncFrame(frame.Sequence, destination, requestTime(request.Now))))
	default:
		response.Reply = strings.ToUpper(hex.EncodeToString(BuildGB26875AckFrame(frame.Sequence, destination, requestTime(request.Now))))
	}
	return response
}

func encode(request Request) Response {
	var state SessionState
	if err := json.Unmarshal(request.State, &state); err != nil {
		return Response{Error: "encode requires valid session state"}
	}
	address, err := hex.DecodeString(state.Source)
	if err != nil || len(address) != 6 {
		return Response{Error: "session state source must contain 12 hexadecimal digits"}
	}
	var command struct {
		Type      string `json:"type"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(request.Command, &command); err != nil {
		return Response{Error: "encode requires a valid command object"}
	}
	if command.Type != "time-sync" {
		return Response{Error: fmt.Sprintf("unsupported command type %q", command.Type)}
	}
	at := requestTime(request.Now)
	if command.Timestamp != 0 {
		at = requestTime(command.Timestamp)
	}
	state.Sequence++
	if state.Sequence == 0 {
		state.Sequence = 1
	}
	var destination [6]byte
	copy(destination[:], address)
	frame := BuildGB26875TimeSyncFrame(state.Sequence, destination, at)
	return Response{Reply: strings.ToUpper(hex.EncodeToString(frame)), State: &state, CorrelationID: strconv.Itoa(int(state.Sequence))}
}
