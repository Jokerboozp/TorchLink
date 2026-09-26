package opscenter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

// This opt-in test requires a disposable Alertmanager and its mounted config
// directory. It delivers exclusively to the test SMTP listener, never 163.
func TestDeviceMailIntegration(t *testing.T) {
	url := os.Getenv("IOT_TEST_ALERTMANAGER_URL")
	if url == "" {
		t.Skip("requires isolated Alertmanager; see docs/OPS_CENTER.md")
	}
	dir := os.Getenv("IOT_TEST_ALERTMANAGER_DIR")
	smtpHost := os.Getenv("IOT_TEST_SMTP_HOST")
	if dir == "" || smtpHost == "" {
		t.Fatal("set IOT_TEST_ALERTMANAGER_DIR and IOT_TEST_SMTP_HOST")
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	messages := make(chan string, 20)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go captureSMTP(conn, messages)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	file := filepath.Join(dir, "alertmanager.yml")
	initial := []byte("route:\n  receiver: discard\nreceivers:\n  - name: discard\n")
	if err := os.WriteFile(file, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &Service{AMConfig: observability.NewFileStore(file, t.TempDir(), 0o644), Alerts: observability.NewAlertmanager(url, 5*time.Second)}
	cfg, err := svc.NotificationConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	noTLS := false
	cfg.Receivers = append(cfg.Receivers, model.OpsReceiver{Name: "local-mail", Emails: []model.OpsEmailReceiver{{To: "test@example.invalid", From: "torchlink@example.invalid", Smarthost: net.JoinHostPort(smtpHost, fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)), RequireTLS: &noTLS}}})
	cfg.DeviceAlarmReceiver = "local-mail"
	if _, err := svc.SaveNotificationConfig(ctx, cfg, "integration-test"); err != nil {
		t.Fatal(err)
	}
	bus := local.NewBus()
	defer bus.Close()
	if err := svc.StartDeviceNotifications(ctx, bus, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(repo, archive, bus, local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	bodies := map[string]bool{}
	for phase := 0; phase < 2; phase++ {
		for i := 1; i <= 2; i++ {
			id := fmt.Sprintf("mail-device-%d", i)
			if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "mail-test", ID: id, AccessKey: id, ProductID: "json_sensor", Name: fmt.Sprintf("邮件测试烟感%d", i), Status: "ENABLED"}); err != nil {
				t.Fatal(err)
			}
			raw := model.RawMessage{MessageID: fmt.Sprintf("mail-raw-%d-%d", phase, i), TenantID: "mail-test", DeviceID: id, ProductID: "json_sensor", Protocol: "json", PayloadFormat: "json", ReceivedAt: time.Now().UnixMilli(), Payload: json.RawMessage(`{"messageType":"ALARM_REPORT","event":{"alarmType":"FIRE","alarmLevel":"HIGH","description":"烟雾浓度超限"}}`)}
			if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
				t.Fatal(err)
			}
			// Distinct reports of the same active alarm must not generate more mail.
			raw.MessageID += "-repeat"
			if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
				t.Fatal(err)
			}
			// The same raw message retried still produces just one receipt.
			if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
				t.Fatal(err)
			}
		}
		alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "mail-test"})
		if err != nil || len(alarms) != (phase+1)*2 {
			t.Fatalf("unexpected business alarm count: %d %v", len(alarms), err)
		}
		for range 2 {
			select {
			case raw := <-messages:
				message, err := mail.ReadMessage(strings.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(subject, "邮件测试烟感") || !strings.Contains(string(body), "烟雾浓度超限") || !strings.Contains(string(body), "告警编号") || !strings.Contains(string(body), "记录信息") || !strings.Contains(string(body), "本次上报时间") {
					t.Fatalf("missing mail detail: %s %s", subject, body)
				}
				if bodies[string(body)] {
					t.Fatalf("duplicate mail: %s", subject)
				}
				bodies[string(body)] = true
			case <-ctx.Done():
				t.Fatal("mail delivery timed out")
			}
		}
		select {
		case raw := <-messages:
			t.Fatalf("unexpected extra mail: %.100s", raw)
		case <-time.After(2 * time.Second):
		}
		if phase == 0 {
			for i, alarm := range alarms {
				status := "RECOVERED"
				if i == 1 {
					status = "CLOSED"
				}
				if _, err := engine.SetAlarmStatus(ctx, alarm.TenantID, alarm.ID, status, "integration-test"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Log("Each alarm produced one SMTP email despite repeated reports; recovery and closure allowed new alarm emails")
}

func captureSMTP(conn net.Conn, messages chan<- string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(conn)
	fmt.Fprint(conn, "220 local-test ESMTP\r\n")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			fmt.Fprint(conn, "250 local-test\r\n")
		case strings.HasPrefix(line, "DATA"):
			fmt.Fprint(conn, "354 end with dot\r\n")
			var body strings.Builder
			for {
				line, err = reader.ReadString('\n')
				if err != nil {
					return
				}
				if line == ".\r\n" {
					break
				}
				body.WriteString(strings.TrimPrefix(line, "."))
			}
			messages <- body.String()
			fmt.Fprint(conn, "250 accepted\r\n")
		case strings.HasPrefix(line, "QUIT"):
			fmt.Fprint(conn, "221 bye\r\n")
			return
		default:
			fmt.Fprint(conn, "250 ok\r\n")
		}
	}
}
