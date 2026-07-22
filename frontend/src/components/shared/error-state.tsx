import type { LucideIcon } from "lucide-react";
import { RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface ErrorStateProps extends React.ComponentProps<"div"> {
  icon?: LucideIcon;
  message: React.ReactNode;
  description?: React.ReactNode;
  onRetry?: () => void;
  retryText?: string;
}

function ErrorState({
  icon: Icon,
  message,
  description,
  onRetry,
  retryText = "重新加载",
  className,
  ...props
}: ErrorStateProps) {
  return (
    <div
      className={cn(
        "flex min-h-64 flex-col items-center justify-center px-4 text-center",
        className,
      )}
      {...props}
    >
      {Icon ? <Icon className="mb-3 size-6 text-muted-foreground" /> : null}
      <p className="text-sm font-medium">{message}</p>
      {description ? <p className="mt-2 text-sm text-muted-foreground">{description}</p> : null}
      {onRetry ? (
        <Button type="button" variant="outline" size="sm" className="mt-4" onClick={onRetry}>
          <RefreshCw className="size-4" />
          {retryText}
        </Button>
      ) : null}
    </div>
  );
}

export { ErrorState };
