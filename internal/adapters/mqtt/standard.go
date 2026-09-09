package mqttadapter

import (
	"context"
	"errors"
	"strings"
)

func StandardTopic(topic string) (tenant, product, device, kind string, err error) {
	p := strings.Split(topic, "/")
	if len(p) != 7 || p[0] != "" || p[1] != "iot" || p[2] != "up" || (p[6] != "property" && p[6] != "event" && p[6] != "state" && p[6] != "command-reply" && p[6] != "shadow-get") {
		err = errors.New("expected /iot/up/{tenant}/{product}/{device}/{property|event|state|command-reply|shadow-get}")
		return
	}
	return p[3], p[4], p[5], p[6], nil
}

// Broker authentication/ACL establishes publisher identity. The handler also
// checks the current inventory status and never accepts identity in the body.
func (c *Client) SubscribeStandard(handler func(context.Context, string, string, string, string, []byte) error) error {
	return c.register("standard", []string{"/iot/up/+/+/+/+"}, func(ctx context.Context, topic string, payload []byte) error {
		tenant, product, device, kind, err := StandardTopic(topic)
		if err != nil || len(payload) > 64<<10 {
			return Reject(errors.New("invalid standard topic or payload"))
		}
		return handler(ctx, tenant, product, device, kind, payload)
	})
}
