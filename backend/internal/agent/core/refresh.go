package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/noosphere/backend/internal/agent/contract"
	"github.com/noosphere/backend/internal/gateway/providers"
)

func cloneChecks(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (cp *checkpoint) upgradeVerificationKeys() {
	for old, description := range cp.Checked {
		// Complete command keys and presentation paths retain their old identity.
		if !strings.HasPrefix(old, `["`) || (json.Valid([]byte(old)) && !strings.Contains(old, "[redacted]")) {
			continue
		}
		raw, ok := strings.CutPrefix(description, "run_command ")
		key := ""
		var args toolArgs
		if ok && !strings.Contains(raw, "[redacted]") && json.Unmarshal([]byte(raw), &args) == nil &&
			safeText(verificationCommandArgs(args), 400) == old {
			var call providers.ToolCall
			call.Function.Name, call.Function.Arguments = "run_command", raw
			key = verificationKey(call)
		}
		if key != "" && key != old {
			for _, checks := range []map[string]string{cp.Checked, cp.Recheck, cp.RefreshChecks, cp.FailedChecks} {
				if value, exists := checks[old]; exists {
					if _, alreadyMigrated := checks[key]; !alreadyMigrated {
						checks[key] = value
					}
					delete(checks, old)
				}
			}
			continue
		}
		// A truncated legacy description cannot identify an exact rerun.
		// Require explicit publication review and allow a new exact check.
		if cp.FailedChecks == nil {
			cp.FailedChecks = map[string]string{}
		}
		if cp.FailedChecks[old] == "" {
			cp.FailedChecks[old] = "旧检查点中的命令参数已截断，请重新验证并审阅结果"
		}
		for _, checks := range []map[string]string{cp.Checked, cp.Recheck, cp.RefreshChecks} {
			delete(checks, old)
		}
	}
}
func (e *engine) invalidateCommandChecks(current string) {
	for key, description := range e.Checked {
		if key == current || !strings.HasPrefix(description, "run_command ") {
			continue
		}
		if e.Recheck == nil {
			e.Recheck = map[string]string{}
		}
		e.Recheck[key] = description
	}
}
func (e *engine) touchesConflicts(changes []contract.Change) bool {
	for _, p := range e.ConflictedPaths {
		for _, c := range changes {
			if c.Path == p || strings.HasPrefix(c.Path, p+"/") || strings.HasPrefix(p, c.Path+"/") {
				return true
			}
		}
	}
	return false
}
func (e *engine) refresh() error {
	if err := e.check(); err != nil {
		return err
	}
	info, err := e.d.Refresh(e.ctx)
	if err != nil {
		return e.wait("paused", "工作区刷新尚未完成，已保留当前草稿", map[string]string{"error": safeText(err.Error(), 1000)})
	}
	conflicts, err := e.d.FileConflicts(e.ctx)
	if err != nil {
		return err
	}
	e.ConflictedPaths = nil
	for _, c := range conflicts {
		e.ConflictedPaths = append(e.ConflictedPaths, c.Path)
	}
	e.RefreshCount++
	e.ReviewEpoch++
	e.Refreshing = false
	e.ObservedHashes = nil
	e.Answer, e.PublishingDigest, e.ApprovedDigest, e.WaitingDigest = "", "", "", ""
	e.clearToolApproval()
	e.Recent = nil
	e.MissingAttempts = 0
	e.RefreshChecks = cloneChecks(e.Checked)
	// Invalidated deliverables must be presented again if they survived the
	// refresh. A deleted output is withdrawn from both the timeline projection
	// and the acceptance checklist.
	for _, a := range e.Artifacts {
		key := "交付检查 " + a.Path
		if _, _, err := e.d.Stat(e.ctx, a.Path); err == nil {
			e.RefreshChecks[key] = "present " + a.Path
		} else if errors.Is(err, fs.ErrNotExist) {
			delete(e.RefreshChecks, key)
		} else {
			return err
		}
	}
	e.Artifacts = nil
	e.Recheck = cloneChecks(e.RefreshChecks)
	sample := e.ConflictedPaths[:min(12, len(e.ConflictedPaths))]
	msg := fmt.Sprintf("工作区出现 %d 处输入变化。当前草稿已基于用户的新版本刷新。重叠文件使用用户版本，已有提议仍可通过 list_conflicts/read_conflict 查看。请重新核对影响并完成原验证和交付。重叠文件示例：%s", info.ChangedInputs, strings.Join(sample, "、"))
	e.add(providers.ToolMessage{Role: "user", Content: msg})
	detail := map[string]any{"changed_inputs": info.ChangedInputs, "conflicts": len(e.ConflictedPaths), "paths": sample}
	if err = e.save("workspace_refreshed", "当前工作区已刷新，旧确认和交付检查已失效", detail); err != nil {
		return err
	}
	if e.RefreshCount >= 3 {
		return e.wait("paused", "工作区连续变化三次，请待外部写入稳定后继续", detail)
	}
	return nil
}
func (e *engine) requireChecks() (any, error) {
	keys := make([]string, 0, len(e.Recheck))
	for k := range e.Recheck {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sample := []string{}
	for _, k := range keys[:min(len(keys), 8)] {
		sample = append(sample, e.Recheck[k])
	}
	e.Answer = ""
	e.MissingAttempts++
	e.add(providers.ToolMessage{Role: "user", Content: "草稿内容已变化。提交当前结果前，请重新执行以下检查：\n" + strings.Join(sample, "\n")})
	if e.MissingAttempts >= 3 {
		return nil, e.wait("paused", "草稿变更后的验证尚未完成", map[string]any{"remaining": len(keys), "checks": sample})
	}
	if err := e.save("revalidation_required", "请完成当前草稿的复验", map[string]any{"remaining": len(keys), "checks": sample}); err != nil {
		return nil, err
	}
	return e.run()
}
func (e *engine) reviewDigest(digest string) string {
	if e.ReviewEpoch == 0 {
		return digest
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("revision-%d:%s", e.ReviewEpoch, digest))))
}
func (e *engine) conflictTool(ctx context.Context, call providers.ToolCall) (any, error) {
	var a struct {
		Path    string `json:"path"`
		Version string `json:"version"`
		Offset  int64  `json:"offset"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &a); err != nil {
		return nil, err
	}
	if a.Offset < 0 || a.Limit < 0 {
		return nil, errors.New("冲突读取范围无效")
	}
	if call.Function.Name == "list_conflicts" {
		items, err := e.d.FileConflicts(ctx)
		if err != nil {
			return nil, err
		}
		start := min(int64(len(items)), a.Offset)
		end := min(int64(len(items)), start+int64(min(positive(a.Limit, 8), 32)))
		// Keep enough bytes for complete revision hashes on every returned row.
		page := []map[string]string{}
		for _, v := range items[start:end] {
			page = append(page, map[string]string{"path": v.Path, "base": v.BeforeHash, "proposal": v.ProposalHash, "current": v.CurrentHash})
		}
		return map[string]any{"items": page, "next_offset": end, "done": end == int64(len(items))}, nil
	}
	if a.Version != "base" && a.Version != "proposal" && a.Version != "current" {
		return nil, errors.New("版本应为 base、proposal 或 current")
	}
	data, err := e.d.ReadConflict(ctx, a.Path, a.Version, a.Offset, min(positive(a.Limit, 4096), 10000))
	if err != nil {
		return nil, err
	}
	return map[string]any{"text": safeText(strings.ToValidUTF8(string(data), "�"), 10000), "next_offset": a.Offset + int64(len(data))}, nil
}
