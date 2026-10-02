package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func (c Config) validateKafkaSecurity() error {
	for _, address := range c.KafkaPublicBrokers {
		host, port, err := net.SplitHostPort(address)
		number, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || host == "" || strings.ContainsAny(host, "/@ \t\r\n") || number < 1 || number > 65535 {
			return errors.New("IOT_KAFKA_PUBLIC_BROKERS must contain host:port addresses")
		}
	}
	if (c.KafkaSASLUsername == "") != (c.KafkaSASLPassword == "") {
		return errors.New("IOT_KAFKA_SASL_USERNAME and IOT_KAFKA_SASL_PASSWORD must be configured together")
	}
	mechanism := strings.ToUpper(strings.TrimSpace(c.KafkaSASLMechanism))
	if mechanism != "" && mechanism != "SCRAM-SHA-256" && mechanism != "SCRAM-SHA-512" {
		return errors.New("IOT_KAFKA_SASL_MECHANISM must be SCRAM-SHA-256 or SCRAM-SHA-512")
	}
	if c.KafkaTLSCAFile != "" && !c.KafkaTLS {
		return errors.New("IOT_KAFKA_TLS_CA_FILE requires IOT_KAFKA_TLS=true")
	}
	if c.KafkaAdminURL != "" || c.KafkaAdminUsername != "" || c.KafkaAdminPassword != "" {
		if c.KafkaAdminURL == "" || c.KafkaAdminUsername == "" || c.KafkaAdminPassword == "" {
			return errors.New("IOT_KAFKA_ADMIN_URL, IOT_KAFKA_ADMIN_USERNAME and IOT_KAFKA_ADMIN_PASSWORD must be configured together")
		}
		u, err := url.Parse(c.KafkaAdminURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("IOT_KAFKA_ADMIN_URL must be an HTTP(S) root URL without credentials, query or fragment")
		}
	}
	return nil
}
