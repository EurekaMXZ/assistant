"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useAuth } from "@/hooks/use-auth";
import { Button } from "@/components/ui/button";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/shared/spinner";
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
      <FormField label="邮箱" htmlFor="email" error={errors.email?.message}>
        <Input
          id="email"
          type="email"
          placeholder="you@example.com"
          autoComplete="email"
          {...register("email")}
        />
      </FormField>
      <FormField
        label="密码"
        htmlFor="password"
        error={errors.password?.message}
        labelAction={
          <Button
            type="button"
            variant="link"
            size="xs"
            className="h-auto min-h-10 px-0 py-0 text-xs text-muted-foreground hover:text-foreground md:min-h-0"
            onClick={onForgotPassword}
          >
            忘记密码
          </Button>
        }
      >
        <Input
          id="password"
          type="password"
          minLength={8}
          autoComplete="current-password"
          {...register("password")}
        />
      </FormField>
      {error && <p className="text-destructive">{error}</p>}
      <Button type="submit" disabled={isSubmitting} className="w-full">
        {isSubmitting ? (
          <>
            <Spinner className="mr-2" />
            登录中
          </>
        ) : (
          "登录"
        )}
      </Button>
      <p className="text-center text-muted-foreground">
        还没有账号？{" "}
        <Button
          type="button"
          variant="link"
          className="h-auto min-h-10 px-0 py-0 align-baseline text-inherit underline hover:text-foreground md:min-h-0"
          onClick={onSwitchToRegister}
        >
          去注册
        </Button>
      </p>
    </form>
  );
}
