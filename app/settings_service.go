package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"nib/internal/domain"
)

const (
	providerTestTimeout        = 10 * time.Second
	providerFetchModelsTimeout = 15 * time.Second
	autoEnableModelLimit       = 5
)

// SettingsService 提供多模型供应商（脱敏透出）、5 环节路由与本地导出目录选择绑定。
type SettingsService struct {
	rt *Runtime
}

// NewSettingsService 构造绑定到共享 Runtime 的 SettingsService。
func NewSettingsService(rt *Runtime) *SettingsService {
	return &SettingsService{rt: rt}
}

// ListProviders 返回全部供应商的脱敏视图（严禁向前端透出明文 API Key）。
func (s *SettingsService) ListProviders(ctx context.Context) ([]domain.ProviderPublic, error) {
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	providers, err := st.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ProviderPublic, len(providers))
	for i, p := range providers {
		out[i] = p.ToPublic()
	}
	return out, nil
}

// UpsertProvider 校验并保存供应商配置，仅返回脱敏后的 ProviderPublic。
func (s *SettingsService) UpsertProvider(ctx context.Context, input domain.UpsertProviderInput) (domain.ProviderPublic, error) {
	if err := input.Validate(); err != nil {
		return domain.ProviderPublic{}, err
	}
	if input.Models == nil {
		input.Models = []string{}
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.ProviderPublic{}, err
	}
	saved, err := st.UpsertProvider(ctx, input)
	if err != nil {
		return domain.ProviderPublic{}, err
	}
	return saved.ToPublic(), nil
}

// DeleteProvider 删除指定供应商配置。
func (s *SettingsService) DeleteProvider(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	st, err := s.rt.store()
	if err != nil {
		return err
	}
	return st.DeleteProvider(ctx, id)
}

