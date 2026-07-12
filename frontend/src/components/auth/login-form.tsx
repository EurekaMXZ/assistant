"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useAuth } from "@/hooks/use-auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2 } from "lucide-react";
import { isEmailVerificationRequiredError } from "@/lib/api";

interface LoginFormProps {
  onSwitchToRegister: () => void;
  onForgotPassword: () => void;
  onVerificationRequired: (email: string) => void;
}

const schema = z.object({
  email: z.string().email("请输入有效的邮箱"),
  password: z.string().min(8, "密码至少 8 个字符"),
});

type FormData = z.infer<typeof schema>;

export function LoginForm({
  onSwitchToRegister,
  onForgotPassword,
  onVerificationRequired,
}: LoginFormProps) {
  const { login } = useAuth();
  const [error, setError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormData>({
    resolver: zodResolver(schema),
  });

  const onSubmit = async (data: FormData) => {
    setError(null);
    try {
      await login(data.email, data.password);
    } catch (err) {
      if (isEmailVerificationRequiredError(err)) {
        onVerificationRequired(data.email);
        return;
      }
      setError(err instanceof Error ? err.message : "登录失败");
    }
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="grid gap-4">
      <div className="grid gap-2">
        <Label htmlFor="email">邮箱</Label>
        <Input
          id="email"
          type="email"
          placeholder="you@example.com"
          autoComplete="email"
          {...register("email")}
        />
        {errors.email && <p className="text-destructive">{errors.email.message}</p>}
      </div>
      <div className="grid gap-2">
        <div className="flex items-center justify-between">
          <Label htmlFor="password">密码</Label>
          <button
            type="button"
            className="text-xs text-muted-foreground hover:text-foreground"
            onClick={onForgotPassword}
          >
            忘记密码
          </button>
        </div>
        <Input
          id="password"
          type="password"
          minLength={8}
          autoComplete="current-password"
          {...register("password")}
        />
        {errors.password && <p className="text-destructive">{errors.password.message}</p>}
      </div>
      {error && <p className="text-destructive">{error}</p>}
      <Button type="submit" disabled={isSubmitting} className="w-full">
        {isSubmitting ? (
          <>
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            登录中
          </>
        ) : (
          "登录"
        )}
      </Button>
      <p className="text-center text-muted-foreground">
        还没有账号？{" "}
        <button
          type="button"
          className="cursor-pointer underline hover:text-foreground"
          onClick={onSwitchToRegister}
        >
          去注册
        </button>
      </p>
    </form>
  );
}
