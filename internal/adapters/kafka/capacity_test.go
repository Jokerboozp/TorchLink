package kafkaadapter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type capacityRecord struct {
	offset int64
	body   []byte
}
type fakeCapacityBroker struct {
	state   capacityQueueSnapshot
	records map[partitionKey][]capacityRecord
	deleted []ports.CapacityQueuePartition
	scanned []partitionKey
}

func (f *fakeCapacityBroker) snapshot(context.Context) (capacityQueueSnapshot, error) {
	return f.state, nil
}
func (f *fakeCapacityBroker) identityPrefix(_ context.Context, p ports.CapacityQueuePartition, match func([]byte) bool) (int64, string, error) {
	key := partitionKey{p.Topic, p.Partition}
	f.scanned = append(f.scanned, key)
	cut := p.Start
	for _, r := range f.records[key] {
		if r.offset < p.Start || r.offset >= p.End {
			continue
		}
		if !match(r.body) {
			return cut, "mixed/unknown identity retained", nil
		}
		cut = r.offset + 1
	}
	return cut, "", nil
}
func (f *fakeCapacityBroker) deletePrefix(_ context.Context, p ports.CapacityQueuePartition) error {
	f.deleted = append(f.deleted, p)
	key := partitionKey{p.Topic, p.Partition}
	part := f.state.partitions[key]
	part.Start = p.DeleteBefore
	f.state.partitions[key] = part
	return nil
}
func capacityBody(t, p, d string) []byte {
	b, _ := json.Marshal(map[string]string{"tenantId": t, "productId": p, "deviceId": d})
	return b
}
func newFakeCapacityBroker(topic string, bodies ...[]byte) *fakeCapacityBroker {
	key := partitionKey{topic, 0}
	f := &fakeCapacityBroker{state: capacityQueueSnapshot{partitions: map[partitionKey]ports.CapacityQueuePartition{key: {Topic: topic, Partition: 0, Start: 0, End: int64(len(bodies))}}, groups: map[string]capacityQueueGroup{}}, records: map[partitionKey][]capacityRecord{}}
	for i, b := range bodies {
		f.records[key] = append(f.records[key], capacityRecord{int64(i), b})
	}
	return f
}
func capacityBatch() model.CapacityCleanupBatch {
	return model.CapacityCleanupBatch{Product: "p", Devices: []string{"fixture", "shared"}, RemoveDevices: []string{"fixture"}}
}

func TestCapacityQueueStopsAtFirstSharedOrMissingIdentity(t *testing.T) {
	for _, topic := range []string{model.TopicAlarmConfirmed, model.TopicAlarmRecovered, model.TopicAlarmAIAnalysis} {
		f := newFakeCapacityBroker(topic, capacityBody("t", "p", "fixture"), capacityBody("t", "p", "shared"), capacityBody("t", "p", "fixture"))
		plan, err := previewCapacityQueue(context.Background(), f, "t", capacityBatch())
		if err != nil || plan.Partitions[0].DeleteBefore != 1 || len(plan.Warnings) == 0 {
			t.Fatal(plan, err)
		}
		n, err := cleanupCapacityQueue(context.Background(), f, "t", capacityBatch(), plan)
		if err != nil || n.QueueOffsetSpan != 1 || n.QueueSkippedPartitions != 1 || len(f.deleted) != 1 {
			t.Fatal(n, err)
		}
	}
	f := newFakeCapacityBroker(model.TopicAlarmAIAnalysis, []byte(`{"tenantId":"t","alarmId":"only-alarm-id"}`), capacityBody("t", "p", "fixture"))
	plan, err := previewCapacityQueue(context.Background(), f, "t", capacityBatch())
	if err != nil || plan.Partitions[0].DeleteBefore != 0 {
		t.Fatal("alarm-only result was guessed", plan, err)
	}
}

