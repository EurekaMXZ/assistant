import { describe, expect, it } from "vitest";
import { buildAdminModelCreatePayload, buildAdminModelUpdatePayload } from "./admin-model-payloads";
import { formatNanos, parseDecimalNanos } from "./decimal-nanos";
import type { Model, ProviderCredential } from "./types";

const form = {
  credentialId: "credential-1",
  slug: "model",
  upstreamModel: "upstream",
  displayName: "Model",
  description: "Description",
  contextWindow: "128000",
  maxOutput: "8192",
  supportsTools: true,
  supportsParallelTools: false,
  reasoningEfforts: ["high" as const],
  defaultParameters: '{"temperature":0}',
};

describe("admin model payloads", () => {
  it("derives create provider from the credential", () => {
    const credential = { id: "credential-1", provider: "custom" } as ProviderCredential;
    expect(buildAdminModelCreatePayload(form, credential)).toMatchObject({
      provider: "custom",
      input_modalities: ["text"],
      output_modalities: ["text"],
      context_window_tokens: 128000,
    });
  });

  it("preserves non-editable capabilities in update payloads", () => {
    const original = {
      provider: "custom",
      input_modalities: ["text", "image"],
      output_modalities: ["text", "image"],
    } as Model;
    const payload = buildAdminModelUpdatePayload(form, original);
    expect(payload).not.toHaveProperty("provider");
    expect(payload).not.toHaveProperty("slug");
    expect(payload.input_modalities).toEqual(["text", "image"]);
    expect(payload.output_modalities).toEqual(["text", "image"]);
  });

  it("rejects invalid token fields", () => {
    expect(() => buildAdminModelUpdatePayload({ ...form, contextWindow: "1.5" }, {} as Model)).toThrow("正整数");
  });
});

describe("decimal nanos", () => {
  it("parses nanos without floating point rounding", () => {
    expect(parseDecimalNanos("0.000000001")).toBe(1);
    expect(parseDecimalNanos("12.345678901")).toBe(12_345_678_901);
    expect(formatNanos(12_345_678_901)).toBe("12.345678901");
  });

  it("rejects excess precision and unsafe values", () => {
    expect(() => parseDecimalNanos("0.0000000001")).toThrow("9 位小数");
    expect(() => parseDecimalNanos("9007199.254740992")).toThrow("安全提交");
  });
});
