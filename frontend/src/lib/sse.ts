import { ApiError, handleSessionUnauthorized } from "./api";
import type { SseFrame } from "./types";
import { parseTurnStreamFrame, turnStreamEventSchemas } from "./api-schemas";

export class SseValidationError extends Error {
  constructor(event: string) {
    super(`Invalid SSE payload for ${event}`);
    this.name = "SseValidationError";
  }
}

export function parseSseFrame(frame: string): SseFrame | null {
  const lines = frame.replace(/\r\n/g, "\n").split("\n");
  let event = "message";
  const dataLines: string[] = [];
  for (const line of lines) {
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trimStart());
    }
  }
  const dataText = dataLines.join("\n");
  if (!dataText) return null;
  try {
    const data: unknown = JSON.parse(dataText);
    const parsed = parseTurnStreamFrame(event, data);
    if (parsed) return parsed;
    if (event in turnStreamEventSchemas) throw new SseValidationError(event);
    return null;
  } catch {
    throw new SseValidationError(event);
  }
}

export async function* streamEvents(
  url: string,
  token: string | null,
  signal?: AbortSignal,
): AsyncGenerator<SseFrame, void, unknown> {
  const headers: Record<string, string> = {
    Accept: "text/event-stream",
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const res = await fetch(url, { headers, signal });
  if (!res.ok) {
    let message = `SSE failed (${res.status})`;
    try {
      const body = await res.json();
      if (body && typeof body.error === "string") message = body.error;
    } catch {
      // ignore
    }
    handleSessionUnauthorized(res.status, message);
    throw new ApiError(message, res.status);
  }

  const reader = res.body?.getReader();
  if (!reader) throw new Error("No response body");

  const decoder = new TextDecoder();
  let buffer = "";

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      buffer = buffer.replace(/\r\n/g, "\n");
      const frames = buffer.split("\n\n");
      buffer = frames.pop() || "";
      for (const frame of frames) {
        const parsed = parseSseFrame(frame);
        if (parsed) yield parsed;
      }
    }
    if (buffer.trim()) {
      const parsed = parseSseFrame(buffer);
      if (parsed) yield parsed;
    }
  } finally {
    reader.releaseLock();
  }
}
