import type { User, UserRole } from "@/lib/types";

export function canAccessAdmin(role?: UserRole | null) {
  return role === "system" || role === "admin";
}

export function canManageUser(actor: User, target: User) {
  if (actor.id === target.id || target.role === "system") return false;
  if (actor.role === "system") return target.role === "admin" || target.role === "user";
  return actor.role === "admin" && target.role === "user";
}

export function manageableUserRoles(actor: User): Array<Exclude<UserRole, "system">> {
  return actor.role === "system" ? ["user", "admin"] : ["user"];
}
