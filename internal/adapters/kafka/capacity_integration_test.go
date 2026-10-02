package kafkaadapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"iot-platform/internal/ports"
)

// Only this fixture topic/group is read or changed. Normal tests do not connect
// to infrastructure; opt in with the existing IOT_TEST_DISPOSABLE_KAFKA setting.
func TestCapacityQueueIsolatedKafkaPrefix(t *testing.T) {
	broker := os.Getenv("IOT_TEST_DISPOSABLE_KAFKA")
	if broker == "" {
		t.Skip("IOT_TEST_DISPOSABLE_KAFKA is not configured")
	}
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(random[:]))
	topic, group := "iot.capacity-cleanup-test-"+suffix, "iot-capacity-cleanup-test-"+suffix
	if !strings.HasPrefix(topic, "iot.capacity-cleanup-test-") || !strings.HasPrefix(group, "iot-capacity-cleanup-test-") {
		t.Fatal("unsafe fixture names")
	}
	brokers := strings.Split(broker, ",")
	transport := &kafka.Transport{MetadataTopics: []string{topic}, MetadataTTL: time.Second}
	defer transport.CloseIdleConnections()
	client := &kafka.Client{Addr: kafka.TCP(brokers...), Timeout: 10 * time.Second, Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	created, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: []kafka.TopicConfig{{Topic: topic, NumPartitions: 2, ReplicationFactor: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := created.Errors[topic]; err != nil {
		t.Fatal(err)
	}
	groupCreated := false
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if groupCreated {
			result, err := client.DeleteGroups(cleanup, &kafka.DeleteGroupsRequest{GroupIDs: []string{group}})
			if err != nil || result.Errors[group] != nil {
				t.Errorf("synthetic group cleanup failed: %v", err)
			}
		}
		result, err := client.DeleteTopics(cleanup, &kafka.DeleteTopicsRequest{Topics: []string{topic}})
		if err != nil || result.Errors[topic] != nil {
			t.Errorf("synthetic topic cleanup failed: %v", err)
		}
	})
	for part, bodies := range [][]([]byte){
		{capacityBody("t", "p", "fixture"), capacityBody("t", "p", "fixture"), capacityBody("t", "p", "shared"), capacityBody("t", "p", "fixture")},
		{capacityBody("t", "p", "fixture"), capacityBody("t", "p", "fixture")},
	} {
		records := make([]kafka.Record, 0, len(bodies))
		for _, body := range bodies {
			records = append(records, kafka.Record{Key: kafka.NewBytes([]byte("known-fixture")), Value: kafka.NewBytes(body)})
		}
		result, err := client.Produce(ctx, &kafka.ProduceRequest{Topic: topic, Partition: part, RequiredAcks: kafka.RequireAll, Records: kafka.NewRecordReader(records...)})
		if err != nil {
			t.Fatal(err)
		}
		if result.Error != nil {
			t.Fatal(result.Error)
		}
	}
	backend := &isolatedCapacityBroker{kafkaCapacityQueueBackend: &kafkaCapacityQueueBackend{client: client, addr: brokers}, topic: topic, group: group}
	committed, err := client.OffsetCommit(ctx, &kafka.OffsetCommitRequest{GroupID: group, GenerationID: -1, Topics: map[string][]kafka.OffsetCommit{topic: {{Partition: 0, Offset: 0}, {Partition: 1, Offset: 0}}}})
	if err != nil {
		t.Fatal(err)
	}
	groupCreated = true
	for _, p := range committed.Topics[topic] {
		if p.Error != nil {
			t.Fatal(p.Error)
		}
	}
	backend.withGroup = true
	blocked, err := previewCapacityQueue(ctx, backend, "t", capacityBatch())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range blocked.Partitions {
		if p.DeleteBefore != 0 {
			t.Fatal("unknown native group did not preserve queue")
		}
	}
	removedGroup, err := client.DeleteGroups(ctx, &kafka.DeleteGroupsRequest{GroupIDs: []string{group}})
	if err != nil {
		t.Fatal(err)
	}
	if removedGroup.Errors[group] != nil {
		t.Fatal(removedGroup.Errors[group])
	}
	groupCreated = false
	backend.withGroup = false
	plan, err := previewCapacityQueue(ctx, backend, "t", capacityBatch())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Partitions) != 2 || plan.Partitions[0].DeleteBefore != 2 || plan.Partitions[1].DeleteBefore != 2 {
		t.Fatal("wrong native fixture prefix", plan.Partitions)
	}
	counts, err := cleanupCapacityQueue(ctx, backend, "t", capacityBatch(), plan)
	if err != nil || counts.QueueOffsetSpan != 4 || counts.QueueSkippedPartitions != 1 {
		t.Fatal("native DeleteRecords failed", counts, err)
	}
	after, err := backend.snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.partitions[partitionKey{topic, 0}].Start != 2 || after.partitions[partitionKey{topic, 0}].End != 4 || after.partitions[partitionKey{topic, 1}].Start != 2 {
		t.Fatal("unexpected low/high watermarks")
	}
	// Fetch the protected record at the new low watermark; a whole-topic purge
	// or an exclusive/inclusive off-by-one would lose this exact identity.
	_, match, _, err := capacityQueueScope("t", capacityBatch())
	if err != nil {
		t.Fatal(err)
	}
	protected := after.partitions[partitionKey{topic, 0}]
	cut, reason, err := backend.identityPrefix(ctx, protected, func(body []byte) bool { return match(topic, body) })
	if err != nil || cut != 2 || reason == "" {
		t.Fatal("protected record was lost", cut, reason, err)
	}
	t.Log("isolated native Kafka: unknown group blocked; p0 fixture prefix 0..1 removed, protected p0 offset2 retained; pure p1 removed; fixture topic/group cleanup registered")
}

