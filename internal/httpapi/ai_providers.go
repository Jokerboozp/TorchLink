package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Server) aiProviders(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items := []ports.AIPluginInfo{}
	if s.engine.AIPlugins != nil {
		items = s.engine.AIPlugins.List()
	}
	for index := range items {
		if !s.canConfigureAI(r) {
			items[index].DefaultBaseURL = ""
		}
	}
	active := ports.AIPluginInfo{ID: "disabled", Name: "未启用", Enabled: false}
	if provider, ok := s.engine.AI.(ports.AIInspectable); ok {
		active = provider.ProviderInfo()
	}
	healthy := false
	healthMessage := "AI 尚未启用"
	if active.ID == "deepseek" && !active.Enabled {
		healthMessage = "待配置 DeepSeek API Key"
	}
	if active.Enabled && s.engine.AI != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.engine.AI.Health(ctx); err != nil {
			healthMessage = "连接异常"
			if s.log != nil {
				s.log.Warn("AI provider health check failed", "provider", active.ID, "model", active.Model, "error", err)
			}
		} else {
			healthy = true
			healthMessage = "连接正常"
		}
	}
	active.DefaultBaseURL = ""
	items, total := pageItems(items, pagination)
	meta := map[string]any{"active": active, "healthy": healthy, "healthMessage": healthMessage, "mode": "plugin-harness"}
	if s.aiProviderRuntime != nil {
		meta["config"] = s.aiProviderConfigView(r, s.aiProviderRuntime.CurrentConfig(), active)
	}
	writeList(w, 200, items, total, pagination, meta)
}

func effectiveAIMaxTokens(value int) int {
	if value < 128 || value > 8192 {
		return 2048
	}
	return value
}

func (s *Server) aiProviderConfigView(r *http.Request, config ports.AIPluginConfig, info ports.AIPluginInfo) map[string]any {
	key := strings.TrimSpace(config.APIKey)
	view := map[string]any{
		"provider":         config.Provider,
		"providerName":     info.Name,
		"model":            config.Model,
		"maxTokens":        effectiveAIMaxTokens(config.MaxTokens),
		"apiKeyConfigured": key != "",
		"active":           info.Enabled,
	}
	if s.canConfigureAI(r) {
		view["baseUrl"] = config.BaseURL
		if key != "" {
			view["apiKeyHint"] = key[:minInt(len(key), 4)] + "***"
		}
	}
	return view
}

func (s *Server) aiProviderConfig(w http.ResponseWriter, r *http.Request) {
	if s.aiProviderRuntime == nil {
		problem(w, http.StatusServiceUnavailable, "AI provider runtime is unavailable")
		return
	}
	info := s.aiProviderRuntime.ProviderInfo()
	write(w, http.StatusOK, s.aiProviderConfigView(r, s.aiProviderRuntime.CurrentConfig(), info))
}

