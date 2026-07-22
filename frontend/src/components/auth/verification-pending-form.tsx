"use client";

import { useState } from "react";
import { Spinner } from "@/components/shared/spinner";
import { Button } from "@/components/ui/button";
import { resendVerification } from "@/lib/api";

export function VerificationPendingForm({
  email,
  emailSent,
  onBackToLogin,
}: {
  email: string;
  emailSent: boolean;
  onBackToLogin: () => void;
}) {
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(emailSent);
  const [error, setError] = useState<string | null>(null);

  const resend = async () => {
    setSending(true);
    setError(null);
    try {
      await resendVerification(email);
      setSent(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "验证邮件发送失败");
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="grid gap-4">
      <p className="break-all text-sm text-muted-foreground">
        {sent ? "验证邮件已发送至" : "等待发送验证邮件至"}{" "}
        <span className="text-foreground">{email}</span>
      </p>
      {error ? <p className="text-sm text-destructive">{error}</p> : null}
      <Button type="button" disabled={sending} onClick={() => void resend()}>
        {sending ? <Spinner /> : null}
        重新发送
      </Button>
      <Button type="button" variant="ghost" onClick={onBackToLogin}>
        返回登录
      </Button>
    </div>
  );
}
