import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface PageHeaderProps { title: string; subtitle?: string; icon?: ReactNode; accent?: string; actions?: ReactNode; className?: string; }
export function PageHeader({ title, subtitle, actions, icon, className }: PageHeaderProps) {
  return (
    <header className={cn("ns-page-header", className)}>
      <div className="min-w-0 flex-1"><h1>{title}</h1>{subtitle && <p>{subtitle}</p>}</div>
      {icon && <div className="ns-page-symbol" aria-hidden="true">{icon}</div>}
      {actions && <div className="ns-page-actions">{actions}</div>}
    </header>
  );
}
export function SectionLabel({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("ns-section-label", className)}>{children}</div>;
}
