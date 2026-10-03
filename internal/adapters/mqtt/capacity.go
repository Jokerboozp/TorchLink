package mqttadapter

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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
