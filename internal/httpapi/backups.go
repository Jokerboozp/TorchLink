package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var backupCredentialPattern = regexp.MustCompile(`(?i)(://[^/\s:@]+:)[^@\s/]+(@)`) /* 声明 backupCredentialPattern。 */

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) { /* 定义 listBackups 函数。 */
	pagination := parseListPagination(r)              /* 更新 pagination 的值。 */
	query := url.Values{}                             /* 更新 query 的值。 */
	for _, name := range []string{"type", "status"} { /* 循环处理当前数据。 */
		if value := strings.TrimSpace(r.URL.Query().Get(name)); value != "" { /* 判断条件并选择处理分支。 */
			query.Set(name, value) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	query.Set("limit", strconv.Itoa(pagination.PageSize))                           /* 执行当前语句并推进处理流程。 */
	query.Set("offset", strconv.Itoa(pagination.Offset))                            /* 执行当前语句并推进处理流程。 */
	s.proxyBackupJSON(w, r, http.MethodGet, "/backups", query, nil, 30*time.Second) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) getBackup(w http.ResponseWriter, r *http.Request) { /* 定义 getBackup 函数。 */
	id, err := backupPathSegment(r.PathValue("id"), "backup id") /* 更新 err 的值。 */
	if err != nil {                                              /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadRequest, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.proxyBackupJSON(w, r, http.MethodGet, "/backups/"+id, nil, nil, 30*time.Second) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) backupFiles(w http.ResponseWriter, r *http.Request) { /* 定义 backupFiles 函数。 */
	id, err := backupPathSegment(r.PathValue("id"), "backup id") /* 更新 err 的值。 */
	if err != nil {                                              /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadRequest, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pagination := parseListPagination(r)                                                         /* 更新 pagination 的值。 */
	query := url.Values{}                                                                        /* 更新 query 的值。 */
	query.Set("limit", strconv.Itoa(pagination.PageSize))                                        /* 执行当前语句并推进处理流程。 */
	query.Set("offset", strconv.Itoa(pagination.Offset))                                         /* 执行当前语句并推进处理流程。 */
	s.proxyBackupJSON(w, r, http.MethodGet, "/backups/"+id+"/files", query, nil, 30*time.Second) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) downloadBackupFile(w http.ResponseWriter, r *http.Request) { /* 定义 downloadBackupFile 函数。 */
	id, err := backupPathSegment(r.PathValue("id"), "backup id") /* 更新 err 的值。 */
	if err != nil {                                              /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadRequest, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	filename, err := backupPathSegment(r.PathValue("filename"), "artifact filename") /* 更新 err 的值。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadRequest, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.proxyBackupDownload(w, r, "/backups/"+id+"/files/"+filename) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) runBackup(w http.ResponseWriter, r *http.Request) { /* 定义 runBackup 函数。 */
	kind := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type"))) /* 更新 kind 的值。 */
	if r.Body != nil {                                                    /* 判断条件并选择处理分支。 */
		body, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20)) /* 更新 readErr 的值。 */
		if readErr != nil {                                                /* 判断条件并选择处理分支。 */
			problem(w, http.StatusBadRequest, "invalid backup request") /* 执行当前语句并推进处理流程。 */
			return                                                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(bytes.TrimSpace(body)) > 0 { /* 判断条件并选择处理分支。 */
			var input struct { /* 声明 input。 */
				Type string `json:"type"` /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if err := json.Unmarshal(body, &input); err != nil { /* 判断条件并选择处理分支。 */
				problem(w, http.StatusBadRequest, "invalid backup request: "+err.Error()) /* 执行当前语句并推进处理流程。 */
				return                                                                    /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if strings.TrimSpace(input.Type) != "" { /* 判断条件并选择处理分支。 */
				kind = strings.ToUpper(strings.TrimSpace(input.Type)) /* 更新 kind 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if kind != "DEVICE_DAILY" && kind != "FULL" && kind != "INCREMENTAL" && kind != "RAW_LOGS" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "type must be FULL or DEVICE_DAILY") /* 执行当前语句并推进处理流程。 */
		return                                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	query := url.Values{"type": []string{kind}}                                          /* 更新 query 的值。 */
	if s.proxyBackupJSON(w, r, http.MethodPost, "/backup", query, nil, 15*time.Minute) { /* 判断条件并选择处理分支。 */
		s.audit(r, "backup.run", "backup", kind, map[string]any{"type": kind}) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) { /* 定义 restoreBackup 函数。 */
	id, err := backupPathSegment(r.PathValue("id"), "backup id") /* 更新 err 的值。 */
	if err != nil {                                              /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadRequest, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	query := url.Values{"backupId": []string{id}}                                               /* 更新 query 的值。 */
	if s.proxyBackupJSON(w, r, http.MethodPost, "/restore/drill", query, nil, 15*time.Minute) { /* 判断条件并选择处理分支。 */
		s.audit(r, "backup.restore_drill", "backup", id, map[string]any{"backupId": id}) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// proxyBackupJSON keeps the backup admin token on the server side. Browsers
// only receive the platform API response and never connect to backup-service
// directly.
func (s *Server) proxyBackupJSON(w http.ResponseWriter, r *http.Request, method, path string, query url.Values, body io.Reader, timeout time.Duration) bool { /* 定义 proxyBackupJSON 函数。 */
	response, ok := s.callBackup(w, r, method, path, query, body, timeout) /* 更新 ok 的值。 */
	if !ok {                                                               /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer response.Body.Close()       /* 安排函数结束时执行清理。 */
	if response.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		s.backupUpstreamProblem(w, response) /* 执行当前语句并推进处理流程。 */
		return false                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	w.Header().Set("Content-Type", response.Header.Get("Content-Type")) /* 执行当前语句并推进处理流程。 */
	if w.Header().Get("Content-Type") == "" {                           /* 判断条件并选择处理分支。 */
		w.Header().Set("Content-Type", "application/json; charset=utf-8") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	w.Header().Set("Cache-Control", "no-store") /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(response.StatusCode)          /* 执行当前语句并推进处理流程。 */
	_, _ = io.Copy(w, response.Body)            /* 更新 _ 的值。 */
	return true                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) proxyBackupDownload(w http.ResponseWriter, r *http.Request, path string) { /* 定义 proxyBackupDownload 函数。 */
	response, ok := s.callBackup(w, r, http.MethodGet, path, nil, nil, 15*time.Minute) /* 更新 ok 的值。 */
	if !ok {                                                                           /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer response.Body.Close()       /* 安排函数结束时执行清理。 */
	if response.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		s.backupUpstreamProblem(w, response) /* 执行当前语句并推进处理流程。 */
		return                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, header := range []string{"Content-Type", "Content-Length", "Content-Disposition", "X-Checksum-SHA256"} { /* 循环处理当前数据。 */
		if value := response.Header.Get(header); value != "" { /* 判断条件并选择处理分支。 */
			w.Header().Set(header, value) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	w.Header().Set("Cache-Control", "no-store") /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(response.StatusCode)          /* 执行当前语句并推进处理流程。 */
	_, _ = io.Copy(w, response.Body)            /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) callBackup(w http.ResponseWriter, r *http.Request, method, path string, query url.Values, body io.Reader, timeout time.Duration) (*http.Response, bool) { /* 定义 callBackup 函数。 */
	if strings.TrimSpace(s.cfg.BackupURL) == "" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "backup service is not configured") /* 执行当前语句并推进处理流程。 */
		return nil, false                                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(s.cfg.BackupToken) == "" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "backup service token is not configured") /* 执行当前语句并推进处理流程。 */
		return nil, false                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	base, err := url.Parse(s.cfg.BackupURL)                 /* 更新 err 的值。 */
	if err != nil || base.Scheme == "" || base.Host == "" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "backup service URL is invalid") /* 执行当前语句并推进处理流程。 */
		return nil, false                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	base.Path = strings.TrimRight(base.Path, "/") + path                                 /* 更新 base.Path 的值。 */
	base.RawQuery = query.Encode()                                                       /* 更新 base.RawQuery 的值。 */
	request, err := http.NewRequestWithContext(r.Context(), method, base.String(), body) /* 更新 err 的值。 */
	if err != nil {                                                                      /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadGateway, "could not create backup service request") /* 执行当前语句并推进处理流程。 */
		return nil, false                                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	request.Header.Set("Authorization", "Bearer "+s.cfg.BackupToken) /* 执行当前语句并推进处理流程。 */
	if body != nil {                                                 /* 判断条件并选择处理分支。 */
		request.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	client := &http.Client{Timeout: timeout} /* 更新 client 的值。 */
	response, err := client.Do(request)      /* 更新 err 的值。 */
	if err != nil {                          /* 判断条件并选择处理分支。 */
		if r.Context().Err() != nil { /* 判断条件并选择处理分支。 */
			problem(w, http.StatusGatewayTimeout, "backup service request timed out") /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			problem(w, http.StatusBadGateway, "backup service is unavailable") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return nil, false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return response, true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) backupUpstreamProblem(w http.ResponseWriter, response *http.Response) { /* 定义 backupUpstreamProblem 函数。 */
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10)) /* 更新 _ 的值。 */
	detail := strings.TrimSpace(string(data))                   /* 更新 detail 的值。 */
	var payload struct {                                        /* 声明 payload。 */
		Error   string `json:"error"`   /* 执行当前语句并推进处理流程。 */
		Detail  string `json:"detail"`  /* 执行当前语句并推进处理流程。 */
		Message string `json:"message"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if json.Unmarshal(data, &payload) == nil { /* 判断条件并选择处理分支。 */
		for _, candidate := range []string{payload.Error, payload.Detail, payload.Message} { /* 循环处理当前数据。 */
			if strings.TrimSpace(candidate) != "" { /* 判断条件并选择处理分支。 */
				detail = strings.TrimSpace(candidate) /* 更新 detail 的值。 */
				break                                 /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	detail = sanitizeBackupErrorDetail(detail) /* 更新 detail 的值。 */
	if detail == "" {                          /* 判断条件并选择处理分支。 */
		detail = "upstream service did not provide an error detail" /* 更新 detail 的值。 */
	} /* 结束当前表达式或代码块。 */
	problem(w, http.StatusBadGateway, fmt.Sprintf("backup service returned HTTP %d: %s", response.StatusCode, detail)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func sanitizeBackupErrorDetail(value string) string { /* 定义 sanitizeBackupErrorDetail 函数。 */
	value = strings.Join(strings.Fields(value), " ")                       /* 更新 value 的值。 */
	value = backupCredentialPattern.ReplaceAllString(value, `${1}***${2}`) /* 更新 value 的值。 */
	if len(value) > 512 {                                                  /* 判断条件并选择处理分支。 */
		value = value[:512] + "…" /* 更新 value 的值。 */
	} /* 结束当前表达式或代码块。 */
	return value /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func backupPathSegment(value, label string) (string, error) { /* 定义 backupPathSegment 函数。 */
	value = strings.TrimSpace(value)                                                       /* 更新 value 的值。 */
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("invalid %s", label) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return url.PathEscape(value), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
