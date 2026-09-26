package protocolworker

import (
	"strings"
	"testing"
)

func TestIngressResponseCannotConsumeOrAcknowledgeIncompleteFrames(t *testing.T) {
	for _, r := range []Response{
		{NeedMore: true, Consumed: 1}, {NeedMore: true, Reply: "AA"}, {NeedMore: true, DeviceID: "device"},
		{Consumed: -1, DeviceID: "device"}, {Consumed: 3, DeviceID: "device"}, {Consumed: 2, DeviceID: "../other"},
		{Consumed: 2, DeviceID: "device", Reply: "XY"}, {Consumed: 2, DeviceID: "device", Reply: strings.Repeat("00", MaxFrameBytes+1)},
		{Consumed: 2, DeviceID: "device", State: []byte(strings.Repeat(" ", MaxStateBytes+1))},
	} {
		if err := validateResponse("ingress", 2, r); err == nil {
			t.Fatalf("unsafe result accepted: consumed=%d needMore=%v device=%q", r.Consumed, r.NeedMore, r.DeviceID)
		}
	}
	for _, r := range []Response{{NeedMore: true}, {Consumed: 1, DeviceID: "device", Reply: "AA"}} {
		if err := validateResponse("ingress", 2, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateResponse("encode", 0, Response{}); err == nil {
		t.Fatal("empty downlink accepted")
	}
	if err := validateResponse("decode", 0, Response{}); err == nil {
		t.Fatal("empty standard message accepted")
	}
}

func TestDeviceIdentityIsSafeAndStable(t *testing.T) {
	for _, id := range []string{"", " device", "device ", "a/b", "a\\b", "a\x00b", "a\nb", strings.Repeat("a", 129)} {
		if ValidDeviceID(id) {
			t.Fatalf("accepted %q", id)
		}
	}
	if !ValidDeviceID("gb26875_123456789012") {
		t.Fatal("valid address rejected")
	}
}
