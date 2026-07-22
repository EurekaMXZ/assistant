"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Spinner } from "@/components/shared/spinner";
import { PublicAuthShell } from "@/components/auth/public-auth-shell";
import { Button } from "@/components/ui/button";
import { verifyEmail } from "@/lib/api";
import { openAuthDialog } from "@/lib/auth-dialog-events";

type VerificationState = "verifying" | "verified" | "error";

export function VerifyEmailView({ token }: { token: string }) {
  const router = useRouter();
  const started = useRef(false);
  const [state, setState] = useState<VerificationState>(token ? "verifying" : "error");
  const [error, setError] = useState(token ? "" : "验证链接无效");

  useEffect(() => {
    if (!token || started.current) return;
    started.current = true;
    void verifyEmail(token)
      .then(() => setState("verified"))
      .catch((err) => {
        setError(err instanceof Error ? err.message : "邮箱验证失败");
        setState("error");
      });
  }, [token]);

  const openLogin = () => {
    router.push("/");
    openAuthDialog("login");
  };

  return (
    <PublicAuthShell title="验证邮箱">
      {state === "verifying" ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Spinner />
          验证中
        </div>
      ) : state === "verified" ? (
        <div className="grid gap-5">
          <p className="text-sm text-muted-foreground">邮箱已验证。</p>
          <Button onClick={openLogin}>登录</Button>
        </div>
      ) : (
        <div className="grid gap-5">
          <p className="text-sm text-destructive">{error}</p>
          <Button variant="outline" onClick={openLogin}>
            返回登录
          </Button>
        </div>
      )}
    </PublicAuthShell>
  );
}
