package deleterecords

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/segmentio/kafka-go/protocol"
)

func TestDeleteRecordsWireContractAndExactLeader(t *testing.T) {
	request := &Request{Topics: []RequestTopic{{Name: "t", Partitions: []RequestPartition{{Partition: 4, Offset: 42}}}}, TimeoutMs: 10000}
	response := &Response{Topics: []ResponseTopic{{Name: "t", Partitions: []ResponsePartition{{Partition: 4, LowWatermark: 42}}}}}
	for _, version := range []int16{0, 1} {
		wire, err := protocol.Marshal(version, *request)
		if err != nil {
			t.Fatal(err)
		}
		expected, _ := hex.DecodeString("000000010001740000000100000004000000000000002a00002710")
		if !bytes.Equal(wire, expected) {
			t.Fatalf("v%d invalid DeleteRecords wire: %x", version, wire)
		}
		var buffer bytes.Buffer
		if err := protocol.WriteResponse(&buffer, version, 9, response); err != nil {
			t.Fatal(err)
		}
		id, decoded, err := protocol.ReadResponse(&buffer, protocol.DeleteRecords, version)
		if err != nil || id != 9 || !reflect.DeepEqual(decoded, response) {
			t.Fatal("DeleteRecords response registration/codec failed", decoded, err)
		}
	}
	cluster := protocol.Cluster{Topics: map[string]protocol.Topic{"t": {Partitions: map[int32]protocol.Partition{4: {ID: 4, Leader: 2}}}}, Brokers: map[int32]protocol.Broker{2: {ID: 2, Host: "leader", Port: 9092}}}
	broker, err := request.Broker(cluster)
	if err != nil || broker.ID != 2 {
		t.Fatal("wrong partition leader", broker, err)
	}
	request.Topics[0].Partitions = append(request.Topics[0].Partitions, RequestPartition{Partition: 5, Offset: 42})
	if _, err := request.Broker(cluster); err == nil {
		t.Fatal("multiple non-routed partitions accepted")
	}
}
