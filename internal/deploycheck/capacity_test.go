package deploycheck

import (
	"testing"
	"time"
)

func TestCapacityPlanNeverMultipliesSingleNodeRate(t *testing.T) {
	checks := AssessCapacity(CapacityPlan{Replicas: 5, PoolPerProcess: 64, PostgresLimit: 300, ReservedConnections: 32, MinPartitions: 12, MinReplication: 1, AIRPM: 12, ProviderRPM: 30, AIConcurrency: 2, ModelLatency: 9 * time.Second})
	status := map[string]string{}
	for _, c := range checks {
		status[c.Name] = c.Status
	}
	for _, name := range []string{"PostgreSQL 连接预算", "Kafka 副本容错", "接入会话路由", "MQTT Broker 观测", "模型请求预算"} {
		if status[name] != "blocked" {
			t.Errorf("%s: %s", name, status[name])
		}
	}
	if status["模型容量估算"] != "estimate" || status["存储分片与高可用"] != "unverified" {
		t.Fatal("an estimate was presented as verified")
	}
}

func TestCapacityPlanMissingObservationsAreUnknown(t *testing.T) {
	for _, c := range AssessCapacity(CapacityPlan{Replicas: 3, PoolPerProcess: 64, AIRPM: 12}) {
		if (c.Name == "PostgreSQL 连接预算" || c.Name == "模型请求预算") && c.Status != "unverified" {
			t.Fatalf("missing input was treated as measured: %+v", c)
		}
	}
}
