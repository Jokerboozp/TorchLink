// dlq-replay recovers explicitly selected storage failures through the original
// consumer path. It neither deletes dead letters nor changes consumer offsets.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/segmentio/kafka-go"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"os"
	"sort"
	"time"
)

type deadLetter struct {
	SourceTopic     string          `json:"sourceTopic"`
	ConsumerGroup   string          `json:"consumerGroup"`
	Payload         json.RawMessage `json:"payload"`
	PayloadEncoding string          `json:"payloadEncoding"`
}

func selectMessage(body []byte, tenant, source string, ids map[string]bool) (model.StandardMessage, []byte, error) {
	var dlq deadLetter
	var msg model.StandardMessage
	if err := json.Unmarshal(body, &dlq); err != nil {
		return msg, nil, err
	}
	if dlq.ConsumerGroup != "storage" || dlq.SourceTopic != source || dlq.PayloadEncoding != "" {
		return msg, nil, nil
	}
	if err := json.Unmarshal(dlq.Payload, &msg); err != nil {
		return msg, nil, err
	}
	if msg.TenantID != tenant || !ids[msg.MessageID] {
		return msg, nil, nil
	}
	if msg.DeviceID == "" || msg.RawMessageID == "" {
		return msg, nil, errors.New("selected message lacks device/raw identity")
	}
	return msg, dlq.Payload, nil
}
func run() error {
	envFile := flag.String("env-file", ".env.local", "环境文件")
	tenant := flag.String("tenant", "", "必须明确指定租户")
	file := flag.String("ids-file", "", "待恢复标准消息 ID 的 JSON 数组文件，最多 10000 条")
	source := flag.String("source-topic", model.TopicPropertyReport, "原业务主题：property / event / parsed")
	execute := flag.Bool("execute", false, "实际重新发布；默认仅核对，不写数据")
	maxScan := flag.Int("max-scan", 10000, "最大读取死信条数；超出则整批拒绝发布")
	flag.Parse()
	if *tenant == "" || *file == "" || *maxScan < 1 {
		return errors.New("tenant, ids-file and positive max-scan are required")
	}
	if *source != model.TopicPropertyReport && *source != model.TopicEventReport && *source != model.TopicParsed {
		return errors.New("unsupported storage source topic")
	}
	b, err := os.ReadFile(*file)
	if err != nil {
		return errors.New("cannot read IDs file")
	}
	var list []string
	if json.Unmarshal(b, &list) != nil || len(list) == 0 || len(list) > 10000 {
		return errors.New("IDs file must contain 1..10000 message IDs")
	}
	ids := map[string]bool{}
	for _, id := range list {
		if id == "" {
			return errors.New("empty message ID")
		}
		ids[id] = true
	}
	if err = config.LoadEnvFile(*envFile); err != nil {
		return errors.New("cannot load environment")
	}
	cfg := config.Load()
	if len(cfg.KafkaBrokers) == 0 {
		return errors.New("Kafka is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dialer := kafka.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", cfg.KafkaBrokers[0])
	if err != nil {
		return errors.New("Kafka metadata connection failed")
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	parts, err := conn.ReadPartitions()
	_ = conn.Close()
	if err != nil {
		return errors.New("Kafka metadata read failed")
	}
	selected := map[string]kafka.Message{}
	scanned, matched, partitions := 0, 0, 0
	for _, p := range parts {
		if p.Topic != "iot.dlq.storage" {
			continue
		}
		partitions++
		reader, err := dialer.DialLeader(ctx, "tcp", cfg.KafkaBrokers[0], p.Topic, p.ID)
		if err != nil {
			return errors.New("DLQ partition connection failed")
		}
		_ = reader.SetDeadline(time.Now().Add(30 * time.Second))
		first, last, err := reader.ReadOffsets()
		if err != nil {
			reader.Close()
			return errors.New("DLQ offsets read failed")
		}
		if first == last {
			reader.Close()
			continue
		}
		if _, err = reader.Seek(first, kafka.SeekAbsolute); err != nil {
			reader.Close()
			return errors.New("DLQ seek failed")
		}
		for {
			m, e := reader.ReadMessage(4 << 20)
			if e != nil {
				reader.Close()
				return fmt.Errorf("DLQ partition %d read failed before snapshot end", p.ID)
			}
			if m.Offset >= last {
				break
			}
			scanned++
			if scanned > *maxScan {
				reader.Close()
				return errors.New("max-scan exceeded; no messages published")
			}
			msg, payload, e := selectMessage(m.Value, *tenant, *source, ids)
			if e != nil {
				reader.Close()
				return e
			}
			if payload != nil {
				matched++
				if previous, ok := selected[msg.MessageID]; ok && !bytes.Equal(previous.Value, payload) {
					reader.Close()
					return errors.New("conflicting dead letters for one message ID")
				}
				selected[msg.MessageID] = kafka.Message{Key: []byte(msg.DeviceID), Value: payload}
			}
			if m.Offset+1 >= last {
				break
			}
		}
		reader.Close()
	}
	if partitions == 0 {
		return errors.New("storage DLQ topic does not exist")
	}
	if len(selected) != len(ids) {
		return fmt.Errorf("only %d/%d selected IDs found; no messages published", len(selected), len(ids))
	}
	published := 0
	if *execute {
		writer := kafka.Writer{Addr: kafka.TCP(cfg.KafkaBrokers...), Topic: *source, Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll, AllowAutoTopicCreation: false}
		defer writer.Close()
		order := make([]string, 0, len(selected))
		for id := range selected {
			order = append(order, id)
		}
		sort.Strings(order)
		for _, id := range order {
			if err = writer.WriteMessages(ctx, selected[id]); err != nil {
				return fmt.Errorf("publish failed after %d/%d; safe to rerun the same IDs through idempotent consumer", published, len(selected))
			}
			published++
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"dryRun": !*execute, "scanned": scanned, "matchedDeadLetters": matched, "uniqueMessages": len(selected), "published": published, "completionVerified": false})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
