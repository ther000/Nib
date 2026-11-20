package domain

import (
	"net/url"
	"strings"
)

// Provider 是本地 SQLite 持久化的供应商完整记录。
// APIKey 标记 json:"-" 防止意外序列化泄露给前端或日志；对外返回必须调用 ToPublic()。
type Provider struct {
	ID            int64            `json:"id"`
	Name          string           `json:"name"`
	Protocol      ProviderProtocol `json:"protocol"`
	BaseURL       string           `json:"baseUrl"`
	APIKey        string           `json:"-"`
	Models        []string         `json:"models"`
	FetchedModels []string         `json:"fetchedModels"`
	Enabled       bool             `json:"enabled"`
	CreatedAt     string           `json:"createdAt"`
	UpdatedAt     string           `json:"updatedAt"`
}

// ProviderPublic 是向前端透出的脱敏供应商视图，仅暴露 HasAPIKey 与尾号掩码。
type ProviderPublic struct {
	ID            int64            `json:"id"`
	Name          string           `json:"name"`
	Protocol      ProviderProtocol `json:"protocol"`
	BaseURL       string           `json:"baseUrl"`
	HasAPIKey     bool             `json:"hasApiKey"`
	APIKeyMasked  string           `json:"apiKeyMasked"`
	Models        []string         `json:"models"`
	FetchedModels []string         `json:"fetchedModels"`
	Enabled       bool             `json:"enabled"`
	CreatedAt     string           `json:"createdAt"`
	UpdatedAt     string           `json:"updatedAt"`
}

// ToPublic 将完整 Provider 转为安全脱敏的 ProviderPublic。
func (p Provider) ToPublic() ProviderPublic {
	models := make([]string, len(p.Models))
	copy(models, p.Models)
	fetched := make([]string, len(p.FetchedModels))
	copy(fetched, p.FetchedModels)
	trimmedKey := strings.TrimSpace(p.APIKey)
	return ProviderPublic{
		ID:            p.ID,
		Name:          p.Name,
		Protocol:      p.Protocol,
		BaseURL:       p.BaseURL,
		HasAPIKey:     trimmedKey != "",
		APIKeyMasked:  MaskAPIKey(trimmedKey),
		Models:        models,
		FetchedModels: fetched,
		Enabled:       p.Enabled,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

// MaskAPIKey 将 API Key 脱敏为尾号掩码（如 "sk-...a8f2" 或 "***...a8f2"）。
// 任何非空短密钥（<=8 字符）统一返回 "****"，绝不回显原文。
func MaskAPIKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 8 {
		return "****"
	}
	tail := string(runes[len(runes)-4:])

	// 若具备短前缀（如 sk- / pk- / gsk-）且总长度足够安全，保留前缀与尾 4 位。
	if len(runes) >= 12 {
		if dashIdx := strings.IndexRune(trimmed, '-'); dashIdx >= 2 && dashIdx <= 4 {
			prefix := trimmed[:dashIdx]
			if isASCIIAlphaNum(prefix) {
				return prefix + "-..." + tail
			}
		}
	}
	return "***..." + tail
}

func isASCIIAlphaNum(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'z'
		isUpper := c >= 'A' && c <= 'Z'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

// RedactSecretHeaders 对 HTTP 请求头中的认证与密钥字段进行脱敏，便于安全记录调试日志。
func RedactSecretHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if isSensitiveHeaderName(k) {
			out[k] = maskHeaderValue(v)
		} else {
			out[k] = v
		}
	}
	return out
}

func isSensitiveHeaderName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key", "cookie", "set-cookie":
		return true
	}
	return strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key")
}

func maskHeaderValue(val string) string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > 7 && strings.EqualFold(trimmed[:7], "bearer ") {
		return "Bearer " + MaskAPIKey(trimmed[7:])
	}
	return MaskAPIKey(trimmed)
}

// UpsertProviderInput 是新增/更新供应商的入参 DTO。
// 更新既有供应商（ID > 0）时，APIKey 留空表示沿用数据库中已保存的密钥。
type UpsertProviderInput struct {
	ID            int64            `json:"id"`
	Name          string           `json:"name"`
	Protocol      ProviderProtocol `json:"protocol"`
	BaseURL       string           `json:"baseUrl"`
	APIKey        string           `json:"apiKey"`
	Models        []string         `json:"models"`
	FetchedModels []string         `json:"fetchedModels,omitempty"`
	Enabled       bool             `json:"enabled"`
}

func (in UpsertProviderInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return newValidationError("name", "供应商名称不能为空")
	}
	if !in.Protocol.Valid() {
		return newValidationError("protocol", "不支持的协议类型")
	}
	rawURL := strings.TrimSpace(in.BaseURL)
	if rawURL == "" {
		return newValidationError("baseUrl", "Base URL 不能为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return newValidationError("baseUrl", "Base URL 必须是合法的 http/https 地址")
	}
	return nil
}

// ProviderTestResult 表示一次供应商连通性与测速探测结果。
type ProviderTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	ModelUsed string `json:"modelUsed"`
	Message   string `json:"message"`
}

// FetchModelsResult 表示自动拉取供应商可用模型列表的结果。
type FetchModelsResult struct {
	Models        []string `json:"models"`
	EnabledModels []string `json:"enabledModels"`
	Count         int      `json:"count"`
	Message       string   `json:"message"`
}

// ModelRoute 表示单个创作环节的模型路由配置（对应 model_routes）。
type ModelRoute struct {
	Stage       PipelineStage `json:"stage"`
	ProviderID  *int64        `json:"providerId,omitempty"`
	ModelName   string        `json:"modelName"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"maxTokens"`
	UpdatedAt   string        `json:"updatedAt"`
}

func (r ModelRoute) Validate() error {
	if !r.Stage.Valid() {
		return newValidationError("stage", "无效的流水线阶段")
	}
	if r.Temperature < 0 || r.Temperature > 2.0 {
		return newValidationError("temperature", "温度参数必须在 0.0 到 2.0 之间")
	}
	if r.MaxTokens < 0 || r.MaxTokens > 200000 {
		return newValidationError("maxTokens", "最大输出 Token 范围无效")
	}
	if r.ProviderID != nil && *r.ProviderID <= 0 {
		return newValidationError("providerId", "供应商 ID 必须大于 0")
	}
	return nil
}

// DefaultModelRoutes 返回文档规定的 5 环节推荐默认参数。
func DefaultModelRoutes() []ModelRoute {
	return []ModelRoute{
		{Stage: StageOutline, Temperature: 0.65, MaxTokens: 4096},
		{Stage: StageDraft, Temperature: 0.80, MaxTokens: 4096},
		{Stage: StageCritic, Temperature: 0.20, MaxTokens: 2048},
		{Stage: StagePolish, Temperature: 0.55, MaxTokens: 2048},
		{Stage: StageExtract, Temperature: 0.10, MaxTokens: 2048},
	}
}
