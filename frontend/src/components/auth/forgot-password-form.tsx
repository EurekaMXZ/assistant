"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2 } from "lucide-react";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { forgotPassword } from "@/lib/api";

const schema = z.object({
  email: z.string().email("请输入有效的邮箱"),
});

type FormData = z.infer<typeof schema>;

export function ForgotPasswordForm({ onBackToLogin }: { onBackToLogin: () => void }) {
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormData>({ resolver: zodResolver(schema) });

  const onSubmit = async (data: FormData) => {
    setError(null);
    try {
      await forgotPassword(data.email);
      setSent(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "重置邮件发送失败");
    }
  };

  if (sent) {
    return (
      <div className="grid gap-4">
        <p className="text-sm text-muted-foreground">重置邮件已发送。</p>
        <Button type="button" variant="outline" onClick={onBackToLogin}>返回登录</Button>
      </div>
    );
  }

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="grid gap-4">
      <div className="grid gap-2">
        <Label htmlFor="forgot-email">邮箱</Label>
        <Input id="forgot-email" type="email" autoComplete="email" {...register("email")} />
        {errors.email ? <p className="text-sm text-destructive">{errors.email.message}</p> : null}
      </div>
      {error ? <p className="text-sm text-destructive">{error}</p> : null}
      <Button type="submit" disabled={isSubmitting}>
        {isSubmitting ? <Loader2 className="size-4 animate-spin" /> : null}
        发送重置邮件
      </Button>
      <Button type="button" variant="ghost" onClick={onBackToLogin}>返回登录</Button>
    </form>
  );
}
