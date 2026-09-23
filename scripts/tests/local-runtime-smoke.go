//go:build integration

// Run from the repository root:
// go run scripts/tests/local-runtime-smoke.go --env-file .env.local
// Uses temporary test resources; never prints credentials.
package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"flag"          /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	mqtt "github.com/eclipse/paho.mqtt.golang"     /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5"                      /* 执行当前语句并推进处理流程。 */
	"github.com/minio/minio-go/v7"                 /* 执行当前语句并推进处理流程。 */
	"github.com/minio/minio-go/v7/pkg/credentials" /* 执行当前语句并推进处理流程。 */
	"github.com/redis/go-redis/v9"                 /* 执行当前语句并推进处理流程。 */
	"github.com/segmentio/kafka-go"                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"                   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"                 /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func main() { /* 定义 main 函数。 */
	if err := run(); err != nil { /* 判断条件并选择处理分支。 */
		// Network errors can contain connection URLs with passwords.
		fmt.Fprintln(os.Stderr, err) /* 执行当前语句并推进处理流程。 */
		os.Exit(1)                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func run() error { /* 定义 run 函数。 */
	envFile := flag.String("env-file", ".env.local", "local dependency configuration") /* 更新 envFile 的值。 */
	flag.Parse()                                                                       /* 执行当前语句并推进处理流程。 */
	if config.LoadEnvFile(*envFile) != nil {                                           /* 判断条件并选择处理分支。 */
		return fmt.Errorf("FAIL load environment file") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                  /* 更新 cfg 的值。 */
	if cfg.PostgresDSN == "" || cfg.RedisAddr == "" || cfg.ClickHouseURL == "" || cfg.MinIOEndpoint == "" || len(cfg.KafkaBrokers) == 0 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("FAIL configuration must include PostgreSQL, Redis, ClickHouse, MinIO and Kafka") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	id := fmt.Sprintf("iot_smoke_%d", time.Now().UnixNano()) /* 更新 id 的值。 */
	checks := []struct {                                     /* 更新 checks 的值。 */
		name string                      /* 执行当前语句并推进处理流程。 */
		run  func(context.Context) error /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{"PostgreSQL transaction and readback", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			conn, err := pgx.Connect(ctx, cfg.PostgresDSN) /* 更新 err 的值。 */
			if err != nil {                                /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			defer conn.Close(context.Background())                                                                          /* 安排函数结束时执行清理。 */
			_, err = conn.Exec(ctx, "CREATE TEMP TABLE iot_smoke (value text); INSERT INTO iot_smoke VALUES ('roundtrip')") /* 更新 err 的值。 */
			if err != nil {                                                                                                 /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			var value string                                                     /* 声明 value。 */
			err = conn.QueryRow(ctx, "SELECT value FROM iot_smoke").Scan(&value) /* 更新 err 的值。 */
			if err == nil && value != "roundtrip" {                              /* 判断条件并选择处理分支。 */
				return fmt.Errorf("readback mismatch") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return err /* 返回当前处理结果。 */
		}}, /* 结束当前表达式或代码块。 */
		{"Redis authenticated write/read", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			client := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword}) /* 更新 client 的值。 */
			defer client.Close()                                                                        /* 安排函数结束时执行清理。 */
			defer client.Del(context.Background(), id)                                                  /* 安排函数结束时执行清理。 */
			if err := client.Set(ctx, id, "roundtrip", time.Minute).Err(); err != nil {                 /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			value, err := client.Get(ctx, id).Result() /* 更新 err 的值。 */
			if err == nil && value != "roundtrip" {    /* 判断条件并选择处理分支。 */
				return fmt.Errorf("readback mismatch") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return err /* 返回当前处理结果。 */
		}}, /* 结束当前表达式或代码块。 */
		{"ClickHouse authenticated write/read", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			query := func(sql string) ([]byte, error) { return request(ctx, "POST", cfg.ClickHouseURL, sql) } /* 更新 query 的值。 */
			if _, err := query("CREATE TABLE " + id + " (value String) ENGINE = Memory"); err != nil {        /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			defer func() { /* 安排函数结束时执行清理。 */
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)   /* 更新 cancel 的值。 */
				defer cancel()                                                                 /* 安排函数结束时执行清理。 */
				_, _ = request(cleanup, "POST", cfg.ClickHouseURL, "DROP TABLE IF EXISTS "+id) /* 更新 _ 的值。 */
			}() /* 结束当前表达式或代码块。 */
			if _, err := query("INSERT INTO " + id + " VALUES ('roundtrip')"); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			value, err := query("SELECT value FROM " + id)                     /* 更新 err 的值。 */
			if err == nil && strings.TrimSpace(string(value)) != "roundtrip" { /* 判断条件并选择处理分支。 */
				return fmt.Errorf("readback mismatch") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return err /* 返回当前处理结果。 */
		}}, /* 结束当前表达式或代码块。 */
		{"MinIO object upload/download", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			client, err := minio.New(cfg.MinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""), Secure: cfg.MinIOUseTLS}) /* 更新 err 的值。 */
			if err != nil {                                                                                                                                                  /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			bucket := strings.ReplaceAll(id, "_", "-")                                       /* 更新 bucket 的值。 */
			if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			defer client.RemoveBucket(context.Background(), bucket)                                                                       /* 安排函数结束时执行清理。 */
			defer client.RemoveObject(context.Background(), bucket, "probe", minio.RemoveObjectOptions{})                                 /* 安排函数结束时执行清理。 */
			if _, err = client.PutObject(ctx, bucket, "probe", strings.NewReader("roundtrip"), 9, minio.PutObjectOptions{}); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			object, err := client.GetObject(ctx, bucket, "probe", minio.GetObjectOptions{}) /* 更新 err 的值。 */
			if err != nil {                                                                 /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			defer object.Close()                            /* 安排函数结束时执行清理。 */
			value, err := io.ReadAll(object)                /* 更新 err 的值。 */
			if err == nil && string(value) != "roundtrip" { /* 判断条件并选择处理分支。 */
				return fmt.Errorf("readback mismatch") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return err /* 返回当前处理结果。 */
		}}, /* 结束当前表达式或代码块。 */
		{"Kafka advertised address and produce/consume", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			client := &kafka.Client{Addr: kafka.TCP(cfg.KafkaBrokers...), Timeout: 10 * time.Second}                                                                /* 更新 client 的值。 */
			response, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: []kafka.TopicConfig{{Topic: id, NumPartitions: 1, ReplicationFactor: 1}}}) /* 更新 err 的值。 */
			if err != nil {                                                                                                                                         /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if err = response.Errors[id]; err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			defer client.DeleteTopics(context.Background(), &kafka.DeleteTopicsRequest{Topics: []string{id}})                                        /* 安排函数结束时执行清理。 */
			writer := &kafka.Writer{Addr: kafka.TCP(cfg.KafkaBrokers...), Topic: id, RequiredAcks: kafka.RequireAll, BatchTimeout: time.Millisecond} /* 更新 writer 的值。 */
			defer writer.Close()                                                                                                                     /* 安排函数结束时执行清理。 */
			// Metadata propagation can lag successful topic creation.
			for { /* 循环处理当前数据。 */
				err = writer.WriteMessages(ctx, kafka.Message{Value: []byte("roundtrip")}) /* 更新 err 的值。 */
				if err == nil {                                                            /* 判断条件并选择处理分支。 */
					break /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				select { /* 根据条件选择处理路径。 */
				case <-ctx.Done(): /* 处理当前分支。 */
					return ctx.Err() /* 返回当前处理结果。 */
				case <-time.After(time.Second): /* 处理当前分支。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			reader := kafka.NewReader(kafka.ReaderConfig{Brokers: cfg.KafkaBrokers, Topic: id, Partition: 0, MinBytes: 1, MaxBytes: 1024}) /* 更新 reader 的值。 */
			defer reader.Close()                                                                                                           /* 安排函数结束时执行清理。 */
			message, err := reader.ReadMessage(ctx)                                                                                        /* 更新 err 的值。 */
			if err == nil && string(message.Value) != "roundtrip" {                                                                        /* 判断条件并选择处理分支。 */
				return fmt.Errorf("readback mismatch") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return err /* 返回当前处理结果。 */
		}}, /* 结束当前表达式或代码块。 */
		{"Ollama embedding inference", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			data, err := request(ctx, "POST", strings.TrimRight(cfg.OllamaURL, "/")+"/api/embeddings", `{"model":"nomic-embed-text","prompt":"消防设备连接验证"}`) /* 更新 err 的值。 */
			if err != nil {                                                                                                                                /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			var result struct { /* 声明 result。 */
				Embedding []float64 `json:"embedding"` /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if err = json.Unmarshal(data, &result); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if len(result.Embedding) == 0 { /* 判断条件并选择处理分支。 */
				return fmt.Errorf("empty embedding") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return nil /* 返回当前处理结果。 */
		}}, /* 结束当前表达式或代码块。 */
		{"MQTT JWT authentication and publish/subscribe", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			topic := "iot-verification/" + id                                                                /* 更新 topic 的值。 */
			token, err := auth.New(cfg.JWTSecret).IssueWithACL(id, "system", "service", nil, []auth.ACLRule{ /* 检查错误并决定后续处理。 */
				{Permission: "allow", Action: "subscribe", Topic: topic}, /* 执行当前语句并推进处理流程。 */
				{Permission: "allow", Action: "publish", Topic: topic},   /* 执行当前语句并推进处理流程。 */
			}, time.Minute) /* 结束当前表达式或代码块。 */
			if err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(cfg.MQTTBroker).SetClientID(id).SetUsername(id).SetPassword(token).SetAutoReconnect(false).SetConnectTimeout(10 * time.Second)) /* 更新 client 的值。 */
			wait := func(token mqtt.Token) error {                                                                                                                                                     /* 更新 wait 的值。 */
				select { /* 根据条件选择处理路径。 */
				case <-ctx.Done(): /* 处理当前分支。 */
					return ctx.Err() /* 返回当前处理结果。 */
				case <-token.Done(): /* 处理当前分支。 */
					return token.Error() /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if err = wait(client.Connect()); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			defer client.Disconnect(250)                                                   /* 安排函数结束时执行清理。 */
			messages := make(chan string, 1)                                               /* 更新 messages 的值。 */
			if err = wait(client.Subscribe(topic, 1, func(_ mqtt.Client, m mqtt.Message) { /* 判断条件并选择处理分支。 */
				select { /* 根据条件选择处理路径。 */
				case messages <- string(m.Payload()): /* 处理当前分支。 */
				default: /* 处理当前分支。 */
				} /* 结束当前表达式或代码块。 */
			})); err != nil { /* 结束当前表达式或代码块。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if err = wait(client.Publish(topic, 1, false, "roundtrip")); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			select { /* 根据条件选择处理路径。 */
			case <-ctx.Done(): /* 处理当前分支。 */
				return ctx.Err() /* 返回当前处理结果。 */
			case value := <-messages: /* 处理当前分支。 */
				if value != "roundtrip" { /* 判断条件并选择处理分支。 */
					return fmt.Errorf("readback mismatch") /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				return nil /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		}}, /* 结束当前表达式或代码块。 */
		{"MQTT invalid JWT and username mismatch rejection", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			token, err := auth.New(cfg.JWTSecret).IssueWithACL(id, "system", "service", nil, nil, time.Minute) /* 检查错误并决定后续处理。 */
			if err != nil {                                                                                    /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			for i, login := range []struct{ user, password string }{{id, "invalid-jwt"}, {id + "-wrong", token}} { /* 循环处理当前数据。 */
				client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(cfg.MQTTBroker).SetClientID(fmt.Sprintf("%s-deny-%d", id, i)).SetUsername(login.user).SetPassword(login.password).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(5 * time.Second)) /* 更新 client 的值。 */
				attempt := client.Connect()                                                                                                                                                                                                                                                           /* 更新 attempt 的值。 */
				select {                                                                                                                                                                                                                                                                              /* 根据条件选择处理路径。 */
				case <-ctx.Done(): /* 处理当前分支。 */
					client.Disconnect(100) /* 执行当前语句并推进处理流程。 */
					return ctx.Err()       /* 返回当前处理结果。 */
				case <-attempt.Done(): /* 处理当前分支。 */
				} /* 结束当前表达式或代码块。 */
				code := attempt.(*mqtt.ConnectToken).ReturnCode()       /* 更新 code 的值。 */
				client.Disconnect(100)                                  /* 执行当前语句并推进处理流程。 */
				if attempt.Error() == nil || (code != 4 && code != 5) { /* 判断条件并选择处理分支。 */
					return fmt.Errorf("MQTT login boundary %d was not rejected", i) /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			return nil /* 返回当前处理结果。 */
		}}, /* 结束当前表达式或代码块。 */
		{"Weaviate readiness", func(ctx context.Context) error { /* 执行当前语句并推进处理流程。 */
			_, err := request(ctx, "GET", strings.TrimRight(cfg.WeaviateURL, "/")+"/v1/.well-known/ready", "") /* 更新 err 的值。 */
			return err                                                                                         /* 返回当前处理结果。 */
		}}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	failed := false                /* 更新 failed 的值。 */
	for _, check := range checks { /* 循环处理当前数据。 */
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) /* 更新 cancel 的值。 */
		err := check.run(ctx)                                                    /* 更新 err 的值。 */
		cancel()                                                                 /* 执行当前语句并推进处理流程。 */
		if err != nil {                                                          /* 判断条件并选择处理分支。 */
			fmt.Println("FAIL", check.name) /* 执行当前语句并推进处理流程。 */
			failed = true                   /* 更新 failed 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			fmt.Println("PASS", check.name) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if failed { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("dependency verification failed; inspect the corresponding service without printing credentials") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func request(ctx context.Context, method, url, body string) ([]byte, error) { /* 定义 request 函数。 */
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBufferString(body)) /* 更新 err 的值。 */
	if err != nil {                                                                       /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	response, err := http.DefaultClient.Do(req) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer response.Body.Close()                                  /* 安排函数结束时执行清理。 */
	if response.StatusCode < 200 || response.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("HTTP %d", response.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return io.ReadAll(io.LimitReader(response.Body, 1<<20)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
