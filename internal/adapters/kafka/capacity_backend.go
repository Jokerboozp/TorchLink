package kafkaadapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/segmentio/kafka-go"
	"iot-platform/internal/adapters/kafka/deleterecords"
	"iot-platform/internal/ports"
)

type kafkaCapacityQueueBackend struct {
	client *kafka.Client
	addr   []string
}

func newCapacityQueueBackend(brokers []string, transports ...*kafka.Transport) capacityQueueBackend {
	client := &kafka.Client{Addr: kafka.TCP(brokers...), Timeout: 10 * time.Second}
	if len(transports) > 0 && transports[0] != nil {
		client.Transport = transports[0]
	}
	return &kafkaCapacityQueueBackend{client: client, addr: brokers}
}

func (b *kafkaCapacityQueueBackend) snapshot(ctx context.Context) (capacityQueueSnapshot, error) {
	out := capacityQueueSnapshot{partitions: map[partitionKey]ports.CapacityQueuePartition{}, groups: map[string]capacityQueueGroup{}}
	if len(b.addr) == 0 {
		return out, errors.New("Kafka capacity cleanup is unavailable")
	}
	meta, err := b.client.Metadata(ctx, &kafka.MetadataRequest{Topics: capacityQueueTopics()})
	if err != nil {
		return out, err
	}
	requests := map[string][]kafka.OffsetRequest{}
	for _, topic := range meta.Topics {
		if errors.Is(topic.Error, kafka.UnknownTopicOrPartition) {
			continue
		}
		if topic.Error != nil {
			return out, topic.Error
		}
		for _, p := range topic.Partitions {
			if p.Error != nil {
				return out, p.Error
			}
			key := partitionKey{topic.Name, p.ID}
			out.partitions[key] = ports.CapacityQueuePartition{Topic: topic.Name, Partition: p.ID, Start: -1, End: -1}
			requests[topic.Name] = append(requests[topic.Name], kafka.FirstOffsetOf(p.ID), kafka.LastOffsetOf(p.ID))
		}
	}
	if len(out.partitions) == 0 {
		return out, errors.New("Kafka capacity topic metadata unavailable")
	}
	offsets, err := b.client.ListOffsets(ctx, &kafka.ListOffsetsRequest{Topics: requests})
	if err != nil {
		return out, err
	}
	for topic, ps := range offsets.Topics {
		for _, p := range ps {
			if p.Error != nil {
				return out, p.Error
			}
			key := partitionKey{topic, p.Partition}
			part, ok := out.partitions[key]
			if !ok {
				return out, errors.New("unexpected Kafka offset metadata")
			}
			if p.FirstOffset >= 0 {
				part.Start = p.FirstOffset
			}
			if p.LastOffset >= 0 {
				part.End = p.LastOffset
			}
			out.partitions[key] = part
		}
	}
	for _, part := range out.partitions {
		if part.Start < 0 || part.End < part.Start {
			return out, errors.New("incomplete Kafka offset metadata")
		}
	}
	groups, err := b.client.ListGroups(ctx, &kafka.ListGroupsRequest{})
	if err != nil {
		return out, err
	}
	if groups.Error != nil {
		return out, groups.Error
	}
	groupIDs := make([]string, 0, len(groups.Groups))
	for _, group := range groups.Groups {
		groupIDs = append(groupIDs, group.GroupID)
	}
	sort.Strings(groupIDs)
	if len(groupIDs) == 0 {
		return out, nil
	}
	details, err := b.client.DescribeGroups(ctx, &kafka.DescribeGroupsRequest{GroupIDs: groupIDs})
	if err != nil {
		return out, err
	}
	for _, group := range details.Groups {
		if group.Error != nil {
			return out, group.Error
		}
		saved := capacityQueueGroup{state: group.GroupState, members: len(group.Members), topics: map[string]bool{}, commits: map[partitionKey]int64{}}
		for _, member := range group.Members {
			for _, topic := range member.MemberMetadata.Topics {
				saved.topics[topic] = true
			}
			for _, assignment := range member.MemberAssignments.Topics {
				saved.topics[assignment.Topic] = true
			}
		}
		// Fetch all existing commits, including unexpected subscriptions. An
		// unknown group with no commit is still retained in the inventory.
		commits, err := b.client.OffsetFetch(ctx, &kafka.OffsetFetchRequest{GroupID: group.GroupID})
		if err != nil {
			return out, err
		}
		if commits.Error != nil {
			return out, commits.Error
		}
		for topic, ps := range commits.Topics {
			for _, p := range ps {
				if p.Error != nil {
					return out, p.Error
				}
				if p.CommittedOffset >= 0 {
					saved.topics[topic] = true
					saved.commits[partitionKey{topic, p.Partition}] = p.CommittedOffset
				}
			}
		}
		out.groups[group.GroupID] = saved
	}
	if len(out.groups) != len(groupIDs) {
		return out, errors.New("incomplete Kafka consumer group metadata")
	}
	return out, nil
}

