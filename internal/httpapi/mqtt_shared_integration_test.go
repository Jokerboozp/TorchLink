package httpapi

import (
	"context"
	mqttadapter "iot-platform/internal/adapters/mqtt"
	"iot-platform/internal/auth"
	"os"
	"testing"
	"time"
)

func TestMQTTSharedGatewaySubscription(t *testing.T) {
	broker, secret := os.Getenv("IOT_TEST_MQTT_BROKER"), os.Getenv("IOT_TEST_MQTT_JWT_SECRET")
	if broker == "" || secret == "" {
		t.Skip("MQTT integration environment is not configured")
	}
	suffix := randomHex(8)
	tenant := "shared-" + suffix
	topic := "/iot/up/" + tenant + "/product/device/property"
	manager := auth.New(secret)
	received := make(chan string, 8)
	clients := []*mqttadapter.Client{}
	for _, name := range []string{"first", "second"} {
		username := "shared-" + name + "-" + suffix
		token, err := manager.IssueWithACL(username, tenant, "service", nil, []auth.ACLRule{{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"}, {Permission: "allow", Action: "publish", Topic: topic}}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		client, err := mqttadapter.New(broker, username, token, username)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err = client.ConfigureSharedSubscriptions("test-" + suffix); err != nil {
			t.Fatal(err)
		}
		if err = client.SubscribeStandard(func(ctx context.Context, tnt, product, device, kind string, payload []byte) error {
			if tnt == tenant {
				received <- name
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := clients[0].Publish(ctx, topic, []byte(`{"id":"one","timestamp":1788850000000,"data":{"temperature":42}}`), 1, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("shared subscription did not receive message")
	}
	select {
	case <-received:
		t.Fatal("one publish was delivered to both gateway workers")
	case <-time.After(300 * time.Millisecond):
	}
}
