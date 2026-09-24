package kafkaadapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDeadLetterPayloadIsDecodableForMalformedSource(t *testing.T) {
	for _, payload := range [][]byte{[]byte(`{"device":`), {0xff, 0x00, 0x7d}} {
		body := deadLetterPayload("iot.raw", "archive", errors.New("decode failed"), payload)
		if !json.Valid(body) {
			t.Fatalf("dead letter is not JSON for source %x: %q", payload, body)
		}
		var event map[string]any
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		if event["sourceTopic"] != "iot.raw" || event["consumerGroup"] != "archive" || event["error"] != "decode failed" {
			t.Fatalf("dead letter metadata was lost: %#v", event)
		}
		encoded, ok := event["payload"].(string)
		if !ok || event["payloadEncoding"] != "base64" {
			t.Fatalf("malformed source must be recoverable as base64: %#v", event)
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || !bytes.Equal(decoded, payload) {
			t.Fatalf("dead letter source changed: %x, %v", decoded, err)
		}
	}
}

func TestDeadLetterPayloadPreservesStructuredSource(t *testing.T) {
	body := deadLetterPayload("iot.raw", "archive", errors.New("decode failed"), []byte(`{"device":"sensor-1"}`))
	var event struct {
		Payload struct {
			Device string `json:"device"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &event); err != nil || event.Payload.Device != "sensor-1" {
		t.Fatalf("structured source changed: %q, %v", body, err)
	}
}

func TestDeadLetterPayloadEscapesMetadata(t *testing.T) {
	body := deadLetterPayload("iot.raw", "archive", errors.New("decode\x00failed"), []byte(`{}`))
	if !json.Valid(body) {
		t.Fatalf("dead letter metadata is not JSON: %q", body)
	}
}

func TestDeadLetterPublishRetriesBeforeAdvancing(t *testing.T) {
	attempts := 0
	err := retryUntilSuccess(context.Background(), time.Millisecond, func() error {
		attempts++
		if attempts == 1 {
			return errors.New("broker temporarily unavailable")
		}
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("dead letter must publish before next source message: attempts=%d, err=%v", attempts, err)
	}
}

func TestDeadLetterRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := retryUntilSuccess(ctx, time.Hour, func() error {
		attempts++
		cancel()
		return errors.New("broker unavailable")
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("retry should stop after cancellation: attempts=%d, err=%v", attempts, err)
	}
}
