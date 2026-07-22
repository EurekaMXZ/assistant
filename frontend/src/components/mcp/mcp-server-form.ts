import type {
  CreateMCPSecretInput,
  MCPSecret,
  UpdateMCPSecretInput,
  UserMCPServer,
} from "@/lib/types";
import {
  nextSecretDraftID,
  sameSecretName,
  type SecretDraft,
  type SecretKind,
} from "./mcp-secret-drafts";

export interface MCPServerForm {
  name: string;
  slug: string;
  endpointURL: string;
  enabled: boolean;
  parameters: SecretDraft[];
  headers: SecretDraft[];
  enabledTools: string[];
}

const parameterNamePattern = /^[A-Za-z0-9._~-]+$/;
const headerNamePattern = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const managedHeaders = new Set([
  "accept",
  "connection",
  "content-length",
  "content-type",
  "host",
  "keep-alive",
  "mcp-protocol-version",
  "mcp-session-id",
  "proxy-authorization",
  "proxy-connection",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
]);

export function emptyForm(): MCPServerForm {
  return {
    name: "",
    slug: "",
    endpointURL: "",
    enabled: true,
    parameters: [],
    headers: [],
    enabledTools: [],
  };
}

function secretDrafts(secrets: MCPSecret[], kind: SecretKind): SecretDraft[] {
  return secrets.map((secret) => ({
    id: nextSecretDraftID(kind),
    name: secret.name,
    value: "",
    configured: secret.configured,
    keyHint: secret.key_hint,
    originalName: secret.name,
  }));
}

export function formFromServer(server: UserMCPServer): MCPServerForm {
  return {
    name: server.name,
    slug: server.slug,
    endpointURL: server.endpoint_url,
    enabled: server.enabled,
    parameters: secretDrafts(server.parameters, "parameter"),
    headers: secretDrafts(server.headers, "header"),
    enabledTools: server.tools.filter((tool) => tool.enabled).map((tool) => tool.name),
  };
}

export function formFingerprint(form: MCPServerForm) {
  return JSON.stringify(form);
}

export function validationMessage(form: MCPServerForm, creating: boolean): string | null {
  const name = form.name.trim();
  if (!name || Array.from(name).length > 100) return "名称应为 1 至 100 个字符";

  const slug = form.slug.trim();
  if (!slugPattern.test(slug) || slug.length > 64) {
    return "标识只能包含小写字母、数字和单个连字符";
  }

  const endpointError = validateEndpointURL(form.endpointURL);
  if (endpointError) return endpointError;

  return (
    validateSecretDrafts(form.parameters, "parameter", creating) ||
    validateSecretDrafts(form.headers, "header", creating)
  );
}

function validateEndpointURL(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed || trimmed.length > 2048) return "请输入有效的服务器 URL";
  try {
    const url = new URL(trimmed);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return "服务器 URL 必须使用 HTTP 或 HTTPS";
    }
    if (url.username || url.password || url.search || url.hash) {
      return "服务器 URL 不能包含账户信息、查询参数或片段";
    }
  } catch {
    return "请输入有效的服务器 URL";
  }
  return null;
}

function validateSecretDrafts(
  drafts: SecretDraft[],
  kind: SecretKind,
  creating: boolean,
): string | null {
  const label = kind === "parameter" ? "查询参数" : "请求头";
  if (drafts.length > 32) return `${label}最多可添加 32 项`;

  const names = new Set<string>();
  for (const draft of drafts) {
    const name = draft.name.trim();
    if (!name || name.length > 128) return `${label}名称应为 1 至 128 个字符`;
    if (kind === "parameter" && !parameterNamePattern.test(name)) {
      return "查询参数名称包含不支持的字符";
    }
    if (kind === "header" && !headerNamePattern.test(name)) {
      return "请求头名称包含不支持的字符";
    }
    if (kind === "header" && managedHeaders.has(name.toLowerCase())) {
      return `请求头 ${name} 由系统管理，不能手动设置`;
    }

    const lookupName = kind === "header" ? name.toLowerCase() : name;
    if (names.has(lookupName)) return `${label}名称不能重复`;
    names.add(lookupName);

    if (draft.value.length > 8192) return `${label}值不能超过 8192 个字符`;
    const canKeepExisting =
      !creating &&
      draft.configured &&
      sameSecretName(draft.originalName, name, kind) &&
      draft.value === "";
    if (!draft.value && !canKeepExisting) return `请填写 ${name} 的值`;
  }
  return null;
}

export function createSecretPayload(drafts: SecretDraft[]): CreateMCPSecretInput[] {
  return drafts.map((draft) => ({ name: draft.name.trim(), value: draft.value }));
}

export function updateSecretPayload(drafts: SecretDraft[]): UpdateMCPSecretInput[] {
  return drafts.map((draft) => ({
    name: draft.name.trim(),
    ...(draft.value ? { value: draft.value } : {}),
  }));
}

export function localizedError(error: unknown, fallback: string) {
  if (!(error instanceof Error)) return fallback;
  const knownMessages: Record<string, string> = {
    "MCP server slug already exists": "该服务器标识已被使用",
    "endpoint_url host is not allowed": "该服务器地址不允许访问",
    "unable to connect to MCP server": "无法连接到 MCP 服务器",
    "MCP server tools/list failed": "无法读取 MCP 工具清单",
    "MCP server validation failed": "MCP 服务器验证失败",
    "secret value is required for a new entry": "新增或改名的凭据必须填写值",
  };
  return knownMessages[error.message] || fallback;
}
