package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Admin uses dedicated EMQX API credentials. Errors never include response bodies
// or authentication material. Ban first, then kick, so stale JWTs cannot reconnect.
type Admin struct { /* 定义 Admin 类型。 */
	URL, Key, Secret string       /* 执行当前语句并推进处理流程。 */
	Client           *http.Client /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (a *Admin) request(ctx context.Context, method, path string, body any, out any) (int, error) { /* 定义 request 函数。 */
	base, e := url.Parse(a.URL)                                                                                                                             /* 更新 e 的值。 */
	if e != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" { /* 判断条件并选择处理分支。 */
		return 0, errors.New("invalid EMQX administration URL") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, e := json.Marshal(body) /* 更新 e 的值。 */
	if e != nil {              /* 判断条件并选择处理分支。 */
		return 0, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.URL, "/")+"/api/v5"+path, bytes.NewReader(b)) /* 更新 e 的值。 */
	if e != nil {                                                                                                       /* 判断条件并选择处理分支。 */
		return 0, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.SetBasicAuth(a.Key, a.Secret)                  /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	c := http.Client{Timeout: 10 * time.Second}        /* 更新 c 的值。 */
	if a.Client != nil {                               /* 判断条件并选择处理分支。 */
		c = *a.Client /* 更新 c 的值。 */
	} /* 结束当前表达式或代码块。 */
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse } /* 更新 c.CheckRedirect 的值。 */
	res, e := c.Do(req)                                                                             /* 更新 e 的值。 */
	if e != nil {                                                                                   /* 判断条件并选择处理分支。 */
		return 0, errors.New("EMQX administration request failed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer res.Body.Close()                             /* 安排函数结束时执行清理。 */
	if res.StatusCode >= 200 && res.StatusCode < 300 { /* 判断条件并选择处理分支。 */
		if out != nil { /* 判断条件并选择处理分支。 */
			e = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out) /* 更新 e 的值。 */
		} /* 结束当前表达式或代码块。 */
		return res.StatusCode, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return res.StatusCode, fmt.Errorf("EMQX administration HTTP %d", res.StatusCode) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (a *Admin) RevokeUsername(ctx context.Context, username string) error { /* 定义 RevokeUsername 函数。 */
	if username == "" { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if a.Key == "" || a.Secret == "" { /* 判断条件并选择处理分支。 */
		return errors.New("EMQX API credentials unavailable") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	code, e := a.request(ctx, "POST", "/banned", map[string]any{"as": "username", "who": username, "by": "iot-platform", "reason": "device credential revoked", "until": "infinity"}, nil) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                                          /* 判断条件并选择处理分支。 */
		if code != 400 && code != 409 { /* 判断条件并选择处理分支。 */
			return e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var existing struct { /* 声明 existing。 */
			Data []struct { /* 执行当前语句并推进处理流程。 */
				As    string `json:"as"`    /* 执行当前语句并推进处理流程。 */
				Who   string `json:"who"`   /* 执行当前语句并推进处理流程。 */
				Until string `json:"until"` /* 执行当前语句并推进处理流程。 */
			} `json:"data"` /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		_, err := a.request(ctx, "GET", "/banned?username="+url.QueryEscape(username), nil, &existing) /* 检查错误并决定后续处理。 */
		if err != nil {                                                                                /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		found := false                    /* 更新 found 的值。 */
		for _, v := range existing.Data { /* 循环处理当前数据。 */
			if v.As == "username" && v.Who == username && v.Until == "infinity" { /* 判断条件并选择处理分支。 */
				found = true /* 更新 found 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if !found { /* 判断条件并选择处理分支。 */
			return e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for page := 0; page < 100; page++ { /* 循环处理当前数据。 */
		var clients struct { /* 声明 clients。 */
			Data []struct { /* 执行当前语句并推进处理流程。 */
				ID       string `json:"clientid"` /* 执行当前语句并推进处理流程。 */
				Username string `json:"username"` /* 执行当前语句并推进处理流程。 */
			} `json:"data"` /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		_, e = a.request(ctx, "GET", "/clients?username="+url.QueryEscape(username)+"&page=1&limit=100", nil, &clients) /* 更新 e 的值。 */
		if e != nil {                                                                                                   /* 判断条件并选择处理分支。 */
			return e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(clients.Data) == 0 { /* 判断条件并选择处理分支。 */
			return nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, c := range clients.Data { /* 循环处理当前数据。 */
			if c.Username != username || c.ID == "" { /* 判断条件并选择处理分支。 */
				return errors.New("EMQX returned an unexpected client identity") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			code, e = a.request(ctx, "DELETE", "/clients/"+url.PathEscape(c.ID), nil, nil) /* 更新 e 的值。 */
			if e != nil && code != 404 {                                                   /* 判断条件并选择处理分支。 */
				return e /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return errors.New("EMQX client cleanup limit reached") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
