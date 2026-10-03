package messagetopics

import (
	"context"
	"errors"

	"iot-platform/internal/ports"
)

type routingBus struct {
	ports.EventBus
	service *Service
}

type routingCapacityBus struct {
	*routingBus
	ports.CapacityQueueCleaner
}

func (s *Service) WrapBus(bus ports.EventBus) ports.EventBus {
	if bus == nil {
		return nil
	}
	wrapped := &routingBus{EventBus: bus, service: s}
	// Capacity cleanup discovers this optional capability on engine.Bus.
	// Preserve it only when the actual broker provides it; local buses must
	// not appear to support destructive broker operations.
	if cleaner, ok := bus.(ports.CapacityQueueCleaner); ok {
		return &routingCapacityBus{routingBus: wrapped, CapacityQueueCleaner: cleaner}
	}
	return wrapped
}

func (b *routingBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	publications, err := b.service.Publications(ctx, "kafka", topic, payload)
	for _, publication := range publications {
		err = errors.Join(err, b.EventBus.Publish(ctx, publication.Topic, key, publication.Payload))
	}
	return err
}

type routingRealtime struct {
	ports.RealtimePublisher
	service *Service
}

func (s *Service) WrapRealtime(publisher ports.RealtimePublisher) ports.RealtimePublisher {
	if publisher == nil {
		return nil
	}
	return &routingRealtime{RealtimePublisher: publisher, service: s}
}

func (p *routingRealtime) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool) error {
	publications, err := p.service.Publications(ctx, "mqtt", topic, payload)
	for _, publication := range publications {
		err = errors.Join(err, p.RealtimePublisher.Publish(ctx, publication.Topic, publication.Payload, qos, retained))
	}
	return err
}
