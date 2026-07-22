"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { changePassword, isSessionUnauthorizedError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/shared/spinner";
import { toast } from "sonner";

const schema = z
  .object({
    currentPassword: z.string().min(8, "当前密码至少 8 个字符"),
    newPassword: z.string().min(8, "新密码至少 8 个字符"),
    confirmPassword: z.string().min(1, "请确认新密码"),
  })
  .refine((data) => data.newPassword === data.confirmPassword, {
    message: "两次输入的新密码不一致",
    path: ["confirmPassword"],
  });

type FormData = z.infer<typeof schema>;

export function ChangePasswordForm() {
  const [isSubmitting, setIsSubmitting] = useState(false);
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<FormData>({
    resolver: zodResolver(schema),
  });

  const onSubmit = async (data: FormData) => {
    setIsSubmitting(true);
    try {
      await changePassword(data.currentPassword, data.newPassword);
      toast.success("密码已修改");
      reset();
    } catch (err) {
      if (isSessionUnauthorizedError(err)) {
        return;
      }
      toast.error(err instanceof Error ? err.message : "修改失败");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="grid max-w-md gap-5">
      <FormField label="当前密码" htmlFor="currentPassword" error={errors.currentPassword?.message}>
        <Input
          id="currentPassword"
          type="password"
          minLength={8}
          autoComplete="current-password"
          {...register("currentPassword")}
        />
      </FormField>
      <FormField label="新密码" htmlFor="newPassword" error={errors.newPassword?.message}>
        <Input
          id="newPassword"
          type="password"
          minLength={8}
          autoComplete="new-password"
          {...register("newPassword")}
        />
      </FormField>
      <FormField
        label="确认新密码"
        htmlFor="confirmPassword"
        error={errors.confirmPassword?.message}
      >
        <Input
          id="confirmPassword"
          type="password"
          minLength={8}
          autoComplete="new-password"
          {...register("confirmPassword")}
        />
      </FormField>
      <Button type="submit" className="w-fit" disabled={isSubmitting}>
        {isSubmitting ? (
          <>
            <Spinner className="mr-2" />
            保存中
          </>
        ) : (
          "修改密码"
        )}
      </Button>
    </form>
  );
}
