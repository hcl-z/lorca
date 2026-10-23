// A Runner's mcp.json: the MCP servers the user added there, asked of that Runner through the core
// (`mcp.*`, sealed to it; see docs/architecture/mcp-servers.md). The phone lists them, turns
// each on or off, and reconnects one; adding and editing a server stay on a computer.

import * as core from "../../modules/lorca-core";
import type { PluginStatus } from "./model";
import { t } from "../i18n";

/// A server's entry as the CLI writes it back (`command`, `args`, `env`, or `url`, `headers`).
export interface McpEntry {
  type?: string;
  command?: string;
  args?: string[];
  url?: string;
  description?: string;
}

export interface McpServer {
  /// Its key in the file.
  name: string;
  /// The plugin id it runs as.
  id: string;
  transport: "http" | "stdio" | null;
  enabled: boolean;
  config: McpEntry | null;
  /// Why it cannot run: an entry the CLI cannot read, or a name another plugin's id takes.
  problem: string | null;
  /// Its plugin's state while it runs and is on.
  status: PluginStatus | null;
  signs_in: boolean;
  signed_in: boolean;
  /// How many tools it offered when it last connected; null before it has.
  tool_count: number | null;
}

export interface McpFile {
  path: string;
  /// Why the file does not read; the Runner keeps the servers it read before.
  error: string | null;
  servers: McpServer[];
}

export function mcpList(runnerId: string): Promise<McpFile> {
  return core.request<McpFile>("mcp.list", { runner_id: runnerId });
}

export function setMcpEnabled(runnerId: string, name: string, enabled: boolean): Promise<unknown> {
  return core.request("mcp.set_enabled", { runner_id: runnerId, name, enabled });
}

/// Drops the server's connection and answers once it has connected again or failed.
export function reconnectMcp(runnerId: string, name: string): Promise<unknown> {
  return core.request("mcp.reconnect", { runner_id: runnerId, name, fresh: true });
}

export type McpTone = "red" | "orange" | "green" | "accent" | "secondary" | "tertiary";

/// How a server stands in a few words, and the tone they take, as the desktop apps word it.
export function mcpState(server: McpServer): { text: string; tone: McpTone } {
  if (server.problem) return { text: server.problem, tone: "red" };
  if (!server.enabled) return { text: t("Off"), tone: "tertiary" };
  const status = server.status;
  if (!status) return { text: "", tone: "secondary" };
  switch (status.state) {
    case "ready":
      if (server.tool_count == null) return { text: t("Not connected yet"), tone: "secondary" };
      return { text: server.tool_count === 1 ? t("1 tool") : t("{count} tools", { count: server.tool_count }), tone: "green" };
    case "needs_auth":
    case "insufficient_access":
      return { text: t("Needs a sign-in"), tone: "orange" };
    case "connecting":
      return { text: status.detail || t("Connecting…"), tone: "accent" };
    case "error":
      return { text: status.detail || t("Couldn't connect"), tone: "red" };
  }
  return { text: status.detail ?? "", tone: "secondary" };
}

export function isRemote(entry: McpEntry): boolean {
  return !!entry.url && entry.type !== "stdio";
}

/// The command line, or the URL, a row shows under the server's name.
export function mcpAddress(entry: McpEntry | null): string {
  if (!entry) return "";
  if (isRemote(entry)) return entry.url ?? "";
  return joinCommandLine([entry.command ?? "", ...(entry.args ?? [])]);
}

/// Words joined as Windows and a shell both read them back: a word with a space or a quote goes in
/// double quotes, its quotes escaped and the backslashes before them doubled.
export function joinCommandLine(words: string[]): string {
  return words.map(quoteWord).join(" ");
}

function quoteWord(word: string): string {
  if (word !== "" && !/[\s"']/.test(word)) return word;
  let out = '"';
  let backslashes = 0;
  for (const c of word) {
    if (c === "\\") {
      backslashes++;
      continue;
    }
    out += "\\".repeat(c === '"' ? backslashes * 2 + 1 : backslashes) + c;
    backslashes = 0;
  }
  return out + "\\".repeat(backslashes * 2) + '"';
}
