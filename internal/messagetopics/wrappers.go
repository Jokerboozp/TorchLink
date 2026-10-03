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

func (s *Service) WrapBus(bus ports.EventBus) ports.EventBus {
	if bus == nil {
		return nil
	}
	return &routingBus{EventBus: bus, service: s}
}

func (b *routingBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	var err error
	for _, publication := range b.service.Publications(ctx, "kafka", topic, payload) {
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
	var err error
	for _, publication := range p.service.Publications(ctx, "mqtt", topic, payload) {
		err = errors.Join(err, p.RealtimePublisher.Publish(ctx, publication.Topic, publication.Payload, qos, retained))
	}
	return err
}
