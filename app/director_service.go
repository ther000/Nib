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

	emit := func(ev domain.DirectorEvent) error {
		if ch == nil {
			return nil
		}
		if sendErr := ch.Send(ev); sendErr != nil {
			cancel()
			return sendErr
		}
		return nil
	}

	reply, err := eng.RunDirectorChat(runCtx, req, emit)
	if err != nil {
		return domain.DirectorReply{}, err
	}
	if reply.ToolCalls == nil {
		reply.ToolCalls = []domain.DirectorToolCall{}
	}
	return reply, nil
}
