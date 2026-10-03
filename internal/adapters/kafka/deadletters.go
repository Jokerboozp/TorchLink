package kafkaadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/protocol"

	"iot-platform/internal/model"
)

// maxDeadLetterPreview bounds the payload text returned by DeadLetters.
const maxDeadLetterPreview = 4096

type partitionRange struct {
	partition   int
	first, last int64
}

func (b *Bus) partitionRanges(ctx context.Context, topic string) ([]partitionRange, error) {
	client := &kafka.Client{Addr: kafka.TCP(b.brokers...), Timeout: 10 * time.Second, Transport: b.transport}
	meta, err := client.Metadata(ctx, &kafka.MetadataRequest{Topics: []string{topic}})
	if err != nil {
		return nil, err
	}
	var partitions []int
	for _, t := range meta.Topics {
		if t.Name != topic {
			continue
		}
		if t.Error != nil {
			if errors.Is(t.Error, kafka.UnknownTopicOrPartition) {
				return nil, nil
			}
			return nil, t.Error
		}
		for _, p := range t.Partitions {
			partitions = append(partitions, p.ID)
		}
	}
	if len(partitions) == 0 {
		return nil, nil
	}
	requests := make([]kafka.OffsetRequest, 0, 2*len(partitions))
	for _, p := range partitions {
		requests = append(requests, kafka.FirstOffsetOf(p), kafka.LastOffsetOf(p))
	}
	offsets, err := client.ListOffsets(ctx, &kafka.ListOffsetsRequest{Topics: map[string][]kafka.OffsetRequest{topic: requests}})
	if err != nil {
		return nil, err
	}
	ranges := map[int]*partitionRange{}
	for _, p := range offsets.Topics[topic] {
		if p.Error != nil {
			return nil, p.Error
		}
		r := ranges[p.Partition]
		if r == nil {
			r = &partitionRange{partition: p.Partition, first: -1, last: -1}
			ranges[p.Partition] = r
		}
		if p.FirstOffset >= 0 {
			r.first = p.FirstOffset
		}
		if p.LastOffset >= 0 {
			r.last = p.LastOffset
		}
	}
	out := []partitionRange{}
	for _, r := range ranges {
		if r.first >= 0 && r.last >= r.first {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].partition < out[j].partition })
	return out, nil
}

func (b *Bus) readPartition(ctx context.Context, topic string, partition int, from, to int64) ([]kafka.Message, error) {
	if from >= to {
		return nil, nil
	}
	client := &kafka.Client{Addr: kafka.TCP(b.brokers...), Timeout: 15 * time.Second, Transport: b.transport}
	out := []kafka.Message{}
	for offset := from; offset < to; {
		response, err := client.Fetch(ctx, &kafka.FetchRequest{Topic: topic, Partition: partition, Offset: offset, MinBytes: 1, MaxBytes: 10 << 20, MaxWait: 500 * time.Millisecond})
		if err != nil {
			return out, err
		}
		if response.Error != nil {
			return out, response.Error
		}
		progressed := false
		for offset < to {
			record, err := response.Records.ReadRecord()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return out, err
			}
			if record.Offset < offset {
				continue // batches may start before the requested offset
			}
			key, _ := protocol.ReadAll(record.Key)
			value, _ := protocol.ReadAll(record.Value)
			out = append(out, kafka.Message{Topic: topic, Partition: partition, Offset: record.Offset, Time: record.Time, Key: key, Value: value})
			offset = record.Offset + 1
			progressed = true
		}
		if !progressed {
			return out, fmt.Errorf("no records at %s/%d offset %d", topic, partition, offset)
		}
	}
	return out, nil
}

