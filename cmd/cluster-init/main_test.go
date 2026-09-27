package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"iot-platform/internal/model"
)

func TestTopicPlanNeverShrinksOrReplicatesSilently(t *testing.T) {
	existing := map[string]topicState{
		model.TopicRaw:            {Partitions: 24, Replication: 3},
		model.TopicDeviceBusiness: {Partitions: 12, Replication: 3},
		model.TopicAlarmRaised:    {Partitions: 24, Replication: 1},
	}
	plan := planTopics(existing, model.AllTopics(), 24, 3)
	got := map[string]string{}
	for _, a := range plan {
		got[a.Topic] = a.Action
	}
	if got[model.TopicRaw] != "ok" || got[model.TopicDeviceBusiness] != "too_few_partitions" || got[model.TopicAlarmRaised] != "under_replicated" || got[model.DLQTopic("processor")] != "create" {
		t.Fatal(got)
	}
	if len(plan) != len(model.AllTopics()) {
		t.Fatal("inventory not fully planned")
	}
}

func TestClickHouseClusterCheckRequiresEveryNodeLayout(t *testing.T) {
	body := `{"host":"ch1","name":"iot_telemetry","engine":"Distributed"}
{"host":"ch1","name":"iot_telemetry_local","engine":"ReplicatedMergeTree"}
{"host":"ch1","name":"iot_raw_message","engine":"Distributed"}
{"host":"ch1","name":"iot_raw_message_local","engine":"ReplicatedMergeTree"}
{"host":"ch2","name":"iot_telemetry","engine":"MergeTree"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	c := clickhouseTables(context.Background(), srv.URL, "iot_cluster")
	if c.OK {
		t.Fatal("a node with single-node tables passed", c.Detail)
	}
}
