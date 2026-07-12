"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2 } from "lucide-react";
import { z } from "zod";
import { PublicAuthShell } from "@/components/auth/public-auth-shell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { resetPassword } from "@/lib/api";
import { openAuthDialog } from "@/lib/auth-dialog-events";

const schema = z
  .object({
    password: z.string().min(8, "密码至少 8 个字符"),
    confirmPassword: z.string().min(8, "密码至少 8 个字符"),
  })
  .refine((data) => data.password === data.confirmPassword, {
    message: "两次输入的密码不一致",
    path: ["confirmPassword"],
  });

type FormData = z.infer<typeof schema>;

export function ResetPasswordView({ token }: { token: string }) {
  const router = useRouter();
  const [complete, setComplete] = useState(false);
  const [error, setError] = useState(token ? "" : "重置链接无效");
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormData>({ resolver: zodResolver(schema) });

  const onSubmit = async (data: FormData) => {
    if (!token) return;
    setError("");
    try {
      await resetPassword(token, data.password);
      setComplete(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "密码重置失败");
    }
  };

  const openLogin = () => {
    router.push("/");
    openAuthDialog("login");
  };

  return (
    <PublicAuthShell title="重置密码">
      {complete ? (
        <div className="grid gap-5">
          <p className="text-sm text-muted-foreground">密码已重置。</p>
          <Button onClick={openLogin}>登录</Button>
        </div>
      ) : (
        <form onSubmit={handleSubmit(onSubmit)} className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="reset-password">新密码</Label>
            <Input
              id="reset-password"
              type="password"
              minLength={8}
              autoComplete="new-password"
              disabled={!token}
              {...register("password")}
            />
            {errors.password ? (
              <p className="text-sm text-destructive">{errors.password.message}</p>
            ) : null}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="reset-password-confirm">确认新密码</Label>
            <Input
              id="reset-password-confirm"
              type="password"
              minLength={8}
              autoComplete="new-password"
              disabled={!token}
              {...register("confirmPassword")}
            />
            {errors.confirmPassword ? (
              <p className="text-sm text-destructive">{errors.confirmPassword.message}</p>
            ) : null}
          </div>
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
          <Button type="submit" disabled={isSubmitting || !token}>
            {isSubmitting ? <Loader2 className="size-4 animate-spin" /> : null}
            重置密码
          </Button>
        </form>
      )}
    </PublicAuthShell>
  );
}
