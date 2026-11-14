package gateway

import (
	"net/url"
	"strings"

	"github.com/noosphere/backend/internal/gateway/providers"
)

// These are adapter/configuration declarations, never a connectivity probe or
// a claim that a particular model has followed the protocol successfully.
type Capability struct {
	Supported    bool   `json:"supported"`
	Available    bool   `json:"available"`
	Code         string `json:"code"`
	Reason       string `json:"reason"`
	Verification string `json:"verification"`
}
type ModelCapabilities struct {
	SchemaVersion    int        `json:"schema_version"`
	Source           string     `json:"source"`
	Chat             Capability `json:"chat"`
	AgentTools       Capability `json:"agent_tools"`
	NativeEvaluation Capability `json:"native_evaluation"`
}
type modelWithCapabilities struct {
	ModelConfig
	Capabilities ModelCapabilities `json:"capabilities"`
}

func chatProviderFactory(name string) (providers.ProviderFactory, bool) {
	if factory := providers.Get(name); factory != nil {
		return factory, false
	}
	return providers.Get("openai_compatible"), true
}
func toolProviderAllowed(name string) bool {
	switch name {
	case "deepseek", "openai", "openai_compatible", "ollama":
		return true
	}
	return false
}
func configuredCapability(cap Capability, model ModelConfig) Capability {
	if !cap.Supported {
		return cap
	}
	if !model.Enabled {
		cap.Code = "MODEL_DISABLED"
		cap.Reason = "此连接已禁用，启用后才能使用。"
		return cap
	}
	u, err := url.Parse(model.BaseURL)
	// Claude/Gemini constructors already resolve an empty URL to their official
	// endpoint, including their registered aliases. Preserve those valid configs.
	adapterDefault := model.BaseURL == "" && (model.Provider == "claude" || model.Provider == "anthropic" || model.Provider == "gemini" || model.Provider == "google")
	validURL := err == nil && u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
	if strings.TrimSpace(model.ModelName) == "" || (!adapterDefault && !validURL) {
		cap.Code = "CONFIGURATION_INCOMPLETE"
		cap.Reason = "请补齐模型名称和有效的 HTTP / HTTPS 服务地址。"
		return cap
	}
	cap.Available = true
	return cap
}
func unavailable(code, reason string) Capability {
	return Capability{Code: code, Reason: reason, Verification: "not_applicable"}
}

func connectionCapabilities(model ModelConfig) ModelCapabilities {
	out := ModelCapabilities{SchemaVersion: 1, Source: "gateway-adapter-policy-v1", Chat: unavailable("ADAPTER_UNAVAILABLE", "此连接没有可用的对话适配器。"), AgentTools: unavailable("TOOLS_UNSUPPORTED", "此协议尚未接入结构化 Agent 工具；可按对话能力使用。"), NativeEvaluation: unavailable("NATIVE_UNSUPPORTED", "原生测评支持本机 Ollama、可核验的 llama.cpp 与 OpenVINO GenAI 适配器。")}
	factory, fallback := chatProviderFactory(model.Provider)
	if factory == nil {
		return out
	}
	// Provider constructors create local HTTP clients only. Do not call Chat,
	// Discover, /props or inspect weights when returning a model catalogue.
	p, err := factory(model.BaseURL, model.APIKey)
	if err != nil || p == nil {
		out.Chat = unavailable("ADAPTER_UNAVAILABLE", "对话适配器初始化失败，请检查连接配置。")
		return out
	}
	out.Chat = Capability{Supported: true, Code: "CHAT_ADAPTER_SUPPORTED", Reason: "已接入对话协议；连接是否在线、模型是否响应需实际测试。", Verification: "not_checked"}
	if fallback {
		out.Chat.Code = "CHAT_COMPATIBLE_FALLBACK"
		out.Chat.Reason = "未知协议沿用现有 OpenAI 兼容对话回退；建议明确选择协议，连接尚未验证。"
	}
	if model.Provider == "openvino" {
		if _, err := sourceFingerprint(&model); err != nil {
			out.Chat = unavailable("LOCAL_ENDPOINT_REQUIRED", "OpenVINO 适配器只接受本机回环服务地址；请使用本机 OpenVINO 预设。")
		}
	}
	if toolProviderAllowed(model.Provider) {
		if _, ok := p.(providers.ToolProvider); ok {
			out.AgentTools = Capability{Supported: true, Code: "TOOLS_ADAPTER_SUPPORTED", Reason: "已接入结构化工具协议；具体模型能否正确选择和调用工具需实际验证。", Verification: "not_checked"}
		}
	}
	if model.Provider == "ollama" || model.Provider == "openai_compatible" || model.Provider == "openvino" {
		if _, err := sourceFingerprint(&model); err != nil {
			out.NativeEvaluation = unavailable("LOCAL_ENDPOINT_REQUIRED", "原生测评要求本机回环地址，且地址不能携带凭据、查询、片段或不受支持的路径；远程兼容服务可用于其他已支持能力。")
		} else if _, ok := p.(providers.EvaluationProvider); ok {
			reason := "可进入本机 Ollama 身份发现；仍需验证模型 digest、引擎、执行器和资源条件。"
			if model.Provider == "openai_compatible" {
				reason = "可尝试本机 llama.cpp 身份核验；需运行时元数据和可读取的本地 GGUF，普通兼容服务不能据此直接测评。"
			}
			if model.Provider == "openvino" {
				reason = "可发现本机 OpenVINO GenAI；须使用项目附带的适配器，并核验 IR、分词器、引擎版本和实际 CPU / GPU 配置。"
			}
			out.NativeEvaluation = Capability{Supported: true, Code: "NATIVE_DISCOVERY_REQUIRED", Reason: reason, Verification: "requires_discovery"}
		}
	}
	out.Chat = configuredCapability(out.Chat, model)
	out.AgentTools = configuredCapability(out.AgentTools, model)
	out.NativeEvaluation = configuredCapability(out.NativeEvaluation, model)
	return out
}
