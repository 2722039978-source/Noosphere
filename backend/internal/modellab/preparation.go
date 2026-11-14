package modellab

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/devops/metrics"
)

type PreparationCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Ready  bool   `json:"ready"`
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
}
type Preparation struct {
	Ready     bool               `json:"ready"`
	CheckedAt string             `json:"checked_at"`
	Checks    []PreparationCheck `json:"checks"`
}

func resourceReadiness(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	snapshot, err := metrics.NewCollector().Snapshot()
	if err != nil {
		return "", identityError("RESOURCE_UNKNOWN", "无法读取本机可用内存，请在 DevOps 检查系统状态")
	}
	free := snapshot.Memory.TotalGB - snapshot.Memory.UsedGB
	if snapshot.Memory.TotalGB <= 0 || math.IsNaN(free) || math.IsInf(free, 0) || free < 0 {
		return "", identityError("RESOURCE_UNKNOWN", "本机内存数据不可用，不能据此判断资源足够")
	}
	if free < 1.5 {
		return "", identityError("RESOURCE_INSUFFICIENT", fmt.Sprintf("当前可用内存 %.2f GiB，低于 1.5 GiB 保护线；请释放内存后重新检查", free))
	}
	return fmt.Sprintf("当前可用内存 %.2f GiB，高于 1.5 GiB 保护线；不代表已证明单模型显存足够，执行时仍受模型服务保护", free), nil
}

// PrepareEvaluation does not create runs, freeze artifacts or invoke inference.
// It is an observation, not an admission token: submission rechecks conditions.
func (s *Service) PrepareEvaluation(parent context.Context, workspaceID string, spec EvaluationSpec) (Preparation, error) {
	out := Preparation{Ready: true, CheckedAt: time.Now().UTC().Format(time.RFC3339), Checks: []PreparationCheck{}}
	add := func(id, label, detail, action string, err error) {
		check := PreparationCheck{ID: id, Label: label, Ready: err == nil, Detail: detail, Action: action}
		if err != nil {
			out.Ready = false
			check.Code = errorCode(err)
			check.Detail = preparationHelp(id, check.Code)
			if id == "runner" && label == "本机文本评分器" {
				check.Detail = "本机环境信息不可用，请检查本地测评服务状态后重新准备。"
			}
		}
		out.Checks = append(out.Checks, check)
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	ctx, done, err := s.operation(ctx)
	if err != nil {
		add("runner", "本地执行器", "", "settings", err)
		return out, nil
	}
	defer done()
	r := s.runtime
	if _, err = r.config.Workspaces.Workspace(workspaceID); err != nil {
		return out, err
	}
	spec = normalizeSpec(spec)
	var setupErr error
	dataset, datasetErr := s.resolveDataset(ctx, workspaceID, spec.DatasetID, false)
	if !supportedWorkflow(spec.WorkflowID) || datasetErr == nil && spec.WorkflowID != dataset.workflowID() || spec.EvaluationMode != "" && spec.EvaluationMode != "baseline" || (spec.StableRevisionID == spec.CandidateRevisionID) != (spec.EvaluationMode == "baseline") {
		setupErr = identityError("INVALID_REQUEST", "请检查测评方式及模型选择：基线使用同一版本，比较使用两个不同版本")
	}
	add("selection", "测评方式", "当前工作区与模型选择匹配", "model-hub", setupErr)
	add("dataset", "任务集版本", "任务版本存在且可用于当前工作区", "datasets", datasetErr)
	profile, profileErr := s.GetProfile(ctx, spec.ProfileID)
	if profileErr == nil {
		profileErr = r.config.Budget.validate(profile)
	}
	if profileErr == nil && dataset != nil {
		profileErr = validateReviewerProfile(dataset, profile)
	}
	add("profile", "已保存参数", "参数未超过服务端预算", "parameters", profileErr)
	env, envErr := r.config.CollectEnvironment(ctx)
	if dataset != nil && dataset.isDeclarative() {
		add("runner", "本机文本评分器", "确定规则评分器可用，无需 Go 工具链或 CodeLens", "settings", envErr)
	} else {
		if envErr == nil && (r.config.Runner == nil || !r.fixture && strings.TrimSpace(env.ToolchainVersion) == "") {
			envErr = identityError("RUNNER_UNAVAILABLE", "请配置可信 Go 工具链后重启")
		}
		add("runner", "本地执行器", "Go / WASI 执行器可用", "settings", envErr)
		var codeErr error
		if r.build == nil {
			if r.config.ContextAdapter == nil || r.config.ContextAdapter() == nil {
				codeErr = identityError("CODELENS_UNAVAILABLE", "CodeLens 服务未就绪，请检查 Python 服务")
			} else {
				health, _, e := r.config.ContextAdapter().HealthEvidence(ctx)
				codeErr = e
				if e == nil && (health.Status != "ok" || health.Version != "0.6.0") {
					codeErr = identityError("CODELENS_UNAVAILABLE", "CodeLens 状态或版本不符合测评要求")
				}
			}
		}
		add("codelens", "代码分析服务", "CodeLens 健康状态与协议可用", "codelens", codeErr)
	}
	resourceDetail, resourceErr := r.resources(ctx)
	add("resources", "本机可用资源", resourceDetail, "devops", resourceErr)
	var modelErr error
	seen := map[string]bool{}
	for _, id := range []string{spec.StableRevisionID, spec.CandidateRevisionID} {
		if seen[id] {
			continue
		}
		seen[id] = true
		m, e := s.GetModel(ctx, id)
		if e == nil {
			e = r.verify(ctx, m)
		}
		if e == nil && profileErr == nil {
			options, _, optErr := evaluationOptions(profile)
			e = optErr
			if e == nil {
				e = s.gateway.CheckEvaluationOptions(ctx, m.GatewayModelID, options)
			}
		}
		if e != nil && modelErr == nil {
			modelErr = e
		}
	}
	add("models", "模型身份与上下文", "已核对精确版本与服务配置，检查未触发推理", "model-hub", modelErr)
	if err := ctx.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// Controlled messages explain recovery without exposing raw provider responses.
func preparationHelp(id, code string) string {
	switch code {
	case "RESOURCE_INSUFFICIENT":
		return "本机可用内存低于 1.5 GiB 保护线，请释放内存后重新检查。"
	case "RESOURCE_UNKNOWN":
		return "无法确认本机可用内存，请在 DevOps 检查系统状态。"
	case "MODEL_DRIFT":
		return "模型文件、服务来源或版本已改变，请在 Model Hub 重新发现并注册当前版本。"
	case "BUDGET_EXCEEDED":
		return "参数超出服务预算或当前模型的上下文容量，请减少高级推理参数并重新保存。"
	case "USER_CANCELLED", "INFERENCE_TIMEOUT":
		return "准备检查被取消或超时，请确认本地服务就绪后重新检查。"
	}
	switch id {
	case "selection":
		return "请检查测评方式：单模型基线使用同一版本，比较需选择两个不同版本。"
	case "dataset":
		return "任务集不可用，请检查当前工作区、任务版本及归档状态。"
	case "profile":
		return "已保存配置不可用或不符合预算，请调整参数并重新保存。"
	case "runner":
		return "Go / WASI 执行器未就绪，请检查工具链配置并重启服务。"
	case "codelens":
		return "CodeLens 不可用或协议版本不匹配，请检查代码分析服务。"
	case "models":
		return "无法核对精确本地模型身份或上下文，请检查连接与已注册版本。"
	case "resources":
		return "资源检查失败，请在 DevOps 检查系统状态后重试。"
	}
	return "准备条件未满足，请检查服务状态后重试。"
}
