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

func TestRoleConnectionBudgetIncludesRollingSurge(t *testing.T) {
	roles := []RolePool{{"api", 3, 12}, {"gateway", 3, 12}, {"parser", 3, 16}, {"processor", 3, 24}, {"ai", 2, 4}, {"jobs", 2, 4}}
	b, c := AssessConnectionBudget(roles, 40, 300)
	// Plan §4.3: steady 36+36+48+72+8+8 = 208, one extra processor = 232.
	if b.Steady != 208 || b.Surge != 24 || b.SurgeRole != "processor" || b.Headroom != 300-208-24-40 || c.Status != "passed" {
		t.Fatalf("%+v %+v", b, c)
	}
	if _, c = AssessConnectionBudget(roles, 40, 250); c.Status != "blocked" {
		t.Fatal("over-budget plan passed")
	}
	if _, c = AssessConnectionBudget(roles, 40, 0); c.Status != "unverified" {
		t.Fatal("unknown limit must not pass")
	}
}
