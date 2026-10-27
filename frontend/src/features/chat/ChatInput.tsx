import { useEffect, useRef, useState } from "react";
import { Bot, Box, Paperclip, Send, Square, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { ModelConfig } from "@/hooks/useBackend";

interface Props {
  models: ModelConfig[];
  modelId: string;
  defaultModelId?: string;
  onModelChange: (id: string) => void;
  streaming: boolean;
  busy?: boolean;
  onSend: (text: string, options: ChatSubmitOptions) => boolean;
  onStop: () => void;
}

export interface ChatSubmitOptions {
  agentMode: boolean;
  reviewedActions?: boolean;
  attachments: { name: string; content: string }[];
}

const MAX_ATTACHMENT_CHARS = 12_000;

/** 概念稿输入框: 紧凑移动布局、宽阔桌面布局，底部附件/模式 + 右侧发送 */
export function ChatInput({ models, modelId, defaultModelId, onModelChange, streaming, busy, onSend, onStop }: Props) {
  const [text, setText] = useState("");
  const [agentMode, setAgentMode] = useState(false);
  const [reviewedActions, setReviewedActions] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const [reading, setReading] = useState(false);
  const [fileError, setFileError] = useState("");
  const readingRef = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const [compact, setCompact] = useState(() => window.matchMedia("(max-width: 639px)").matches);
  const ref = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const query = window.matchMedia("(max-width: 639px)");
    const update = (event: MediaQueryListEvent) => setCompact(event.matches);
    setCompact(query.matches);
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);

  const resize = () => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 168)}px`;
  };

  const submit = async () => {
    const t = text.trim();
    if (!t || streaming || busy || !models.length || readingRef.current) return;
    readingRef.current = true;
    setReading(true);
    setFileError("");
    try {
      const attachments = await Promise.all(files.map(async (file) => ({
        name: file.name, content: (await file.slice(0, MAX_ATTACHMENT_CHARS * 4).text()).slice(0, MAX_ATTACHMENT_CHARS),
      })));
      if (!mounted.current) return;
      if (onSend(t, { agentMode, reviewedActions: agentMode && reviewedActions, attachments })) {
        setText(""); setFiles([]);
        if (fileRef.current) fileRef.current.value = "";
        requestAnimationFrame(() => { if (ref.current) ref.current.style.height = "auto"; });
      }
    } catch { setFileError("附件读取失败，请重新选择文件。"); }
    finally { readingRef.current = false; setReading(false); }
  };

  const canSend = !!text.trim() && models.some(model => model.enabled !== false) && !streaming && !busy && !reading;

  return (
    <div className="ns-chat-composer px-5 pb-4 pt-3 sm:px-8">
      <div className="mx-auto max-w-5xl">
        <div className="border border-border-strong bg-bg-elevated transition-colors focus-within:border-brand">
          {fileError && <p role="alert" className="px-4 pt-3 text-xs text-err">{fileError}</p>}
          {files.length > 0 ? (
            <div className="flex flex-wrap gap-2 border-b border-border-subtle px-3 py-2.5">
              {files.map((file) => (
                <span key={`${file.name}-${file.lastModified}`} className="flex max-w-full items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-xs text-text-2">
                  <Paperclip aria-hidden="true" className="size-3.5" />
                  <span className="max-w-48 truncate">{file.name}</span>
                  <button
                    type="button"
                    aria-label={`移除附件 ${file.name}`}
                    className="text-text-3 hover:text-text-1"
                    onClick={() => setFiles((current) => current.filter((item) => item !== file))}
                  >
                    <X aria-hidden="true" className="size-3" />
                  </button>
                </span>
              ))}
            </div>
          ) : null}
          <textarea
            aria-label="输入问题"
            ref={ref}
            value={text}
            rows={1}
            placeholder={compact ? "输入问题" : "输入问题，Enter 发送 · Shift+Enter 换行"}
            className="block min-h-[46px] w-full resize-none bg-transparent px-[14px] pt-4 pb-1 text-[14px] leading-6 text-text-1 placeholder:text-text-3 outline-none sm:min-h-[67px] sm:px-5 sm:pt-5 sm:pb-2 sm:text-[15px]"
            onChange={(e) => {
              setText(e.target.value);
              resize();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                void submit();
              }
            }}
          />

          <input
            ref={fileRef}
            type="file"
            aria-label="对话附件"
            multiple
            className="sr-only"
            accept=".txt,.md,.json,.yaml,.yml,.ts,.tsx,.js,.jsx,.go,.py,.rs"
            onChange={(event) => setFiles(Array.from(event.target.files ?? []))}
          />

          <div className="flex items-center gap-2 px-3.5 pt-0 pb-[10px] sm:gap-4 sm:px-5 sm:pt-1 sm:pb-3">
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-label="选择附件"
              className="h-8 rounded-[8px] px-1.5 text-[14px] font-normal text-text-2 has-[>svg]:px-1.5 sm:h-10 sm:px-4 sm:text-[15px] sm:has-[>svg]:px-4"
              onClick={() => fileRef.current?.click()}
            >
              <Paperclip data-icon="inline-start" aria-hidden="true" />
              <span>附件</span>
            </Button>
            <Button
              type="button"
              variant={agentMode ? "secondary" : "outline"}
              size="sm"
              aria-pressed={agentMode}
              title="开启后可搜索、读取项目及检查系统状态；最多 6 轮模型、12 次工具、120 秒"
              className={`h-8 rounded-[8px] px-2 text-[14px] font-normal has-[>svg]:px-2 sm:h-10 sm:px-5 sm:text-[15px] sm:has-[>svg]:px-5 ${agentMode ? "text-brand-bright" : "text-text-2"}`}
              onClick={() => setAgentMode((value) => !value)}
            >
              <Bot data-icon="inline-start" aria-hidden="true" />
              {agentMode && reviewedActions ? "Agent · 审批" : "只读 Agent"}
            </Button>

            {agentMode && <Button type="button" variant={reviewedActions?'secondary':'outline'} size="sm" aria-pressed={reviewedActions} onClick={()=>setReviewedActions(v=>!v)} title="允许生成修改、测试和服务操作预览；每个操作须由你确认执行">操作提案</Button>}
            <Select disabled={streaming || busy} value={modelId || '__default__'} onValueChange={(value) => onModelChange(value === '__default__' ? '' : value)}>
              <SelectTrigger size="sm" className="ml-auto h-8 min-w-0 flex-1 rounded-[8px] px-2.5 text-[14px] sm:h-11 sm:w-[176px] sm:flex-none sm:px-3 sm:text-[15px] [&_[data-slot=select-value]]:truncate">
                <Box aria-hidden="true" />
                <SelectValue placeholder={models.length ? "选择模型" : "未配置模型"} />
              </SelectTrigger>
              <SelectContent><SelectGroup><SelectItem value="__default__">{defaultModelId ? '工作区默认' : '全局默认'} · {(defaultModelId ? models.find(model => model.id === defaultModelId)?.name || '工作区模型不可用' : models.find(model => model.is_default && model.enabled !== false)?.name || models.find(model => model.enabled !== false)?.name) || '未配置'}</SelectItem>
                {models.map((m) => (
                  <SelectItem key={m.id} value={m.id} disabled={m.enabled === false}>
                    {m.name || m.model_name}{m.enabled === false ? ' · 已禁用' : ''}
                  </SelectItem>
                ))}
              </SelectGroup></SelectContent>
            </Select>

            {streaming ? (
              <Button
                size="icon"
                variant="secondary"
                className="size-9 rounded-[8px] sm:h-11 sm:w-[72px]"
                onClick={onStop}
                aria-label="停止生成"
              >
                <Square aria-hidden="true" />
              </Button>
            ) : (
              <Button
                size="icon"
                className="size-9 rounded-[8px] disabled:bg-secondary disabled:text-text-3 disabled:opacity-100 sm:h-11 sm:w-[72px]"
                onClick={() => void submit()}
                disabled={!canSend}
                aria-label="发送"
              >
                <Send aria-hidden="true" />
              </Button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
