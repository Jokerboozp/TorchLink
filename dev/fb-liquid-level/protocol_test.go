package main

import (
	"encoding/hex"
	"testing"
)

func TestLegacyCombinedPacketAndCRC(t *testing.T) {
	data, err := hex.DecodeString("38363838393230373432343334343601460000000D1A00000064003C0000000000160005000000000000000000010000503801460016000102000021B0")
	if err != nil {
		t.Fatal(err)
	}
	first, err := ingressSensor(data, Context{})
	if err != nil || first.Consumed != 50 || first.DeviceID != "868892074243446" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	msg, err := decodeSensor(data[:first.Consumed], Context{})
	if err != nil || msg.Properties["batteryLevel"] != 100 || msg.Properties["signalStrength"] != 22 {
		t.Fatalf("message=%+v err=%v", msg, err)
	}
	second, err := ingressSensor(data[first.Consumed:], Context{DeviceID: first.DeviceID, State: first.State})
	if err != nil || second.Consumed != 11 || second.DeviceID != first.DeviceID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	alarm, err := decodeSensor(data[first.Consumed:], Context{DeviceID: first.DeviceID, State: first.State})
	if err != nil || alarm.Properties["alarm"] != false {
		t.Fatalf("alarm=%+v err=%v", alarm, err)
	}
	broken := append([]byte(nil), data[:first.Consumed]...)
	broken[len(broken)-1] ^= 1
	if _, err = ingressSensor(broken, Context{}); err == nil {
		t.Fatal("CRC corruption accepted")
	}
}

func TestCommandConfirmation(t *testing.T) {
	sent, err := encodeSensor(Command{Type: "setMultipleParams", Params: map[string]any{"collectionTime": 5, "alarmLowerLimit": -100, "alarmUpperLimit": 300}}, Context{State: map[string]any{"unit": 1}})
	if err != nil || len(sent.Reply) != 19 || sent.CorrelationID != "10-0006" {
		t.Fatalf("sent=%+v err=%v", sent, err)
	}
	ack := rtu([]byte{1, 0x10, 0, 6, 0, 5})
	got, err := ingressSensor(ack, Context{DeviceID: "868892074243446", State: sent.State})
	if err != nil || got.CorrelationID != sent.CorrelationID {
		t.Fatalf("ack=%+v err=%v", got, err)
	}
}
