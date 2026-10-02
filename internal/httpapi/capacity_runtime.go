package httpapi

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type capacityMQTTCleaner interface {
	ports.CapacityInboxCleaner
	ports.CapacityRetainedCleaner
}

func (s *Server) SetCapacityMQTT(cleaner capacityMQTTCleaner) { s.capacityMQTT = cleaner }

func (s *Server) cleanupCapacityRuntime(ctx context.Context, tenant string, q model.CapacityCleanupBatch) (ports.RuntimeCleanupCounts, error) {
	var result ports.RuntimeCleanupCounts
	if len(q.RemoveDevices) == 0 && !(q.Historical && q.RemoveProduct) {
		return result, nil
	}
	if s.capacityMQTT != nil {
		n, err := s.capacityMQTT.CleanupCapacityInbox(ctx, tenant, q)
		result.Add(n)
		if err != nil {
			return result, err
		}
		n, err = s.capacityMQTT.ClearCapacityRetained(ctx, tenant, q)
		result.Add(n)
		if err != nil {
			return result, err
		}
	} else if s.cfg.MQTTBroker != "" {
		result.Warnings = append(result.Warnings, "当前进程未持有 MQTT 接收缓存，其他接入网关的本地缓存保留")
	}
	// Historical product cleanup happens once after all device batches, so
	// interleaved device IDs in broker partitions do not depend on batch order.
	if q.Historical && !q.RemoveProduct {
		return result, nil
	}
	if cleaner, ok := s.engine.Bus.(ports.CapacityQueueCleaner); ok {
		plan, err := cleaner.PreviewCapacityQueue(ctx, tenant, q)
		if err != nil {
			return result, err
		}
		n, err := cleaner.CleanupCapacityQueue(ctx, tenant, q, plan)
		result.Add(n)
		if err != nil {
			return result, err
		}
	} else if len(s.cfg.KafkaBrokers) > 0 {
		result.Warnings = append(result.Warnings, "当前消息队列不支持精确清理，队列记录按原保留策略处理")
	}
	return result, nil
}
