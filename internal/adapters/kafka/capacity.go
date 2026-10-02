package kafkaadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type capacityQueueGroup struct {
	state   string
	members int
	topics  map[string]bool
	commits map[partitionKey]int64
}
type capacityQueueSnapshot struct {
	partitions map[partitionKey]ports.CapacityQueuePartition
	groups     map[string]capacityQueueGroup
}
type capacityQueueBackend interface {
	snapshot(context.Context) (capacityQueueSnapshot, error)
	identityPrefix(context.Context, ports.CapacityQueuePartition, func([]byte) bool) (int64, string, error)
	deletePrefix(context.Context, ports.CapacityQueuePartition) error
}

var capacityKnownGroups = map[string][]string{
	"iot-platform-parser": {model.TopicRaw}, "iot-platform-processor": {model.TopicDeviceBusiness},
	"iot-platform-state": {model.TopicDeviceState}, "iot-platform-device-alarm-notifications": {model.TopicAlarmReported},
	// These older external storage/AI consumers can remain registered after a
	// restore. They are inspected; their offsets are never reset or sought.
	"iot-platform-storage": {model.TopicPropertyReport, model.TopicEventReport, model.TopicParsed},
	"iot-platform-ai":      {model.TopicAlarmRaised},
}

func capacityQueueScope(tenant string, q model.CapacityCleanupBatch) (string, func(string, []byte) bool, bool, error) {
	if tenant == "" || q.Product == "" {
		return "", nil, false, errors.New("capacity queue cleanup requires exact tenant and product")
	}
	manifest := map[string]bool{}
	for _, id := range q.Devices {
		manifest[id] = true
	}
	remove := map[string]bool{}
	for _, id := range q.RemoveDevices {
		if id == "" || !manifest[id] {
			return "", nil, false, errors.New("queue cleanup device is outside exclusive manifest")
		}
		remove[id] = true
	}
	wholeProduct := q.Historical && q.RemoveProduct && len(q.Devices) == 0 && len(q.RemoveDevices) == 0
	ids := make([]string, 0, len(remove))
	for id := range remove {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	scope, _ := json.Marshal(struct {
		Tenant, Product                         string
		Devices                                 []string
		Historical, RemoveProduct, WholeProduct bool
	}{tenant, q.Product, ids, q.Historical, q.RemoveProduct, wholeProduct})
	hash := sha256.Sum256(scope)
	match := func(topic string, payload []byte) bool {
		var identity struct {
			Tenant  string `json:"tenantId"`
			Product string `json:"productId"`
			Device  string `json:"deviceId"`
			Details struct {
				Message struct {
					Product string `json:"productId"`
				} `json:"message"`
			} `json:"details"`
			Payload     json.RawMessage `json:"payload"`
			SourceTopic string          `json:"sourceTopic"`
		}
		if len(payload) > 1<<20 || json.Unmarshal(payload, &identity) != nil {
			return false
		}
		// Only the formal dead-letter wrapper may supply a nested identity.
		if strings.HasPrefix(topic, "iot.dlq.") {
			if !slices.Contains(capacityQueueTopics(), identity.SourceTopic) || len(identity.Payload) == 0 {
				return false
			}
			var nested struct {
				Tenant  string `json:"tenantId"`
				Product string `json:"productId"`
				Device  string `json:"deviceId"`
				Details struct {
					Message struct {
						Product string `json:"productId"`
					} `json:"message"`
				} `json:"details"`
			}
			if json.Unmarshal(identity.Payload, &nested) != nil {
				return false
			}
			identity.Tenant, identity.Product, identity.Device = nested.Tenant, nested.Product, nested.Device
			identity.Details.Message.Product = nested.Details.Message.Product
		}
		if identity.Product == "" {
			identity.Product = identity.Details.Message.Product
		}
		return identity.Tenant == tenant && identity.Product == q.Product && identity.Device != "" && (wholeProduct || remove[identity.Device])
	}
	return hex.EncodeToString(hash[:]), match, wholeProduct || len(remove) > 0, nil
}

func (b *Bus) PreviewCapacityQueue(ctx context.Context, tenant string, q model.CapacityCleanupBatch) (ports.CapacityQueuePlan, error) {
	return previewCapacityQueue(ctx, newCapacityQueueBackend(b.brokers, b.transport), tenant, q)
}

func previewCapacityQueue(ctx context.Context, backend capacityQueueBackend, tenant string, q model.CapacityCleanupBatch) (ports.CapacityQueuePlan, error) {
	var plan ports.CapacityQueuePlan
	hash, match, scoped, err := capacityQueueScope(tenant, q)
	if err != nil {
		return plan, err
	}
	plan.ScopeHash = hash
	snapshot, err := backend.snapshot(ctx)
	if err != nil {
		return plan, err
	}
	unsafeGroups := false
	for name, group := range snapshot.groups {
		plan.Groups = append(plan.Groups, name)
		if _, known := capacityKnownGroups[name]; !known || (group.state != "Stable" && group.state != "Empty") {
			unsafeGroups = true
		}
		for topic := range group.topics {
			if !slices.Contains(capacityKnownGroups[name], topic) {
				unsafeGroups = true
			}
		}
	}
	sort.Strings(plan.Groups)
	keys := make([]partitionKey, 0, len(snapshot.partitions))
	for key := range snapshot.partitions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].topic != keys[j].topic {
			return keys[i].topic < keys[j].topic
		}
		return keys[i].partition < keys[j].partition
	})
	for _, key := range keys {
		part := snapshot.partitions[key]
		part.DeleteBefore = part.Start
		if part.Start < 0 || part.End < part.Start {
			return plan, errors.New("invalid live Kafka partition bounds")
		}
		if !scoped || unsafeGroups {
			if part.End > part.Start {
				part.Reason = "没有独占清理范围，或存在未知/未稳定消费组，已保留"
			}
			plan.Partitions = append(plan.Partitions, part)
			continue
		}
		consumed, hasConsumer, active := part.End, false, false
		for name, group := range snapshot.groups {
			related := group.topics[key.topic] || slices.Contains(capacityKnownGroups[name], key.topic)
			if !related {
				continue
			}
			hasConsumer = true
			commit, ok := group.commits[key]
			if !ok || commit < part.Start {
				commit = part.Start
			}
			if commit > part.End {
				return plan, errors.New("Kafka committed offset is beyond current high watermark")
			}
			consumed = min(consumed, commit)
			active = active || group.state != "Empty" || group.members != 0
		}
		// Commits grant no ownership of normal business records. Audit from
		// the actual low watermark every time. Active groups only constrain
		// the upper bound to their minimum committed offset; Empty groups may
		// also remove unconsumed fixtures, without skipping protected records.
		limit := part.End
		if hasConsumer && active {
			limit = consumed
		}
		if limit > part.Start {
			scan := part
			scan.End = limit
			cut, reason, err := backend.identityPrefix(ctx, scan, func(payload []byte) bool { return match(part.Topic, payload) })
			if err != nil {
				return plan, err
			}
			if cut < part.Start || cut > limit {
				return plan, errors.New("Kafka identity prefix is outside frozen bounds")
			}
			part.DeleteBefore, part.Reason = cut, reason
		}
		if part.Reason == "" && limit < part.End {
			part.Reason = "活跃消费组的未消费或正在处理记录已保留"
		}
		plan.Partitions = append(plan.Partitions, part)
	}
	for _, part := range plan.Partitions {
		if part.Reason != "" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s 分区 %d：%s", part.Topic, part.Partition, part.Reason))
		}
	}
	return plan, nil
}

