package deploycheck

import (
	"fmt"
	"time"
)

type CapacityCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type CapacityPlan struct {
	Replicas, PoolPerProcess, PostgresLimit, ReservedConnections int
	MinPartitions, MinReplication                                int
	AccessCoordinated, MQTTObserved                              bool
	AIConcurrency, AIRPM, ProviderRPM                            int
	ModelLatency                                                 time.Duration
}

func AssessCapacity(p CapacityPlan) []CapacityCheck {
	checks := []CapacityCheck{}
	add := func(name string, ok bool, detail string) {
		status := "blocked"
		if ok {
			status = "passed"
		}
		checks = append(checks, CapacityCheck{name, status, detail})
	}
	add("PostgreSQL 连接预算", p.Replicas > 0 && p.PoolPerProcess > 0 && p.PostgresLimit > 0 && p.Replicas*p.PoolPerProcess+p.ReservedConnections <= p.PostgresLimit, fmt.Sprintf("%d 进程 × %d 连接 + %d 预留，对比实测 max_connections=%d；其他应用连接需计入预留", p.Replicas, p.PoolPerProcess, p.ReservedConnections, p.PostgresLimit))
	add("Kafka 消费副本", p.MinPartitions >= p.Replicas && p.MinPartitions > 0, fmt.Sprintf("业务主题最少 %d 分区，计划 %d 消费副本；跨主题、跨副本的同设备顺序仍需实测", p.MinPartitions, p.Replicas))
	add("Kafka 副本容错", p.Replicas <= 1 || p.MinReplication >= 3, fmt.Sprintf("业务分区最小复制因子=%d；多节点容错计划要求至少 3，仍需验证 ISR/min.insync.replicas 和故障恢复", p.MinReplication))
	add("接入会话路由", p.Replicas <= 1 || p.AccessCoordinated, "多副本必须启用 PostgreSQL 租约、各节点独立可达的 IOT_ACCESS_NODE_URL 与 MQTT 共享订阅；运行中的 TCP/SIP 连接仍需重连演练")
	add("MQTT Broker 观测", p.MQTTObserved, "需要成功读取平台会话管理数据；监控不可用时不能将 Broker 丢弃数解释为零")
	add("模型请求预算", p.AIRPM > 0 && p.ProviderRPM > 0 && p.AIRPM*p.Replicas <= p.ProviderRPM, fmt.Sprintf("自动研判预算 %d × %d 请求/分钟，提供方分配给本系统的预算 %d；还须预留手动研判、巡检、报告与 token 配额", p.Replicas, p.AIRPM, p.ProviderRPM))
	if p.ModelLatency > 0 && p.ProviderRPM > 0 && p.AIRPM > 0 {
		ceiling := min(float64(p.Replicas*p.AIConcurrency)/p.ModelLatency.Seconds(), float64(p.ProviderRPM)/60, float64(p.Replicas*p.AIRPM)/60)
		checks = append(checks, CapacityCheck{"模型容量估算", "estimate", fmt.Sprintf("按提供的平均延迟 %s 推算，自动研判上限约 %.3f 次/秒；未计 token 配额、长尾和其他工作流，不是集群实测", p.ModelLatency, ceiling)})
	}
	for _, v := range []CapacityCheck{
		{"存储分片与高可用", "unverified", "当前应用连接一个 PostgreSQL 写入口及一个 ClickHouse HTTP 入口；须验证目标集群的副本、分布式表、路由、故障切换和一致性，不能按 API 节点数乘算吞吐"},
		{"本地持久队列", "unverified", "每个接入进程须有独立稳定的 data/mqtt-inbox 与 protocol 队列目录；fsync 与锁保证本地重启，不保证节点磁盘丢失后的恢复；须演练故障卷重挂载与设备重发"},
		{"会话与模型故障演练", "unverified", "验证 TCP/MQTT/SIP 重连、命令回执路由、API 权限变化、Harness 并发与模型 token 预算，再执行相同设备群的端到端阶梯压测"},
	} {
		checks = append(checks, v)
	}
	// Missing observations and missing supplier quotas are unknown, not measured zero.
	for i := range checks {
		if checks[i].Name == "PostgreSQL 连接预算" && p.PostgresLimit == 0 {
			checks[i].Status = "unverified"
			checks[i].Detail = "未读到 PostgreSQL max_connections，不能计算连接预算"
		}
		if checks[i].Name == "模型请求预算" && p.ProviderRPM == 0 {
			checks[i].Status = "unverified"
			checks[i].Detail = "尚未提供模型供应商为本系统分配的请求配额，不能判定集群预算通过"
		}
	}
	return checks
}
