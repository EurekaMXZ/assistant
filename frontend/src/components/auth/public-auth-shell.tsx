import Link from "next/link";
import { AssistantLogo } from "@/components/assistant-logo";

export function PublicAuthShell({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="grid min-h-full place-items-center px-5 py-10">
      <div className="w-full max-w-sm">
        <Link href="/" className="inline-flex items-center gap-2 text-sm font-semibold">
          <AssistantLogo className="size-5" />
          Assistant
        </Link>
        <div className="mt-7">
          <h1 className="mb-6 text-2xl font-semibold">{title}</h1>
          {children}
        </div>
      </div>
    </div>
  );
}