func (s *Server) updateAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	if !s.platformActionAllowed(r) {
		problem(w, http.StatusForbidden, "模型、向量服务与智能体配置对全平台生效，只能由平台管理员或运维租户修改")
		return
	}
	if s.aiProviderRuntime == nil {
		problem(w, http.StatusServiceUnavailable, "AI provider runtime is unavailable")
		return
	}
	s.aiProviderUpdateMu.Lock()
	defer s.aiProviderUpdateMu.Unlock()
	var in struct {
		Provider  string  `json:"provider"`
		BaseURL   string  `json:"baseUrl"`
		Model     string  `json:"model"`
		APIKey    *string `json:"apiKey"`
		MaxTokens *int    `json:"maxTokens"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if provider != "deepseek" && provider != "openai-compatible" {
		problem(w, http.StatusUnprocessableEntity, "模型来源必须是 deepseek 或 openai-compatible")
		return
	}
	defer s.lockAISync()()
	current := s.aiProviderRuntime.CurrentConfig()
	if s.aiProviderStore != nil {
		// Another replica may have saved since this process last reconciled;
		// the stored settings decide the kept API key and the rollback target.
		if saved, found, err := s.aiProviderStore.LoadAIProviderConfig(r.Context()); err != nil {
			problem(w, http.StatusServiceUnavailable, "读取已保存的模型配置失败，请稍后重试")
			return
		} else if found {
			current = saved
		}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if baseURL == "" && provider == "deepseek" {
		baseURL = "https://api.deepseek.com"
	}
	if err := validateAIProviderURL(baseURL); err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len([]rune(baseURL)) > 2048 {
		problem(w, http.StatusUnprocessableEntity, "baseUrl 不能超过 2048 个字符")
		return
	}
	modelName := strings.TrimSpace(in.Model)
	if modelName == "" {
		if provider == current.Provider {
			modelName = current.Model
		}
		if modelName == "" && s.engine.AIPlugins != nil {
			for _, item := range s.engine.AIPlugins.List() {
				if item.ID == provider {
					modelName = item.DefaultModel
					break
				}
			}
		}
	}
	if !validAIModelName(modelName) {
		problem(w, http.StatusUnprocessableEntity, "model 名称只能以字母或数字开头，并包含字母、数字、点、冒号、斜线、下划线或短横线")
		return
	}
	apiKey := ""
	if in.APIKey != nil {
		apiKey = strings.TrimSpace(*in.APIKey)
	} else if provider == current.Provider && sameProviderURL(baseURL, current.BaseURL) {
		// The stored key is only ever sent to the address it was saved for.
		apiKey = current.APIKey
	}
	// Some compatible API services do not require an API key.
	if provider == "deepseek" && apiKey == "" {
		problem(w, http.StatusUnprocessableEntity, "DeepSeek 必须填写 API Key")
		return
	}
	maxTokens := effectiveAIMaxTokens(current.MaxTokens)
	if in.MaxTokens != nil {
		if *in.MaxTokens < 128 || *in.MaxTokens > 8192 {
			problem(w, http.StatusUnprocessableEntity, "最大输出词元必须在 128 到 8192 之间")
			return
		}
		maxTokens = *in.MaxTokens
	}
	candidate := ports.AIPluginConfig{Provider: provider, BaseURL: baseURL, Model: modelName, APIKey: apiKey, MaxTokens: maxTokens}
	configureCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.aiProviderRuntime.Configure(configureCtx, candidate); err != nil {
		problem(w, http.StatusBadGateway, "模型服务配置无效，请检查地址、模型和接口密钥")
		return
	}
	if s.aiWorkflowProvider != nil {
		if err := s.aiWorkflowProvider.ConfigureProvider(configureCtx, candidate); err != nil {
			// Some Harness instances may already have switched before another
			// refused; put every path back on the previous configuration.
			s.rollbackAIProvider(current)
			if errors.Is(err, ports.ErrAIWorkflowRunsActive) {
				problem(w, http.StatusConflict, "有 AI 工作流正在运行，请等待任务结束后重试；模型配置未保存，原配置继续生效")
				return
			}
			problem(w, http.StatusBadGateway, "AI Workflow Harness 更新失败，Provider 未切换")
			return
		}
	}
	if s.aiProviderStore != nil {
		persistCtx, persistCancel := context.WithTimeout(r.Context(), 5*time.Second)
		err := s.aiProviderStore.SaveAIProviderConfig(persistCtx, candidate)
		persistCancel()
		if err != nil {
			if s.log != nil {
				s.log.Error("persist AI provider config", "provider", provider, "model", modelName, "error", err)
			}
			// Reconciliation pushes the stored configuration, so an unsaved
			// switch would be undone silently a few seconds later; undo it now
			// and report the configuration that actually stays in effect.
			s.rollbackAIProvider(current)
			s.failure(w, r, err, "模型配置保存失败，原配置继续生效")
			return
		}
	}
	s.audit(r, "ai.provider.update", "ai-provider", provider, map[string]any{"model": modelName, "apiKeyConfigured": apiKey != ""})
	info := s.aiProviderRuntime.ProviderInfo()
	write(w, http.StatusOK, s.aiProviderConfigView(r, candidate, info))
}

// rollbackAIProvider restores the previous provider on the direct runtime and
// every Harness instance. A failed restore is logged; reconciliation then
// pushes the stored (previous) configuration.
func (s *Server) rollbackAIProvider(previous ports.AIPluginConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.aiProviderRuntime.Configure(ctx, previous); err != nil && s.log != nil {
		s.log.Error("restore AI provider runtime", "provider", previous.Provider, "error", err)
	}
	if s.aiWorkflowProvider == nil {
		return
	}
	if err := s.aiWorkflowProvider.ConfigureProvider(ctx, previous); err != nil && s.log != nil {
		s.log.Warn("restore Harness provider; reconciliation will retry", "provider", previous.Provider, "error", err)
	}
}

func validateAIProviderURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("baseUrl 必须是没有凭据、查询参数或片段的 HTTP(S) 地址")
	}
	return nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func validAIModelName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 128 {
		return false
	}
	for index, char := range value {
		if index == 0 && !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return false
		}
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:/-", char)) {
			return false
		}
	}
	return true
}

func (s *Server) testAIProvider(w http.ResponseWriter, r *http.Request) {
	if !s.platformActionAllowed(r) {
		problem(w, http.StatusForbidden, "模型、向量服务与智能体配置对全平台生效，只能由平台管理员或运维租户修改")
		return
	}
	var in struct {
		ports.AIPluginConfig
		Question string `json:"question"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if s.engine.AIPlugins == nil {
		problem(w, 503, "AI plugin registry is unavailable")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if provider != "deepseek" && provider != "openai-compatible" {
		problem(w, http.StatusUnprocessableEntity, "模型来源必须是 deepseek 或 openai-compatible")
		return
	}
	in.Provider = provider
	current := ports.AIPluginConfig{}
	if s.aiProviderRuntime != nil {
		current = s.aiProviderRuntime.CurrentConfig()
	}
	// The key is intentionally redacted from GET responses. When an
	// administrator tests the already active API provider with a blank key,
	// reuse the server-side key instead of forcing it to be entered again.
	reuseStoredKey := strings.TrimSpace(in.APIKey) == "" && provider == strings.ToLower(strings.TrimSpace(current.Provider))
	if strings.TrimSpace(in.Question) == "" {
		in.Question = "请用一句话说明你已经连接到消防物联网 AI 测试台。"
	}
	if len([]rune(in.Question)) > 2000 {
		problem(w, 422, "question is too long")
		return
	}
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" {
		if provider == strings.ToLower(strings.TrimSpace(current.Provider)) {
			baseURL = strings.TrimSpace(current.BaseURL)
		}
		if baseURL == "" && provider == "deepseek" {
			baseURL = "https://api.deepseek.com"
		}
	}
	if err := validateAIProviderURL(baseURL); err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// The stored key is only ever sent to the address it was saved for.
	if reuseStoredKey && sameProviderURL(baseURL, current.BaseURL) {
		in.APIKey = current.APIKey
	}
	in.BaseURL = baseURL
	client, err := s.engine.AIPlugins.Create(in.AIPluginConfig)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	info := ports.AIPluginInfo{ID: in.Provider, Model: in.Model}
	if provider, ok := client.(ports.AIInspectable); ok {
		info = provider.ProviderInfo()
	}
	if !info.Enabled {
		problem(w, 422, "请选择已启用的模型服务")
		return
	}
	traceID := "ai_trace_" + randomHex(10)
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	answer, callErr := client.Chat(ctx, claims(r).TenantID, in.Question)
	latency := time.Since(started).Milliseconds()
	audit := model.AIToolCallLog{ID: traceID, TenantID: claims(r).TenantID, Actor: claims(r).Username, Tool: "ai.provider.test", Input: map[string]any{"provider": info.ID, "model": info.Model, "questionLength": len([]rune(in.Question))}, Success: callErr == nil, CreatedAt: time.Now().UnixMilli()}
	result := map[string]any{"traceId": traceID, "success": callErr == nil, "provider": info.ID, "providerName": info.Name, "model": info.Model, "latencyMs": latency}
	if callErr != nil {
		errorCode, publicError := safeProviderTestError(callErr)
		audit.Error = errorCode
		result["errorCode"] = errorCode
		result["error"] = publicError
	} else {
		audit.Output = map[string]any{"answerLength": len([]rune(answer)), "latencyMs": latency}
		result["answer"] = answer
	}
	auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer auditCancel()
	if auditErr := s.engine.Repo.SaveAIToolCall(auditCtx, audit); auditErr != nil {
		if s.log != nil {
			s.log.Error("persist AI provider test audit", "traceId", traceID, "error", auditErr)
		}
		s.failure(w, r, auditErr, "AI provider test completed but its audit trace could not be persisted")
		return
	}
	write(w, 200, result)
}

func safeProviderTestError(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "AI_PROVIDER_TIMEOUT", "模型服务请求超时，请检查服务状态后重试"
	}
	if errors.Is(err, context.Canceled) {
		return "AI_PROVIDER_CANCELED", "模型服务请求已取消"
	}
	return "AI_PROVIDER_REQUEST_FAILED", "模型服务请求失败，请检查地址、接口密钥、模型和服务状态"
}

// sameProviderURL compares model service addresses ignoring a trailing slash.
func sameProviderURL(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}
