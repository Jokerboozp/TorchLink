package kafkaadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

// subscription records a consumer group so its backlog can be measured.
type subscription struct {
	topic, group string
}

// ConsumerLag returns, per consumer group, the messages published to its
// topics but not yet committed by the group. It is the backlog to watch when
// judging whether an ingest rate is sustainable; a group that has not
// committed yet counts from the earliest retained offset, where it starts.
func (b *Bus) ConsumerLag(ctx context.Context) (map[string]int64, error) {
	b.mu.Lock()
	subscriptions := append([]subscription(nil), b.subscriptions...)
	b.mu.Unlock()
	client := &kafka.Client{Addr: kafka.TCP(b.brokers...), Timeout: 10 * time.Second}
	lags := map[string]int64{}
	for _, s := range subscriptions {
		lag, err := topicLag(ctx, client, s)
		if err != nil {
			return nil, fmt.Errorf("consumer lag of %s on %s: %w", s.group, s.topic, err)
		}
		lags[s.group] += lag
	}
	return lags, nil
}

func topicLag(ctx context.Context, client *kafka.Client, s subscription) (int64, error) {
	meta, err := client.Metadata(ctx, &kafka.MetadataRequest{Topics: []string{s.topic}})
	if err != nil {
		return 0, err
	}
	var partitions []int
	for _, topic := range meta.Topics {
		if topic.Name != s.topic {
			continue
		}
		if topic.Error != nil {
			return 0, topic.Error
		}
		for _, partition := range topic.Partitions {
			partitions = append(partitions, partition.ID)
		}
	}
	if len(partitions) == 0 {
		return 0, nil
	}
	requests := make([]kafka.OffsetRequest, 0, 2*len(partitions))
	for _, partition := range partitions {
		requests = append(requests, kafka.FirstOffsetOf(partition), kafka.LastOffsetOf(partition))
	}
	offsets, err := client.ListOffsets(ctx, &kafka.ListOffsetsRequest{Topics: map[string][]kafka.OffsetRequest{s.topic: requests}})
	if err != nil {
		return 0, err
	}
	committed, err := client.OffsetFetch(ctx, &kafka.OffsetFetchRequest{GroupID: s.group, Topics: map[string][]int{s.topic: partitions}})
	if err != nil {
		return 0, err
	}
	if committed.Error != nil {
		return 0, committed.Error
	}
	done := map[int]int64{}
	for _, partition := range committed.Topics[s.topic] {
		if partition.Error != nil {
			return 0, partition.Error
		}
		done[partition.Partition] = partition.CommittedOffset
	}
	// The first and last offsets of a partition may come back as separate entries.
	type bounds struct{ first, last int64 }
	ranges := map[int]*bounds{}
	for _, partition := range offsets.Topics[s.topic] {
		if partition.Error != nil {
			return 0, partition.Error
		}
		r := ranges[partition.Partition]
		if r == nil {
			r = &bounds{first: -1, last: -1}
			ranges[partition.Partition] = r
		}
		if partition.FirstOffset >= 0 && (r.first < 0 || partition.FirstOffset < r.first) {
			r.first = partition.FirstOffset
		}
		if partition.LastOffset > r.last {
			r.last = partition.LastOffset
		}
	}
	var lag int64
	for id, r := range ranges {
		position, ok := done[id]
		if !ok || position < r.first {
			position = max(r.first, 0)
		}
		if r.last > position {
			lag += r.last - position
		}
	}
	return lag, nil
}

// GroupLag returns the total backlog of the given consumer group on the given
// topics, whether or not this process consumes them; an ingest-only process
// uses it to see the backlog the processing replicas face.
func (b *Bus) GroupLag(ctx context.Context, group string, topics ...string) (int64, error) {
	client := &kafka.Client{Addr: kafka.TCP(b.brokers...), Timeout: 10 * time.Second}
	var total int64
	for _, topic := range topics {
		lag, err := topicLag(ctx, client, subscription{topic: topic, group: "iot-platform-" + group})
		if err != nil {
			return 0, fmt.Errorf("consumer lag of %s on %s: %w", group, topic, err)
		}
		total += lag
	}
	return total, nil
}
