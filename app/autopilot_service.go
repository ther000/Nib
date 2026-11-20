package app

import (
	"context"

	"github.com/egoist/mygo"

	"nib/internal/domain"
)

// AutopilotProgressEvent 向前端实时广播全自动托管流水线的推进状态与决策闸门（映射为 events.autopilotProgress）。
var AutopilotProgressEvent = mygo.NewEvent[domain.AutopilotState]("autopilot:progress")

// EmitAutopilotProgress 广播全自动托管实时状态。
func EmitAutopilotProgress(state domain.AutopilotState) {
	_ = AutopilotProgressEvent.Broadcast(state.Normalized())
}

// AutopilotService 提供全自动托管写作的启动、查询、停止与决策闸门恢复 RPC 绑定。
type AutopilotService struct {
	rt *Runtime
}

// NewAutopilotService 构造绑定到共享 Runtime 的 AutopilotService。
func NewAutopilotService(rt *Runtime) *AutopilotService {
	return &AutopilotService{rt: rt}
}

// StartAutopilot 启动指定书籍的全自动托管写作循环。
func (s *AutopilotService) StartAutopilot(ctx context.Context, req domain.AutopilotRequest) (domain.AutopilotState, error) {
	if err := req.Validate(); err != nil {
		return domain.AutopilotState{}, err
	}
	eng, err := s.rt.autopilotEngine()
	if err != nil {
		return domain.AutopilotState{}, err
	}
	state, err := eng.StartAutopilot(ctx, req)
	if err != nil {
		return domain.AutopilotState{}, err
	}
	return state.Normalized(), nil
}

// ResumeAutopilot 从最近一次中断或暂停的持久化托管记录继续推进剩余章节。
func (s *AutopilotService) ResumeAutopilot(ctx context.Context, bookID int64) (domain.AutopilotState, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.AutopilotState{}, err
	}
	eng, err := s.rt.autopilotEngine()
	if err != nil {
		return domain.AutopilotState{}, err
	}
	state, err := eng.ResumeAutopilot(ctx, bookID)
	if err != nil {
		return domain.AutopilotState{}, err
	}
	return state.Normalized(), nil
}

// GetAutopilotState 查询指定书籍当前或最近一次托管任务状态。
func (s *AutopilotService) GetAutopilotState(ctx context.Context, bookID int64) (domain.AutopilotState, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.AutopilotState{}, err
	}
	eng, err := s.rt.autopilotEngine()
	if err != nil {
		return domain.AutopilotState{}, err
	}
	return eng.GetAutopilotState(bookID).Normalized(), nil
}

// StopAutopilot 请求平滑中止指定书籍的托管任务。
func (s *AutopilotService) StopAutopilot(ctx context.Context, bookID int64) (domain.AutopilotState, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.AutopilotState{}, err
	}
	eng, err := s.rt.autopilotEngine()
	if err != nil {
		return domain.AutopilotState{}, err
	}
	state, err := eng.StopAutopilot(bookID)
	if err != nil {
		return domain.AutopilotState{}, err
	}
	return state.Normalized(), nil
}

// ResolveAutopilotGate 提交作者在决策闸门中的选择，恢复或停止托管流水线。
func (s *AutopilotService) ResolveAutopilotGate(ctx context.Context, bookID int64, action domain.AutopilotDecisionAction) (domain.AutopilotState, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.AutopilotState{}, err
	}
	if !action.Valid() {
		return domain.AutopilotState{}, &domain.ValidationError{
			Field:   "action",
			Message: "无效的决策操作",
		}
	}
	eng, err := s.rt.autopilotEngine()
	if err != nil {
		return domain.AutopilotState{}, err
	}
	state, err := eng.ResolveAutopilotGate(bookID, action)
	if err != nil {
		return domain.AutopilotState{}, err
	}
	return state.Normalized(), nil
}
