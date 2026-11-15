package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/noosphere/backend/internal/workspaces"
)

// ChatRewriter sends the current memory and an explicit edit request through
// the chosen chat provider. It receives no project files or conversation history.
type ChatRewriter func(ctx context.Context, workspaceID, sessionID, modelID, current, instruction string) (string, error)

func (s *Service) SetChatRewriter(rewrite ChatRewriter) { s.rewrite = rewrite }

type memoryRevision struct {
	Revision            int     `json:"revision"`
	ExpectedProjectPath *string `json:"expected_project_path"`
}

type rewriteRequest struct {
	memoryRevision
	SessionID   string `json:"session_id,omitempty"`
	ModelID     string `json:"model_id,omitempty"`
	Instruction string `json:"instruction"`
}

type rewriteProposal struct {
	Before Memory `json:"before"`
	After  string `json:"after"`
}

// checkedMemory is called with s.mu held. An expected path is explicit even
// for the local workspace before a project folder is attached.
func (s *Service) checkedMemory(wsID, id string, input memoryRevision) (workspaces.Workspace, map[string]Memory, Memory, error) {
	if s.eng == nil {
		return workspaces.Workspace{}, nil, Memory{}, fmt.Errorf("记忆引擎不可用")
	}
	ws, err := s.manager.Workspace(wsID)
	if err != nil {
		return workspaces.Workspace{}, nil, Memory{}, err
	}
	if input.ExpectedProjectPath == nil || *input.ExpectedProjectPath != ws.ProjectPath {
		return ws, nil, Memory{}, ErrConflict
	}
	items := s.records(wsID)
	item, exists := items[id]
	if !exists || item.Revision != input.Revision || item.Source.ProjectPath != ws.ProjectPath {
		return ws, nil, Memory{}, ErrConflict
	}
	return ws, items, item, nil
}

func (s *Service) suggest(ctx context.Context, wsID, id string, input rewriteRequest) (rewriteProposal, error) {
	instruction := strings.TrimSpace(input.Instruction)
	if instruction == "" || len(instruction) > 4000 || len(input.SessionID) > 128 || len(input.ModelID) > 256 {
		return rewriteProposal{}, fmt.Errorf("请输入 1–4000 字节的记忆修改要求")
	}

	s.mu.Lock()
	_, _, before, err := s.checkedMemory(wsID, id, input.memoryRevision)
	s.mu.Unlock()
	if err != nil {
		return rewriteProposal{}, err
	}
	if before.Status == "superseded" || before.Status == "rejected" {
		return rewriteProposal{}, fmt.Errorf("请从当前的记忆发起改写")
	}
	if s.rewrite == nil {
		return rewriteProposal{}, fmt.Errorf("模型改写服务不可用")
	}
	after, err := s.rewrite(ctx, wsID, input.SessionID, input.ModelID, before.Content, instruction)
	if err != nil {
		return rewriteProposal{}, err
	}
	if err := ctx.Err(); err != nil {
		return rewriteProposal{}, err
	}
	after = strings.TrimSpace(after)
	if after == "" || len(after) > 16000 {
		return rewriteProposal{}, fmt.Errorf("模型未生成可保存的记忆，请调整要求后重试")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, _, err = s.checkedMemory(wsID, id, input.memoryRevision); err != nil {
		return rewriteProposal{}, err
	}
	return rewriteProposal{Before: before, After: after}, nil
}

func (s *Service) remove(ctx context.Context, wsID, id string, input memoryRevision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, items, _, err := s.checkedMemory(wsID, id, input)
	if err != nil {
		return err
	}
	delete(items, id)
	return s.publish(ctx, ws, items)
}
