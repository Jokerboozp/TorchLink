package mqttadapter

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

// ClearCapacityRetained uses the normal MQTT publisher's exact state-topic
// permission. MQTT removes a retained value on an acknowledged empty retained
// publication; no broker administration, wildcards or whole-retainer clear is used.
func (c *Client) ClearCapacityRetained(ctx context.Context, tenant, product string, devices []string) (int64, error) {
	valid := func(v string) bool { return v != "" && !strings.ContainsAny(v, "/+#\x00") }
	if !valid(tenant) || !valid(product) {
		return 0, errors.New("capacity MQTT cleanup requires exact tenant and product")
	}
	for _, id := range devices {
		if !valid(id) {
			return 0, errors.New("capacity MQTT cleanup device ID is not an exact topic level")
		}
	}
	if len(devices) == 0 {
		return 0, nil
	}
	if c.client == nil {
		return 0, errors.New("MQTT retained cleanup is unavailable")
	}
	var n int64
	for _, device := range slices.Compact(slices.Sorted(slices.Values(devices))) {
		if err := c.Publish(ctx, fmt.Sprintf("/iot/device/state/%s/%s/%s", tenant, product, device), []byte{}, 1, true); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// DiscardCapacityInbox removes pending and quarantined receipts of exact
// fixture devices from this process's durable inbox, so removed devices leave
// no entries that would later fail or block identical retransmissions. Each
// shard is held against its drain loop while discarding.
func (c *Client) DiscardCapacityInbox(ctx context.Context, tenant, product string, devices []string) (int64, error) {
	if c.inbox == nil || tenant == "" || product == "" || len(devices) == 0 {
		return 0, nil
	}
	fixtures := make(map[string]bool, len(devices))
	for _, id := range devices {
		fixtures[id] = true
	}
	match := func(raw model.RawMessage) bool {
		t, p, d, ok := inboxTopicIdentity(raw.Headers["topic"])
		return ok && t == tenant && p == product && fixtures[d]
	}
	var removed int64
	for i, q := range c.inbox.queues {
		c.inbox.drainMu[i].Lock()
		n, _, err := q.DiscardMatching(ctx, match)
		c.inbox.drainMu[i].Unlock()
		removed += n
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// inboxTopicIdentity reads tenant, product and device from a received topic.
func inboxTopicIdentity(topic string) (tenant, product, device string, ok bool) {
	parts := topicParts(topic)
	switch route(topic) {
	case "standard":
		if len(parts) >= 5 {
			return parts[2], parts[3], parts[4], true
		}
	case "raw":
		if len(parts) == 5 {
			return parts[2], parts[3], parts[4], true
		}
	case "state":
		if len(parts) == 6 {
			return parts[3], parts[4], parts[5], true
		}
	}
	return "", "", "", false
}
