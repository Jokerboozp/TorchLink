package video /* 声明 video 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"net/url"           /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"sync/atomic"       /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type staticStreamResolver struct { /* 定义 staticStreamResolver 类型。 */
	stream model.VideoStream /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (r staticStreamResolver) Resolve(context.Context, StreamRequest) (model.VideoStream, error) { /* 定义 Resolve 函数。 */
	return r.stream, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestPreviewResolvesSDKURLAndCreatesZLMProxy(t *testing.T) { /* 定义 TestPreviewResolvesSDKURLAndCreatesZLMProxy 函数。 */
	var sdkAuth string                                                                        /* 声明 sdkAuth。 */
	sdk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 sdk 的值。 */
		sdkAuth = r.Header.Get("Authorization")                                                                                                         /* 更新 sdkAuth 的值。 */
		_ = json.NewEncoder(w).Encode(map[string]any{"streamUrl": "rtsp://camera.internal/live/001", "streamType": "rtsp", "expiresAt": 1730000000000}) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer sdk.Close() /* 安排函数结束时执行清理。 */

	var proxyForm url.Values                                                                  /* 声明 proxyForm。 */
	zlm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 zlm 的值。 */
		if err := r.ParseForm(); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		proxyForm = r.Form                                                         /* 更新 proxyForm 的值。 */
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success"}) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer zlm.Close() /* 安排函数结束时执行清理。 */

	service, err := New(Config{ZLMAPIURL: zlm.URL, ZLMPlaybackBaseURL: "https://video.example.internal", ZLMSecret: "zlm-secret", ZLMVhost: "__defaultVhost__", ZLMApp: "iot", DahuaSDKToken: "sdk-token", AllowedSourceHosts: []string{"127.0.0.1", "camera.internal"}}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	preview, err := service.Preview(context.Background(), model.VideoCameraMapping{TenantID: "tenant-001", CameraID: "camera-001", CameraName: "一号摄像头", IngestMode: DahuaSDKMode, SDKEndpoint: sdk.URL, SDKCameraID: "channel-001"}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if preview.CameraName != "一号摄像头" || preview.StreamType != "hls" || preview.Provider != DahuaSDKMode || preview.PlaybackURL != "https://video.example.internal/iot/"+zlmStreamName("tenant-001", "camera-001")+"/hls.m3u8" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected preview: %#v", preview) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if sdkAuth != "Bearer sdk-token" { /* 判断条件并选择处理分支。 */
		t.Fatalf("SDK authorization = %q", sdkAuth) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for key, expected := range map[string]string{"secret": "zlm-secret", "app": "iot", "url": "rtsp://camera.internal/live/001", "enable_hls": "true"} { /* 循环处理当前数据。 */
		if proxyForm.Get(key) != expected { /* 判断条件并选择处理分支。 */
			t.Fatalf("ZLMediaKit form %s = %q, want %q", key, proxyForm.Get(key), expected) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestPreviewRejectsBrowserIncompatibleDirectStreamWithoutGateway(t *testing.T) { /* 定义 TestPreviewRejectsBrowserIncompatibleDirectStreamWithoutGateway 函数。 */
	service, err := New(Config{DahuaSDKURL: "http://sdk.invalid"}) /* 更新 err 的值。 */
	if err != nil {                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, err = service.Preview(context.Background(), model.VideoCameraMapping{CameraID: "camera-001", IngestMode: DirectMode, StreamURL: "rtmp://camera.internal/live"}) /* 更新 err 的值。 */
	if err == nil {                                                                                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal("expected RTMP preview to require ZLMediaKit") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestPreviewResolvesHikvisionOfficialArtemisURL(t *testing.T) { /* 定义 TestPreviewResolvesHikvisionOfficialArtemisURL 函数。 */
	sdk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 sdk 的值。 */
		if r.Method != http.MethodPost || r.URL.Path != "/artemis/api/video/v2/cameras/previewURLs" { /* 判断条件并选择处理分支。 */
			t.Fatalf("unexpected Hikvision request: %s %s", r.Method, r.URL.Path) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		var request struct { /* 声明 request。 */
			CameraIndexCode string `json:"cameraIndexCode"` /* 执行当前语句并推进处理流程。 */
			Protocol        string `json:"protocol"`        /* 执行当前语句并推进处理流程。 */
			Transmode       int    `json:"transmode"`       /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if request.CameraIndexCode != "channel-007" || request.Protocol != "rtsp" || request.Transmode != 1 || r.Header.Get("X-Ca-Key") != "hik-key" { /* 判断条件并选择处理分支。 */
			t.Fatalf("unexpected official Hikvision request: %#v key=%q", request, r.Header.Get("X-Ca-Key")) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		_ = json.NewEncoder(w).Encode(map[string]any{ /* 更新 _ 的值。 */
			"code": "0", /* 执行当前语句并推进处理流程。 */
			"data": map[string]any{ /* 执行当前语句并推进处理流程。 */
				"url":        "https://media.example.internal/hikvision/camera-007.m3u8", /* 执行当前语句并推进处理流程。 */
				"protocol":   "hls",                                                      /* 执行当前语句并推进处理流程。 */
				"expireTime": "2025-02-20T10:00:00+08:00",                                /* 执行当前语句并推进处理流程。 */
			}, /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer sdk.Close() /* 安排函数结束时执行清理。 */

	resolver, err := NewHikvisionArtemis(HikvisionArtemisConfig{BaseURL: sdk.URL, AppKey: "hik-key", AppSecret: "hik-secret"}) /* 更新 err 的值。 */
	if err != nil {                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	service, err := New(Config{HikvisionAPIURL: sdk.URL, HikvisionResolver: resolver, AllowedSourceHosts: []string{"127.0.0.1", "media.example.internal"}}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	preview, err := service.Preview(context.Background(), model.VideoCameraMapping{ /* 更新 err 的值。 */
		TenantID:         "tenant-001",         /* 执行当前语句并推进处理流程。 */
		CameraID:         "camera-007",         /* 执行当前语句并推进处理流程。 */
		IngestMode:       HikvisionMode,        /* 执行当前语句并推进处理流程。 */
		SDKCameraID:      "channel-007",        /* 执行当前语句并推进处理流程。 */
		SDKCredentialRef: "credential-hik-007", /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if preview.PlaybackURL != "https://media.example.internal/hikvision/camera-007.m3u8" || preview.StreamType != "hls" || preview.Provider != HikvisionMode || preview.ExpiresAt == 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected Hikvision preview: %#v", preview) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestPreviewOfficialHikvisionRTSPUsesZLMediaKit(t *testing.T) { /* 定义 TestPreviewOfficialHikvisionRTSPUsesZLMediaKit 函数。 */
	hikcentral := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 hikcentral 的值。 */
		_ = json.NewEncoder(w).Encode(map[string]any{ /* 更新 _ 的值。 */
			"code": "0", /* 执行当前语句并推进处理流程。 */
			"data": map[string]any{ /* 执行当前语句并推进处理流程。 */
				"url":      "rtsp://camera.internal/live/official", /* 执行当前语句并推进处理流程。 */
				"protocol": "rtsp",                                 /* 执行当前语句并推进处理流程。 */
			}, /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer hikcentral.Close()                                                                                                          /* 安排函数结束时执行清理。 */
	resolver, err := NewHikvisionArtemis(HikvisionArtemisConfig{BaseURL: hikcentral.URL, AppKey: "hik-key", AppSecret: "hik-secret"}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	var proxySource string                                                                    /* 声明 proxySource。 */
	zlm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 zlm 的值。 */
		if err := r.ParseForm(); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		proxySource = r.Form.Get("url")                                            /* 更新 proxySource 的值。 */
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success"}) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer zlm.Close() /* 安排函数结束时执行清理。 */

	service, err := New(Config{ /* 更新 err 的值。 */
		ZLMAPIURL:          zlm.URL,                                  /* 执行当前语句并推进处理流程。 */
		ZLMPlaybackBaseURL: "https://video.example.internal",         /* 执行当前语句并推进处理流程。 */
		ZLMSecret:          "zlm-secret",                             /* 执行当前语句并推进处理流程。 */
		HikvisionAPIURL:    hikcentral.URL,                           /* 执行当前语句并推进处理流程。 */
		HikvisionResolver:  resolver,                                 /* 执行当前语句并推进处理流程。 */
		AllowedSourceHosts: []string{"127.0.0.1", "camera.internal"}, /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	preview, err := service.Preview(context.Background(), model.VideoCameraMapping{ /* 更新 err 的值。 */
		TenantID:    "tenant-001",       /* 执行当前语句并推进处理流程。 */
		CameraID:    "camera-official",  /* 执行当前语句并推进处理流程。 */
		IngestMode:  HikvisionMode,      /* 执行当前语句并推进处理流程。 */
		SDKCameraID: "channel-official", /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if proxySource != "rtsp://camera.internal/live/official" || preview.StreamType != "hls" || preview.Provider != HikvisionMode { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected official Hikvision ZLMediaKit preview: source=%q preview=%#v", proxySource, preview) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestEligibleSDKCameraDefersShortLivedURLResolution(t *testing.T) { /* 定义 TestEligibleSDKCameraDefersShortLivedURLResolution 函数。 */
	resolver := staticStreamResolver{stream: model.VideoStream{URL: "https://media.example.internal/live/camera.m3u8", StreamType: "hls"}}                                    /* 更新 resolver 的值。 */
	service, err := New(Config{HikvisionAPIURL: "https://sdk.example.internal/hikvision", HikvisionResolver: resolver, AllowedSourceHosts: []string{"sdk.example.internal"}}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	camera := model.VideoCameraMapping{Enabled: true, IngestMode: HikvisionMode, SDKCameraID: "channel-007"} /* 更新 camera 的值。 */
	if !service.Eligible(camera, []string{"https://media.example.internal"}) {                               /* 判断条件并选择处理分支。 */
		t.Fatal("SDK camera should remain provisionally preview-eligible until its expiring URL is resolved") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if service.Eligible(camera, nil) { /* 判断条件并选择处理分支。 */
		t.Fatal("SDK camera should not be preview-eligible without a browser origin allowlist") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestPreviewRejectsSDKEndpointOutsideAllowlist(t *testing.T) { /* 定义 TestPreviewRejectsSDKEndpointOutsideAllowlist 函数。 */
	var contacted atomic.Bool                                                                 /* 声明 contacted。 */
	sdk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 sdk 的值。 */
		contacted.Store(true)                         /* 执行当前语句并推进处理流程。 */
		w.WriteHeader(http.StatusInternalServerError) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	defer sdk.Close() /* 安排函数结束时执行清理。 */

	service, err := New(Config{HikvisionAPIURL: sdk.URL, AllowedSourceHosts: []string{"sdk.example.internal"}}) /* 更新 err 的值。 */
	if err != nil {                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, err = service.Preview(context.Background(), model.VideoCameraMapping{IngestMode: HikvisionMode, SDKCameraID: "channel-007"}) /* 更新 err 的值。 */
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {                                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected disallowed SDK endpoint error: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if contacted.Load() { /* 判断条件并选择处理分支。 */
		t.Fatal("disallowed SDK endpoint was contacted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestPreviewRefreshesZLMProxyWhenSDKURLChanges(t *testing.T) { /* 定义 TestPreviewRefreshesZLMProxyWhenSDKURLChanges 函数。 */
	sdkCalls := 0                                                                             /* 更新 sdkCalls 的值。 */
	sdk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 sdk 的值。 */
		sdkCalls++        /* 执行当前语句并推进处理流程。 */
		token := "one"    /* 更新 token 的值。 */
		if sdkCalls > 1 { /* 判断条件并选择处理分支。 */
			token = "two" /* 更新 token 的值。 */
		} /* 结束当前表达式或代码块。 */
		_ = json.NewEncoder(w).Encode(map[string]any{ /* 更新 _ 的值。 */
			"streamUrl":  "rtsp://camera.internal/live/001?token=" + token, /* 执行当前语句并推进处理流程。 */
			"streamType": "rtsp",                                           /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer sdk.Close() /* 安排函数结束时执行清理。 */

	var paths []string                                                                        /* 声明 paths。 */
	var forms []url.Values                                                                    /* 声明 forms。 */
	zlm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 zlm 的值。 */
		if err := r.ParseForm(); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		paths = append(paths, r.URL.Path)              /* 更新 paths 的值。 */
		forms = append(forms, r.Form)                  /* 更新 forms 的值。 */
		if r.URL.Path == "/index/api/addStreamProxy" { /* 判断条件并选择处理分支。 */
			_ = json.NewEncoder(w).Encode(map[string]any{ /* 更新 _ 的值。 */
				"code": 0,                                                                        /* 执行当前语句并推进处理流程。 */
				"data": map[string]string{"key": "__defaultVhost__/iot/" + r.Form.Get("stream")}, /* 执行当前语句并推进处理流程。 */
			}) /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success"}) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer zlm.Close() /* 安排函数结束时执行清理。 */

	service, err := New(Config{ /* 更新 err 的值。 */
		ZLMAPIURL:          zlm.URL,                                  /* 执行当前语句并推进处理流程。 */
		ZLMPlaybackBaseURL: "https://video.example.internal",         /* 执行当前语句并推进处理流程。 */
		ZLMSecret:          "zlm-secret",                             /* 执行当前语句并推进处理流程。 */
		DahuaSDKToken:      "sdk-token",                              /* 执行当前语句并推进处理流程。 */
		AllowedSourceHosts: []string{"127.0.0.1", "camera.internal"}, /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	camera := model.VideoCameraMapping{ /* 更新 camera 的值。 */
		TenantID:    "tenant-001",  /* 执行当前语句并推进处理流程。 */
		CameraID:    "camera-001",  /* 执行当前语句并推进处理流程。 */
		IngestMode:  DahuaSDKMode,  /* 执行当前语句并推进处理流程。 */
		SDKEndpoint: sdk.URL,       /* 执行当前语句并推进处理流程。 */
		SDKCameraID: "channel-001", /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := service.Preview(context.Background(), camera); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := service.Preview(context.Background(), camera); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	if got, want := len(paths), 3; got != want { /* 判断条件并选择处理分支。 */
		t.Fatalf("ZLMediaKit call count = %d, want %d (%v)", got, want, paths) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	wantPaths := []string{"/index/api/addStreamProxy", "/index/api/delStreamProxy", "/index/api/addStreamProxy"} /* 更新 wantPaths 的值。 */
	for i, want := range wantPaths {                                                                             /* 循环处理当前数据。 */
		if paths[i] != want { /* 判断条件并选择处理分支。 */
			t.Fatalf("ZLMediaKit call %d = %q, want %q", i, paths[i], want) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if got, want := forms[1].Get("key"), "__defaultVhost__/iot/"+zlmStreamName("tenant-001", "camera-001"); got != want { /* 判断条件并选择处理分支。 */
		t.Fatalf("deleted proxy key = %q, want %q", got, want) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if got, want := forms[2].Get("url"), "rtsp://camera.internal/live/001?token=two"; got != want { /* 判断条件并选择处理分支。 */
		t.Fatalf("refreshed proxy URL = %q, want %q", got, want) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestZLMStreamNameIsTenantScoped(t *testing.T) { /* 定义 TestZLMStreamNameIsTenantScoped 函数。 */
	if first, second := zlmStreamName("tenant-001", "camera-001"), zlmStreamName("tenant-002", "camera-001"); first == second { /* 判断条件并选择处理分支。 */
		t.Fatalf("tenant-scoped stream names collided: %q", first) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
