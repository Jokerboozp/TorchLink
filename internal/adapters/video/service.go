package video /* 声明 video 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"path"          /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	DirectMode     = "direct"        /* 更新 DirectMode 的值。 */
	DahuaSDKMode   = "dahua_sdk"     /* 更新 DahuaSDKMode 的值。 */
	HikvisionMode  = "hikvision_sdk" /* 更新 HikvisionMode 的值。 */
	maxSDKResponse = 1 << 20         /* 更新 maxSDKResponse 的值。 */
) /* 结束当前表达式或代码块。 */

type Config struct { /* 定义 Config 类型。 */
	ZLMAPIURL          string         /* 执行当前语句并推进处理流程。 */
	ZLMPlaybackBaseURL string         /* 执行当前语句并推进处理流程。 */
	ZLMSecret          string         /* 执行当前语句并推进处理流程。 */
	ZLMVhost           string         /* 执行当前语句并推进处理流程。 */
	ZLMApp             string         /* 执行当前语句并推进处理流程。 */
	DahuaSDKURL        string         /* 执行当前语句并推进处理流程。 */
	DahuaSDKToken      string         /* 执行当前语句并推进处理流程。 */
	HikvisionAPIURL    string         /* 执行当前语句并推进处理流程。 */
	HikvisionResolver  StreamResolver /* 执行当前语句并推进处理流程。 */
	AllowedSourceHosts []string       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// StreamRequest is the provider-neutral input used by an in-process vendor
// adapter. Credentials are represented by a reference only; the platform
// never receives or persists vendor passwords.
type StreamRequest struct { /* 定义 StreamRequest 类型。 */
	Provider      string /* 执行当前语句并推进处理流程。 */
	TenantID      string /* 执行当前语句并推进处理流程。 */
	CameraID      string /* 执行当前语句并推进处理流程。 */
	CredentialRef string /* 执行当前语句并推进处理流程。 */
	Endpoint      string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// StreamResolver obtains a short-lived vendor stream URL. A vendor adapter is
// injected so preview and ZLMediaKit stay independent from one SDK package.
type StreamResolver interface { /* 定义 StreamResolver 类型。 */
	Resolve(context.Context, StreamRequest) (model.VideoStream, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Service struct { /* 定义 Service 类型。 */
	config  Config       /* 执行当前语句并推进处理流程。 */
	client  *http.Client /* 执行当前语句并推进处理流程。 */
	gateway *zlmGateway  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(config Config) (*Service, error) { /* 定义 New 函数。 */
	config.ZLMAPIURL = strings.TrimRight(strings.TrimSpace(config.ZLMAPIURL), "/")                   /* 更新 config.ZLMAPIURL 的值。 */
	config.ZLMPlaybackBaseURL = strings.TrimRight(strings.TrimSpace(config.ZLMPlaybackBaseURL), "/") /* 更新 config.ZLMPlaybackBaseURL 的值。 */
	config.ZLMVhost = strings.TrimSpace(config.ZLMVhost)                                             /* 更新 config.ZLMVhost 的值。 */
	if config.ZLMVhost == "" {                                                                       /* 判断条件并选择处理分支。 */
		config.ZLMVhost = "__defaultVhost__" /* 更新 config.ZLMVhost 的值。 */
	} /* 结束当前表达式或代码块。 */
	config.ZLMApp = strings.Trim(strings.TrimSpace(config.ZLMApp), "/") /* 更新 config.ZLMApp 的值。 */
	if config.ZLMApp == "" {                                            /* 判断条件并选择处理分支。 */
		config.ZLMApp = "iot" /* 更新 config.ZLMApp 的值。 */
	} /* 结束当前表达式或代码块。 */
	if config.ZLMAPIURL == "" && config.DahuaSDKURL == "" && config.HikvisionAPIURL == "" && config.HikvisionResolver == nil { /* 判断条件并选择处理分支。 */
		return nil, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	client := &http.Client{Timeout: 20 * time.Second}   /* 更新 client 的值。 */
	service := &Service{config: config, client: client} /* 更新 service 的值。 */
	if config.ZLMAPIURL != "" {                         /* 判断条件并选择处理分支。 */
		if config.ZLMPlaybackBaseURL == "" { /* 判断条件并选择处理分支。 */
			return nil, errors.New("ZLMediaKit playback base URL is required when its API URL is configured") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, err := absoluteHTTPURL(config.ZLMAPIURL); err != nil { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("invalid ZLMediaKit API URL: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, err := absoluteHTTPURL(config.ZLMPlaybackBaseURL); err != nil { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("invalid ZLMediaKit playback base URL: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		service.gateway = &zlmGateway{ /* 更新 service.gateway 的值。 */
			apiURL:       config.ZLMAPIURL,          /* 执行当前语句并推进处理流程。 */
			playbackBase: config.ZLMPlaybackBaseURL, /* 执行当前语句并推进处理流程。 */
			secret:       config.ZLMSecret,          /* 执行当前语句并推进处理流程。 */
			vhost:        config.ZLMVhost,           /* 执行当前语句并推进处理流程。 */
			app:          config.ZLMApp,             /* 执行当前语句并推进处理流程。 */
			client:       client,                    /* 执行当前语句并推进处理流程。 */
			proxies:      make(map[string]zlmProxy), /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return service, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) Preview(ctx context.Context, camera model.VideoCameraMapping) (model.VideoPreview, error) { /* 定义 Preview 函数。 */
	source, err := s.resolveSource(ctx, camera) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		return model.VideoPreview{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.gateway != nil && gatewayStreamType(source) { /* 判断条件并选择处理分支。 */
		if !s.sourceHostAllowed(source.URL) { /* 判断条件并选择处理分支。 */
			return model.VideoPreview{}, errors.New("camera source host is not allowlisted for ZLMediaKit ingest") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		preview, previewErr := s.gateway.proxy(ctx, camera.TenantID, camera.CameraID, source) /* 更新 previewErr 的值。 */
		if previewErr == nil {                                                                /* 判断条件并选择处理分支。 */
			preview.CameraName = camera.CameraName /* 更新 preview.CameraName 的值。 */
		} /* 结束当前表达式或代码块。 */
		return preview, previewErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	streamType := sourceType(source.URL, source.StreamType)                                           /* 更新 streamType 的值。 */
	if streamType != "hls" && streamType != "mp4" && streamType != "webm" && streamType != "native" { /* 判断条件并选择处理分支。 */
		return model.VideoPreview{}, errors.New("browser preview requires ZLMediaKit for RTSP/RTMP or a browser-compatible stream") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return model.VideoPreview{CameraID: camera.CameraID, CameraName: camera.CameraName, PlaybackURL: source.URL, StreamType: streamType, Provider: source.Provider, ExpiresAt: source.ExpiresAt}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) Eligible(camera model.VideoCameraMapping, allowedOrigins []string) bool { /* 定义 Eligible 函数。 */
	if !camera.Enabled { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	mode := normalizeMode(camera.IngestMode) /* 更新 mode 的值。 */
	if mode != DirectMode {                  /* 判断条件并选择处理分支。 */
		if mode == HikvisionMode && s.config.HikvisionResolver == nil { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		endpoint := s.sdkEndpoint(mode, camera)               /* 更新 endpoint 的值。 */
		if endpoint == "" || !s.sourceHostAllowed(endpoint) { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if s.gateway != nil && (mode != DirectMode || gatewayStreamType(model.VideoStream{URL: camera.StreamURL, StreamType: camera.StreamType})) { /* 判断条件并选择处理分支。 */
		return streamOriginAllowed(s.gateway.playbackURL(camera.TenantID, camera.CameraID), allowedOrigins) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if mode != DirectMode { /* 判断条件并选择处理分支。 */
		// Vendor SDK URLs are deliberately resolved only during preview because
		// they are short-lived. The preview endpoint re-checks the returned
		// playback origin after resolution; the list endpoint can only expose a
		// provisional eligibility flag here.
		return camera.Enabled && len(allowedOrigins) > 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	streamType := sourceType(camera.StreamURL, camera.StreamType)                                                                                                  /* 更新 streamType 的值。 */
	return (streamType == "hls" || streamType == "mp4" || streamType == "webm" || streamType == "native") && streamOriginAllowed(camera.StreamURL, allowedOrigins) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) resolveSource(ctx context.Context, camera model.VideoCameraMapping) (model.VideoStream, error) { /* 定义 resolveSource 函数。 */
	mode := normalizeMode(camera.IngestMode) /* 更新 mode 的值。 */
	if mode == DirectMode {                  /* 判断条件并选择处理分支。 */
		if strings.TrimSpace(camera.StreamURL) == "" { /* 判断条件并选择处理分支。 */
			return model.VideoStream{}, errors.New("direct camera stream URL is not configured") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := validateSourceURL(camera.StreamURL); err != nil { /* 判断条件并选择处理分支。 */
			return model.VideoStream{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return model.VideoStream{URL: camera.StreamURL, StreamType: sourceType(camera.StreamURL, camera.StreamType), Provider: DirectMode}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	endpoint := s.sdkEndpoint(mode, camera) /* 更新 endpoint 的值。 */
	if endpoint == "" {                     /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("%s SDK adapter is not configured", mode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return s.resolveSDK(ctx, camera, mode, endpoint) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) sdkEndpoint(mode string, camera model.VideoCameraMapping) string { /* 定义 sdkEndpoint 函数。 */
	if endpoint := strings.TrimSpace(camera.SDKEndpoint); endpoint != "" { /* 判断条件并选择处理分支。 */
		return endpoint /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch mode { /* 根据条件选择处理路径。 */
	case DahuaSDKMode: /* 处理当前分支。 */
		return strings.TrimSpace(s.config.DahuaSDKURL) /* 返回当前处理结果。 */
	case HikvisionMode: /* 处理当前分支。 */
		return strings.TrimSpace(s.config.HikvisionAPIURL) /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) resolveSDK(ctx context.Context, camera model.VideoCameraMapping, mode, endpoint string) (model.VideoStream, error) { /* 定义 resolveSDK 函数。 */
	if camera.SDKCameraID == "" { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("%s SDK camera ID is required", mode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateSourceEndpoint(endpoint); err != nil { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("invalid %s SDK endpoint: %w", mode, err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !s.sourceHostAllowed(endpoint) { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("%s SDK endpoint host is not allowlisted", mode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if mode == HikvisionMode { /* 判断条件并选择处理分支。 */
		if s.config.HikvisionResolver == nil { /* 判断条件并选择处理分支。 */
			return model.VideoStream{}, errors.New("official Hikvision Go adapter is not configured") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		stream, err := s.config.HikvisionResolver.Resolve(ctx, StreamRequest{ /* 更新 err 的值。 */
			Provider:      mode,                    /* 执行当前语句并推进处理流程。 */
			TenantID:      camera.TenantID,         /* 执行当前语句并推进处理流程。 */
			CameraID:      camera.SDKCameraID,      /* 执行当前语句并推进处理流程。 */
			CredentialRef: camera.SDKCredentialRef, /* 执行当前语句并推进处理流程。 */
			Endpoint:      endpoint,                /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			return model.VideoStream{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := validateSourceURL(stream.URL); err != nil { /* 判断条件并选择处理分支。 */
			return model.VideoStream{}, fmt.Errorf("Hikvision SDK returned an invalid stream URL: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		stream.Provider = mode                                        /* 更新 stream.Provider 的值。 */
		stream.StreamType = sourceType(stream.URL, stream.StreamType) /* 更新 stream.StreamType 的值。 */
		return stream, nil                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	statusCode, body, err := s.requestSDK(ctx, camera, mode, endpoint) /* 更新 err 的值。 */
	if err != nil {                                                    /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if statusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("%s SDK returned HTTP %d", mode, statusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	stream, err := decodeSDKStream(body) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("decode %s SDK response: %w", mode, err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	stream.Provider = mode /* 更新 stream.Provider 的值。 */
	return stream, nil     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) requestSDK(ctx context.Context, camera model.VideoCameraMapping, mode, endpoint string) (int, []byte, error) { /* 定义 requestSDK 函数。 */
	payload, _ := json.Marshal(map[string]any{ /* 更新 _ 的值。 */
		"provider":      mode,                    /* 执行当前语句并推进处理流程。 */
		"cameraId":      camera.SDKCameraID,      /* 执行当前语句并推进处理流程。 */
		"credentialRef": camera.SDKCredentialRef, /* 执行当前语句并推进处理流程。 */
		"tenantId":      camera.TenantID,         /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload)) /* 更新 err 的值。 */
	if err != nil {                                                                                  /* 判断条件并选择处理分支。 */
		return 0, nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	token := s.config.DahuaSDKToken                    /* 更新 token 的值。 */
	if strings.TrimSpace(token) != "" {                /* 判断条件并选择处理分支。 */
		req.Header.Set("Authorization", "Bearer "+token) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := s.client.Do(req) /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		return 0, nil, fmt.Errorf("request %s SDK: %w", mode, err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                                      /* 安排函数结束时执行清理。 */
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxSDKResponse+1)) /* 更新 err 的值。 */
	if err != nil {                                                              /* 判断条件并选择处理分支。 */
		return resp.StatusCode, nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(responseBody) > maxSDKResponse { /* 判断条件并选择处理分支。 */
		return resp.StatusCode, nil, errors.New("video SDK response is too large") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return resp.StatusCode, responseBody, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func decodeSDKStream(body []byte) (model.VideoStream, error) { /* 定义 decodeSDKStream 函数。 */
	var value any                                     /* 声明 value。 */
	decoder := json.NewDecoder(bytes.NewReader(body)) /* 更新 decoder 的值。 */
	decoder.UseNumber()                               /* 执行当前语句并推进处理流程。 */
	if err := decoder.Decode(&value); err != nil {    /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	streamURL, streamType, expiresAt, ok := findSDKStream(value, 0) /* 更新 ok 的值。 */
	if !ok {                                                        /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, errors.New("SDK did not return a stream URL") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateSourceURL(streamURL); err != nil { /* 判断条件并选择处理分支。 */
		return model.VideoStream{}, fmt.Errorf("SDK returned an invalid stream URL: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return model.VideoStream{URL: streamURL, StreamType: sourceType(streamURL, streamType), ExpiresAt: expiresAt}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func findSDKStream(value any, depth int) (string, string, int64, bool) { /* 定义 findSDKStream 函数。 */
	if depth > 4 { /* 判断条件并选择处理分支。 */
		return "", "", 0, false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch item := value.(type) { /* 根据条件选择处理路径。 */
	case string: /* 处理当前分支。 */
		var nested any                                    /* 声明 nested。 */
		if json.Unmarshal([]byte(item), &nested) == nil { /* 判断条件并选择处理分支。 */
			return findSDKStream(nested, depth+1) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case map[string]any: /* 处理当前分支。 */
		streamURL := firstString(item, "streamUrl", "streamURL", "stream_url", "url", "rtspUrl", "rtspURL", "rtmpUrl", "rtmpURL", "hlsUrl", "hlsURL") /* 更新 streamURL 的值。 */
		streamType := firstString(item, "streamType", "stream_type", "protocol")                                                                      /* 更新 streamType 的值。 */
		expiresAt := firstTimestamp(item, "expiresAt", "expires_at", "expireAt", "expire_at", "expireTime", "expire_time")                            /* 更新 expiresAt 的值。 */
		if streamURL != "" {                                                                                                                          /* 判断条件并选择处理分支。 */
			return streamURL, streamType, expiresAt, true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, key := range []string{"data", "result", "body"} { /* 循环处理当前数据。 */
			if nested, exists := item[key]; exists { /* 判断条件并选择处理分支。 */
				if streamURL, streamType, expiresAt, ok := findSDKStream(nested, depth+1); ok { /* 判断条件并选择处理分支。 */
					return streamURL, streamType, expiresAt, true /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "", "", 0, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func firstString(values map[string]any, keys ...string) string { /* 定义 firstString 函数。 */
	for _, key := range keys { /* 循环处理当前数据。 */
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" { /* 判断条件并选择处理分支。 */
			return strings.TrimSpace(value) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func firstTimestamp(values map[string]any, keys ...string) int64 { /* 定义 firstTimestamp 函数。 */
	for _, key := range keys { /* 循环处理当前数据。 */
		value, ok := values[key] /* 更新 ok 的值。 */
		if !ok {                 /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		switch timestamp := value.(type) { /* 根据条件选择处理路径。 */
		case json.Number: /* 处理当前分支。 */
			if parsed, err := timestamp.Int64(); err == nil { /* 判断条件并选择处理分支。 */
				return parsed /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		case float64: /* 处理当前分支。 */
			return int64(timestamp) /* 返回当前处理结果。 */
		case string: /* 处理当前分支。 */
			if parsed, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64); err == nil { /* 判断条件并选择处理分支。 */
				return parsed /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} { /* 循环处理当前数据。 */
				if parsed, err := time.Parse(layout, strings.TrimSpace(timestamp)); err == nil { /* 判断条件并选择处理分支。 */
					return parsed.UnixMilli() /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return 0 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type zlmGateway struct { /* 定义 zlmGateway 类型。 */
	apiURL       string              /* 执行当前语句并推进处理流程。 */
	playbackBase string              /* 执行当前语句并推进处理流程。 */
	secret       string              /* 执行当前语句并推进处理流程。 */
	vhost        string              /* 执行当前语句并推进处理流程。 */
	app          string              /* 执行当前语句并推进处理流程。 */
	client       *http.Client        /* 执行当前语句并推进处理流程。 */
	mu           sync.Mutex          /* 执行当前语句并推进处理流程。 */
	proxies      map[string]zlmProxy /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type zlmProxy struct { /* 定义 zlmProxy 类型。 */
	sourceURL string /* 执行当前语句并推进处理流程。 */
	key       string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (g *zlmGateway) proxy(ctx context.Context, tenantID, cameraID string, source model.VideoStream) (model.VideoPreview, error) { /* 定义 proxy 函数。 */
	stream := zlmStreamName(tenantID, cameraID) /* 更新 stream 的值。 */
	g.mu.Lock()                                 /* 执行当前语句并推进处理流程。 */
	defer g.mu.Unlock()                         /* 安排函数结束时执行清理。 */
	if g.proxies == nil {                       /* 判断条件并选择处理分支。 */
		g.proxies = make(map[string]zlmProxy) /* 更新 g.proxies 的值。 */
	} /* 结束当前表达式或代码块。 */

	previous, hasPrevious := g.proxies[stream]           /* 更新 hasPrevious 的值。 */
	if hasPrevious && previous.sourceURL == source.URL { /* 判断条件并选择处理分支。 */
		return model.VideoPreview{CameraID: cameraID, PlaybackURL: g.playbackURL(tenantID, cameraID), StreamType: "hls", Provider: source.Provider, ExpiresAt: source.ExpiresAt}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if hasPrevious && previous.sourceURL != source.URL { /* 判断条件并选择处理分支。 */
		if err := g.deleteStreamProxy(ctx, previous.key); err != nil { /* 判断条件并选择处理分支。 */
			return model.VideoPreview{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		delete(g.proxies, stream) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */

	key, err := g.addStreamProxy(ctx, stream, source.URL) /* 更新 err 的值。 */
	if err != nil {                                       /* 判断条件并选择处理分支。 */
		return model.VideoPreview{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if key == "" { /* 判断条件并选择处理分支。 */
		key = zlmProxyKey(g.vhost, g.app, stream) /* 更新 key 的值。 */
	} /* 结束当前表达式或代码块。 */
	g.proxies[stream] = zlmProxy{sourceURL: source.URL, key: key} /* 更新 g.proxies[stream] 的值。 */

	return model.VideoPreview{CameraID: cameraID, PlaybackURL: g.playbackURL(tenantID, cameraID), StreamType: "hls", Provider: source.Provider, ExpiresAt: source.ExpiresAt}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (g *zlmGateway) addStreamProxy(ctx context.Context, stream, sourceURL string) (string, error) { /* 定义 addStreamProxy 函数。 */
	form := url.Values{}                                                                                                                 /* 更新 form 的值。 */
	form.Set("secret", g.secret)                                                                                                         /* 执行当前语句并推进处理流程。 */
	form.Set("vhost", g.vhost)                                                                                                           /* 执行当前语句并推进处理流程。 */
	form.Set("app", g.app)                                                                                                               /* 执行当前语句并推进处理流程。 */
	form.Set("stream", stream)                                                                                                           /* 执行当前语句并推进处理流程。 */
	form.Set("url", sourceURL)                                                                                                           /* 执行当前语句并推进处理流程。 */
	form.Set("retry_count", "3")                                                                                                         /* 执行当前语句并推进处理流程。 */
	form.Set("timeout_sec", "10")                                                                                                        /* 执行当前语句并推进处理流程。 */
	form.Set("enable_hls", "true")                                                                                                       /* 执行当前语句并推进处理流程。 */
	form.Set("hls_demand", "false")                                                                                                      /* 执行当前语句并推进处理流程。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.apiURL+"/index/api/addStreamProxy", strings.NewReader(form.Encode())) /* 更新 err 的值。 */
	if err != nil {                                                                                                                      /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded") /* 执行当前语句并推进处理流程。 */
	resp, err := g.client.Do(req)                                       /* 更新 err 的值。 */
	if err != nil {                                                     /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("request ZLMediaKit: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                              /* 安排函数结束时执行清理。 */
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSDKResponse+1)) /* 更新 err 的值。 */
	if err != nil {                                                      /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(body) > maxSDKResponse { /* 判断条件并选择处理分支。 */
		return "", errors.New("ZLMediaKit response is too large") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var result struct { /* 声明 result。 */
		Code int      `json:"code"` /* 执行当前语句并推进处理流程。 */
		Msg  string   `json:"msg"`  /* 执行当前语句并推进处理流程。 */
		Data struct { /* 执行当前语句并推进处理流程。 */
			Key string `json:"key"` /* 执行当前语句并推进处理流程。 */
		} `json:"data"` /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err = json.Unmarshal(body, &result); err != nil { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("decode ZLMediaKit response: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode/100 != 2 || result.Code != 0 && !proxyAlreadyExists(result.Msg) { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("ZLMediaKit addStreamProxy failed: %s", firstNonEmpty(result.Msg, resp.Status)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return result.Data.Key, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (g *zlmGateway) deleteStreamProxy(ctx context.Context, key string) error { /* 定义 deleteStreamProxy 函数。 */
	if strings.TrimSpace(key) == "" { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	form := url.Values{}                                                                                                                 /* 更新 form 的值。 */
	form.Set("secret", g.secret)                                                                                                         /* 执行当前语句并推进处理流程。 */
	form.Set("key", key)                                                                                                                 /* 执行当前语句并推进处理流程。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.apiURL+"/index/api/delStreamProxy", strings.NewReader(form.Encode())) /* 更新 err 的值。 */
	if err != nil {                                                                                                                      /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded") /* 执行当前语句并推进处理流程。 */
	resp, err := g.client.Do(req)                                       /* 更新 err 的值。 */
	if err != nil {                                                     /* 判断条件并选择处理分支。 */
		return fmt.Errorf("request ZLMediaKit proxy removal: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                              /* 安排函数结束时执行清理。 */
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSDKResponse+1)) /* 更新 err 的值。 */
	if err != nil {                                                      /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(body) > maxSDKResponse { /* 判断条件并选择处理分支。 */
		return errors.New("ZLMediaKit response is too large") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var result struct { /* 声明 result。 */
		Code int    `json:"code"` /* 执行当前语句并推进处理流程。 */
		Msg  string `json:"msg"`  /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.Unmarshal(body, &result); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("decode ZLMediaKit removal response: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode/100 == 2 && (result.Code == 0 || proxyMissing(result.Msg)) { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fmt.Errorf("ZLMediaKit delStreamProxy failed: %s", firstNonEmpty(result.Msg, resp.Status)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func proxyMissing(message string) bool { /* 定义 proxyMissing 函数。 */
	message = strings.ToLower(strings.TrimSpace(message))                                                                       /* 更新 message 的值。 */
	return strings.Contains(message, "not found") || strings.Contains(message, "not exist") || strings.Contains(message, "不存在") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func proxyAlreadyExists(message string) bool { /* 定义 proxyAlreadyExists 函数。 */
	message = strings.ToLower(strings.TrimSpace(message))                                                                      /* 更新 message 的值。 */
	return strings.Contains(message, "already exist") || strings.Contains(message, "已存在") || strings.Contains(message, "已经存在") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func zlmProxyKey(vhost, app, stream string) string { /* 定义 zlmProxyKey 函数。 */
	return path.Join(vhost, app, stream) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (g *zlmGateway) playbackURL(tenantID, cameraID string) string { /* 定义 playbackURL 函数。 */
	u, _ := url.Parse(g.playbackBase)                                                /* 更新 _ 的值。 */
	u.Path = path.Join(u.Path, g.app, zlmStreamName(tenantID, cameraID), "hls.m3u8") /* 更新 u.Path 的值。 */
	query := u.Query()                                                               /* 更新 query 的值。 */
	if g.vhost != "" && g.vhost != "__defaultVhost__" {                              /* 判断条件并选择处理分支。 */
		query.Set("vhost", g.vhost) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	u.RawQuery = query.Encode() /* 更新 u.RawQuery 的值。 */
	return u.String()           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func zlmStreamName(tenantID, cameraID string) string { /* 定义 zlmStreamName 函数。 */
	hash := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(cameraID))) /* 更新 hash 的值。 */
	return "camera_" + hex.EncodeToString(hash[:8])                                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func gatewayStreamType(stream model.VideoStream) bool { /* 定义 gatewayStreamType 函数。 */
	typ := sourceType(stream.URL, stream.StreamType)      /* 更新 typ 的值。 */
	return typ == "hls" || typ == "rtsp" || typ == "rtmp" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeMode(mode string) string { /* 定义 normalizeMode 函数。 */
	switch strings.ToLower(strings.TrimSpace(mode)) { /* 根据条件选择处理路径。 */
	case DahuaSDKMode: /* 处理当前分支。 */
		return DahuaSDKMode /* 返回当前处理结果。 */
	case HikvisionMode: /* 处理当前分支。 */
		return HikvisionMode /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return DirectMode /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func sourceType(rawURL, configured string) string { /* 定义 sourceType 函数。 */
	if configured = strings.ToLower(strings.TrimSpace(configured)); configured != "" { /* 判断条件并选择处理分支。 */
		return configured /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	u, err := url.Parse(strings.TrimSpace(rawURL)) /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		return "native" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch strings.ToLower(u.Scheme) { /* 根据条件选择处理路径。 */
	case "rtsp", "rtmp": /* 处理当前分支。 */
		return strings.ToLower(u.Scheme) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	lower := strings.ToLower(u.Path) /* 更新 lower 的值。 */
	switch {                         /* 根据条件选择处理路径。 */
	case strings.HasSuffix(lower, ".m3u8") || strings.EqualFold(u.Query().Get("format"), "hls"): /* 处理当前分支。 */
		return "hls" /* 返回当前处理结果。 */
	case strings.HasSuffix(lower, ".mp4"): /* 处理当前分支。 */
		return "mp4" /* 返回当前处理结果。 */
	case strings.HasSuffix(lower, ".webm"): /* 处理当前分支。 */
		return "webm" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return "native" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func validateSourceURL(raw string) error { /* 定义 validateSourceURL 函数。 */
	u, err := url.Parse(strings.TrimSpace(raw)) /* 更新 err 的值。 */
	if err != nil || u.Host == "" {             /* 判断条件并选择处理分支。 */
		return errors.New("camera stream URL must be an absolute URL") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch strings.ToLower(u.Scheme) { /* 根据条件选择处理路径。 */
	case "http", "https", "rtsp", "rtmp": /* 处理当前分支。 */
		return nil /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return fmt.Errorf("unsupported camera stream URL scheme %q", u.Scheme) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func validateSourceEndpoint(raw string) error { /* 定义 validateSourceEndpoint 函数。 */
	u, err := absoluteHTTPURL(raw) /* 更新 err 的值。 */
	if err != nil {                /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if u.User != nil { /* 判断条件并选择处理分支。 */
		return errors.New("endpoint userinfo is not allowed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) sourceHostAllowed(rawURL string) bool { /* 定义 sourceHostAllowed 函数。 */
	u, err := url.Parse(strings.TrimSpace(rawURL))                                 /* 更新 err 的值。 */
	if err != nil || u.Hostname() == "" || len(s.config.AllowedSourceHosts) == 0 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, allowed := range s.config.AllowedSourceHosts { /* 循环处理当前数据。 */
		if strings.EqualFold(strings.TrimSpace(allowed), u.Hostname()) { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func absoluteHTTPURL(raw string) (*url.URL, error) { /* 定义 absoluteHTTPURL 函数。 */
	u, err := url.Parse(strings.TrimSpace(raw))                                    /* 更新 err 的值。 */
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") { /* 判断条件并选择处理分支。 */
		return nil, errors.New("an absolute HTTP(S) URL is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return u, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func streamOriginAllowed(rawURL string, allowed []string) bool { /* 定义 streamOriginAllowed 函数。 */
	target, err := absoluteHTTPURL(rawURL) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	targetOrigin := strings.ToLower(target.Scheme + "://" + target.Host) /* 更新 targetOrigin 的值。 */
	for _, candidate := range allowed {                                  /* 循环处理当前数据。 */
		origin, originErr := absoluteHTTPURL(candidate)                                           /* 更新 originErr 的值。 */
		if originErr == nil && strings.ToLower(origin.Scheme+"://"+origin.Host) == targetOrigin { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func firstNonEmpty(values ...string) string { /* 定义 firstNonEmpty 函数。 */
	for _, value := range values { /* 循环处理当前数据。 */
		if strings.TrimSpace(value) != "" { /* 判断条件并选择处理分支。 */
			return strings.TrimSpace(value) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
