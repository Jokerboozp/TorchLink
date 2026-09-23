package video /* 声明 video 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"           /* 执行当前语句并推进处理流程。 */
	"context"         /* 执行当前语句并推进处理流程。 */
	"crypto/hmac"     /* 执行当前语句并推进处理流程。 */
	"crypto/md5"      /* 执行当前语句并推进处理流程。 */
	"crypto/rand"     /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"   /* 执行当前语句并推进处理流程。 */
	"encoding/base64" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"io"              /* 执行当前语句并推进处理流程。 */
	"net/http"        /* 执行当前语句并推进处理流程。 */
	"path"            /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const hikvisionPreviewPath = "/artemis/api/video/v2/cameras/previewURLs" /* 声明 hikvisionPreviewPath。 */

// HikvisionArtemisConfig contains the credentials for HikCentral Professional
// OpenAPI. The credentials belong to the platform deployment, not to a camera
// row, and must be injected from a secret store or environment.
type HikvisionArtemisConfig struct { /* 定义 HikvisionArtemisConfig 类型。 */
	BaseURL    string       /* 执行当前语句并推进处理流程。 */
	AppKey     string       /* 执行当前语句并推进处理流程。 */
	AppSecret  string       /* 执行当前语句并推进处理流程。 */
	HTTPClient *http.Client /* 执行当前语句并推进处理流程。 */
	StreamType int          /* 执行当前语句并推进处理流程。 */
	Protocol   string       /* 执行当前语句并推进处理流程。 */
	Transmode  int          /* 执行当前语句并推进处理流程。 */
	Expand     string       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// HikvisionArtemis is an in-process Go client for the official Hikvision
// Artemis OpenAPI. It signs the previewURLs request directly; it does not call
// a Java bridge, a local compatibility service, or a generic SDK sidecar.
type HikvisionArtemis struct { /* 定义 HikvisionArtemis 类型。 */
	baseURL    string       /* 执行当前语句并推进处理流程。 */
	appKey     string       /* 执行当前语句并推进处理流程。 */
	appSecret  string       /* 执行当前语句并推进处理流程。 */
	client     *http.Client /* 执行当前语句并推进处理流程。 */
	streamType int          /* 执行当前语句并推进处理流程。 */
	protocol   string       /* 执行当前语句并推进处理流程。 */
	transmode  int          /* 执行当前语句并推进处理流程。 */
	expand     string       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewHikvisionArtemis(config HikvisionArtemisConfig) (*HikvisionArtemis, error) { /* 定义 NewHikvisionArtemis 函数。 */
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/") /* 更新 baseURL 的值。 */
	if _, err := absoluteHTTPURL(baseURL); err != nil {                  /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid Hikvision Artemis API URL: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(config.AppKey) == "" || strings.TrimSpace(config.AppSecret) == "" { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Hikvision Artemis app key and app secret are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	client := config.HTTPClient /* 更新 client 的值。 */
	if client == nil {          /* 判断条件并选择处理分支。 */
		client = &http.Client{Timeout: 20 * time.Second} /* 更新 client 的值。 */
	} /* 结束当前表达式或代码块。 */
	protocol := strings.ToLower(strings.TrimSpace(config.Protocol)) /* 更新 protocol 的值。 */
	if protocol == "" {                                             /* 判断条件并选择处理分支。 */
		protocol = "rtsp" /* 更新 protocol 的值。 */
	} /* 结束当前表达式或代码块。 */
	if protocol != "rtsp" && protocol != "rtmp" && protocol != "hls" { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("unsupported Hikvision preview protocol %q", protocol) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	streamType := config.StreamType       /* 更新 streamType 的值。 */
	if streamType < 0 || streamType > 2 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Hikvision stream type must be 0, 1, or 2") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	transmode := config.Transmode /* 更新 transmode 的值。 */
	if transmode == 0 {           /* 判断条件并选择处理分支。 */
		// TCP is the safe/default transport for server-side preview. Keep the
		// zero value useful for callers that only configure the endpoint and
		// credentials.
		transmode = 1 /* 更新 transmode 的值。 */
	} /* 结束当前表达式或代码块。 */
	if transmode != 1 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Hikvision transmode must be 1 (TCP)") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return &HikvisionArtemis{ /* 返回当前处理结果。 */
		baseURL:    baseURL,                             /* 执行当前语句并推进处理流程。 */
		appKey:     strings.TrimSpace(config.AppKey),    /* 执行当前语句并推进处理流程。 */
		appSecret:  strings.TrimSpace(config.AppSecret), /* 执行当前语句并推进处理流程。 */
		client:     client,                              /* 执行当前语句并推进处理流程。 */
		streamType: streamType,                          /* 执行当前语句并推进处理流程。 */
		protocol:   protocol,                            /* 执行当前语句并推进处理流程。 */
		transmode:  transmode,                           /* 执行当前语句并推进处理流程。 */
		expand:     strings.TrimSpace(config.Expand),    /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (c *HikvisionArtemis) Resolve(ctx context.Context, request StreamRequest) (model.VideoStream, error) { /* 定义 Resolve 函数。 */
	if c == nil { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, errors.New("Hikvision Artemis adapter is nil") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(request.CameraID) == "" { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, errors.New("Hikvision camera index code is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	endpoint := c.baseURL                          /* 更新 endpoint 的值。 */
	if strings.TrimSpace(request.Endpoint) != "" { /* 判断条件并选择处理分支。 */
		endpoint = strings.TrimRight(strings.TrimSpace(request.Endpoint), "/") /* 更新 endpoint 的值。 */
	} /* 结束当前表达式或代码块。 */
	requestURL, err := hikvisionPreviewURL(endpoint) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	body, err := json.Marshal(struct { /* 更新 err 的值。 */
		CameraIndexCode string `json:"cameraIndexCode"`  /* 执行当前语句并推进处理流程。 */
		StreamType      int    `json:"streamType"`       /* 执行当前语句并推进处理流程。 */
		Protocol        string `json:"protocol"`         /* 执行当前语句并推进处理流程。 */
		Transmode       int    `json:"transmode"`        /* 执行当前语句并推进处理流程。 */
		Expand          string `json:"expand,omitempty"` /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		CameraIndexCode: request.CameraID, /* 执行当前语句并推进处理流程。 */
		StreamType:      c.streamType,     /* 执行当前语句并推进处理流程。 */
		Protocol:        c.protocol,       /* 执行当前语句并推进处理流程。 */
		Transmode:       c.transmode,      /* 执行当前语句并推进处理流程。 */
		Expand:          c.expand,         /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("encode Hikvision preview request: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body)) /* 更新 err 的值。 */
	if err != nil {                                                                                 /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("create Hikvision preview request: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	date := time.Now().UTC().Format(http.TimeFormat)                /* 更新 date 的值。 */
	applyHikvisionSignature(req, body, c.appKey, c.appSecret, date) /* 执行当前语句并推进处理流程。 */
	resp, err := c.client.Do(req)                                   /* 更新 err 的值。 */
	if err != nil {                                                 /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("request Hikvision Artemis preview URL: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                                      /* 安排函数结束时执行清理。 */
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxSDKResponse+1)) /* 更新 err 的值。 */
	if err != nil {                                                              /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("read Hikvision Artemis response: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(responseBody) > maxSDKResponse { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, errors.New("Hikvision Artemis response is too large") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("Hikvision Artemis returned HTTP %d", resp.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	stream, err := decodeHikvisionPreviewResponse(responseBody) /* 更新 err 的值。 */
	if err != nil {                                             /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return stream, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func hikvisionPreviewURL(raw string) (string, error) { /* 定义 hikvisionPreviewURL 函数。 */
	u, err := absoluteHTTPURL(raw) /* 更新 err 的值。 */
	if err != nil {                /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("invalid Hikvision Artemis endpoint: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if u.User != nil { /* 判断条件并选择处理分支。 */
		return "", errors.New("Hikvision Artemis endpoint userinfo is not allowed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !strings.Contains(u.Path, "/artemis/api/") { /* 判断条件并选择处理分支。 */
		u.Path = path.Join(u.Path, hikvisionPreviewPath) /* 更新 u.Path 的值。 */
	} /* 结束当前表达式或代码块。 */
	return u.String(), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func applyHikvisionSignature(req *http.Request, body []byte, appKey, appSecret, date string) { /* 定义 applyHikvisionSignature 函数。 */
	contentMD5 := md5.Sum(body)                      /* 更新 contentMD5 的值。 */
	nonceBytes := make([]byte, 16)                   /* 更新 nonceBytes 的值。 */
	if _, err := rand.Read(nonceBytes); err != nil { /* 判断条件并选择处理分支。 */
		copy(nonceBytes, []byte(strconv.FormatInt(time.Now().UnixNano(), 10))) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	nonce := hex.EncodeToString(nonceBytes)                    /* 更新 nonce 的值。 */
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10) /* 更新 timestamp 的值。 */
	const signedHeaders = "x-ca-key,x-ca-nonce,x-ca-timestamp" /* 声明 signedHeaders。 */
	stringToSign := strings.Join([]string{                     /* 更新 stringToSign 的值。 */
		http.MethodPost, /* 执行当前语句并推进处理流程。 */
		"*/*",           /* 执行当前语句并推进处理流程。 */
		base64.StdEncoding.EncodeToString(contentMD5[:]), /* 执行当前语句并推进处理流程。 */
		"application/json",            /* 执行当前语句并推进处理流程。 */
		date,                          /* 执行当前语句并推进处理流程。 */
		"x-ca-key:" + appKey,          /* 执行当前语句并推进处理流程。 */
		"x-ca-nonce:" + nonce,         /* 执行当前语句并推进处理流程。 */
		"x-ca-timestamp:" + timestamp, /* 执行当前语句并推进处理流程。 */
		req.URL.RequestURI(),          /* 执行当前语句并推进处理流程。 */
	}, "\n") /* 结束当前表达式或代码块。 */
	hmacHash := hmac.New(sha256.New, []byte(appSecret))                             /* 更新 hmacHash 的值。 */
	_, _ = hmacHash.Write([]byte(stringToSign))                                     /* 更新 _ 的值。 */
	signature := base64.StdEncoding.EncodeToString(hmacHash.Sum(nil))               /* 更新 signature 的值。 */
	req.Header.Set("Accept", "*/*")                                                 /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Content-Type", "application/json")                              /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(contentMD5[:])) /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Date", date)                                                    /* 执行当前语句并推进处理流程。 */
	req.Header.Set("X-Ca-Key", appKey)                                              /* 执行当前语句并推进处理流程。 */
	req.Header.Set("X-Ca-Nonce", nonce)                                             /* 执行当前语句并推进处理流程。 */
	req.Header.Set("X-Ca-Timestamp", timestamp)                                     /* 执行当前语句并推进处理流程。 */
	req.Header.Set("X-Ca-Signature-Headers", signedHeaders)                         /* 执行当前语句并推进处理流程。 */
	req.Header.Set("X-Ca-Signature", signature)                                     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type hikvisionPreviewResponse struct { /* 定义 hikvisionPreviewResponse 类型。 */
	Code any             `json:"code"` /* 执行当前语句并推进处理流程。 */
	Msg  string          `json:"msg"`  /* 执行当前语句并推进处理流程。 */
	Data hikvisionStream `json:"data"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type hikvisionStream struct { /* 定义 hikvisionStream 类型。 */
	URL        string          `json:"url"`        /* 执行当前语句并推进处理流程。 */
	Protocol   string          `json:"protocol"`   /* 执行当前语句并推进处理流程。 */
	ExpireTime json.RawMessage `json:"expireTime"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func decodeHikvisionPreviewResponse(body []byte) (model.VideoStream, error) { /* 定义 decodeHikvisionPreviewResponse 函数。 */
	var response hikvisionPreviewResponse                   /* 声明 response。 */
	if err := json.Unmarshal(body, &response); err != nil { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("decode Hikvision Artemis response: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if code := fmt.Sprint(response.Code); code != "0" && code != "200" { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("Hikvision Artemis failed: code=%s msg=%s", code, strings.TrimSpace(response.Msg)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	streamURL := strings.TrimSpace(response.Data.URL) /* 更新 streamURL 的值。 */
	if streamURL == "" {                              /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, errors.New("Hikvision Artemis did not return a preview URL") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateSourceURL(streamURL); err != nil { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("Hikvision Artemis returned an invalid preview URL: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return model.VideoStream{ /* 返回当前处理结果。 */
		URL:        streamURL,                                         /* 执行当前语句并推进处理流程。 */
		StreamType: sourceType(streamURL, response.Data.Protocol),     /* 执行当前语句并推进处理流程。 */
		ExpiresAt:  parseHikvisionTimestamp(response.Data.ExpireTime), /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func parseHikvisionTimestamp(raw json.RawMessage) int64 { /* 定义 parseHikvisionTimestamp 函数。 */
	if len(raw) == 0 || string(raw) == "null" { /* 判断条件并选择处理分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var number json.Number                                                        /* 声明 number。 */
	if err := json.Unmarshal(raw, &number); err == nil && number.String() != "" { /* 判断条件并选择处理分支。 */
		if value, err := strconv.ParseInt(number.String(), 10, 64); err == nil { /* 判断条件并选择处理分支。 */
			if value > 0 && value < 100000000000 { /* 判断条件并选择处理分支。 */
				return value * 1000 /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return value /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	var value string                        /* 声明 value。 */
	if json.Unmarshal(raw, &value) != nil { /* 判断条件并选择处理分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	value = strings.TrimSpace(value)                                /* 更新 value 的值。 */
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil { /* 判断条件并选择处理分支。 */
		if parsed > 0 && parsed < 100000000000 { /* 判断条件并选择处理分支。 */
			return parsed * 1000 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return parsed /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05.000-07:00"} { /* 循环处理当前数据。 */
		if parsed, err := time.Parse(layout, value); err == nil { /* 判断条件并选择处理分支。 */
			return parsed.UnixMilli() /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return 0 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
