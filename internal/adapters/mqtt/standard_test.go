package mqttadapter

import "testing"

func TestStandardTopicIdentity(t *testing.T) {
	tenant, product, device, kind, err := StandardTopic("/iot/up/t/p/d/property")
	if err != nil || tenant != "t" || product != "p" || device != "d" || kind != "property" {
		t.Fatal(tenant, product, device, kind, err)
	}
	for _, topic := range []string{"/iot/up/t/p/d/command", "iot/up/t/p/d/property", "/iot/up/t/p/d/property/extra", "/external/raw/t/p/d"} {
		if _, _, _, _, err := StandardTopic(topic); err == nil {
			t.Fatalf("accepted %q", topic)
		}
	}
}
