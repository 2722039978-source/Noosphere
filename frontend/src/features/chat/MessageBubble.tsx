import { memo, useCallback, useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import "highlight.js/styles/github-dark.css";
import { Check, Copy, Paperclip, RefreshCw } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { useChatStore, type ChatMessage } from "./chat-store";
import { AgentTaskRecord } from '@/components/AgentTaskRecord';
import { MessageEvidence } from './MessageEvidence';

/** 从 ReactNode 提取纯文本 (代码复制用) */
function nodeToText(node: ReactNode): string {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(nodeToText).join("");
  const el = node as { props?: { children?: ReactNode } };
  return nodeToText(el?.props?.children);
}

function CodeBlock({ children, ...props }: React.HTMLAttributes<HTMLPreElement>) {
  const [copied, setCopied] = useState(false);
  const copy = useCallback(() => {
    navigator.clipboard.writeText(nodeToText(children)).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }, [children]);

  // children 是 <code className="hljs language-xxx">
  const codeEl = (Array.isArray(children) ? children[0] : children) as
    | { props?: { className?: string } }
    | undefined;
  const lang = String(codeEl?.props?.className || "").match(/language-([\w-]+)/)?.[1] || "";

  return (
    <div className="group/code my-3.5 overflow-hidden rounded-[10px] border border-border-soft shadow-soft">
      <div className="flex items-center justify-between border-b border-border-subtle bg-bg-raised px-3.5 py-2">
        <span className="font-mono text-[11px] tracking-[.08em] text-text-3 uppercase">
          {lang || "code"}
        </span>
        <button
          onClick={copy}
          className="flex items-center gap-1 rounded-md px-2 py-0.5 text-xs text-text-3 transition-colors hover:bg-secondary hover:text-text-1"
        >
          {copied ? <Check size={12} className="text-ok" /> : <Copy size={12} />}
          {copied ? "已复制" : "复制"}
        </button>
      </div>
      <pre {...props} className="overflow-x-auto bg-[#181715] p-4 text-[12.5px] leading-relaxed text-[#eae5dc]">
        {children}
      </pre>
    </div>
  );
}

const markdownComponents = {
  pre: CodeBlock,
  code: ({ className, children, ...props }: React.HTMLAttributes<HTMLElement> & { inline?: boolean }) => (
    <code
      className={cn(
        !className?.includes("language-") &&
          "rounded bg-secondary px-1.5 py-0.5 font-mono text-[0.85em] text-brand-bright",
        className
      )}
      {...props}
    >
      {children}
    </code>
  ),
  a: ({ href, children }: React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a href={href} target="_blank" rel="noreferrer" className="text-brand hover:underline">
      {children}
    </a>
  ),
  table: ({ children }: React.HTMLAttributes<HTMLTableElement>) => (
    <div className="my-3 overflow-x-auto rounded-lg border border-border-subtle">
      <table className="w-full text-sm">{children}</table>
    </div>
  ),
  th: ({ children }: React.ThHTMLAttributes<HTMLTableCellElement>) => (
    <th className="border-b border-border-subtle bg-secondary/50 px-3 py-1.5 text-left font-medium">
      {children}
    </th>
  ),
  td: ({ children }: React.TdHTMLAttributes<HTMLTableCellElement>) => (
    <td className="border-b border-border-subtle px-3 py-1.5">{children}</td>
  ),
};

interface Props {
  workspaceId?: string;
  sessionId?: string;
  message: ChatMessage;
  onRegenerate?: () => void;
  highlightRequest?: number;
  scrollContainer?: RefObject<HTMLDivElement>;
}

export const MessageBubble = memo(function MessageBubble({ message, onRegenerate, highlightRequest, scrollContainer, workspaceId, sessionId }: Props) {
  const isUser = message.role === "user";
  const messageRef = useRef<HTMLDivElement>(null);
  const highlighted = highlightRequest !== undefined;

  // Runs after this lazy-loaded message is mounted; a new request also handles repeated clicks.
  useEffect(() => {
    const element = messageRef.current;
    const container = scrollContainer?.current;
    if (highlightRequest === undefined || !element || !container) return;
    const frame = requestAnimationFrame(() => {
      const bounds = element.getBoundingClientRect();
      const viewport = container.getBoundingClientRect();
      container.scrollTop += bounds.top - viewport.top - Math.max(0, (container.clientHeight - bounds.height) / 2);
      element.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [highlightRequest, scrollContainer]);

  const anchorProps = {
    ref: messageRef,
    tabIndex: highlighted ? -1 : undefined,
    "data-search-match": highlighted ? "true" : undefined,
    "aria-label": highlighted ? "搜索命中的消息" : undefined,
  };
  const highlightClass = highlighted && "bg-brand-dim ring-1 ring-inset ring-brand/40 outline-none";

  if (isUser) {
    return (
      <div {...anchorProps} className={cn("ns-chat-message ns-chat-message-user", highlightClass)}>
        <div className="ns-user-card">
          <div className="ns-chat-message-label"><span>你的消息</span></div>
          <div className="ns-message-body whitespace-pre-wrap text-text-1">
            {message.content}
          </div>
          {!!message.attachments?.length && <ul aria-label="消息附件" className="mt-2.5 flex flex-wrap gap-2 text-xs text-text-2">
            {message.attachments.map(file => <li key={file.id} title={file.name} className="ns-msg-attachment-pill">
              <Paperclip aria-hidden="true" className="size-3.5 shrink-0 text-brand" /><span className="truncate">{file.name}</span>
            </li>)}
          </ul>}
        </div>
      </div>
    );
  }

  return (
    <div {...anchorProps} className={cn("group ns-chat-message ns-chat-message-assistant", message.streaming && "is-streaming", highlightClass)}>
      <div className="min-w-0">
        <div className="ns-chat-message-label">
          <span className="ns-assistant-signature">
            <i className="ns-assistant-dot" data-streaming={!!message.streaming} aria-hidden="true" />
            Noosphere
          </span>
          {!!message.contextCount && (
            <Badge variant="info" className="text-[11px]">
              {message.contextCount} 条参考依据
            </Badge>
          )}
          {message.status === "cancelled" && <Badge variant="secondary">已停止</Badge>}
          {message.error && (
            <Badge variant="err" className="text-[11px]">
              {message.error}
            </Badge>
          )}
        </div>

        <div className="ns-message-body text-text-1">
          {message.content ? (
            <Markdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[rehypeHighlight]}
              components={markdownComponents}
            >
              {message.content}
            </Markdown>
          ) : message.agentEngine === 2 && message.agentRunId ? null : (
            <div className="ns-assistant-pending">
              {message.streaming && <span className="ns-mini-orbit" aria-hidden="true"><i /><b /></span>}
              <span>{message.streaming ? (message.agentProgress || (message.agentMode ? '正在准备 Agent 任务…' : '')) || (message.status === "preparing" ? "正在检索项目上下文…" : "等待首字输出…") : message.status === "cancelled" ? "已停止生成" : message.error ? "本次生成未完成，可重新尝试。" : "模型未返回内容。"}</span>
            </div>
          )}
          {message.streaming && message.content && (
            <span className="ns-stream-caret" aria-hidden="true" />
          )}
        </div>

        {message.agentRunId && workspaceId && <AgentTaskRecord runId={message.agentRunId} workspaceId={workspaceId} expanded={message.agentEngine === 2} showAnswer={!message.content}
          onAnswer={message.agentEngine === 2 && !message.content && sessionId ? answer => {
            useChatStore.getState().updateMessage(sessionId, message.id, { content: answer, streaming: false, status: 'completed', error: undefined });
          } : undefined} />}
        <MessageEvidence message={message} workspaceId={workspaceId} sessionId={sessionId} />
        {!message.streaming && message.agentEngine !== 2 && onRegenerate && (
          <div className="ns-message-actions mt-3 flex gap-1">
            <button
              onClick={onRegenerate}
              className="ns-message-action-btn"
            >
              <RefreshCw size={12} aria-hidden="true" />
              重新生成
            </button>
          </div>
        )}
      </div>
    </div>
  );
});
