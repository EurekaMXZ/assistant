const NANOS_PER_UNIT = BigInt(1_000_000_000);

export function parseDecimalNanos(value: string) {
  const normalized = value.trim();
  const match = /^(\d+)(?:\.(\d{1,9}))?$/.exec(normalized);
  if (!match) throw new Error("金额必须是最多 9 位小数的非负数");
  const whole = BigInt(match[1]);
  const fraction = BigInt((match[2] || "").padEnd(9, "0"));
  const nanos = whole * NANOS_PER_UNIT + fraction;
  if (nanos > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error("金额超出前端可安全提交的范围");
  }
  return Number(nanos);
}

export function formatNanos(nanos: number) {
  if (!Number.isSafeInteger(nanos)) return String(nanos / 1_000_000_000);
  const value = BigInt(nanos);
  const whole = value / NANOS_PER_UNIT;
  const fraction = (value % NANOS_PER_UNIT).toString().padStart(9, "0").replace(/0+$/, "");
  return fraction ? `${whole}.${fraction}` : whole.toString();
}
