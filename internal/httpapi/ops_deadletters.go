package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// deadLetterBus is implemented by the Kafka bus; the local development bus
// has no dead-letter topics.
type deadLetterBus interface {
	DeadLetters(ctx context.Context, group string, limit int) (int64, []model.DeadLetter, error)
	ReplayDeadLetter(ctx context.Context, group string, partition int, offset int64) (model.DeadLetter, error)
}

const (
	deadLettersPath      = "/api/v1/ops/overview/dead-letters"
	deadLetterReplayPath = "/api/v1/ops/overview/dead-letters/:group/:partition/:offset/replay"
)

func (s *Server) deadLetterRoutes() {
	a := s.authorize("admin")
	s.router.GET(deadLettersPath, a, s.endpoint(s.opsDeadLetters))
	s.router.POST(deadLetterReplayPath, a, s.endpoint(s.opsReplayDeadLetter, "group", "partition", "offset"))
}

func (s *Server) deadLetterStore(w http.ResponseWriter) (deadLetterBus, bool) {
	// The engine wraps the adapter (message-topic routing); look through
	// wrappers for the Kafka bus.
	bus := s.engine.Bus
	for bus != nil {
		if store, ok := bus.(deadLetterBus); ok {
			return store, true
		}
		wrapper, ok := bus.(interface{ Unwrap() ports.EventBus })
		if !ok {
			break
		}
		bus = wrapper.Unwrap()
	}
	problem(w, 409, "当前部署未使用 Kafka，没有死信主题")
	return nil, false
}

// opsDeadLetters lists, per consumer group, the retained dead-letter count
// and the newest messages. Dead letters hold device data of every tenant, so
// the route is platform-level like the rest of the ops center.
func (s *Server) opsDeadLetters(w http.ResponseWriter, r *http.Request) {
	bus, ok := s.deadLetterStore(w)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	groups := []string{}
	if g := r.URL.Query().Get("group"); g != "" {
		if _, known := model.DeadLetterGroups[g]; !known {
			problem(w, 422, "未知的消费组")
			return
		}
		groups = append(groups, g)
	} else {
		for g := range model.DeadLetterGroups {
			groups = append(groups, g)
		}
		sort.Strings(groups)
	}
	type groupResult struct {
		Group string             `json:"group"`
		Total int64              `json:"total"`
		Items []model.DeadLetter `json:"items"`
		Error string             `json:"error,omitempty"`
	}
	out := []groupResult{}
	for _, g := range groups {
		total, items, err := bus.DeadLetters(r.Context(), g, limit)
		result := groupResult{Group: g, Total: total, Items: items}
		if err != nil {
			result.Error = "读取死信失败"
			if s.log != nil {
				s.log.WarnContext(r.Context(), "read dead letters", "group", g, "error", err)
			}
		}
		if result.Items == nil {
			result.Items = []model.DeadLetter{}
		}
		out = append(out, result)
	}
	write(w, 200, map[string]any{"groups": out})
}

// opsReplayDeadLetter republishes one dead letter to its group's source
// topic. Processing is idempotent per message, the dead letter is kept, and
// the replay is audited.
func (s *Server) opsReplayDeadLetter(w http.ResponseWriter, r *http.Request) {
	bus, ok := s.deadLetterStore(w)
	if !ok {
		return
	}
	group := r.PathValue("group")
	partition, partitionErr := strconv.Atoi(r.PathValue("partition"))
	offset, offsetErr := strconv.ParseInt(r.PathValue("offset"), 10, 64)
	if _, known := model.DeadLetterGroups[group]; !known || partitionErr != nil || offsetErr != nil || partition < 0 || offset < 0 {
		problem(w, 422, "死信位置无效")
		return
	}
	item, err := bus.ReplayDeadLetter(r.Context(), group, partition, offset)
	details := map[string]any{"group": group, "partition": partition, "offset": offset}
	if err != nil {
		details["error"] = err.Error()
		s.audit(r, "ops.dead_letter.replay.failed", "dead_letter", group, details)
		if errors.Is(err, model.ErrNotFound) {
			problem(w, 404, "死信不存在或已过保留期")
			return
		}
		problem(w, 502, "重新投递失败，请稍后重试")
		return
	}
	details["sourceTopic"], details["key"] = item.SourceTopic, item.Key
	s.audit(r, "ops.dead_letter.replay", "dead_letter", group, details)
	write(w, 202, map[string]any{"replayed": true, "item": item})
}
