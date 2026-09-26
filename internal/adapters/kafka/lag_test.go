package kafkaadapter

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

// Uses a throwaway topic on the broker in IOT_TEST_DISPOSABLE_KAFKA.
func TestConsumerLagCountsUncommittedMessages(t *testing.T) {
	broker := os.Getenv("IOT_TEST_DISPOSABLE_KAFKA")
	if broker == "" {
		t.Skip("IOT_TEST_DISPOSABLE_KAFKA is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	topic := fmt.Sprintf("lag-test-%d", time.Now().UnixNano())
	brokers := strings.Split(broker, ",")
	conn, err := kafka.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 3, ReplicationFactor: 1}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	defer func() {
		if c, err := kafka.Dial("tcp", brokers[0]); err == nil {
			_ = c.DeleteTopics(topic)
			_ = c.Close()
		}
	}()

	b := New(brokers)
	defer b.Close()
	for i := range 5 {
		if err = b.Publish(ctx, topic, fmt.Sprintf("key-%d", i), []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	// A group that has not consumed yet owes every published message.
	b.subscriptions = append(b.subscriptions, subscription{topic: topic, group: "iot-platform-lag-idle"})
	lags, err := b.ConsumerLag(ctx)
	if err != nil || lags["iot-platform-lag-idle"] != 5 {
		t.Fatalf("idle group lag = %v, %v; want 5", lags, err)
	}

	// After the bus consumes and commits, the group has no backlog.
	handled := make(chan struct{}, 5)
	if err = b.Subscribe(ctx, topic, "lag-busy", func(context.Context, []byte) error { handled <- struct{}{}; return nil }); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		select {
		case <-handled:
		case <-ctx.Done():
			t.Fatal("messages were not consumed")
		}
	}
	for {
		lags, err = b.ConsumerLag(ctx)
		if err == nil && lags["iot-platform-lag-busy"] == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("busy group lag = %v, %v; want 0", lags, err)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
