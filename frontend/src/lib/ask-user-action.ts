export interface SafeAskUserActionURL {
  protocol: "https" | "weixin";
  host: string;
  url: string;
}

function unsafeHTTPSHostname(value: string) {
  const hostname = value.trim().toLowerCase().replace(/\.$/, "");
  if (
    !hostname ||
    hostname === "localhost" ||
    hostname.endsWith(".localhost") ||
    hostname === "metadata" ||
    hostname.startsWith("metadata.") ||
    hostname === "instance-data"
  ) {
    return true;
  }
  if (hostname.includes(":") || hostname.includes("[") || hostname.includes("]")) return true;
  if (!/^\d+(?:\.\d+){3}$/.test(hostname)) return false;
  const octets = hostname.split(".").map(Number);
  if (octets.some((octet) => !Number.isInteger(octet) || octet < 0 || octet > 255)) return true;
  const [first, second] = octets;
  return (
    first === 0 ||
    first === 10 ||
    first === 127 ||
    (first === 100 && second >= 64 && second <= 127) ||
    (first === 169 && second === 254) ||
    (first === 172 && second >= 16 && second <= 31) ||
    (first === 192 && (second === 0 || second === 168)) ||
    (first === 198 && (second === 18 || second === 19)) ||
    first >= 224
  );
}

export function parseSafeAskUserActionURL(value: string): SafeAskUserActionURL | null {
  try {
    const parsed = new URL(value);
    if (parsed.username || parsed.password || parsed.hash) return null;
    if (parsed.protocol === "https:") {
      if (unsafeHTTPSHostname(parsed.hostname)) return null;
      return { protocol: "https", host: parsed.host, url: parsed.toString() };
    }
    if (
      parsed.protocol === "weixin:" &&
      parsed.hostname.toLowerCase() === "wap" &&
      !parsed.port &&
      parsed.pathname === "/pay"
    ) {
      return { protocol: "weixin", host: parsed.hostname, url: parsed.toString() };
    }
  } catch {
    return null;
  }
  return null;
}
