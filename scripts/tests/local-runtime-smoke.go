//go:build integration

// Run from the repository root:
// go run scripts/tests/local-runtime-smoke.go --env-file .env.local
// Uses temporary test resources; never prints credentials.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
)

func main() {
	if err := run(); err != nil {
		// Network errors can contain connection URLs with passwords.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	envFile := flag.String("env-file", ".env.local", "local dependency configuration")
	flag.Parse()
	if config.LoadEnvFile(*envFile) != nil {
		return fmt.Errorf("FAIL load environment file")
	}
	cfg := config.Load()
	if cfg.PostgresDSN == "" || cfg.RedisAddr == "" || cfg.ClickHouseURL == "" || cfg.MinIOEndpoint == "" || len(cfg.KafkaBrokers) == 0 {
		return fmt.Errorf("FAIL configuration must include PostgreSQL, Redis, ClickHouse, MinIO and Kafka")
	}
	id := fmt.Sprintf("iot_smoke_%d", time.Now().UnixNano())
	checks := []struct {
		name string
		run  func(context.Context) error
	}{
		{"PostgreSQL transaction and readback", func(ctx context.Context) error {
			conn, err := pgx.Connect(ctx, cfg.PostgresDSN)
			if err != nil {
				return err
			}
			defer conn.Close(context.Background())
			_, err = conn.Exec(ctx, "CREATE TEMP TABLE iot_smoke (value text); INSERT INTO iot_smoke VALUES ('roundtrip')")
			if err != nil {
				return err
			}
			var value string
			err = conn.QueryRow(ctx, "SELECT value FROM iot_smoke").Scan(&value)
			if err == nil && value != "roundtrip" {
				return fmt.Errorf("readback mismatch")
			}
			return err
		}},
		{"Redis authenticated write/read", func(ctx context.Context) error {
			client := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword})
			defer client.Close()
			defer client.Del(context.Background(), id)
			if err := client.Set(ctx, id, "roundtrip", time.Minute).Err(); err != nil {
				return err
			}
			value, err := client.Get(ctx, id).Result()
			if err == nil && value != "roundtrip" {
				return fmt.Errorf("readback mismatch")
			}
			return err
		}},
		{"ClickHouse authenticated write/read", func(ctx context.Context) error {
			query := func(sql string) ([]byte, error) { return request(ctx, "POST", cfg.ClickHouseURL, sql) }
			if _, err := query("CREATE TABLE " + id + " (value String) ENGINE = Memory"); err != nil {
				return err
			}
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, _ = request(cleanup, "POST", cfg.ClickHouseURL, "DROP TABLE IF EXISTS "+id)
			}()
			if _, err := query("INSERT INTO " + id + " VALUES ('roundtrip')"); err != nil {
				return err
			}
			value, err := query("SELECT value FROM " + id)
			if err == nil && strings.TrimSpace(string(value)) != "roundtrip" {
				return fmt.Errorf("readback mismatch")
			}
			return err
		}},
		{"MinIO object upload/download", func(ctx context.Context) error {
			client, err := minio.New(cfg.MinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""), Secure: cfg.MinIOUseTLS})
			if err != nil {
				return err
			}
			bucket := strings.ReplaceAll(id, "_", "-")
			if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
				return err
			}
			defer client.RemoveBucket(context.Background(), bucket)
			defer client.RemoveObject(context.Background(), bucket, "probe", minio.RemoveObjectOptions{})
			if _, err = client.PutObject(ctx, bucket, "probe", strings.NewReader("roundtrip"), 9, minio.PutObjectOptions{}); err != nil {
				return err
			}
			object, err := client.GetObject(ctx, bucket, "probe", minio.GetObjectOptions{})
			if err != nil {
				return err
			}
			defer object.Close()
			value, err := io.ReadAll(object)
			if err == nil && string(value) != "roundtrip" {
				return fmt.Errorf("readback mismatch")
			}
			return err
		}},
		{"Kafka advertised address and produce/consume", func(ctx context.Context) error {
			client := &kafka.Client{Addr: kafka.TCP(cfg.KafkaBrokers...), Timeout: 10 * time.Second}
			response, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: []kafka.TopicConfig{{Topic: id, NumPartitions: 1, ReplicationFactor: 1}}})
			if err != nil {
				return err
			}
			if err = response.Errors[id]; err != nil {
				return err
			}
			defer client.DeleteTopics(context.Background(), &kafka.DeleteTopicsRequest{Topics: []string{id}})
			writer := &kafka.Writer{Addr: kafka.TCP(cfg.KafkaBrokers...), Topic: id, RequiredAcks: kafka.RequireAll, BatchTimeout: time.Millisecond}
			defer writer.Close()
			// Metadata propagation can lag successful topic creation.
			for {
				err = writer.WriteMessages(ctx, kafka.Message{Value: []byte("roundtrip")})
				if err == nil {
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
				}
			}
			reader := kafka.NewReader(kafka.ReaderConfig{Brokers: cfg.KafkaBrokers, Topic: id, Partition: 0, MinBytes: 1, MaxBytes: 1024})
			defer reader.Close()
			message, err := reader.ReadMessage(ctx)
			if err == nil && string(message.Value) != "roundtrip" {
				return fmt.Errorf("readback mismatch")
			}
			return err
		}},
		{"Ollama embedding inference", func(ctx context.Context) error {
			data, err := request(ctx, "POST", strings.TrimRight(cfg.OllamaURL, "/")+"/api/embeddings", `{"model":"nomic-embed-text","prompt":"消防设备连接验证"}`)
			if err != nil {
				return err
			}
			var result struct {
				Embedding []float64 `json:"embedding"`
			}
			if err = json.Unmarshal(data, &result); err != nil {
				return err
			}
			if len(result.Embedding) == 0 {
				return fmt.Errorf("empty embedding")
			}
			return nil
		}},
		{"MQTT JWT authentication and publish/subscribe", func(ctx context.Context) error {
			topic := "iot-verification/" + id
			token, err := auth.New(cfg.JWTSecret).IssueWithACL(id, "system", "service", nil, []auth.ACLRule{
				{Permission: "allow", Action: "subscribe", Topic: topic},
				{Permission: "allow", Action: "publish", Topic: topic},
			}, time.Minute)
			if err != nil {
				return err
			}
			client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(cfg.MQTTBroker).SetClientID(id).SetUsername(id).SetPassword(token).SetAutoReconnect(false).SetConnectTimeout(10 * time.Second))
			wait := func(token mqtt.Token) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-token.Done():
					return token.Error()
				}
			}
			if err = wait(client.Connect()); err != nil {
				return err
			}
			defer client.Disconnect(250)
			messages := make(chan string, 1)
			if err = wait(client.Subscribe(topic, 1, func(_ mqtt.Client, m mqtt.Message) {
				select {
				case messages <- string(m.Payload()):
				default:
				}
			})); err != nil {
				return err
			}
			if err = wait(client.Publish(topic, 1, false, "roundtrip")); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case value := <-messages:
				if value != "roundtrip" {
					return fmt.Errorf("readback mismatch")
				}
				return nil
			}
		}},
		{"Weaviate readiness", func(ctx context.Context) error {
			_, err := request(ctx, "GET", strings.TrimRight(cfg.WeaviateURL, "/")+"/v1/.well-known/ready", "")
			return err
		}},
	}
	failed := false
	for _, check := range checks {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := check.run(ctx)
		cancel()
		if err != nil {
			fmt.Println("FAIL", check.name)
			failed = true
		} else {
			fmt.Println("PASS", check.name)
		}
	}
	if failed {
		return fmt.Errorf("dependency verification failed; inspect the corresponding service without printing credentials")
	}
	return nil
}

func request(ctx context.Context, method, url, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 1<<20))
}
