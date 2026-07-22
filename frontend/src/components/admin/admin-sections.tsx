import {
  Activity,
  Boxes,
  CreditCard,
  KeyRound,
  LayoutDashboard,
  Mail,
  Users,
  type LucideIcon,
} from "lucide-react";
import { AdminAudit } from "./admin-audit";
import { AdminBilling } from "./admin-billing";
import { AdminCredentials } from "./admin-credentials";
import { AdminMailSettings } from "./admin-mail-settings";
import { AdminModels } from "./admin-models";
import { AdminOverview } from "./admin-overview";
import { AdminUsers } from "./admin-users";
import type { User, UserRole } from "@/lib/types";

export type AdminSection =
  "overview" | "models" | "credentials" | "mail" | "users" | "billing" | "audit";

interface AdminSectionContext {
  actor: User;
  navigate: (section: AdminSection) => void;
}

export interface AdminSectionDefinition {
  id: AdminSection;
  label: string;
  icon: LucideIcon;
  systemOnly: boolean;
  render: (context: AdminSectionContext) => React.ReactNode;
}

export const adminSections: readonly AdminSectionDefinition[] = [
  {
    id: "overview",
    label: "概览",
    icon: LayoutDashboard,
    systemOnly: false,
    render: ({ actor, navigate }) => <AdminOverview actor={actor} onNavigate={navigate} />,
  },
  { id: "models", label: "模型", icon: Boxes, systemOnly: true, render: () => <AdminModels /> },
  {
    id: "credentials",
    label: "提供方凭据",
    icon: KeyRound,
    systemOnly: true,
    render: () => <AdminCredentials />,
  },
  {
    id: "mail",
    label: "发信邮箱",
    icon: Mail,
    systemOnly: true,
    render: () => <AdminMailSettings />,
  },
  {
    id: "users",
    label: "用户",
    icon: Users,
    systemOnly: false,
    render: ({ actor }) => <AdminUsers actor={actor} />,
  },
  {
    id: "billing",
    label: "计费",
    icon: CreditCard,
    systemOnly: false,
    render: () => <AdminBilling />,
  },
  {
    id: "audit",
    label: "审计日志",
    icon: Activity,
    systemOnly: false,
    render: () => <AdminAudit />,
  },
];

export function adminSectionsForRole(role: UserRole) {
  return adminSections.filter((section) => !section.systemOnly || role === "system");
}
