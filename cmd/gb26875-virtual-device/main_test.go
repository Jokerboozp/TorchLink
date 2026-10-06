package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"
)

func TestScenarioValues(t *testing.T) {
	for name, want := range map[string]struct {
		kind   byte
		status uint16
	}{"manual-alarm": {23, 2}, "manual-normal": {23, 0}, "smoke-alarm": {40, 2}, "sound-light-start": {137, 96}, "sound-light-stop": {137, 0}, "time-sync-request": {0, 0}} {
		kind, status, desc, err := scenarioValues(name)
		if err != nil || kind != want.kind || status != want.status || desc == "" {
			t.Fatalf("%s: %d %d %q %v", name, kind, status, desc, err)
		}
	}
	if _, _, _, err := scenarioValues("fire-drill"); err == nil {
		t.Fatal("unknown scenario accepted")
	}
}

// frame is a GB26875 frame with the given application data length: "@@",
// a 25-byte header whose bytes 22-23 hold the length, the data, then
// checksum and "##".
func frame(length int) []byte {
	header := make([]byte, 25)
	binary.LittleEndian.PutUint16(header[22:24], uint16(length))
	out := append([]byte("@@"), header...)
	out = append(out, bytes.Repeat([]byte{0x5a}, length)...)
	return append(out, 0x00, '#', '#')
}

func TestReadGB26875FrameSkipsNoiseAndBoundsLength(t *testing.T) {
	want := frame(4)
	input := append([]byte("noise@x"), want...)
	got, err := readGB26875Frame(bufio.NewReader(bytes.NewReader(input)))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("frame % x %v", got, err)
	}
	if _, err = readGB26875Frame(bufio.NewReader(bytes.NewReader(frame(513)))); err == nil {
		t.Fatal("application data over 512 bytes accepted")
	}
	if _, err = readGB26875Frame(bufio.NewReader(bytes.NewReader(want[:20]))); err == nil {
		t.Fatal("truncated frame accepted")
	}
}
