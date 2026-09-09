package parser

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
)

const ModbusRTUParserName = "modbus_rtu_parser_v2"

type ModbusRTUParser struct{}

func (ModbusRTUParser) Name() string    { return ModbusRTUParserName }
func (ModbusRTUParser) Version() string { return ModbusTCPParserVersion }
func (ModbusRTUParser) Match(Meta) bool { return false }
func (ModbusRTUParser) Parse(model.RawMessage) (*model.StandardMessage, error) {
	return nil, errors.New("Modbus RTU requires a versioned point table")
}
func (ModbusRTUParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	frame, err := decodeHexPayload(raw.Payload)
	if err != nil {
		return nil, err
	}
	if err = modbusframe.Validate(frame); err != nil {
		return nil, err
	}
	// Reuse point decoding for the common PDU. The archive and returned raw
	// evidence remain the original RTU frame, including its CRC.
	header := make([]byte, 6)
	binary.BigEndian.PutUint16(header[4:], uint16(len(frame)-2))
	copyRaw := raw
	copyRaw.Payload, _ = json.Marshal(hex.EncodeToString(append(header, frame[:len(frame)-2]...)))
	message, err := (ModbusTCPParser{}).ParseWithConfig(copyRaw, config)
	if err == nil {
		message.Raw["payload"] = string(raw.Payload)
		message.Raw["transport"] = "MODBUS_RTU"
	}
	return message, err
}
