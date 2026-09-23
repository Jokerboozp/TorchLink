package rawstore /* 声明 rawstore 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"sync"    /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	PostgreSQLBucket = "postgres"   /* 更新 PostgreSQLBucket 的值。 */
	ClickHouseBucket = "clickhouse" /* 更新 ClickHouseBucket 的值。 */
) /* 结束当前表达式或代码块。 */

// DeviceStateResolver is intentionally smaller than ports.Repository so the
// routing decision can be tested without constructing the whole platform.
type DeviceStateResolver interface { /* 定义 DeviceStateResolver 类型。 */
	GetDeviceState(context.Context, string, string) (model.DeviceState, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Config struct { /* 定义 Config 类型。 */
	PostgreSQL               ports.RawMessageDatabase /* 执行当前语句并推进处理流程。 */
	ClickHouse               ports.RawMessageDatabase /* 执行当前语句并推进处理流程。 */
	Resolver                 DeviceStateResolver      /* 执行当前语句并推进处理流程。 */
	Legacy                   ports.RawMessageReader   /* 执行当前语句并推进处理流程。 */
	HighFrequencyIntervalSec int64                    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// Store routes raw messages to a database and leaves MinIO out of the ingest
// path. A device is considered high frequency when its configured reporting
// interval, or its observed inter-arrival interval, is at or below the
// configured threshold. Unknown devices start in PostgreSQL until their
// frequency can be observed.
type Store struct { /* 定义 Store 类型。 */
	postgres                 ports.RawMessageDatabase /* 执行当前语句并推进处理流程。 */
	clickhouse               ports.RawMessageDatabase /* 执行当前语句并推进处理流程。 */
	resolver                 DeviceStateResolver      /* 执行当前语句并推进处理流程。 */
	legacy                   ports.RawMessageReader   /* 执行当前语句并推进处理流程。 */
	highFrequencyIntervalSec int64                    /* 执行当前语句并推进处理流程。 */

	mu   sync.Mutex       /* 执行当前语句并推进处理流程。 */
	last map[string]int64 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(cfg Config) *Store { /* 定义 New 函数。 */
	if cfg.HighFrequencyIntervalSec <= 0 { /* 判断条件并选择处理分支。 */
		cfg.HighFrequencyIntervalSec = 60 /* 更新 cfg.HighFrequencyIntervalSec 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &Store{ /* 返回当前处理结果。 */
		postgres:                 cfg.PostgreSQL,               /* 执行当前语句并推进处理流程。 */
		clickhouse:               cfg.ClickHouse,               /* 执行当前语句并推进处理流程。 */
		resolver:                 cfg.Resolver,                 /* 执行当前语句并推进处理流程。 */
		legacy:                   cfg.Legacy,                   /* 执行当前语句并推进处理流程。 */
		highFrequencyIntervalSec: cfg.HighFrequencyIntervalSec, /* 执行当前语句并推进处理流程。 */
		last:                     map[string]int64{},           /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Store) PutRaw(ctx context.Context, value model.RawMessage) (model.RawArchiveIndex, error) { /* 定义 PutRaw 函数。 */
	backend := s.chooseBackend(ctx, value) /* 更新 backend 的值。 */
	database := s.postgres                 /* 更新 database 的值。 */
	if backend == ClickHouseBucket {       /* 判断条件并选择处理分支。 */
		database = s.clickhouse /* 更新 database 的值。 */
	} /* 结束当前表达式或代码块。 */
	if database == nil { /* 判断条件并选择处理分支。 */
		// A single-database deployment should still work for every frequency.
		if backend == ClickHouseBucket { /* 判断条件并选择处理分支。 */
			database = s.postgres      /* 更新 database 的值。 */
			backend = PostgreSQLBucket /* 更新 backend 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			database = s.clickhouse    /* 更新 database 的值。 */
			backend = ClickHouseBucket /* 更新 backend 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if database == nil { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, fmt.Errorf("no database raw message store is configured") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := database.SaveRawMessage(ctx, value); err != nil { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return model.RawArchiveIndex{ /* 返回当前处理结果。 */
		MessageID:     value.MessageID,        /* 执行当前语句并推进处理流程。 */
		TenantID:      value.TenantID,         /* 执行当前语句并推进处理流程。 */
		ProductID:     value.ProductID,        /* 执行当前语句并推进处理流程。 */
		DeviceID:      value.DeviceID,         /* 执行当前语句并推进处理流程。 */
		Protocol:      value.Protocol,         /* 执行当前语句并推进处理流程。 */
		PayloadFormat: value.PayloadFormat,    /* 执行当前语句并推进处理流程。 */
		ObjectBucket:  backend,                /* 执行当前语句并推进处理流程。 */
		ObjectKey:     value.MessageID,        /* 执行当前语句并推进处理流程。 */
		PayloadHash:   value.PayloadHash(),    /* 执行当前语句并推进处理流程。 */
		PayloadSize:   len(value.Payload),     /* 执行当前语句并推进处理流程。 */
		ReceivedAt:    value.ReceivedAt,       /* 执行当前语句并推进处理流程。 */
		ArchivedAt:    time.Now().UnixMilli(), /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Store) GetRaw(ctx context.Context, index model.RawArchiveIndex) (model.RawMessage, error) { /* 定义 GetRaw 函数。 */
	switch index.ObjectBucket { /* 根据条件选择处理路径。 */
	case PostgreSQLBucket: /* 处理当前分支。 */
		if s.postgres == nil { /* 判断条件并选择处理分支。 */
			return model.RawMessage{}, fmt.Errorf("postgres raw message store is not configured") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return s.postgres.GetRawMessage(ctx, index.TenantID, index.MessageID) /* 返回当前处理结果。 */
	case ClickHouseBucket: /* 处理当前分支。 */
		if s.clickhouse == nil { /* 判断条件并选择处理分支。 */
			return model.RawMessage{}, fmt.Errorf("clickhouse raw message store is not configured") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return s.clickhouse.GetRawMessage(ctx, index.TenantID, index.MessageID) /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		if s.legacy == nil { /* 判断条件并选择处理分支。 */
			return model.RawMessage{}, fmt.Errorf("legacy raw message reader is not configured") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return s.legacy.GetRaw(ctx, index) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Store) chooseBackend(ctx context.Context, value model.RawMessage) string { /* 定义 chooseBackend 函数。 */
	threshold := s.highFrequencyIntervalSec /* 更新 threshold 的值。 */
	configuredInterval := int64(0)          /* 更新 configuredInterval 的值。 */
	stateLastSeen := int64(0)               /* 更新 stateLastSeen 的值。 */
	if s.resolver != nil {                  /* 判断条件并选择处理分支。 */
		if state, err := s.resolver.GetDeviceState(ctx, value.TenantID, value.DeviceID); err == nil { /* 判断条件并选择处理分支。 */
			configuredInterval = state.ReportIntervalSec /* 更新 configuredInterval 的值。 */
			stateLastSeen = state.LastSeenAt             /* 更新 stateLastSeen 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	key := value.TenantID + "\x00" + value.DeviceID /* 更新 key 的值。 */
	s.mu.Lock()                                     /* 执行当前语句并推进处理流程。 */
	previous := s.last[key]                         /* 更新 previous 的值。 */
	s.last[key] = value.ReceivedAt                  /* 更新 s.last[key] 的值。 */
	s.mu.Unlock()                                   /* 执行当前语句并推进处理流程。 */

	if configuredInterval > 0 && configuredInterval <= threshold { /* 判断条件并选择处理分支。 */
		return ClickHouseBucket /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if isHighFrequency(value.ReceivedAt, stateLastSeen, threshold) || isHighFrequency(value.ReceivedAt, previous, threshold) { /* 判断条件并选择处理分支。 */
		return ClickHouseBucket /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return PostgreSQLBucket /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func isHighFrequency(current, previous, thresholdSec int64) bool { /* 定义 isHighFrequency 函数。 */
	if current <= previous || previous <= 0 || thresholdSec <= 0 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return current-previous <= thresholdSec*1000 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