func TestCapacityQueueUsesAllConsumedOffsetsAndRejectsUnknownConsumers(t *testing.T) {
	f := newFakeCapacityBroker(model.TopicRaw, capacityBody("t", "p", "fixture"), capacityBody("t", "p", "fixture"), capacityBody("t", "p", "fixture"))
	key := partitionKey{model.TopicRaw, 0}
	f.state.groups["iot-platform-parser"] = capacityQueueGroup{state: "Stable", members: 1, topics: map[string]bool{model.TopicRaw: true}, commits: map[partitionKey]int64{key: 2}}
	plan, err := previewCapacityQueue(context.Background(), f, "t", capacityBatch())
	if err != nil || plan.Partitions[0].DeleteBefore != 2 || len(f.scanned) != 1 {
		t.Fatal("active unconsumed tail scanned/deleted", plan, err)
	}
	f.state.groups["iot-platform-parser"] = capacityQueueGroup{state: "Stable", members: 1, topics: map[string]bool{model.TopicRaw: true}, commits: map[partitionKey]int64{key: 1}}
	if _, err := cleanupCapacityQueue(context.Background(), f, "t", capacityBatch(), plan); err == nil || len(f.deleted) != 0 {
		t.Fatal("seek-back did not prevent deletion", err)
	}
	f.state.groups["new-no-commit"] = capacityQueueGroup{state: "Empty", topics: map[string]bool{}, commits: map[partitionKey]int64{}}
	plan, err = previewCapacityQueue(context.Background(), f, "t", capacityBatch())
	if err != nil || plan.Partitions[0].DeleteBefore != 0 || len(plan.Warnings) == 0 {
		t.Fatal("unknown consumer did not preserve queue", plan, err)
	}
	delete(f.state.groups, "new-no-commit")
	if _, err := cleanupCapacityQueue(context.Background(), f, "t", capacityBatch(), plan); err == nil {
		t.Fatal("changed group inventory accepted")
	}
}

func TestCapacityQueueNeverSkipsConsumedProtectedBusinessPrefix(t *testing.T) {
	for _, state := range []string{"Stable", "Empty"} {
		f := newFakeCapacityBroker(model.TopicRaw, capacityBody("real-tenant", "real-product", "current"), capacityBody("t", "p", "fixture"))
		key := partitionKey{model.TopicRaw, 0}
		members := 0
		if state == "Stable" {
			members = 1
		}
		f.state.groups["iot-platform-parser"] = capacityQueueGroup{state: state, members: members, topics: map[string]bool{model.TopicRaw: true}, commits: map[partitionKey]int64{key: 2}}
		plan, err := previewCapacityQueue(context.Background(), f, "t", capacityBatch())
		if err != nil || plan.Partitions[0].DeleteBefore != 0 || len(plan.Warnings) == 0 {
			t.Fatal("consumed normal prefix was skipped", state, plan, err)
		}
		n, err := cleanupCapacityQueue(context.Background(), f, "t", capacityBatch(), plan)
		if err != nil || n.QueueOffsetSpan != 0 || len(f.deleted) != 0 {
			t.Fatal("consumed normal records were deleted", state, n, err)
		}
	}
}

func TestCapacityQueueFinalHistoricalProductAndDeadLetterIdentity(t *testing.T) {
	final := model.CapacityCleanupBatch{Product: "p", Historical: true, RemoveProduct: true}
	f := newFakeCapacityBroker(model.TopicAlarmRecovered, capacityBody("t", "p", "previously-removed"), capacityBody("t", "p", "other-batch"), capacityBody("t", "real", "current"))
	plan, err := previewCapacityQueue(context.Background(), f, "t", final)
	if err != nil || plan.Partitions[0].DeleteBefore != 2 {
		t.Fatal(plan, err)
	}
	if _, err := cleanupCapacityQueue(context.Background(), f, "t", capacityBatch(), plan); err == nil {
		t.Fatal("scope expansion/hash mismatch accepted")
	}
	_, match, _, err := capacityQueueScope("t", capacityBatch())
	if err != nil {
		t.Fatal(err)
	}
	dlq := []byte(`{"sourceTopic":"iot.property.report","payload":{"tenantId":"t","productId":"p","deviceId":"fixture"}}`)
	if !match("iot.dlq.storage", dlq) || match(model.TopicPropertyReport, dlq) {
		t.Fatal("dead-letter wrapper identity not constrained to DLQ")
	}
	if match("iot.dlq.storage", []byte(strings.ReplaceAll(string(dlq), `"t"`, `"other"`))) {
		t.Fatal("foreign DLQ selected")
	}
	if _, _, _, err := capacityQueueScope("t", model.CapacityCleanupBatch{Product: "p", Devices: []string{"shared"}, RemoveDevices: []string{"fixture"}}); err == nil {
		t.Fatal("outside manifest accepted")
	}
}
