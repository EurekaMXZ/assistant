import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  createMCPServer,
  deleteMCPServer,
  getMCPServer,
  listMCPServers,
  testMCPServer,
  updateMCPServer,
} from "./api";
import { userMCPServerSchema } from "./api-schemas";

const timestamp = "2026-07-21T12:00:00Z";
const server = {
  id: "server-1",
  name: "知识库",
  slug: "knowledge-base",
  endpoint_url: "https://mcp.example.com/mcp",
  enabled: true,
  revision: 2,
  parameters: [{ name: "api_key", configured: true, key_hint: "...cret" }],
  headers: [{ name: "Authorization", configured: true, key_hint: "...oken" }],
  tools: [
    {
      name: "search",
      description: "搜索知识库",
      input_schema: { type: "object", properties: { query: { type: "string" } } },
      enabled: true,
      created_at: timestamp,
      updated_at: timestamp,
    },
  ],
  last_validation_status: "valid",
  last_validated_at: timestamp,
  created_at: timestamp,
  updated_at: timestamp,
} as const;

beforeEach(() => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
    clear: () => values.clear(),
    key: (index: number) => [...values.keys()][index] ?? null,
    get length() {
      return values.size;
    },
  } satisfies Storage;
  vi.stubGlobal("localStorage", storage);
  Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  storage.setItem("assistant_access_token", "token");
});

describe("HTTP MCP API", () => {
  it("validates server metadata and discovered tool schemas", () => {
    expect(userMCPServerSchema.parse(server).tools[0]?.input_schema).toEqual(
      server.tools[0].input_schema,
    );
    expect(
      userMCPServerSchema.safeParse({
        ...server,
        last_validation_status: "unknown",
      }).success,
    ).toBe(false);
  });

  it("sends required secret values when creating a server", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(Response.json({ server }, { status: 201 }));

    await createMCPServer({
      name: " 知识库 ",
      slug: " knowledge-base ",
      endpoint_url: " https://mcp.example.com/mcp ",
      parameters: [{ name: " api_key ", value: "query-secret" }],
      headers: [{ name: " Authorization ", value: "header-secret" }],
    });

    const body = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body));
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/v1/mcp-servers");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
    expect(body).toMatchObject({
      name: "知识库",
      slug: "knowledge-base",
      endpoint_url: "https://mcp.example.com/mcp",
      parameters: [{ name: "api_key", value: "query-secret" }],
      headers: [{ name: "Authorization", value: "header-secret" }],
    });
  });

  it("preserves configured secrets when PATCH omits or nulls the value", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(Response.json({ server }));

    await updateMCPServer("server/1", {
      parameters: [{ name: "api_key" }],
      headers: [{ name: "Authorization", value: null }],
      enabled_tools: ["search"],
    });

    const body = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body));
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/v1/mcp-servers/server%2F1");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("PATCH");
    expect(body.parameters).toEqual([{ name: "api_key" }]);
    expect(body.headers).toEqual([{ name: "Authorization", value: null }]);
    expect(body.enabled_tools).toEqual(["search"]);
  });

  it("routes list, detail, test, and delete operations", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(Response.json({ servers: [server] }))
      .mockResolvedValueOnce(Response.json({ server }))
      .mockResolvedValueOnce(Response.json({ server }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));

    await listMCPServers();
    await getMCPServer("server-1");
    await testMCPServer("server-1");
    await deleteMCPServer("server-1");

    expect(fetchMock.mock.calls.map((call) => [call[0], call[1]?.method || "GET"])).toEqual([
      ["/api/v1/mcp-servers", "GET"],
      ["/api/v1/mcp-servers/server-1", "GET"],
      ["/api/v1/mcp-servers/server-1/test", "POST"],
      ["/api/v1/mcp-servers/server-1", "DELETE"],
    ]);
  });
});
