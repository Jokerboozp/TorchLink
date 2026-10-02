package mqttadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func capacityDevices(tenant string, q model.CapacityCleanupBatch) ([]string, error) {
	valid := func(v string) bool { return v != "" && !strings.ContainsAny(v, "/+#\x00") }
	if !valid(tenant) || !valid(q.Product) {
		return nil, errors.New("capacity MQTT cleanup requires exact tenant and product")
	}
	manifest := map[string]bool{}
	for _, id := range q.Devices {
		manifest[id] = true
	}
	set := map[string]bool{}
	for _, id := range q.RemoveDevices {
		if !valid(id) || !manifest[id] {
			return nil, errors.New("capacity MQTT cleanup device is outside the exclusive manifest")
		}
		set[id] = true
	}
	devices := make([]string, 0, len(set))
	for id := range set {
		devices = append(devices, id)
	}
	sort.Strings(devices)
	return devices, nil
}

// ClearCapacityRetained uses the normal MQTT publisher's exact state-topic
// permission. MQTT removes a retained value on an acknowledged empty retained
// publication; no broker administration, wildcards or whole-retainer clear is used.
func (c *Client) ClearCapacityRetained(ctx context.Context, tenant string, q model.CapacityCleanupBatch) (ports.RuntimeCleanupCounts, error) {
	var n ports.RuntimeCleanupCounts
	devices, err := capacityDevices(tenant, q)
	if err != nil || len(devices) == 0 {
		return n, err
	}
	if c.client == nil {
		return n, errors.New("MQTT retained cleanup is unavailable")
	}
	for _, device := range devices {
		topic := fmt.Sprintf("/iot/device/state/%s/%s/%s", tenant, q.Product, device)
		if err := c.Publish(ctx, topic, []byte{}, 1, true); err != nil {
			return n, err
		}
		n.RetainedRequests++
	}
	return n, nil
}

// CleanupCapacityInbox serializes each shard's active handler with discarding,
// so a fetched message cannot recreate business data after it was discarded.
// The caller first disables/removes fixture ingress through the normal inventory
// boundary. Unknown or corrupt envelopes are preserved and counted as skipped.
func (c *Client) CleanupCapacityInbox(ctx context.Context, tenant string, q model.CapacityCleanupBatch) (ports.RuntimeCleanupCounts, error) {
	var n ports.RuntimeCleanupCounts
	devices, err := capacityDevices(tenant, q)
	wholeProduct := q.Historical && q.RemoveProduct && len(q.Devices) == 0 && len(q.RemoveDevices) == 0
	if err != nil || (len(devices) == 0 && !wholeProduct) {
		return n, err
	}
	if c.inbox == nil {
		n.Warnings = append(n.Warnings, "本实例未部署 MQTT durable inbox，未清理其他实例队列")
		return n, nil
	}
	remove := map[string]bool{}
	for _, id := range devices {
		remove[id] = true
	}
	match := func(raw model.RawMessage) bool {
		if raw.TenantID != "mqtt-inbox" || raw.Source != "mqtt-receive-inbox" {
			return false
		}
		var payload []byte
		topic := raw.Headers["topic"]
		if json.Unmarshal(raw.Payload, &payload) != nil || raw.MessageID != ingressID(topic, payload) {
			return false
		}
		t, p, d, known := capacityTopicIdentity(topic)
		return known && t == tenant && p == q.Product && d != "" && (wholeProduct || remove[d])
	}
	for index, queue := range c.inbox.queues {
		c.inbox.drainMu[index].Lock()
		removed, skipped, err := queue.DiscardMatching(ctx, match)
		c.inbox.drainMu[index].Unlock()
		n.Inbox += removed
		n.InboxSkipped += skipped
		if err != nil {
			return n, err
		}
	}
	if n.InboxSkipped > 0 {
		n.Warnings = append(n.Warnings, "MQTT inbox 中不属于独占测试设备或身份无法确认的记录已保留")
	}
	return n, nil
}

func capacityTopicIdentity(topic string) (tenant, product, device string, known bool) {
	if tenant, product, device, _, err := StandardTopic(topic); err == nil {
		return tenant, product, device, true
	}
	var raw model.RawMessage
	if applyRawTopicIdentity(topic, &raw) == nil {
		return raw.TenantID, raw.ProductID, raw.DeviceID, true
	}
	var state model.DeviceState
	if applyStateTopicIdentity(topic, &state) == nil {
		return state.TenantID, state.ProductID, state.DeviceID, true
	}
	return "", "", "", false
}
