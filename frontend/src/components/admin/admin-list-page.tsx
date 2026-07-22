import type { LucideIcon } from "lucide-react";

import { AdminLoading, adminTableScrollClass } from "@/components/admin/admin-shared";
import { CursorTableScroll } from "@/components/shared/cursor-table-scroll";
import { EmptyState } from "@/components/shared/empty-state";
import { ErrorState } from "@/components/shared/error-state";
import { cn } from "@/lib/utils";

interface AdminListPageProps {
  ariaLabel: string;
  children: React.ReactNode;
  className?: string;
  emptyIcon: LucideIcon;
  emptyTitle: string;
  error?: string;
  hasItems: boolean;
  hasMore?: boolean;
  loading?: boolean;
  loadingMore?: boolean;
  loadMoreError?: string;
  onLoadMore?: () => Promise<void>;
  onRetry: () => void;
}

function AdminListPage({
  ariaLabel,
  children,
  className,
  emptyIcon,
  emptyTitle,
  error,
  hasItems,
  hasMore,
  loading,
  loadingMore,
  loadMoreError,
  onLoadMore,
  onRetry,
}: AdminListPageProps) {
  if (loading) return <AdminLoading />;
  if (error) return <ErrorState message={error} onRetry={onRetry} />;
  if (!hasItems) return <EmptyState icon={emptyIcon} title={emptyTitle} />;

  return (
    <CursorTableScroll
      className={cn(adminTableScrollClass, className)}
      hasMore={hasMore}
      loadingMore={loadingMore}
      loadMoreError={loadMoreError}
      onLoadMore={onLoadMore}
      aria-label={ariaLabel}
    >
      {children}
    </CursorTableScroll>
  );
}

export { AdminListPage };
