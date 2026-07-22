import type { LucideIcon } from "lucide-react";

import { cn } from "@/lib/utils";

interface EmptyStateProps extends Omit<React.ComponentProps<"div">, "title"> {
  icon?: LucideIcon;
  title: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  titleClassName?: string;
}

function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  titleClassName,
  className,
  ...props
}: EmptyStateProps) {
  return (
    <div
      data-slot="empty-state"
      className={cn(
        "flex min-h-64 flex-col items-center justify-center px-4 text-center",
        className,
      )}
      {...props}
    >
      {Icon ? <Icon className="size-5 text-muted-foreground" /> : null}
      <p data-slot="empty-state-title" className={cn("mt-3 text-sm font-medium", titleClassName)}>
        {title}
      </p>
      {description ? (
        <p className="mt-1 max-w-sm text-sm text-muted-foreground">{description}</p>
      ) : null}
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  );
}

export { EmptyState };
