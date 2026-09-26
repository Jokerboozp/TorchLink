package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

func TestParseListPaginationSupportsPageAndLegacyOffset(t *testing.T) {
	tests := []struct {
		name                string
		query               string
		page, pageSize, off int
	}{
		{name: "page contract", query: "page=3&pageSize=50", page: 3, pageSize: 50, off: 100},
		{name: "legacy contract", query: "limit=7&offset=14", page: 3, pageSize: 7, off: 14},
		{name: "server cap", query: "page=2&pageSize=1000", page: 2, pageSize: maxPageSize, off: maxPageSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/products?"+tt.query, nil)
			got := parseListPagination(r)
			if got.Page != tt.page || got.PageSize != tt.pageSize || got.Offset != tt.off {
				t.Fatalf("pagination=%+v, want page=%d pageSize=%d offset=%d", got, tt.page, tt.pageSize, tt.off)
			}
		})
	}
}

func TestMemoryProductPaginationReturnsPageAndTotal(t *testing.T) {
	repo := memory.NewRepository()
	for index := 0; index < 3; index++ {
		if err := repo.SaveProduct(context.Background(), model.Product{TenantID: "tenant_001", ID: "product_" + string(rune('a'+index)), UpdatedAt: int64(index + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := repo.ListProductsPage(context.Background(), "tenant_001", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 2 {
		t.Fatalf("page len=%d total=%d, want len=2 total=3", len(items), total)
	}
}

func TestMemoryPropertyHistoryPaginationKeepsLatestPageChronological(t *testing.T) {
	repo := memory.NewRepository()
	for index := 1; index <= 3; index++ {
		if err := repo.SaveStandardMessage(context.Background(), model.StandardMessage{
			TenantID: "tenant_001", DeviceID: "device_001", MessageID: "message_" + string(rune('0'+index)), Timestamp: int64(index),
			Properties: map[string]any{"temperature": index},
		}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := repo.PropertyHistoryPage(context.Background(), "tenant_001", "device_001", "temperature", 0, 0, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 2 || items[0]["timestamp"] != int64(2) || items[1]["timestamp"] != int64(3) {
		t.Fatalf("history page=%#v total=%d, want timestamps 2,3 and total 3", items, total)
	}
}
