package app

import (
	"context"

	"github.com/egoist/mygo"

	"nib/internal/domain"
)

// DirectorService 提供「全局 AI 导演」自然语言多步操控全书的桌面 RPC 绑定。
type DirectorService struct {
	rt *Runtime
}

// NewDirectorService 构造绑定到共享 Runtime 的 DirectorService。
func NewDirectorService(rt *Runtime) *DirectorService {
	return &DirectorService{rt: rt}
}

// SendInstruction 向 AI 导演下达自然语言指令，通过 *mygo.Channel[domain.DirectorEvent] 实时推送规划与工具执行过程。
func (s *DirectorService) SendInstruction(
	ctx context.Context,
	req domain.DirectorChatRequest,
	ch *mygo.Channel[domain.DirectorEvent],
) (domain.DirectorReply, error) {
	if err := req.Validate(); err != nil {
		return domain.DirectorReply{}, err
	}
	eng, runCtx, cancel, release, err := s.rt.trackDirectorEngine(ctx)
	if err != nil {
		return domain.DirectorReply{}, err
	}
	defer release()

	reply, err := eng.RunDirectorChat(runCtx, req, channelEmitter(runCtx, cancel, ch))
	if err != nil {
		return domain.DirectorReply{}, err
	}
	if reply.ToolCalls == nil {
		reply.ToolCalls = []domain.DirectorToolCall{}
	}
	return reply, nil
}

// ListMessages 返回指定书籍的 AI 导演持久化对话记录。
func (s *DirectorService) ListMessages(ctx context.Context, bookID int64) ([]domain.DirectorMessage, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	msgs, err := st.ListDirectorMessages(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if msgs == nil {
		return []domain.DirectorMessage{}, nil
	}
	return msgs, nil
}

// ClearMessages 清空指定书籍的 AI 导演历史对话。
func (s *DirectorService) ClearMessages(ctx context.Context, bookID int64) error {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return err
	}
	st, err := s.rt.store()
	if err != nil {
		return err
	}
	return st.ClearDirectorMessages(ctx, bookID)
}

// UndoDirectorTurn 通过安全撤销点 ID 将书库原子恢复到该轮 AI 导演执行写库工具之前的状态。
func (s *DirectorService) UndoDirectorTurn(ctx context.Context, snapshotID int64) (string, error) {
	if err := validatePositiveID("snapshotId", snapshotID); err != nil {
		return "", err
	}
	st, err := s.rt.store()
	if err != nil {
		return "", err
	}
	return st.RestoreSafetySnapshot(ctx, snapshotID)
}
