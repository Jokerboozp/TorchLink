package modbusframe

import (
	"encoding/binary"
	"errors"
)

// CRC is the Modbus RTU CRC16; the wire order is low byte first.
func CRC(data []byte) uint16 {
	v := uint16(0xffff)
	for _, b := range data {
		v ^= uint16(b)
		for i := 0; i < 8; i++ {
			if v&1 != 0 {
				v = v>>1 ^ 0xa001
			} else {
				v >>= 1
			}
		}
	}
	return v
}
func AppendCRC(data []byte) []byte {
	v := CRC(data)
	return append(data, byte(v), byte(v>>8))
}
func Validate(data []byte) error {
	if len(data) < 5 || len(data) > 256 {
		return errors.New("invalid Modbus RTU frame length")
	}
	if CRC(data[:len(data)-2]) != binary.LittleEndian.Uint16(data[len(data)-2:]) {
		return errors.New("Modbus RTU CRC mismatch")
	}
	return nil
}