func decodeDeadLetter(group string, m kafka.Message) model.DeadLetter {
	var body struct {
		SourceTopic     string          `json:"sourceTopic"`
		Error           string          `json:"error"`
		RetryCount      int             `json:"retryCount"`
		Payload         json.RawMessage `json:"payload"`
		PayloadEncoding string          `json:"payloadEncoding"`
	}
	item := model.DeadLetter{Group: group, Partition: m.Partition, Offset: m.Offset, Time: m.Time, Key: string(m.Key)}
	if json.Unmarshal(m.Value, &body) != nil {
		item.Error = "死信内容无法解析"
		return item
	}
	item.SourceTopic, item.Error, item.RetryCount, item.PayloadEncoding = body.SourceTopic, body.Error, body.RetryCount, body.PayloadEncoding
	payload := string(body.Payload)
	if body.PayloadEncoding == "base64" {
		_ = json.Unmarshal(body.Payload, &payload)
	}
	if len(payload) > maxDeadLetterPreview {
		payload, item.Truncated = payload[:maxDeadLetterPreview], true
	}
	item.Payload = payload
	return item
}

// DeadLetters returns how many dead letters the group's topic retains and
// the newest of them, at most limit.
func (b *Bus) DeadLetters(ctx context.Context, group string, limit int) (int64, []model.DeadLetter, error) {
	if _, ok := model.DeadLetterGroups[group]; !ok {
		return 0, nil, fmt.Errorf("unknown dead-letter group %q", group)
	}
	return b.deadLetters(ctx, group, model.DLQTopic(group), limit)
}

func (b *Bus) deadLetters(ctx context.Context, group, topic string, limit int) (int64, []model.DeadLetter, error) {
	limit = min(max(limit, 1), 200)
	ranges, err := b.partitionRanges(ctx, topic)
	if err != nil {
		return 0, nil, err
	}
	var total int64
	items := []model.DeadLetter{}
	for _, r := range ranges {
		total += r.last - r.first
		messages, err := b.readPartition(ctx, topic, r.partition, max(r.first, r.last-int64(limit)), r.last)
		if err != nil {
			return total, nil, err
		}
		for _, m := range messages {
			items = append(items, decodeDeadLetter(group, m))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
	if len(items) > limit {
		items = items[:limit]
	}
	return total, items, nil
}

// ReplayDeadLetter publishes one dead letter's original payload back to the
// topic its group consumes. Consumers are idempotent per message identity,
// so a repeated replay does not duplicate business effects. The dead letter
// itself is kept.
func (b *Bus) ReplayDeadLetter(ctx context.Context, group string, partition int, offset int64) (model.DeadLetter, error) {
	target, ok := model.DeadLetterGroups[group]
	if !ok {
		return model.DeadLetter{}, fmt.Errorf("unknown dead-letter group %q", group)
	}
	return b.replay(ctx, group, model.DLQTopic(group), target, partition, offset)
}

func (b *Bus) replay(ctx context.Context, group, topic, target string, partition int, offset int64) (model.DeadLetter, error) {
	messages, err := b.readPartition(ctx, topic, partition, offset, offset+1)
	if err != nil || len(messages) == 0 || messages[0].Offset != offset {
		return model.DeadLetter{}, fmt.Errorf("dead letter %d/%d not found: %w", partition, offset, errors.Join(err, model.ErrNotFound))
	}
	m := messages[0]
	var body struct {
		SourceTopic     string          `json:"sourceTopic"`
		Payload         json.RawMessage `json:"payload"`
		PayloadEncoding string          `json:"payloadEncoding"`
	}
	if err = json.Unmarshal(m.Value, &body); err != nil || body.SourceTopic != target {
		return model.DeadLetter{}, fmt.Errorf("dead letter %d/%d does not belong to %s", partition, offset, target)
	}
	payload := []byte(body.Payload)
	if body.PayloadEncoding == "base64" {
		var encoded string
		if err = json.Unmarshal(body.Payload, &encoded); err != nil {
			return model.DeadLetter{}, err
		}
		if payload, err = base64.StdEncoding.DecodeString(encoded); err != nil {
			return model.DeadLetter{}, err
		}
	}
	if err = b.Publish(ctx, target, string(m.Key), payload); err != nil {
		return model.DeadLetter{}, err
	}
	return decodeDeadLetter(group, m), nil
}
