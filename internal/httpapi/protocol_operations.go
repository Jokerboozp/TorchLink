package httpapi

import "iot-platform/internal/protocolworker"

type protocolOperationCase struct {
	Name     string                 `json:"name"`
	Request  protocolworker.Request `json:"request"`
	Expected map[string]any         `json:"expected"`
}
