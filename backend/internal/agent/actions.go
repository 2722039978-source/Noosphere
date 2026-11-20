package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/noosphere/backend/internal/workspaces"
)

const maxActionJournalBytes = 512 * 1024

type Action struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	RunID         string          `json:"run_id"`
	Root          string          `json:"project_path"`
	ProjectID     string          `json:"project_id"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	Reason        string          `json:"reason"`
	Path          string          `json:"path,omitempty"`
	Before        string          `json:"before,omitempty"`
	After         string          `json:"after,omitempty"`
	Existed       bool            `json:"existed"`
	BeforeHash    string          `json:"before_hash,omitempty"`
	AfterHash     string          `json:"after_hash,omitempty"`
	FileDirectory string          `json:"file_directory_identity,omitempty"`
	Mode          uint32          `json:"file_mode,omitempty"`
	Runner        string          `json:"runner,omitempty"`
	Command       string          `json:"command,omitempty"`
	Test          *testInvocation `json:"test,omitempty"`
	Service       string          `json:"service,omitempty"`
	Target        string          `json:"target,omitempty"`
	Previous      string          `json:"previous,omitempty"`
	Output        string          `json:"output,omitempty"`
	Error         string          `json:"error,omitempty"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}
type ActionExecutor interface {
	ServiceState(context.Context, string) (string, error)
	Execute(context.Context, Action) (string, error)
}
type Actions struct {
	mu       sync.Mutex
	dir      string
	manager  *workspaces.Manager
	executor ActionExecutor
	items    map[string]Action
	cancel   map[string]context.CancelFunc
	workers  sync.WaitGroup
	closed   bool
}

func actionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func hashText(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func OpenActions(dir string, m *workspaces.Manager, executor ActionExecutor) (*Actions, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Actions{dir: dir, manager: m, executor: executor, items: map[string]Action{}, cancel: map[string]context.CancelFunc{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("linked action journal")
		}
		info, e := entry.Info()
		if e != nil || info.Size() > maxActionJournalBytes {
			return nil, fmt.Errorf("invalid action journal size")
		}
		raw, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			return nil, e
		}
		var a Action
		if json.Unmarshal(raw, &a) != nil || a.ID+".json" != entry.Name() {
			return nil, fmt.Errorf("invalid action journal")
		}
		if a.Status == "running" || a.Status == "applying" || a.Status == "rolling_back" {
			a.Status = "interrupted"
			a.Error = "执行期间应用退出；不会自动重放。文件操作可尝试恢复原文件，其他操作请核查实际状态。"
			if err = s.save(a); err != nil {
				return nil, err
			}
		} else {
			s.items[a.ID] = a
		}
	}
	return s, nil
}
func (s *Actions) save(a Action) error {
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(a); err != nil {
		return err
	}
	limit := maxActionJournalBytes
	if a.Status == "pending" {
		// File proposals retain their payload throughout execution; reserve space
		// for subsequent status and recovery errors before accepting one.
		limit -= 8 * 1024
	}
	if encoded.Len() > limit {
		return fmt.Errorf("操作记录超过存储限制，请缩小修改范围")
	}
	raw := encoded.Bytes()
	name := filepath.Join(s.dir, a.ID+".json")
	temp := name + ".tmp"
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temp, name); err != nil {
		return err
	}
	s.items[a.ID] = a
	return nil
}
func (s *Actions) Close() {
	s.mu.Lock()
	s.closed = true
	for _, cancel := range s.cancel {
		cancel()
	}
	s.mu.Unlock()
	s.workers.Wait()
}
func (s *Actions) List(workspaceID, runID string) []Action {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Action{}
	for _, a := range s.items {
		if a.WorkspaceID == workspaceID && a.RunID == runID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}
func (s *Actions) binding(a Action) error {
	ws, err := s.manager.Workspace(a.WorkspaceID)
	if err != nil {
		return err
	}
	if ws.ProjectPath != a.Root || ws.ProjectID != a.ProjectID {
		return fmt.Errorf("工作区项目已变化，请重新生成操作预览")
	}
	return nil
}

func (s *Actions) Propose(ctx context.Context, ws workspaces.Workspace, runID, name string, input json.RawMessage) (Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a Action
	if s.closed {
		return a, fmt.Errorf("action service closed")
	}
	count := 0
	for _, item := range s.items {
		if item.RunID == runID {
			count++
		}
	}
	if count >= 3 {
		return a, fmt.Errorf("每个任务最多提出 3 个操作，请分步处理")
	}
	a = Action{ID: actionID(), WorkspaceID: ws.ID, Root: ws.ProjectPath, ProjectID: ws.ProjectID, RunID: runID, Status: "pending", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := s.binding(a); err != nil {
		return Action{}, err
	}
	if err := ctx.Err(); err != nil {
		return Action{}, err
	}
	switch name {
	case "propose_file_change":
		var p struct {
			Path   string  `json:"path"`
			Old    string  `json:"old_text"`
			New    *string `json:"new_text"`
			Reason string  `json:"reason"`
		}
		if err := strict(input, &p); err != nil {
			return Action{}, err
		}
		if p.New == nil {
			return Action{}, fmt.Errorf("必须明确提供 new_text 字符串")
		}
		target, err := inspectFile(ws.ProjectPath, p.Path)
		if err != nil {
			return Action{}, err
		}
		target.parent.Close()
		before := target.text
		after := *p.New
		if target.exists {
			if p.Old == "" || strings.Count(before, p.Old) != 1 {
				return Action{}, fmt.Errorf("待替换文本必须在当前文件中唯一匹配，请先读取源码")
			}
			after = strings.Replace(before, p.Old, *p.New, 1)
		} else if p.Old != "" {
			return Action{}, fmt.Errorf("新文件不能指定旧文本")
		}
		if len(after) > 64*1024 || !utf8.ValidString(after) || strings.ContainsRune(after, 0) || before == after {
			return Action{}, fmt.Errorf("修改为空或超出文本限制")
		}
		if redact(before) != before || redact(after) != after {
			return Action{}, fmt.Errorf("文件包含疑似凭据，不允许通过 Agent 修改")
		}
		a.Kind = "file"
		a.Path = p.Path
		a.Before = before
		a.After = after
		a.Existed = target.exists
		a.Mode = uint32(target.mode)
		a.BeforeHash = hashText(before)
		a.AfterHash = hashText(after)
		a.FileDirectory = target.directoryID
		a.Reason = clip(redact(p.Reason), 500)
	case "propose_test":
		var p struct {
			Runner    string `json:"runner"`
			Directory string `json:"directory"`
			Reason    string `json:"reason"`
		}
		if err := strict(input, &p); err != nil {
			return Action{}, err
		}
		if p.Directory == "" {
			p.Directory = "."
		}
		if s.executor == nil {
			return Action{}, fmt.Errorf("测试运行器不可用")
		}
		a.Kind = "test"
		a.Runner = p.Runner
		a.Path = p.Directory
		if err := planTest(&a); err != nil {
			return Action{}, err
		}
		a.Reason = clip(redact(p.Reason), 500)
	case "propose_service_change":
		var p struct {
			Name   string `json:"name"`
			Target string `json:"target"`
			Reason string `json:"reason"`
		}
		if err := strict(input, &p); err != nil {
			return Action{}, err
		}
		if !serviceName.MatchString(p.Name) || (p.Target != "running" && p.Target != "stopped") || s.executor == nil {
			return Action{}, fmt.Errorf("无效服务操作")
		}
		state, err := s.executor.ServiceState(ctx, p.Name)
		if err != nil {
			return Action{}, err
		}
		if state != "running" && state != "stopped" {
			return Action{}, fmt.Errorf("服务不在稳定状态")
		}
		if state == p.Target {
			return Action{}, fmt.Errorf("服务已经处于目标状态")
		}
		a.Kind = "service"
		a.Service = p.Name
		a.Target = p.Target
		a.Previous = state
		a.Command = "sc " + map[string]string{"running": "start", "stopped": "stop"}[p.Target] + " " + p.Name
		a.Reason = clip(redact(p.Reason), 500)
	default:
		return Action{}, fmt.Errorf("未知操作类型")
	}
	if err := ctx.Err(); err != nil {
		return Action{}, err
	}
	if err := s.save(a); err != nil {
		return Action{}, err
	}
	return s.items[a.ID], nil
}
func (s *Actions) Decide(id, workspaceID, decision string) (Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.items[id]
	if !ok || a.WorkspaceID != workspaceID {
		return Action{}, fmt.Errorf("操作不存在或不属于当前工作区")
	}
	if s.closed {
		return a, fmt.Errorf("action service closed")
	}
	if decision == "reject" && a.Status == "pending" {
		a.Status = "rejected"
		return a, s.save(a)
	}
	if decision == "cancel" && a.Status == "running" {
		if cancel := s.cancel[id]; cancel != nil {
			cancel()
		}
		return a, nil
	}
	rollback := decision == "rollback"
	if decision != "approve" && !rollback {
		return a, fmt.Errorf("操作状态不允许该请求")
	}
	if (!rollback && a.Status != "pending") || (rollback && a.Status != "applied" && a.Status != "interrupted") {
		return a, fmt.Errorf("操作已处理或正在执行，请刷新记录")
	}
	if rollback && a.Kind == "test" {
		return a, fmt.Errorf("测试产生的副作用不能自动回滚")
	}
	if err := s.binding(a); err != nil {
		return a, err
	}
	if a.Kind == "file" {
		return s.changeFile(a, rollback)
	}
	if len(s.cancel) > 0 {
		return a, fmt.Errorf("另一个测试或服务操作正在执行，请等待完成")
	}
	if a.Kind == "test" {
		if a.Test == nil {
			return a, fmt.Errorf("测试预览缺少已核实的工具链，请重新提出测试")
		}
		dir, rootID, dirID, err := testDirectory(a)
		if err != nil {
			return a, err
		}
		dir.Close()
		if rootID != a.Test.RootID || dirID != a.Test.DirectoryID {
			return a, fmt.Errorf("测试目录已替换，请重新提出测试")
		}
	}
	if a.Kind == "service" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, err := s.executor.ServiceState(ctx, a.Service)
		cancel()
		if err != nil {
			return a, err
		}
		if rollback && a.Status == "interrupted" && state == a.Previous {
			a.Status = "rolled_back"
			a.Error = ""
			return a, s.save(a)
		}
		expected := a.Previous
		if rollback {
			expected = a.Target
		}
		if state != expected {
			return a, fmt.Errorf("服务状态已变化，未执行；请重新检查")
		}
	}
	a.Status = "running"
	a.Error = ""
	if err := s.save(a); err != nil {
		return a, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	s.cancel[id] = cancel
	s.workers.Add(1)
	go s.execute(ctx, cancel, a, rollback)
	return a, nil
}
func (s *Actions) changeFile(a Action, rollback bool) (Action, error) {
	if !rollback && a.FileDirectory == "" {
		return a, fmt.Errorf("文件预览缺少目录身份，请重新生成操作预览")
	}
	target, err := inspectFile(a.Root, a.Path)
	if err != nil {
		return a, err
	}
	defer target.parent.Close()
	if a.FileDirectory != "" && target.directoryID != a.FileDirectory {
		return a, fmt.Errorf("文件所在目录已替换，请重新生成操作预览")
	}
	expected, expectedExists, content := a.BeforeHash, a.Existed, a.After
	if rollback {
		expected = a.AfterHash
		expectedExists = true
		content = a.Before
		// A crash before writing or after rollback is already safely at the original state.
		if a.Status == "interrupted" && target.exists == a.Existed && hashText(target.text) == a.BeforeHash {
			a.Status = "rolled_back"
			a.Error = ""
			return a, s.save(a)
		}
	}
	if target.exists != expectedExists || hashText(target.text) != expected {
		return a, fmt.Errorf("文件已被外部修改，未覆盖；请重新生成预览")
	}
	a.Status = "applying"
	if rollback {
		a.Status = "rolling_back"
	}
	a.Error = ""
	if err = s.save(a); err != nil {
		return a, err
	}
	recheck := func() error {
		if err := s.binding(a); err != nil {
			return err
		}
		live, err := inspectFile(a.Root, a.Path)
		if err != nil {
			return err
		}
		defer live.parent.Close()
		if live.directoryID != target.directoryID || live.exists != expectedExists || hashText(live.text) != expected {
			return fmt.Errorf("目录或文件已被外部修改，未覆盖")
		}
		return nil
	}
	name := path.Base(a.Path)
	if rollback && !a.Existed {
		if err = recheck(); err == nil {
			err = target.parent.Remove(name)
		}
	} else {
		err = replaceFile(target.parent, name, content, os.FileMode(a.Mode), !expectedExists, recheck)
	}
	if err != nil {
		a.Status = "interrupted"
		a.Error = "写入未完成，请检查文件后恢复"
		_ = s.save(a)
		return a, err
	}
	a.Status = "applied"
	if rollback {
		a.Status = "rolled_back"
	}
	if err = s.save(a); err != nil {
		return a, fmt.Errorf("文件已写入，但记录保存失败；请重启后核查并恢复")
	}
	return a, nil
}
func (s *Actions) execute(ctx context.Context, cancel context.CancelFunc, a Action, rollback bool) {
	defer s.workers.Done()
	defer cancel()
	command := a
	if rollback {
		command.Target = a.Previous
	}
	var output string
	err := ctx.Err()
	if err == nil {
		err = s.binding(a)
	}
	if err == nil {
		output, err = s.executor.Execute(ctx, command)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cancel, a.ID)
	a.Output = clip(redact(output), 16000)
	a.Status = "applied"
	if a.Kind == "test" {
		a.Status = "completed"
	}
	if rollback {
		a.Status = "rolled_back"
	}
	if err != nil || ctx.Err() != nil {
		a.Status = "failed"
		if err == nil {
			err = ctx.Err()
		}
		a.Error = clip(redact(err.Error()), 1000)
		if a.Kind == "service" {
			a.Status = "interrupted"
		}
	}
	if err := s.save(a); err != nil {
		pending := s.items[a.ID]
		pending.Status = "interrupted"
		pending.Error = "执行结果保存失败，请重启后核查实际状态"
		s.items[a.ID] = pending
	}
}
