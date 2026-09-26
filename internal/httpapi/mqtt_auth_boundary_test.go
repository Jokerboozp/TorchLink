package httpapi

import (
	"os"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform/internal/auth"
)

// This probe does not publish or subscribe to any business topic.
func TestMQTTBrokerRejectsInvalidJWT(t *testing.T) {
	broker := os.Getenv("IOT_TEST_MQTT_BROKER")
	if broker == "" {
		t.Skip("IOT_TEST_MQTT_BROKER not configured")
	}
	id := "auth-probe-" + randomHex(8)
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(id).SetUsername(id).SetPassword("invalid-jwt").SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(time.Second))
	defer client.Disconnect(100)
	token := client.Connect()
	if !token.WaitTimeout(2 * time.Second) {
		t.Fatal("broker did not return an authentication verdict")
	}
	code := token.(*mqtt.ConnectToken).ReturnCode()
	if token.Error() == nil || (code != 4 && code != 5) {
		t.Fatalf("invalid JWT was not rejected: CONNACK %d", code)
	}
}

func TestMQTTBrokerBindsJWTUsername(t *testing.T) {
	broker, secret := os.Getenv("IOT_TEST_MQTT_BROKER"), os.Getenv("IOT_TEST_MQTT_JWT_SECRET")
	if broker == "" || secret == "" {
		t.Skip("MQTT integration environment not configured")
	}
	id := "identity-probe-" + randomHex(8)
	jwt, err := auth.New(secret).IssueWithACL(id, id, "device", nil, nil, time.Minute)
	if err != nil {
		t.Fatal("could not issue probe JWT")
	}
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(id).SetUsername(id + "-wrong").SetPassword(jwt).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(time.Second))
	defer client.Disconnect(100)
	token := client.Connect()
	if !token.WaitTimeout(2 * time.Second) {
		t.Fatal("broker did not return an authentication verdict")
	}
	code := token.(*mqtt.ConnectToken).ReturnCode()
	if token.Error() == nil || (code != 4 && code != 5) {
		t.Fatalf("JWT username mismatch was not rejected: CONNACK %d", code)
	}
}