func (b *Bus) CleanupCapacityQueue(ctx context.Context, tenant string, q model.CapacityCleanupBatch, plan ports.CapacityQueuePlan) (ports.RuntimeCleanupCounts, error) {
	return cleanupCapacityQueue(ctx, newCapacityQueueBackend(b.brokers, b.transport), tenant, q, plan)
}

func cleanupCapacityQueue(ctx context.Context, backend capacityQueueBackend, tenant string, q model.CapacityCleanupBatch, plan ports.CapacityQueuePlan) (ports.RuntimeCleanupCounts, error) {
	var counts ports.RuntimeCleanupCounts
	hash, _, _, err := capacityQueueScope(tenant, q)
	if err != nil {
		return counts, err
	}
	if hash != plan.ScopeHash {
		return counts, errors.New("Kafka cleanup fixture scope changed")
	}
	// Re-read every group/commit and re-audit identity prefixes. A client-supplied
	// plan can never grant a larger deletion than the live authorized scope.
	fresh, err := previewCapacityQueue(ctx, backend, tenant, q)
	if err != nil {
		return counts, err
	}
	if !slices.Equal(plan.Groups, fresh.Groups) {
		return counts, errors.New("Kafka consumer group inventory changed; cleanup refused")
	}
	live := map[partitionKey]ports.CapacityQueuePartition{}
	for _, part := range fresh.Partitions {
		live[partitionKey{part.Topic, part.Partition}] = part
	}
	seen := map[partitionKey]bool{}
	for _, part := range plan.Partitions {
		key := partitionKey{part.Topic, part.Partition}
		current, exists := live[key]
		if !exists || seen[key] || part.DeleteBefore < part.Start || part.DeleteBefore > part.End {
			return counts, errors.New("Kafka frozen partition plan is invalid")
		}
		seen[key] = true
		if part.DeleteBefore > current.DeleteBefore || part.DeleteBefore > current.End {
			return counts, errors.New("Kafka live prefix no longer permits frozen deletion")
		}
	}
	if len(seen) != len(live) {
		return counts, errors.New("Kafka partition inventory changed")
	}
	counts.Warnings = append(counts.Warnings, fresh.Warnings...)
	for _, part := range plan.Partitions {
		current := live[partitionKey{part.Topic, part.Partition}]
		if part.DeleteBefore < current.End {
			counts.QueueSkippedPartitions++
		}
		if part.DeleteBefore <= current.Start {
			continue
		}
		part.Start = current.Start
		if err := backend.deletePrefix(ctx, part); err != nil {
			return counts, err
		}
		counts.QueueOffsetSpan += part.DeleteBefore - current.Start
	}
	return counts, nil
}

func capacityQueueTopics() []string {
	set := map[string]bool{}
	for _, topic := range model.AllTopics() {
		set[topic] = true
	}
	for _, group := range []string{"storage", "ai"} {
		set[model.DLQTopic(group)] = true
	}
	result := make([]string, 0, len(set))
	for topic := range set {
		if strings.HasPrefix(topic, "iot.") {
			result = append(result, topic)
		}
	}
	sort.Strings(result)
	return result
}
