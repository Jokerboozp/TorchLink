package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

func TestOversizedPaginationThroughHTTP(t *testing.T) {
	repo := memory.NewRepository()
	if err := repo.SaveProduct(context.Background(), model.Product{ID: "first-product", TenantID: "tenant-a"}); err != nil {
		t.Fatal(err)
	}
	api := New(config.Config{JWTSecret: "audit-only-secret-at-least-32-characters"}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	token, err := api.auth.Issue("audit", "tenant-a", "viewer", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/ai/providers", "/api/v1/products"} {
		for _, query := range []string{"page=1&pageSize=20", "page=9223372036854775807&pageSize=20", "page=999999999999999999999999999999&pageSize=100", "offset=9223372036854775807&limit=1"} {
			r := httptest.NewRequest(http.MethodGet, path+"?"+query, nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			api.Handler().ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("%s?%s: status=%d", path, query, w.Code)
			}
			var result struct {
				Items []json.RawMessage `json:"items"`
				Total int               `json:"total"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if query != "page=1&pageSize=20" && len(result.Items) != 0 {
				t.Errorf("%s?%s: oversized page returned first-page data", path, query)
			}
			if path == "/api/v1/products" && (result.Total != 1 || (query == "page=1&pageSize=20" && len(result.Items) != 1)) {
				t.Errorf("product pagination lost total or normal page: %+v", result)
			}
		}
	}
}

func TestPaginationArithmeticStaysInRange(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, size := range []int{1, 20, 100} {
		for _, param := range []string{"page", "offset"} {
			r := httptest.NewRequest(http.MethodGet, "/?"+param+"="+strconv.Itoa(maxInt)+"&pageSize="+strconv.Itoa(size), nil)
			p := parseListPagination(r)
			if p.Page < 1 || p.Offset < 0 || p.Offset > maxInt-p.PageSize {
				t.Fatalf("unsafe pagination: %+v", p)
			}
		}
	}
}

func TestPageItemsHandlesInvalidAndOverflowingBounds(t *testing.T) {
	for _, p := range []listPagination{{Offset: -1, PageSize: 20}, {Offset: 0, PageSize: -1}, {Offset: 1, PageSize: int(^uint(0) >> 1)}} {
		items, total := pageItems([]int{1, 2, 3}, p)
		if total != 3 {
			t.Fatalf("total=%d", total)
		}
		if p.Offset < 0 || p.PageSize < 0 {
			if len(items) != 0 {
				t.Fatalf("invalid bounds returned data: %v", items)
			}
		} else if len(items) != 2 || items[0] != 2 {
			t.Fatalf("overflowing size lost remaining items: %v", items)
		}
	}
}
