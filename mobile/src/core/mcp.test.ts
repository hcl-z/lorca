import { expect, mock, test } from "bun:test";

mock.module("./prefs", () => ({ loadPrefs: () => ({}), savePrefs: () => {}, coreHome: () => "/unused", pathOf: (p: string) => p, wipePrefs: () => {} }));
mock.module("expo-localization", () => ({ getLocales: () => [{ languageCode: "en", languageTag: "en-US" }] }));
mock.module("../../modules/lorca-core", () => ({ request: async () => ({}) }));

const { joinCommandLine, mcpAddress, mcpState } = await import("./mcp");
type Server = Parameters<typeof mcpState>[0];

const server = (over: Partial<Server>): Server => ({
  name: "fs",
  id: "fs",
  transport: "stdio",
  enabled: true,
  config: { command: "npx" },
  problem: null,
  status: { id: "fs", name: "fs", state: "ready" },
  signs_in: false,
  signed_in: false,
  tool_count: 3,
  ...over,
});

test("a server's state reads as the desktop apps word it", () => {
  expect(mcpState(server({}))).toEqual({ text: "3 tools", tone: "green" });
  expect(mcpState(server({ tool_count: 1 })).text).toBe("1 tool");
  expect(mcpState(server({ tool_count: null })).text).toBe("Not connected yet");
  expect(mcpState(server({ enabled: false })).tone).toBe("tertiary");
  expect(mcpState(server({ problem: "No command" }))).toEqual({ text: "No command", tone: "red" });
  expect(mcpState(server({ status: { id: "fs", name: "fs", state: "needs_auth" } })).tone).toBe("orange");
  expect(mcpState(server({ status: { id: "fs", name: "fs", state: "error", detail: "exited 1" } }))).toEqual({ text: "exited 1", tone: "red" });
});

test("the address is the URL, or the command line quoted as Windows reads it back", () => {
  expect(mcpAddress({ url: "https://mcp.example.com/sse" })).toBe("https://mcp.example.com/sse");
  expect(mcpAddress({ command: "npx", args: ["-y", "server", "/tmp/My Folder"] })).toBe('npx -y server "/tmp/My Folder"');
  expect(joinCommandLine(["C:\\Program Files\\x\\", 'say "hi"'])).toBe('"C:\\Program Files\\x\\\\" "say \\"hi\\""');
  expect(mcpAddress(null)).toBe("");
});
