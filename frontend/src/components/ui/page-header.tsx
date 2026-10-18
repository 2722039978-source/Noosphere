import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

const PAGE_INDEX: Record<string, [string, string]> = {
  "工作台": ["00", "WORKSPACE OVERVIEW"], "AI 对话": ["01", "CONVERSATION"],
  CodeLens: ["02", "CODE INTELLIGENCE"], Nebula: ["03", "MEMORY ARCHIVE"],
  DevOps: ["04", "SYSTEM TELEMETRY"], Gateway: ["05", "MODEL ACCESS"], "设置": ["06", "PREFERENCES"],
};
interface PageHeaderProps { title: string; subtitle?: string; icon?: ReactNode; accent?: string; actions?: ReactNode; className?: string; }
export function PageHeader({ title, subtitle, actions, icon, className }: PageHeaderProps) {
  const [number, english] = PAGE_INDEX[title] ?? ["—", "WORKSPACE"];
  return (
    <header className={cn("ns-page-header", className)}>
      <div className="min-w-0 flex-1"><div className="ns-kicker">工作台<span>/</span>MODULE {number}<span>/</span>{english}</div><h1>{title}</h1>{subtitle && <p>{subtitle}</p>}</div>
      {icon && <div className="ns-page-symbol" aria-hidden="true">{icon}</div>}
      {actions && <div className="ns-page-actions">{actions}</div>}
    </header>
  );
}
export function SectionLabel({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("ns-section-label", className)}>{children}</div>;
}
