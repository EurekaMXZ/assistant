import { Spinner } from "@/components/shared/spinner";
import { Skeleton } from "@/components/ui/skeleton";

export function AdminPageHeader({ title, action }: { title: string; action?: React.ReactNode }) {
  return (
    <header className="flex flex-col gap-4 border-b pb-5 sm:flex-row sm:items-center sm:justify-between">
      <h1 className="text-2xl font-semibold">{title}</h1>
      {action ? <div className="flex shrink-0 items-center gap-2">{action}</div> : null}
    </header>
  );
}

export function AdminLoading() {
  return (
    <div className="space-y-3 pt-6">
      <Skeleton className="h-10 w-full" />
      {Array.from({ length: 6 }).map((_, index) => (
        <Skeleton key={index} className="h-14 w-full" />
      ))}
    </div>
  );
}

export function SavingIcon({ saving }: { saving: boolean }) {
  return saving ? <Spinner /> : null;
}

export const adminTableScrollClass =
  "max-h-none overflow-visible border-y sm:max-h-[min(65vh,40rem)] sm:overflow-auto";
