package mqttadapter

import (
	"context"
	"errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"strings"
	"time"
)

func StandardTopic(topic string) (tenant, product, device, kind string, err error) {
	p := strings.Split(topic, "/")
	if len(p) != 7 || p[0] != "" || p[1] != "iot" || p[2] != "up" || (p[6] != "property" && p[6] != "event" && p[6] != "state" && p[6] != "command-reply") {
		err = errors.New("expected /iot/up/{tenant}/{product}/{device}/{property|event|state}")
		return
	}
	return p[3], p[4], p[5], p[6], nil
}

// Broker authentication/ACL establishes publisher identity. The handler also
// checks the current inventory status and never accepts identity in the body.
func (c *Client) SubscribeStandard(handler func(context.Context, string, string, string, string, []byte) error) error {
	token := c.client.Subscribe("/iot/up/+/+/+/+", 1, func(_ mqtt.Client, m mqtt.Message) {
		tenant, product, device, kind, err := StandardTopic(m.Topic())
		if err != nil || len(m.Payload()) > 64<<10 || m.Retained() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err = handler(ctx, tenant, product, device, kind, m.Payload()); err != nil {
			c.logger().Warn("standard MQTT rejected", "topic", m.Topic(), "error", err)
		}
	})
	if !token.WaitTimeout(10 * time.Second) {
		return errors.New("subscribe standard MQTT timeout")
	}
	return token.Error()
}
