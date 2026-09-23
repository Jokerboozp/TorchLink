package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"errors"   /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"strconv"  /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	defaultPageSize = 20  /* 更新 defaultPageSize 的值。 */
	maxPageSize     = 100 /* 更新 maxPageSize 的值。 */
) /* 结束当前表达式或代码块。 */

type listPagination struct { /* 定义 listPagination 类型。 */
	Page     int /* 执行当前语句并推进处理流程。 */
	PageSize int /* 执行当前语句并推进处理流程。 */
	Offset   int /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// parseListPagination accepts the new page/pageSize contract and the legacy
// limit/offset pair used by older clients. The server always caps one request
// so a caller cannot accidentally turn a list endpoint back into a full-table
// query.
func parseListPagination(r *http.Request) listPagination { /* 定义 parseListPagination 函数。 */
	query := r.URL.Query()                       /* 更新 query 的值。 */
	pageSize := intval(query.Get("pageSize"), 0) /* 更新 pageSize 的值。 */
	if pageSize <= 0 {                           /* 判断条件并选择处理分支。 */
		pageSize = intval(query.Get("limit"), defaultPageSize) /* 更新 pageSize 的值。 */
	} /* 结束当前表达式或代码块。 */
	if pageSize <= 0 { /* 判断条件并选择处理分支。 */
		pageSize = defaultPageSize /* 更新 pageSize 的值。 */
	} /* 结束当前表达式或代码块。 */
	if pageSize > maxPageSize { /* 判断条件并选择处理分支。 */
		pageSize = maxPageSize /* 更新 pageSize 的值。 */
	} /* 结束当前表达式或代码块。 */
	// Leave room for offset + pageSize and offset/pageSize + 1. Oversized
	// pages remain out-of-range pages instead of wrapping back to the start.
	maxOffset := int(^uint(0)>>1) - pageSize /* 更新 maxOffset 的值。 */

	pageNumber := paginationNumber(query.Get("page")) /* 更新 pageNumber 的值。 */
	if pageNumber > 0 {                               /* 判断条件并选择处理分支。 */
		if maxPage := maxOffset/pageSize + 1; pageNumber > maxPage { /* 判断条件并选择处理分支。 */
			pageNumber = maxPage /* 更新 pageNumber 的值。 */
		} /* 结束当前表达式或代码块。 */
		return listPagination{Page: pageNumber, PageSize: pageSize, Offset: (pageNumber - 1) * pageSize} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	offset := paginationNumber(query.Get("offset")) /* 更新 offset 的值。 */
	if offset < 0 {                                 /* 判断条件并选择处理分支。 */
		offset = 0 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	if offset > maxOffset { /* 判断条件并选择处理分支。 */
		offset = maxOffset /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	return listPagination{Page: offset/pageSize + 1, PageSize: pageSize, Offset: offset} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func paginationNumber(value string) int { /* 定义 paginationNumber 函数。 */
	value = strings.TrimSpace(value)                                        /* 更新 value 的值。 */
	number, err := strconv.Atoi(value)                                      /* 更新 err 的值。 */
	if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(value, "-") { /* 判断条件并选择处理分支。 */
		return int(^uint(0) >> 1) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return number /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func writeList(w http.ResponseWriter, status int, items any, total int, pagination listPagination, extra map[string]any) { /* 定义 writeList 函数。 */
	response := map[string]any{ /* 更新 response 的值。 */
		"items":    items,               /* 执行当前语句并推进处理流程。 */
		"count":    total,               /* 执行当前语句并推进处理流程。 */
		"total":    total,               /* 执行当前语句并推进处理流程。 */
		"page":     pagination.Page,     /* 执行当前语句并推进处理流程。 */
		"pageSize": pagination.PageSize, /* 执行当前语句并推进处理流程。 */
		"limit":    pagination.PageSize, /* 执行当前语句并推进处理流程。 */
		"offset":   pagination.Offset,   /* 执行当前语句并推进处理流程。 */
		"pagination": map[string]int{ /* 执行当前语句并推进处理流程。 */
			"page":     pagination.Page,     /* 执行当前语句并推进处理流程。 */
			"pageSize": pagination.PageSize, /* 执行当前语句并推进处理流程。 */
			"total":    total,               /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for key, value := range extra { /* 循环处理当前数据。 */
		response[key] = value /* 更新 response[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	write(w, status, response) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func pageItems[T any](items []T, pagination listPagination) ([]T, int) { /* 定义 pageItems 函数。 */
	total := len(items)                                                                  /* 更新 total 的值。 */
	if pagination.Offset < 0 || pagination.Offset >= total || pagination.PageSize <= 0 { /* 判断条件并选择处理分支。 */
		return []T{}, total /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	end := total                                       /* 更新 end 的值。 */
	if pagination.PageSize < total-pagination.Offset { /* 判断条件并选择处理分支。 */
		end = pagination.Offset + pagination.PageSize /* 更新 end 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items[pagination.Offset:end], total /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
