import { cn } from "@/lib/utils";

export function AssistantLogo({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn("shrink-0 bg-current", className)}
      style={{
        mask: "url('/icon.svg') center / contain no-repeat",
        WebkitMask: "url('/icon.svg') center / contain no-repeat",
      }}
    />
  );
}
