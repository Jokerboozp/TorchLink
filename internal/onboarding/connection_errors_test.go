package onboarding

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/connector"
	"iot-platform/internal/parser"
	"testing"
)

func TestConnectionErrorResults(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{fmt.Errorf("broker: %w", connector.ErrAuthentication), "AUTH_FAILED"}, {fmt.Errorf("dial: %w", context.DeadlineExceeded), "TIMEOUT"}, {errors.New("unreachable"), "NETWORK_ERROR"}} {
		t.Run(tc.code, func(t *testing.T) {
			s, _, q := fixture(t)
			q.Type = connector.MQTT
			s.MQTTHealth = func(context.Context) error { return tc.err }
			r, err := s.Test(context.Background(), "tenant", q)
			if err != nil || r.Success || r.ErrorCode != tc.code || r.Stage != "broker" || r.DeviceID != q.DeviceID || r.ProtocolID != parser.StandardProtocolID || r.Parser == "" || r.TestToken != "" {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
	s, _, q := fixture(t)
	q.ProductID = "missing"
	r, err := s.Test(context.Background(), "tenant", q)
	if err == nil || r == nil || r.ErrorCode != "PROTOCOL_ERROR" || r.Stage != "validate" || r.DeviceID != q.DeviceID {
		t.Fatalf("%+v %v", r, err)
	}
}