// TestProvider 对已保存的供应商发起连通性与延迟探测（默认 10s 超时控制）。
func (s *SettingsService) TestProvider(ctx context.Context, id int64) (domain.ProviderTestResult, error) {
	if err := validatePositiveID("id", id); err != nil {
		return domain.ProviderTestResult{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.ProviderTestResult{}, err
	}
	client, err := s.rt.llm()
	if err != nil {
		return domain.ProviderTestResult{}, err
	}
	provider, err := st.GetProvider(ctx, id)
	if err != nil {
		return domain.ProviderTestResult{}, err
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		callCtx, cancel = context.WithTimeout(ctx, providerTestTimeout)
		defer cancel()
	}
	return client.TestConnection(callCtx, provider)
}

// FetchProviderModels 自动向已保存的供应商拉取上游模型目录：
// - 全量候选模型存入 FetchedModels；
// - 若供应商已有已启用模型，严格保留原有勾选；
// - 若尚未启用任何模型且上游返回 <= 5 个模型，默认全部启用以便立即可用；
// - 若上游返回 > 5 个模型且尚未启用任何模型，保持待用户按需勾选。
func (s *SettingsService) FetchProviderModels(ctx context.Context, id int64) (domain.FetchModelsResult, error) {
	if err := validatePositiveID("id", id); err != nil {
		return domain.FetchModelsResult{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.FetchModelsResult{}, err
	}
	client, err := s.rt.llm()
	if err != nil {
		return domain.FetchModelsResult{}, err
	}
	provider, err := st.GetProvider(ctx, id)
	if err != nil {
		return domain.FetchModelsResult{}, err
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		callCtx, cancel = context.WithTimeout(ctx, providerFetchModelsTimeout)
		defer cancel()
	}

	models, err := client.ListModels(callCtx, provider)
	if err != nil {
		return domain.FetchModelsResult{}, err
	}
	existingEnabled := provider.Models
	if existingEnabled == nil {
		existingEnabled = []string{}
	}
	if len(models) == 0 {
		existingFetched := provider.FetchedModels
		if existingFetched == nil {
			existingFetched = []string{}
		}
		return domain.FetchModelsResult{
			Models:        existingFetched,
			EnabledModels: existingEnabled,
			Count:         0,
			Message:       "上游未返回可用模型，已保留原列表",
		}, nil
	}

	nextEnabled := existingEnabled
	msg := fmt.Sprintf("已拉取 %d 个模型，保留已启用的 %d 个", len(models), len(nextEnabled))
	if len(existingEnabled) == 0 {
		if len(models) <= autoEnableModelLimit {
			nextEnabled = append([]string(nil), models...)
			msg = fmt.Sprintf("已拉取并默认启用 %d 个模型", len(models))
		} else {
			nextEnabled = []string{}
			msg = fmt.Sprintf("已拉取 %d 个模型，请勾选需要启用的模型", len(models))
		}
	}

	_, err = st.UpsertProvider(ctx, domain.UpsertProviderInput{
		ID:            provider.ID,
		Name:          provider.Name,
		Protocol:      provider.Protocol,
		BaseURL:       provider.BaseURL,
		APIKey:        "", // 留空以保留数据库中已保存的密钥
		Models:        nextEnabled,
		FetchedModels: models,
		Enabled:       provider.Enabled,
	})
	if err != nil {
		return domain.FetchModelsResult{}, err
	}
	return domain.FetchModelsResult{
		Models:        models,
		EnabledModels: nextEnabled,
		Count:         len(models),
		Message:       msg,
	}, nil
}

// SetWindowTheme 同步主窗口隐藏标题栏上的系统按钮配色（深色/浅色）。
func (s *SettingsService) SetWindowTheme(ctx context.Context, theme string) error {
	win := mygo.CallerWindow(ctx)
	if win == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "light":
		win.SetWindowButtonsTheme(mygo.ThemeLight)
	case "dark":
		win.SetWindowButtonsTheme(mygo.ThemeDark)
	default:
		win.SetWindowButtonsTheme(mygo.ThemeSystem)
	}
	return nil
}

// ListModelRoutes 返回全部 5 个创作环节的模型路由配置。
func (s *SettingsService) ListModelRoutes(ctx context.Context) ([]domain.ModelRoute, error) {
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	routes, err := st.ListModelRoutes(ctx)
	if err != nil {
		return nil, err
	}
	if routes == nil {
		return []domain.ModelRoute{}, nil
	}
	return routes, nil
}

// SaveModelRoute 校验并更新单个创作环节的模型路由配置。
func (s *SettingsService) SaveModelRoute(ctx context.Context, route domain.ModelRoute) (domain.ModelRoute, error) {
	if err := route.Validate(); err != nil {
		return domain.ModelRoute{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.ModelRoute{}, err
	}
	return st.SaveModelRoute(ctx, route)
}

// PickExportDir 调起系统原生文件夹选择对话框；若用户取消则返回空字符串与 nil 错误。
func (s *SettingsService) PickExportDir(ctx context.Context) (string, error) {
	picker := s.rt.dirPicker()
	return picker(ctx)
}

// GetUsageSummary 聚合指定书籍（或 bookID=0 时全局）的各环节、各模型 Token 消耗与调用统计。
func (s *SettingsService) GetUsageSummary(ctx context.Context, bookID int64) (domain.UsageSummary, error) {
	if bookID < 0 {
		return domain.UsageSummary{}, &domain.ValidationError{
			Field:   "bookId",
			Message: "书籍 ID 不能为负数",
		}
	}
	bs, err := s.rt.batchStore()
	if err != nil {
		return domain.UsageSummary{}, err
	}
	summary, err := bs.GetUsageSummary(ctx, bookID)
	if err != nil {
		return domain.UsageSummary{}, err
	}
	if summary.ByStage == nil {
		summary.ByStage = []domain.StageUsageStat{}
	}
	if summary.ByModel == nil {
		summary.ByModel = []domain.ModelUsageStat{}
	}
	return summary, nil
}
