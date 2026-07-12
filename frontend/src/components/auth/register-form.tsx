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
        <Label htmlFor="username">用户名</Label>
        <Input
          id="username"
          type="text"
          placeholder="alice"
          autoComplete="username"
          {...register("username")}
        />
        {errors.username && <p className="text-destructive">{errors.username.message}</p>}
      </div>
      <div className="grid gap-2">
        <Label htmlFor="password">密码</Label>
        <Input
          id="password"
          type="password"
          minLength={8}
          autoComplete="new-password"
          {...register("password")}
        />
        {errors.password && <p className="text-destructive">{errors.password.message}</p>}
      </div>
      {error && <p className="text-destructive">{error}</p>}
      <Button type="submit" disabled={isSubmitting} className="w-full">
        {isSubmitting ? (
          <>
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            注册中
          </>
        ) : (
          "注册"
        )}
      </Button>
      <p className="text-center text-muted-foreground">
        已有账号？{" "}
        <button
          type="button"
          className="cursor-pointer underline hover:text-foreground"
          onClick={onSwitchToLogin}
        >
          去登录
        </button>
      </p>
    </form>
  );
}
