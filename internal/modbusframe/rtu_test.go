package modbusframe

import "testing"

func TestCRCMatchesModbusReferenceFrames(t *testing.T) {
	// Read holding registers 0..9 of unit 1: the CRC goes on the wire as C5 CD.
	frame := AppendCRC([]byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x0A})
	if got := frame[len(frame)-2:]; got[0] != 0xC5 || got[1] != 0xCD {
		t.Fatalf("crc bytes % X", got)
	}
	if err := Validate(frame); err != nil {
		t.Fatal(err)
	}
	frame[2] ^= 0x01
	if Validate(frame) == nil {
		t.Fatal("corrupted frame accepted")
	}
	if Validate([]byte{1, 3, 0, 0}) == nil || Validate(make([]byte, 257)) == nil {
		t.Fatal("frame outside the RTU length limits accepted")
	}
}
