package kafkaadapter

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/scram"
)

// SecurityConfig is shared by publishers, consumers and administrative tools.
// Empty credentials retain the existing unauthenticated deployment mode.
type SecurityConfig struct {
	Username, Password, Mechanism string
	TLS                           bool
	CAFile                        string
}

func (c SecurityConfig) settings() (*tls.Config, sasl.Mechanism, error) {
	if (c.Username == "") != (c.Password == "") {
		return nil, nil, errors.New("Kafka SASL username and password must be configured together")
	}
	mechanism := strings.ToUpper(strings.TrimSpace(c.Mechanism))
	if mechanism == "" {
		mechanism = "SCRAM-SHA-256"
	}
	if mechanism != "SCRAM-SHA-256" && mechanism != "SCRAM-SHA-512" {
		return nil, nil, errors.New("Kafka SASL mechanism must be SCRAM-SHA-256 or SCRAM-SHA-512")
	}
	var auth sasl.Mechanism
	if c.Username != "" {
		algorithm := scram.SHA256
		if mechanism == "SCRAM-SHA-512" {
			algorithm = scram.SHA512
		}
		var err error
		auth, err = scram.Mechanism(algorithm, c.Username, c.Password)
		if err != nil {
			return nil, nil, errors.New("could not initialize Kafka SASL authentication")
		}
	}
	if c.CAFile != "" && !c.TLS {
		return nil, nil, errors.New("Kafka CA file requires TLS")
	}
	var secure *tls.Config
	if c.TLS {
		secure = &tls.Config{MinVersion: tls.VersionTLS12}
		if c.CAFile != "" {
			pem, err := os.ReadFile(c.CAFile)
			if err != nil {
				return nil, nil, errors.New("could not read Kafka TLS CA file")
			}
			roots, err := x509.SystemCertPool()
			if err != nil || roots == nil {
				roots = x509.NewCertPool()
			}
			if !roots.AppendCertsFromPEM(pem) {
				return nil, nil, errors.New("Kafka TLS CA file contains no valid certificates")
			}
			secure.RootCAs = roots
		}
	}
	return secure, auth, nil
}

func NewDialer(config SecurityConfig) (*kafka.Dialer, error) {
	tlsConfig, mechanism, err := config.settings()
	if err != nil {
		return nil, err
	}
	return &kafka.Dialer{Timeout: 10 * time.Second, TLS: tlsConfig, SASLMechanism: mechanism}, nil
}

func NewTransport(config SecurityConfig) (*kafka.Transport, error) {
	tlsConfig, mechanism, err := config.settings()
	if err != nil {
		return nil, err
	}
	return &kafka.Transport{DialTimeout: 10 * time.Second, TLS: tlsConfig, SASL: mechanism}, nil
}
