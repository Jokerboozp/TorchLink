// Package deleterecords supplies the Kafka DeleteRecords v0/v1 message omitted
// from kafka-go v0.4.49, using its existing negotiated transport and codec.
// Contract: https://github.com/apache/kafka/blob/3.8/clients/src/main/resources/common/message/DeleteRecordsRequest.json
package deleterecords

import (
	"errors"
	"github.com/segmentio/kafka-go/protocol"
)

func init() { protocol.Register(&Request{}, &Response{}) }

type Request struct {
	Topics    []RequestTopic `kafka:"min=v0,max=v1"`
	TimeoutMs int32          `kafka:"min=v0,max=v1"`
}
type RequestTopic struct {
	Name       string             `kafka:"min=v0,max=v1"`
	Partitions []RequestPartition `kafka:"min=v0,max=v1"`
}
type RequestPartition struct {
	Partition int32 `kafka:"min=v0,max=v1"`
	Offset    int64 `kafka:"min=v0,max=v1"`
}

func (*Request) ApiKey() protocol.ApiKey { return protocol.DeleteRecords }
func (r *Request) Broker(cluster protocol.Cluster) (protocol.Broker, error) {
	if len(r.Topics) != 1 || len(r.Topics[0].Partitions) != 1 {
		return protocol.Broker{}, errors.New("DeleteRecords requires one exact topic partition")
	}
	topic, ok := cluster.Topics[r.Topics[0].Name]
	if !ok {
		return protocol.Broker{}, errors.New("DeleteRecords topic is unavailable")
	}
	partition, ok := topic.Partitions[r.Topics[0].Partitions[0].Partition]
	if !ok {
		return protocol.Broker{}, errors.New("DeleteRecords partition is unavailable")
	}
	broker, ok := cluster.Brokers[partition.Leader]
	if !ok {
		return protocol.Broker{}, errors.New("DeleteRecords leader is unavailable")
	}
	return broker, nil
}

type Response struct {
	ThrottleTimeMs int32           `kafka:"min=v0,max=v1"`
	Topics         []ResponseTopic `kafka:"min=v0,max=v1"`
}

func (*Response) ApiKey() protocol.ApiKey { return protocol.DeleteRecords }

type ResponseTopic struct {
	Name       string              `kafka:"min=v0,max=v1"`
	Partitions []ResponsePartition `kafka:"min=v0,max=v1"`
}
type ResponsePartition struct {
	Partition    int32 `kafka:"min=v0,max=v1"`
	LowWatermark int64 `kafka:"min=v0,max=v1"`
	ErrorCode    int16 `kafka:"min=v0,max=v1"`
}
