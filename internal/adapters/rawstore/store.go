package rawstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	PostgreSQLBucket = "postgres"
	ClickHouseBucket = "clickhouse"
)

// DeviceStateResolver is intentionally smaller than ports.Repository so the
// routing decision can be tested without constructing the whole platform.
type DeviceStateResolver interface {
	GetDeviceState(context.Context, string, string) (model.DeviceState, error)
}

type Config struct {
	PostgreSQL               ports.RawMessageDatabase
	ClickHouse               ports.RawMessageDatabase
	Resolver                 DeviceStateResolver
	Legacy                   ports.RawMessageReader
	HighFrequencyIntervalSec int64
}

// Store routes raw messages to a database and leaves MinIO out of the ingest
// path. A device is considered high frequency when its configured reporting
// interval, or its observed inter-arrival interval, is at or below the
// configured threshold. Unknown devices start in PostgreSQL until their
// frequency can be observed.
type Store struct {
	postgres                 ports.RawMessageDatabase
	clickhouse               ports.RawMessageDatabase
	resolver                 DeviceStateResolver
	legacy                   ports.RawMessageReader
	highFrequencyIntervalSec int64

	mu   sync.Mutex
	last map[string]int64
}

func New(cfg Config) *Store {
	if cfg.HighFrequencyIntervalSec <= 0 {
		cfg.HighFrequencyIntervalSec = 60
	}
	return &Store{
		postgres:                 cfg.PostgreSQL,
		clickhouse:               cfg.ClickHouse,
		resolver:                 cfg.Resolver,
		legacy:                   cfg.Legacy,
		highFrequencyIntervalSec: cfg.HighFrequencyIntervalSec,
		last:                     map[string]int64{},
	}
}

func (s *Store) PutRaw(ctx context.Context, value model.RawMessage) (model.RawArchiveIndex, error) {
	backend := s.chooseBackend(ctx, value)
	database := s.postgres
	if backend == ClickHouseBucket {
		database = s.clickhouse
	}
	if database == nil {
		// A single-database deployment should still work for every frequency.
		if backend == ClickHouseBucket {
			database = s.postgres
			backend = PostgreSQLBucket
		} else {
			database = s.clickhouse
			backend = ClickHouseBucket
		}
	}
	if database == nil {
		return model.RawArchiveIndex{}, fmt.Errorf("no database raw message store is configured")
	}
	if err := database.SaveRawMessage(ctx, value); err != nil {
		return model.RawArchiveIndex{}, err
	}
	return model.RawArchiveIndex{
		MessageID:     value.MessageID,
		TenantID:      value.TenantID,
		ProductID:     value.ProductID,
		DeviceID:      value.DeviceID,
		Protocol:      value.Protocol,
		PayloadFormat: value.PayloadFormat,
		ObjectBucket:  backend,
		ObjectKey:     value.MessageID,
		PayloadHash:   value.PayloadHash(),
		PayloadSize:   len(value.Payload),
		ReceivedAt:    value.ReceivedAt,
		ArchivedAt:    time.Now().UnixMilli(),
	}, nil
}

func (s *Store) GetRaw(ctx context.Context, index model.RawArchiveIndex) (model.RawMessage, error) {
	switch index.ObjectBucket {
	case PostgreSQLBucket:
		if s.postgres == nil {
			return model.RawMessage{}, fmt.Errorf("postgres raw message store is not configured")
		}
		return s.postgres.GetRawMessage(ctx, index.TenantID, index.MessageID)
	case ClickHouseBucket:
		if s.clickhouse == nil {
			return model.RawMessage{}, fmt.Errorf("clickhouse raw message store is not configured")
		}
		return s.clickhouse.GetRawMessage(ctx, index.TenantID, index.MessageID)
	default:
		if s.legacy == nil {
			return model.RawMessage{}, fmt.Errorf("legacy raw message reader is not configured")
		}
		return s.legacy.GetRaw(ctx, index)
	}
}

func (s *Store) chooseBackend(ctx context.Context, value model.RawMessage) string {
	threshold := s.highFrequencyIntervalSec
	configuredInterval := int64(0)
	stateLastSeen := int64(0)
	if s.resolver != nil {
		if state, err := s.resolver.GetDeviceState(ctx, value.TenantID, value.DeviceID); err == nil {
			configuredInterval = state.ReportIntervalSec
			stateLastSeen = state.LastSeenAt
		}
	}

	key := value.TenantID + "\x00" + value.DeviceID
	s.mu.Lock()
	previous := s.last[key]
	s.last[key] = value.ReceivedAt
	s.mu.Unlock()

	if configuredInterval > 0 && configuredInterval <= threshold {
		return ClickHouseBucket
	}
	if isHighFrequency(value.ReceivedAt, stateLastSeen, threshold) || isHighFrequency(value.ReceivedAt, previous, threshold) {
		return ClickHouseBucket
	}
	return PostgreSQLBucket
}

func isHighFrequency(current, previous, thresholdSec int64) bool {
	if current <= previous || previous <= 0 || thresholdSec <= 0 {
		return false
	}
	return current-previous <= thresholdSec*1000
}
