"use client";

import { useState } from "react";
import { ForgotPasswordForm } from "@/components/auth/forgot-password-form";
import { LoginForm } from "@/components/auth/login-form";
import { RegisterForm } from "@/components/auth/register-form";
import { VerificationPendingForm } from "@/components/auth/verification-pending-form";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { AuthDialogMode } from "@/lib/auth-dialog-events";

interface AuthDialogProps {
  mode: AuthDialogMode | null;
  onModeChange: (mode: AuthDialogMode | null) => void;
}

export function AuthDialog({ mode, onModeChange }: AuthDialogProps) {
  const [pendingEmail, setPendingEmail] = useState("");
  const [verificationEmailSent, setVerificationEmailSent] = useState(false);

  return (
    <Dialog open={mode !== null} onOpenChange={(open) => !open && onModeChange(null)}>
      <DialogContent showCloseButton={mode !== null} className="sm:max-w-md">
        {mode === "login" ? (
          <>
            <DialogHeader>
              <DialogTitle>登录</DialogTitle>
            </DialogHeader>
            <LoginForm
              onSwitchToRegister={() => onModeChange("register")}
              onForgotPassword={() => onModeChange("forgot-password")}
              onVerificationRequired={(email) => {
                setPendingEmail(email);
                setVerificationEmailSent(false);
                onModeChange("verification-pending");
              }}
            />
          </>
        ) : mode === "register" ? (
          <>
            <DialogHeader>
              <DialogTitle>注册</DialogTitle>
            </DialogHeader>
            <RegisterForm
              onSwitchToLogin={() => onModeChange("login")}
              onVerificationPending={(email, emailSent) => {
                setPendingEmail(email);
                setVerificationEmailSent(emailSent);
                onModeChange("verification-pending");
              }}
            />
          </>
        ) : mode === "verification-pending" ? (
          <>
            <DialogHeader><DialogTitle>验证邮箱</DialogTitle></DialogHeader>
            <VerificationPendingForm
              email={pendingEmail}
              emailSent={verificationEmailSent}
              onBackToLogin={() => onModeChange("login")}
            />
          </>
        ) : mode === "forgot-password" ? (
          <>
            <DialogHeader><DialogTitle>重置密码</DialogTitle></DialogHeader>
            <ForgotPasswordForm onBackToLogin={() => onModeChange("login")} />
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
