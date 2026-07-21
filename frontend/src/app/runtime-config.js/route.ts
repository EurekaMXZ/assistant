import { connection } from "next/server";

function browserRuntimeConfig() {
  return {
    amapJsKey: process.env.AMAP_JS_KEY?.trim() || "",
    amapServiceHost: process.env.AMAP_SERVICE_HOST?.trim() || "",
  };
}

export async function GET() {
  await connection();
  const config = JSON.stringify(browserRuntimeConfig()).replaceAll("<", "\\u003c");
  return new Response(`window.__ASSISTANT_RUNTIME_CONFIG__=${config};`, {
    headers: {
      "Cache-Control": "no-store, max-age=0",
      "Content-Type": "application/javascript; charset=utf-8",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
