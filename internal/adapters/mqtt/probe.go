package mqttadapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform/internal/connector"
)

// Probe verifies current platform credentials with an independent clean session.
// It neither replaces the live subscription client nor publishes device data.
func (c *Client) Probe(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.broker == "" || c.credentials == nil {
		return errors.New("MQTT probe configuration unavailable")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	probe := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(c.broker).SetProtocolVersion(4).SetClientID("iot-probe-" + hex.EncodeToString(nonce[:])).SetCredentialsProvider(c.credentials).SetCleanSession(true).SetConnectRetry(false).SetAutoReconnect(false).SetConnectTimeout(5 * time.Second))
	defer probe.Disconnect(0)
	token := probe.Connect()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-token.Done():
	}
	if token.Error() != nil {
		if ack, ok := token.(*mqtt.ConnectToken); ok && (ack.ReturnCode() == 4 || ack.ReturnCode() == 5) {
			return connector.ErrAuthentication
		}
		return token.Error()
	}
	return nil
}
