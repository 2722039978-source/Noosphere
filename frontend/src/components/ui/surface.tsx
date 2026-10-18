import * as React from "react";

import { cn } from "@/lib/utils";

/** 柔和高程表面: 用背景层级 + 极弱描边建立层级, 替代显式边框 */
function Surface({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="surface"
      className={cn("ns-surface border border-border-soft bg-bg-raised/40", className)}
      {...props}
    />
  );
}

export { Surface };
