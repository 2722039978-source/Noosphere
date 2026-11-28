package core

import (
	"encoding/json"
	"fmt"

	"github.com/noosphere/backend/internal/agent/contract"
	"github.com/noosphere/backend/internal/gateway/providers"
)

type decision string

const (
	allow decision = "allow"
	ask   decision = "ask"
	deny  decision = "deny"
)

type policyResult struct {
	Decision decision `json:"decision"`
	Reason   string   `json:"reason"`
}

func (e *engine) toolPolicy(call providers.ToolCall) policyResult {
	name := call.Function.Name
	if e.protected(name) != nil {
		return policyResult{ask, "需批准此次具体操作"}
	}
	for _, t := range e.definitions() {
		if t.Function.Name == name {
			if err := e.checkToolArguments(call); err != nil {
				return policyResult{deny, safeText(err.Error(), 360)}
			}
			return policyResult{allow, "工作区已授权此能力"}
		}
	}
	return policyResult{deny, "宿主未授予此能力"}
}

func (e *engine) checkToolArguments(call providers.ToolCall) error {
	limit := 32000
	if call.Function.Name == "generate_artifact" {
		limit = 128000
	}
	if len(call.Function.Arguments) > limit {
		return nil // The dispatcher's parameter limit reports this as an input error.
	}
	var args toolArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return nil
	}
	switch call.Function.Name {
	case "read_file", "write_file", "edit_file", "delete_file", "move_file", "read_conflict", "present", "generate_artifact":
		if checker, ok := e.d.(interface{ ValidatePath(string) error }); ok {
			if err := checker.ValidatePath(args.Path); err != nil {
				return err
			}
			if call.Function.Name == "move_file" {
				return checker.ValidatePath(args.To)
			}
			if call.Function.Name == "generate_artifact" {
				var doc struct {
					Sections []struct {
						ImagePath string `json:"image_path"`
					} `json:"sections"`
				}
				if json.Unmarshal([]byte(call.Function.Arguments), &doc) == nil {
					for _, s := range doc.Sections {
						if s.ImagePath != "" {
							if err := checker.ValidatePath(s.ImagePath); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	case "run_command":
		if e.o.CheckCommand != nil {
			return e.o.CheckCommand(args.Cwd, args.Program, args.Args)
		}
	}
	return nil
}

func (e *engine) publicationPolicy(changes []contract.Change) policyResult {
	if e.touchesConflicts(changes) {
		return policyResult{ask, "本轮合并了与外部修改重叠的文件，请审阅最新差异"}
	}
	if e.NeedsInspection {
		return policyResult{ask, "上次工具执行的结果尚未核实，请审阅草稿后决定是否提交"}
	}
	if len(e.FailedChecks) > 0 {
		return policyResult{ask, fmt.Sprintf("仍有 %d 项验证失败，请审阅后决定是否提交", len(e.FailedChecks))}
	}
	for _, c := range changes {
		if c.Kind == "deleted" {
			return policyResult{ask, "本轮包含文件删除，请确认整批改动"}
		}
	}
	if e.Mode == "review" {
		return policyResult{ask, "请审阅本轮文件改动"}
	}
	return policyResult{allow, "已授权发布本轮普通项目变更"}
}
