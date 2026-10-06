package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"iot-platform/internal/ports"
)

// aiQuota admits AI runs against the tenant's per-minute run budget and daily
// token budget (IOT_AI_RUNS_PER_MINUTE, IOT_AI_DAILY_TOKEN_BUDGET). Both are
// off when zero. The run budget is shared across replicas through the
// cluster limiter; the token total is read from AI run records and cached
// briefly, so a tenant may overshoot by the runs started within that window.
type aiQuota struct {
	server *Server
	mu     sync.Mutex
	usage  map[string]cachedAIUsage
	now    func() time.Time
}

type cachedAIUsage struct {
	day    string
	tokens int64
	at     time.Time
}

const aiUsageCacheTTL = 30 * time.Second

func (q *aiQuota) AdmitAIRun(ctx context.Context, tenantID string) error {
	cfg := q.server.cfg
	if cfg.AIRunsPerMinute > 0 && q.server.onboarding != nil {
		allowed, err := q.server.onboarding.Limiter.Allow(ctx, "ai-runs\x00"+tenantID, int(cfg.AIRunsPerMinute), time.Minute)
		if err == nil && !allowed {
			return ports.AIRejected(http.StatusTooManyRequests, fmt.Sprintf("本租户每分钟最多发起 %d 次 AI 任务，请稍后重试", cfg.AIRunsPerMinute))
		}
	}
	if cfg.AIDailyTokenBudget <= 0 || q.server.engine.AIRuns == nil {
		return nil
	}
	used, err := q.tokensToday(ctx, tenantID)
	if err != nil {
		// Without usage records the budget cannot be judged; do not block
		// the feature on a reporting outage.
		return nil
	}
	if used >= cfg.AIDailyTokenBudget {
		return ports.AIRejected(http.StatusTooManyRequests, fmt.Sprintf("本租户今日 AI 用量已达上限（%d 词元），明日自动恢复", cfg.AIDailyTokenBudget))
	}
	return nil
}

func (q *aiQuota) tokensToday(ctx context.Context, tenantID string) (int64, error) {
	now := q.now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := start.Format(time.DateOnly)
	q.mu.Lock()
	cached, ok := q.usage[tenantID]
	q.mu.Unlock()
	if ok && cached.day == day && now.Sub(cached.at) < aiUsageCacheTTL {
		return cached.tokens, nil
	}
	rows, err := q.server.engine.AIRuns.AIRunUsage(ctx, ports.AIRunFilter{TenantID: tenantID, Start: start.UnixMilli(), End: now.UnixMilli()})
	if err != nil {
		return 0, err
	}
	var total int64
	for _, row := range rows {
		total += row.Usage.InputTokens + row.Usage.OutputTokens
	}
	q.mu.Lock()
	if q.usage == nil || len(q.usage) > 4096 {
		q.usage = map[string]cachedAIUsage{}
	}
	q.usage[tenantID] = cachedAIUsage{day: day, tokens: total, at: now}
	q.mu.Unlock()
	return total, nil
}
