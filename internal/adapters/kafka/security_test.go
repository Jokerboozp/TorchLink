package kafkaadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/segmentio/kafka-go"
)

func TestKafkaSecurityRejectsIncompleteConfiguration(t *testing.T) {
	for _, config := range []SecurityConfig{
		{Username: "service"}, {Password: "do-not-log-this-secret"},
		{Username: "service", Password: "do-not-log-this-secret", Mechanism: "PLAIN"},
		{CAFile: "/must-not-be-read-without-tls"}, {TLS: true, CAFile: "/missing-private-file"},
	} {
		if _, err := NewWithSecurity([]string{"unused:9092"}, config); err == nil || strings.Contains(err.Error(), "do-not-log-this-secret") {
			t.Fatal("invalid security config was accepted or disclosed its secret")
		}
	}
	dir := t.TempDir()
	ca := filepath.Join(dir, "invalid.pem")
	if err := os.WriteFile(ca, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTransport(SecurityConfig{TLS: true, CAFile: ca}); err == nil {
		t.Fatal("invalid CA file accepted")
	}
}

func TestKafkaSecurityReachesEveryClient(t *testing.T) {
	bus, err := NewWithSecurity([]string{"unused:9092"}, SecurityConfig{Username: "service", Password: "private-value", Mechanism: "SCRAM-SHA-512", TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	reader := bus.newSource("topic", "group").(*kafka.Reader)
	defer reader.Close()
	if reader.Config().Dialer.SASLMechanism.Name() != "SCRAM-SHA-512" || reader.Config().Dialer.TLS == nil {
		t.Fatal("reader lost SASL/TLS")
	}
	writer := bus.writer("topic")
	transport, ok := writer.Transport.(*kafka.Transport)
	if !ok || transport != bus.transport || transport.SASL.Name() != "SCRAM-SHA-512" || transport.TLS == nil {
		t.Fatal("writer lost SASL/TLS")
	}
	if bus.dialer.SASLMechanism == nil || bus.dialer.TLS == nil {
		t.Fatal("health connection lost SASL/TLS")
	}
}