type isolatedCapacityBroker struct {
	*kafkaCapacityQueueBackend
	topic, group string
	withGroup    bool
}

func (b *isolatedCapacityBroker) snapshot(ctx context.Context) (capacityQueueSnapshot, error) {
	snapshot := capacityQueueSnapshot{partitions: map[partitionKey]ports.CapacityQueuePartition{}, groups: map[string]capacityQueueGroup{}}
	offsets, err := b.client.ListOffsets(ctx, &kafka.ListOffsetsRequest{Topics: map[string][]kafka.OffsetRequest{b.topic: {kafka.FirstOffsetOf(0), kafka.LastOffsetOf(0), kafka.FirstOffsetOf(1), kafka.LastOffsetOf(1)}}})
	if err != nil {
		return snapshot, err
	}
	for _, p := range offsets.Topics[b.topic] {
		if p.Error != nil {
			return snapshot, p.Error
		}
		key := partitionKey{b.topic, p.Partition}
		part, ok := snapshot.partitions[key]
		if !ok {
			part = ports.CapacityQueuePartition{Topic: b.topic, Partition: p.Partition, Start: -1, End: -1}
		}
		if p.FirstOffset >= 0 {
			part.Start = p.FirstOffset
		}
		if p.LastOffset >= 0 {
			part.End = p.LastOffset
		}
		snapshot.partitions[key] = part
	}
	if len(snapshot.partitions) != 2 {
		return snapshot, errors.New("isolated fixture metadata incomplete")
	}
	if b.withGroup {
		described, err := b.client.DescribeGroups(ctx, &kafka.DescribeGroupsRequest{GroupIDs: []string{b.group}})
		if err != nil {
			return snapshot, err
		}
		if len(described.Groups) != 1 || described.Groups[0].Error != nil {
			return snapshot, errors.New("isolated group metadata incomplete")
		}
		g := capacityQueueGroup{state: described.Groups[0].GroupState, members: len(described.Groups[0].Members), topics: map[string]bool{b.topic: true}, commits: map[partitionKey]int64{}}
		commits, err := b.client.OffsetFetch(ctx, &kafka.OffsetFetchRequest{GroupID: b.group, Topics: map[string][]int{b.topic: {0, 1}}})
		if err != nil {
			return snapshot, err
		}
		if commits.Error != nil {
			return snapshot, commits.Error
		}
		for _, p := range commits.Topics[b.topic] {
			if p.Error != nil {
				return snapshot, p.Error
			}
			g.commits[partitionKey{b.topic, p.Partition}] = p.CommittedOffset
		}
		snapshot.groups[b.group] = g
	}
	return snapshot, nil
}