// identityPrefix streams bounded record batches with no consumer group and no
// commits. A mixed/unknown identity stops at the first protected record; payloads
// are never logged, included in errors or retained in the returned plan.
func (b *kafkaCapacityQueueBackend) identityPrefix(ctx context.Context, part ports.CapacityQueuePartition, match func([]byte) bool) (int64, string, error) {
	cut, records, bytesRead := part.Start, 0, int64(0)
	for cut < part.End {
		if records >= 100000 || bytesRead >= 64<<20 {
			return cut, "身份核对达到读取上限，剩余记录已保留", nil
		}
		fetch, err := b.client.Fetch(ctx, &kafka.FetchRequest{Topic: part.Topic, Partition: part.Partition, Offset: cut,
			MinBytes: 1, MaxBytes: 1 << 20, MaxWait: 250 * time.Millisecond, IsolationLevel: kafka.ReadCommitted})
		if err != nil {
			return cut, "", err
		}
		if fetch.Error != nil {
			if fetch.Records != nil {
				_ = closeCapacityRecords(fetch.Records)
			}
			return cut, "", fetch.Error
		}
		if fetch.Records == nil {
			return cut, "当前前缀不可读取，剩余记录已保留", nil
		}
		before := cut
		for {
			record, err := fetch.Records.ReadRecord()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				_ = closeCapacityRecords(fetch.Records)
				return cut, "", err
			}
			if record.Offset < cut {
				_ = closeCapacityRecord(record)
				continue
			}
			if record.Offset >= part.End {
				_ = closeCapacityRecord(record)
				break
			}
			if record.Value == nil {
				_ = closeCapacityRecord(record)
				_ = closeCapacityRecords(fetch.Records)
				return cut, "记录缺少可核对身份，剩余记录已保留", nil
			}
			payload, err := io.ReadAll(io.LimitReader(record.Value, (1<<20)+1))
			closeErr := closeCapacityRecord(record)
			if err == nil {
				err = closeErr
			}
			if err != nil {
				_ = closeCapacityRecords(fetch.Records)
				return cut, "", err
			}
			records++
			bytesRead += int64(len(payload))
			if len(payload) > 1<<20 || !match(payload) {
				_ = closeCapacityRecords(fetch.Records)
				return cut, "首条混合或缺少身份的有效记录及其后记录已保留", nil
			}
			cut = record.Offset + 1
			if records >= 100000 || bytesRead >= 64<<20 {
				break
			}
		}
		if err := closeCapacityRecords(fetch.Records); err != nil {
			return cut, "", err
		}
		if cut == before {
			return cut, "当前前缀不可完整读取，剩余记录已保留", nil
		}
	}
	return cut, "", nil
}

func (b *kafkaCapacityQueueBackend) deletePrefix(ctx context.Context, part ports.CapacityQueuePartition) error {
	if part.DeleteBefore <= part.Start || part.DeleteBefore > part.End {
		return errors.New("invalid exact DeleteRecords cutoff")
	}
	request := &deleterecords.Request{Topics: []deleterecords.RequestTopic{{Name: part.Topic, Partitions: []deleterecords.RequestPartition{{Partition: int32(part.Partition), Offset: part.DeleteBefore}}}}, TimeoutMs: 10000}
	transport := b.client.Transport
	if transport == nil {
		transport = kafka.DefaultTransport
	}
	response, err := transport.RoundTrip(ctx, kafka.TCP(b.addr...), request)
	if err != nil {
		return err
	}
	result, ok := response.(*deleterecords.Response)
	if !ok || len(result.Topics) != 1 || result.Topics[0].Name != part.Topic || len(result.Topics[0].Partitions) != 1 {
		return errors.New("unexpected DeleteRecords response")
	}
	p := result.Topics[0].Partitions[0]
	if p.Partition != int32(part.Partition) {
		return errors.New("unexpected DeleteRecords partition")
	}
	if p.ErrorCode != 0 {
		return fmt.Errorf("DeleteRecords: %w", kafka.Error(p.ErrorCode))
	}
	if p.LowWatermark != part.DeleteBefore {
		return errors.New("DeleteRecords low watermark was not confirmed")
	}
	return nil
}

func closeCapacityRecord(record *kafka.Record) error {
	if record == nil {
		return nil
	}
	var errs []error
	if record.Key != nil {
		errs = append(errs, record.Key.Close())
	}
	if record.Value != nil {
		errs = append(errs, record.Value.Close())
	}
	return errors.Join(errs...)
}
func closeCapacityRecords(records kafka.RecordReader) error {
	if records == nil {
		return nil
	}
	for {
		record, err := records.ReadRecord()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := closeCapacityRecord(record); err != nil {
			return err
		}
	}
}
