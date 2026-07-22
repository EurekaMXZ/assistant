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

interface RegisterFormProps {
  onSwitchToLogin: () => void;
  onVerificationPending: (email: string, emailSent: boolean) => void;
}

const schema = z.object({
  email: z.string().email("请输入有效的邮箱"),
  username: z.string().min(2, "用户名至少 2 个字符"),
  password: z.string().min(8, "密码至少 8 个字符"),
});

type FormData = z.infer<typeof schema>;

export function RegisterForm({ onSwitchToLogin, onVerificationPending }: RegisterFormProps) {
  const { register: registerUser } = useAuth();
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
      const result = await registerUser(data.email, data.username, data.password);
      onVerificationPending(data.email, result.email_sent);
    } catch (err) {
      setError(err instanceof Error ? err.message : "注册失败");
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
      <FormField label="用户名" htmlFor="username" error={errors.username?.message}>
        <Input
          id="username"
          type="text"
          placeholder="alice"
          autoComplete="username"
          {...register("username")}
        />
      </FormField>
      <FormField label="密码" htmlFor="password" error={errors.password?.message}>
        <Input
          id="password"
          type="password"
          minLength={8}
          autoComplete="new-password"
          {...register("password")}
        />
      </FormField>
      {error && <p className="text-destructive">{error}</p>}
      <Button type="submit" disabled={isSubmitting} className="w-full">
        {isSubmitting ? (
          <>
            <Spinner className="mr-2" />
            注册中
          </>
        ) : (
          "注册"
        )}
      </Button>
      <p className="text-center text-muted-foreground">
        已有账号？{" "}
        <Button
          type="button"
          variant="link"
          className="h-auto min-h-10 px-0 py-0 align-baseline text-inherit underline hover:text-foreground md:min-h-0"
          onClick={onSwitchToLogin}
        >
          去登录
        </Button>
      </p>
    </form>
  );
}
