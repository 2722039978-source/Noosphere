import { cn } from "@/lib/utils";

interface BrandMarkProps {
  className?: string;
  size?: "sm" | "md" | "lg";
}

const sizes = {
  sm: "size-8",
  md: "size-10",
  lg: "size-16",
};

export function BrandMark({ className, size = "md" }: BrandMarkProps) {
  return (
    <span className={cn(sizes[size], "relative inline-flex shrink-0 overflow-hidden", className)}>
      <img
        src="/noosphere-mark.png"
        alt=""
        aria-hidden="true"
        draggable={false}
        className="absolute inset-0 size-full object-contain"
      />
    </span>
  );
}
