package opscenter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Saved queries, query history and dashboard favorites belong to one account
// in one tenant. The tenant and username always come from the verified token.

const (
	kindSavedQuery = "saved_query"
	kindHistory    = "history"
	kindFavorite   = "favorite"
	historyLimit   = 100
	savedLimit     = 200
)

var queryLanguages = map[string]bool{"promql": true, "logql": true, "logfilter": true}

// ErrNoPreferences means no preference storage is wired (HTTP 503).
var ErrNoPreferences = errors.New("preference storage unavailable")

func (s *Service) prefs() (ports.OpsPreferenceStore, error) {
	if s.Prefs == nil {
		return nil, ErrNoPreferences
	}
	return s.Prefs, nil
}

type SavedQueryInput struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Language    string         `json:"language"`
	Query       string         `json:"query"`
	Filter      map[string]any `json:"filter"`
	Description string         `json:"description"`
}

func (s *Service) SavedQueries(ctx context.Context, tenant, user, language string) ([]model.OpsUserItem, error) {
	store, err := s.prefs()
	if err != nil {
		return nil, err
	}
	items, err := store.ListOpsItems(ctx, tenant, user, kindSavedQuery, savedLimit)
	return filterLanguage(items, language), err
}

func filterLanguage(items []model.OpsUserItem, language string) []model.OpsUserItem {
	if language == "" {
		return items
	}
	out := []model.OpsUserItem{}
	for _, item := range items {
		if item.Body["language"] == language {
			out = append(out, item)
		}
	}
	return out
}

func (s *Service) SaveQuery(ctx context.Context, tenant, user string, in SavedQueryInput) (model.OpsUserItem, error) {
	store, err := s.prefs()
	if err != nil {
		return model.OpsUserItem{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 100 {
		return model.OpsUserItem{}, invalid("name", "名称为 1～100 个字符")
	}
	if !queryLanguages[in.Language] {
		return model.OpsUserItem{}, invalid("language", "不支持的查询类型")
	}
	if in.Language == "logfilter" {
		if in.Filter == nil {
			return model.OpsUserItem{}, invalid("filter", "缺少筛选条件")
		}
	} else if err := checkQueryText("query", in.Query); err != nil {
		return model.OpsUserItem{}, err
	}
	if len([]rune(in.Description)) > 500 {
		return model.OpsUserItem{}, invalid("description", "说明不能超过 500 字")
	}
	existing, err := store.ListOpsItems(ctx, tenant, user, kindSavedQuery, savedLimit+1)
	if err != nil {
		return model.OpsUserItem{}, err
	}
	now := s.now().UnixMilli()
	item := model.OpsUserItem{TenantID: tenant, Username: user, Kind: kindSavedQuery, ID: in.ID, Name: in.Name, Body: map[string]any{"language": in.Language, "query": in.Query, "filter": in.Filter, "description": in.Description}, CreatedAt: now, UpdatedAt: now}
	if item.ID == "" {
		if len(existing) >= savedLimit {
			return model.OpsUserItem{}, invalid("name", "每个账户最多保存 %d 条查询", savedLimit)
		}
		item.ID = "q_" + randomID()
	} else {
		found := false
		for _, e := range existing {
			if e.ID == item.ID {
				found, item.CreatedAt = true, e.CreatedAt
			}
		}
		if !found {
			return model.OpsUserItem{}, ports.ErrOpsNotFound
		}
	}
	return item, store.SaveOpsItem(ctx, item)
}

func (s *Service) DeleteSavedQuery(ctx context.Context, tenant, user, id string) error {
	store, err := s.prefs()
	if err != nil {
		return err
	}
	ok, err := store.DeleteOpsItem(ctx, tenant, user, kindSavedQuery, id)
	if err == nil && !ok {
		return ports.ErrOpsNotFound
	}
	return err
}

// RecordHistory keeps the most recent distinct queries; repeating a query
// moves it to the top instead of adding a duplicate.
func (s *Service) RecordHistory(ctx context.Context, tenant, user, language, query string, filter map[string]any) {
	store, err := s.prefs()
	if err != nil || !queryLanguages[language] {
		return
	}
	key := language + "\x00" + query
	if filter != nil {
		key += "\x00" + canonicalJSON(filter)
	}
	sum := sha256.Sum256([]byte(key))
	now := s.now().UnixMilli()
	item := model.OpsUserItem{TenantID: tenant, Username: user, Kind: kindHistory, ID: "h_" + hex.EncodeToString(sum[:10]), Body: map[string]any{"language": language, "query": query, "filter": filter}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveOpsItem(ctx, item); err == nil {
		_ = store.TrimOpsItems(ctx, tenant, user, kindHistory, historyLimit)
	}
}

func (s *Service) History(ctx context.Context, tenant, user, language string) ([]model.OpsUserItem, error) {
	store, err := s.prefs()
	if err != nil {
		return nil, err
	}
	items, err := store.ListOpsItems(ctx, tenant, user, kindHistory, historyLimit)
	return filterLanguage(items, language), err
}

func (s *Service) ClearHistory(ctx context.Context, tenant, user string) error {
	store, err := s.prefs()
	if err != nil {
		return err
	}
	return store.TrimOpsItems(ctx, tenant, user, kindHistory, 0)
}

func (s *Service) favoriteSet(ctx context.Context, tenant, user string) (map[string]bool, error) {
	out := map[string]bool{}
	store, err := s.prefs()
	if err != nil {
		return out, nil
	}
	items, err := store.ListOpsItems(ctx, tenant, user, kindFavorite, 1000)
	if err != nil {
		return out, err
	}
	for _, item := range items {
		out[item.ID] = true
	}
	return out, nil
}

func (s *Service) SetFavorite(ctx context.Context, tenant, user, uid string, favorite bool) error {
	store, err := s.prefs()
	if err != nil {
		return err
	}
	if !dashboardUIDPattern.MatchString(uid) {
		return ports.ErrOpsNotFound
	}
	if !favorite {
		_, err := store.DeleteOpsItem(ctx, tenant, user, kindFavorite, uid)
		return err
	}
	if err := requireBackend(s.Dashboards); err != nil {
		return err
	}
	dash, _, err := s.Dashboards.GetDashboard(ctx, uid)
	if err != nil {
		return err
	}
	now := s.now().UnixMilli()
	return store.SaveOpsItem(ctx, model.OpsUserItem{TenantID: tenant, Username: user, Kind: kindFavorite, ID: uid, Name: strv(dash["title"]), Body: map[string]any{}, CreatedAt: now, UpdatedAt: now})
}
