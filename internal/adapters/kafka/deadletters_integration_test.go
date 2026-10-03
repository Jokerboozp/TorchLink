package kafkaadapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

// Uses IOT_TEST_SECURED_KAFKA_* like the consumer admin tests, with topics
// unique to this run so a shared development broker is not disturbed.
func TestDeadLettersListAndReplay(t *testing.T) {
	brokers := os.Getenv("IOT_TEST_SECURED_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("IOT_TEST_SECURED_KAFKA_BROKERS is not configured")
	}
	bus, err := NewWithSecurity(strings.Split(brokers, ","), SecurityConfig{Username: os.Getenv("IOT_TEST_SECURED_KAFKA_USERNAME"), Password: os.Getenv("IOT_TEST_SECURED_KAFKA_PASSWORD"), Mechanism: os.Getenv("IOT_TEST_SECURED_KAFKA_MECHANISM")})
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	suffix := fmt.Sprint(time.Now().UnixNano())
	dlqTopic, target := "iot.test.dlq."+suffix, "iot.test.target."+suffix
	if total, items, err := bus.deadLetters(ctx, "g", dlqTopic, 10); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("missing topic: total=%d items=%v err=%v", total, items, err)
	}
	for i := 0; i < 3; i++ {
		payload := []byte(fmt.Sprintf(`{"messageId":"m%d"}`, i))
		if i == 2 {
			payload = []byte{0xff, 0x00}
		}
		if err := bus.Publish(ctx, dlqTopic, fmt.Sprintf("device-%d", i), deadLetterPayload(target, "g", errors.New("boom"), payload)); err != nil {
			t.Fatal(err)
		}
	}
	total, items, err := bus.deadLetters(ctx, "g", dlqTopic, 2)
	if err != nil || total != 3 || len(items) != 2 || items[0].Error != "boom" || items[0].SourceTopic != target {
		t.Fatalf("total=%d items=%+v err=%v", total, items, err)
	}
	var binary int64 = -1
	_, all, _ := bus.deadLetters(ctx, "g", dlqTopic, 10)
	for _, item := range all {
		if item.Key == "device-2" {
			binary = item.Offset
		}
	}
	if _, err := bus.replay(ctx, "g", dlqTopic, target, 0, binary); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.replay(ctx, "g", dlqTopic, "another.topic", 0, binary); err == nil {
		t.Fatal("replay to a topic other than the group's source must be refused")
	}
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: bus.brokers, Dialer: bus.dialer, Topic: target, Partition: 0})
	defer reader.Close()
	m, err := reader.ReadMessage(ctx)
	if err != nil || string(m.Key) != "device-2" || string(m.Value) != string([]byte{0xff, 0x00}) {
		t.Fatalf("replayed key=%s value=%x err=%v", m.Key, m.Value, err)
	}
}
