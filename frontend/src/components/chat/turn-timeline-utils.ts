import type { TimelineItem } from "@/lib/types";

const sandboxCommandTools = new Set(["sandbox.exec", "sandbox.shell_connect"]);

export function isSandboxCommandTool(toolName: string) {
  return sandboxCommandTools.has(toolName.trim());
}

export function reasoningSummary(item: TimelineItem) {
  return item.content_text?.trim() || "";
}

export function splitLeadingReasoningTitle(summary: string) {
  const match = summary.match(/^\s*\*\*([^*\n]+)\*\*[ \t]*(?:\r?\n+|$)/);
  if (!match) return { body: summary, title: "" };

  return {
    body: summary.slice(match[0].length).trimStart(),
    title: match[1].trim(),
  };
}

export function clipText(value: string, limit = 72) {
  if (value.length <= limit) return value;
  return `${value.slice(0, limit).trimEnd()}...`;
}

export function getMetadataString(item: TimelineItem, key: string) {
  const value = item.metadata?.[key];
  return typeof value === "string" ? value.trim() : "";
}
