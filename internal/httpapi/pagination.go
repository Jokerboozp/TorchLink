package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

type listPagination struct {
	Page     int
	PageSize int
	Offset   int
}

// parseListPagination accepts the new page/pageSize contract and the legacy
// limit/offset pair used by older clients. The server always caps one request
// so a caller cannot accidentally turn a list endpoint back into a full-table
// query.
func parseListPagination(r *http.Request) listPagination {
	query := r.URL.Query()
	pageSize := intval(query.Get("pageSize"), 0)
	if pageSize <= 0 {
		pageSize = intval(query.Get("limit"), defaultPageSize)
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	// Leave room for offset + pageSize and offset/pageSize + 1. Oversized
	// pages remain out-of-range pages instead of wrapping back to the start.
	maxOffset := int(^uint(0)>>1) - pageSize

	pageNumber := paginationNumber(query.Get("page"))
	if pageNumber > 0 {
		if maxPage := maxOffset/pageSize + 1; pageNumber > maxPage {
			pageNumber = maxPage
		}
		return listPagination{Page: pageNumber, PageSize: pageSize, Offset: (pageNumber - 1) * pageSize}
	}

	offset := paginationNumber(query.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	return listPagination{Page: offset/pageSize + 1, PageSize: pageSize, Offset: offset}
}

func paginationNumber(value string) int {
	value = strings.TrimSpace(value)
	number, err := strconv.Atoi(value)
	if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(value, "-") {
		return int(^uint(0) >> 1)
	}
	if err != nil {
		return 0
	}
	return number
}

func writeList(w http.ResponseWriter, status int, items any, total int, pagination listPagination, extra map[string]any) {
	response := map[string]any{
		"items":    items,
		"count":    total,
		"total":    total,
		"page":     pagination.Page,
		"pageSize": pagination.PageSize,
		"limit":    pagination.PageSize,
		"offset":   pagination.Offset,
		"pagination": map[string]int{
			"page":     pagination.Page,
			"pageSize": pagination.PageSize,
			"total":    total,
		},
	}
	for key, value := range extra {
		response[key] = value
	}
	write(w, status, response)
}

func pageItems[T any](items []T, pagination listPagination) ([]T, int) {
	total := len(items)
	if pagination.Offset < 0 || pagination.Offset >= total || pagination.PageSize <= 0 {
		return []T{}, total
	}
	end := total
	if pagination.PageSize < total-pagination.Offset {
		end = pagination.Offset + pagination.PageSize
	}
	return items[pagination.Offset:end], total
}
