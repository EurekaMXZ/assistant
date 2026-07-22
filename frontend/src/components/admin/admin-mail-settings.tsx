"use client";

import { useEffect, useState } from "react";
import { Send } from "lucide-react";
import { toast } from "sonner";
import { AdminLoading, AdminPageHeader, SavingIcon } from "@/components/admin/admin-shared";
import { ErrorState } from "@/components/shared/error-state";
import { Button } from "@/components/ui/button";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { getAdminMailSettings, testAdminMailSettings, updateAdminMailSettings } from "@/lib/api";
import type { MailSettings } from "@/lib/types";

type MailForm = Pick<
  MailSettings,
  "enabled" | "host" | "port" | "security" | "username" | "from_email" | "from_name"
>;

const emptyForm: MailForm = {
  enabled: false,
  host: "",
  port: 587,
  security: "starttls",
  username: "",
  from_email: "",
  from_name: "",
};

export function AdminMailSettings() {
  const [form, setForm] = useState<MailForm>(emptyForm);
  const [password, setPassword] = useState("");
  const [passwordConfigured, setPasswordConfigured] = useState(false);
  const [recipient, setRecipient] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const settings = await getAdminMailSettings();
      setForm({
        enabled: settings.enabled,
        host: settings.host,
        port: settings.port,
        security: settings.security,
        username: settings.username,
        from_email: settings.from_email,
        from_name: settings.from_name,
      });
      setPasswordConfigured(settings.password_configured);
      setPassword("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "发信邮箱加载失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const update = <K extends keyof MailForm>(key: K, value: MailForm[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
  };

  const save = async () => {
    setSaving(true);
    try {
      const settings = await updateAdminMailSettings({
        ...form,
        ...(password ? { password } : {}),
      });
      setPasswordConfigured(settings.password_configured);
      setPassword("");
      toast.success("发信邮箱已保存");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "发信邮箱保存失败");
    } finally {
      setSaving(false);
    }
  };

  const sendTest = async () => {
    setTesting(true);
    try {
      await testAdminMailSettings(recipient.trim());
      toast.success("测试邮件已发送");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "测试邮件发送失败");
    } finally {
      setTesting(false);
    }
  };

  return (
    <div>
      <AdminPageHeader title="发信邮箱" />
      {loading ? <AdminLoading /> : null}
      {!loading && error ? <ErrorState message={error} onRetry={load} /> : null}
      {!loading && !error ? (
        <div className="mt-7 max-w-3xl space-y-8">
          <section className="grid gap-5 sm:grid-cols-2">
            <label className="flex items-center gap-3 sm:col-span-2">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(event) => update("enabled", event.target.checked)}
                className="size-4 accent-foreground"
              />
              <span className="text-sm font-medium">启用发信</span>
            </label>
            <FormField label="SMTP 主机" htmlFor="mail-host">
              <Input
                id="mail-host"
                value={form.host}
                onChange={(event) => update("host", event.target.value)}
              />
            </FormField>
            <div className="grid grid-cols-[1fr_1.25fr] gap-4">
              <FormField label="端口" htmlFor="mail-port">
                <Input
                  id="mail-port"
                  type="number"
                  min={1}
                  max={65535}
                  value={form.port}
                  onChange={(event) => update("port", Number(event.target.value))}
                />
              </FormField>
              <FormField label="安全连接" htmlFor="mail-security">
                <Select
                  items={[
                    { value: "none", label: "无" },
                    { value: "starttls", label: "STARTTLS" },
                    { value: "tls", label: "TLS" },
                  ]}
                  value={form.security}
                  onValueChange={(value) => value && update("security", value)}
                >
                  <SelectTrigger id="mail-security">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">无</SelectItem>
                    <SelectItem value="starttls">STARTTLS</SelectItem>
                    <SelectItem value="tls">TLS</SelectItem>
                  </SelectContent>
                </Select>
              </FormField>
            </div>
            <FormField label="用户名" htmlFor="mail-username">
              <Input
                id="mail-username"
                value={form.username}
                onChange={(event) => update("username", event.target.value)}
                autoComplete="username"
              />
            </FormField>
            <FormField label="密码" htmlFor="mail-password">
              <Input
                id="mail-password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder={passwordConfigured ? "已配置" : undefined}
                autoComplete="new-password"
              />
            </FormField>
            <FormField label="发件邮箱" htmlFor="mail-from-email">
              <Input
                id="mail-from-email"
                type="email"
                value={form.from_email}
                onChange={(event) => update("from_email", event.target.value)}
              />
            </FormField>
            <FormField label="发件人名称" htmlFor="mail-from-name">
              <Input
                id="mail-from-name"
                value={form.from_name}
                onChange={(event) => update("from_name", event.target.value)}
              />
            </FormField>
            <div className="sm:col-span-2">
              <Button
                disabled={saving || !form.host.trim() || !form.port || !form.from_email.trim()}
                onClick={() => void save()}
              >
                <SavingIcon saving={saving} />
                保存
              </Button>
            </div>
          </section>

          <section>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
              <FormField label="收件邮箱" htmlFor="mail-test-recipient" className="flex-1">
                <Input
                  id="mail-test-recipient"
                  type="email"
                  value={recipient}
                  onChange={(event) => setRecipient(event.target.value)}
                />
              </FormField>
              <Button
                variant="outline"
                disabled={testing || !recipient.trim()}
                onClick={() => void sendTest()}
              >
                {testing ? <SavingIcon saving /> : <Send />}
                发送测试邮件
              </Button>
            </div>
          </section>
        </div>
      ) : null}
    </div>
  );
}
