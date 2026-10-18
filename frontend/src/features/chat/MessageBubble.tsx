import { memo, useCallback, useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import "highlight.js/styles/github-dark.css";
import { Check, Copy, RefreshCw } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import type { ChatMessage } from "./chat-store";
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
    <div className="group/code my-3 overflow-hidden border border-border-subtle">
      <div className="flex items-center justify-between border-b border-border-subtle bg-bg-app/70 px-3 py-1.5">
        <span className="font-mono text-xs tracking-wide text-text-3 uppercase">
          {lang || "code"}
        </span>
        <button
          onClick={copy}
          className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-text-3 transition-colors hover:text-text-1"
        >
          {copied ? <Check size={12} /> : <Copy size={12} />}
          {copied ? "已复制" : "复制"}
        </button>
      </div>
      <pre {...props} className="overflow-x-auto bg-[#171a1b] p-3.5 text-[12.5px] leading-relaxed text-[#e6e7e8]">
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
      <div {...anchorProps} className={cn("mx-4 border-b border-border-soft py-6 sm:mx-8", highlightClass)}>
        <div className="grid min-w-0 gap-3 sm:grid-cols-[88px_minmax(0,1fr)]">
          <div className="text-[10px] font-semibold tracking-[0.16em] text-text-3">YOU / 输入</div>
          <div className="min-w-0 break-words text-[14px] leading-7 whitespace-pre-wrap text-text-1">
            {message.content}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div {...anchorProps} className={cn("group mx-4 grid min-w-0 gap-3 border-b border-border-soft py-6 sm:mx-8 sm:grid-cols-[88px_minmax(0,1fr)]", highlightClass)}>
      <div className="text-[10px] font-semibold tracking-[0.12em] text-text-3">NOOSPHERE<br /><span className="mt-1 inline-block font-normal">RESPONSE</span></div>
      <div className="min-w-0">
        <div className="mb-1 flex items-center gap-2">
          {!!message.contextCount && (
            <Badge variant="info" className="text-xs">
              {message.contextCount} 条参考依据
            </Badge>
          )}
          {message.status === "cancelled" && <Badge variant="secondary">已停止</Badge>}
          {message.error && (
            <Badge variant="err" className="text-xs">
              {message.error}
            </Badge>
          )}
        </div>

        <div className="ns-message-body min-w-0 break-words text-[14px] leading-7 text-text-1">
          {message.content ? (
            <Markdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[rehypeHighlight]}
              components={markdownComponents}
            >
              {message.content}
            </Markdown>
          ) : (
            <div className="flex items-center gap-2 py-0.5 text-text-3">
              {message.streaming && <span className="ns-breathe size-1.5 bg-brand" />}
              {message.streaming ? (message.agentProgress || (message.agentMode ? '正在准备 Agent 任务…' : '')) || (message.status === "preparing" ? "正在检索上下文…" : "正在等待模型响应…") : message.status === "cancelled" ? "已停止生成" : message.error ? "本次生成未完成，可重新尝试。" : "模型未返回内容。"}
            </div>
          )}
          {message.streaming && message.content && (
            <span className="ml-0.5 inline-block h-[14px] w-[7px] translate-y-[2px] animate-pulse rounded-[1px] bg-brand" />
          )}
        </div>

        {message.agentRunId && workspaceId && <AgentTaskRecord runId={message.agentRunId} workspaceId={workspaceId} showAnswer={!message.content} />}
        <MessageEvidence message={message} workspaceId={workspaceId} sessionId={sessionId} />
        {!message.streaming && onRegenerate && (
          <div className="mt-3 flex gap-1">
            <button
              onClick={onRegenerate}
              className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-text-3 hover:bg-secondary hover:text-text-1"
            >
              <RefreshCw size={11} />
              重新生成
            </button>
          </div>
        )}
      </div>
    </div>
  );
});
