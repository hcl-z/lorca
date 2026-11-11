// A bot's Access, after the desktop apps' Access sheet: which plugins on its Runner it may use,
// each plugin on or off with each of its tools on or asking first, and its own files and shell
// commands. Only the user changes it; here each change saves at once, as iOS Settings does. The
// plugins and the tools each offered come from the Runner (`bots.permissions`, a sealed request
// through the core); the policy itself is in the roster.

import { create } from "zustand";
import * as core from "../../modules/lorca-core";
import { t } from "../i18n";
import { BROWSER_PLUGIN_ID, type BotPermissions, type ToolPermission } from "./model";
import { useStore } from "./store";

/// A tool's three states, as its switch shows them: on, asking first, or off.
export type ToolState = "on" | "ask" | "off";

export const TOOL_STATES: ToolState[] = ["on", "ask", "off"];

export interface AccessTool {
  name: string;
  title?: string | null;
  description?: string | null;
  /** What it does, as its Runner last read it: read, draft, or write. */
  capability?: string | null;
}

export interface AccessPlugin {
  id: string;
  name: string;
  tools: AccessTool[];
}

export function permission(state: ToolState): ToolPermission {
  return { enabled: state !== "off", ask: state === "ask" };
}

export function stateOf(permission: ToolPermission | undefined, fallback: ToolState = "on"): ToolState {
  if (!permission) return fallback;
  if (!permission.enabled) return "off";
  return permission.ask ? "ask" : "on";
}

/// Whether the bot may use a plugin at all; a plugin a policy's map leaves out is off.
export function pluginOn(policy: BotPermissions | undefined, pluginId: string): boolean {
  if (!policy?.connections) return true;
  return policy.connections[pluginId]?.enabled ?? false;
}

/// One tool of a plugin: its own switch, else the plugin's.
export function toolState(policy: BotPermissions | undefined, pluginId: string, tool: string): ToolState {
  if (!pluginOn(policy, pluginId)) return "off";
  return stateOf(policy?.connections?.[pluginId]?.tools?.[tool]);
}

/// The bot's own tools. A bot made before the switches has every one on.
export function readState(policy: BotPermissions | undefined): ToolState {
  return stateOf(policy?.tools?.read);
}

export function writeState(policy: BotPermissions | undefined): ToolState {
  return stateOf(policy?.tools?.write);
}

export function shellState(policy: BotPermissions | undefined): ToolState {
  return stateOf(policy?.tools?.shell);
}

/// Whether the bot's emails and Slack messages wait in the chat as drafts.
export function draftsOn(policy: BotPermissions | undefined): boolean {
  return policy?.drafts ?? true;
}

/// Whether the bot may use a browser. Browser has one switch, whichever browser the Runner uses.
export function browserOn(policy: BotPermissions | undefined): boolean {
  return pluginOn(policy, BROWSER_PLUGIN_ID);
}

export function isFullAccess(policy: BotPermissions | undefined): boolean {
  return !policy?.connections && readState(policy) !== "off" && writeState(policy) !== "off" && shellState(policy) !== "off";
}

/// The policy with one plugin on or off, the others as they are, and its own tools as saved.
export function withPlugin(policy: BotPermissions | undefined, plugins: AccessPlugin[], pluginId: string, on: boolean): BotPermissions {
  const listed = plugins.filter((plugin) => plugin.id !== BROWSER_PLUGIN_ID);
  const connections: NonNullable<BotPermissions["connections"]> = {};
  for (const plugin of listed) {
    const enabled = plugin.id === pluginId ? on : pluginOn(policy, plugin.id);
    if (!enabled) continue;
    const tools = policy?.connections?.[plugin.id]?.tools;
    connections[plugin.id] = { enabled: true, ...(tools && Object.keys(tools).length > 0 ? { tools } : {}) };
  }
  if (pluginId === BROWSER_PLUGIN_ID ? on : browserOn(policy)) {
    connections[BROWSER_PLUGIN_ID] = { enabled: true };
  }
  return { ...policy, connections };
}

/// The policy with Browser on or off.
export function withBrowser(policy: BotPermissions | undefined, plugins: AccessPlugin[], on: boolean): BotPermissions {
  return withPlugin(policy, plugins, BROWSER_PLUGIN_ID, on);
}

/// The policy with one tool of `plugin` at `state`. A tool back at its plugin's own state drops
/// its own entry, so a plugin installed later still follows the plugin's switch.
export function withTool(policy: BotPermissions | undefined, plugins: AccessPlugin[], plugin: AccessPlugin, name: string, state: ToolState): BotPermissions {
  const tools: Record<string, ToolPermission> = { ...(policy?.connections?.[plugin.id]?.tools ?? {}) };
  if (state === "on") delete tools[name];
  else tools[name] = permission(state);
  const connections: NonNullable<BotPermissions["connections"]> = { ...(policy?.connections ?? {}) };
  if (pluginOn(policy, plugin.id)) {
    connections[plugin.id] = { enabled: true, ...(Object.keys(tools).length > 0 ? { tools } : {}) };
  }
  return { ...policy, connections };
}

/// The policy with one of the bot's own tools, or its drafts, set.
export function withOwnTool(policy: BotPermissions | undefined, key: "read" | "write" | "shell", state: ToolState): BotPermissions {
  return { ...policy, tools: { ...(policy?.tools ?? {}), [key]: permission(state) } };
}

export function withDrafts(policy: BotPermissions | undefined, on: boolean): BotPermissions {
  return { ...policy, drafts: on };
}

// MARK: - Words

export function accessSummary(policy: BotPermissions | undefined): string {
  return isFullAccess(policy) ? t("Full access") : t("Limited");
}

export function stateTitle(state: ToolState): string {
  return { on: t("Allowed"), ask: t("Ask first"), off: t("Off") }[state];
}

/// What a tool does, in a word.
export function toolDoes(capability: string | null | undefined): string {
  return capability === "read" ? t("Reads") : capability === "draft" ? t("Drafts") : t("Changes");
}

/// The tools summary: "All tools", or how many of them the bot may use. Nothing while the plugin
/// is off, or before it has connected once.
export function toolsSummary(policy: BotPermissions | undefined, plugin: AccessPlugin): string | undefined {
  if (plugin.tools.length === 0 || !pluginOn(policy, plugin.id)) return undefined;
  const off = plugin.tools.filter((tool) => toolState(policy, plugin.id, tool.name) === "off").length;
  if (off === 0) return t("All tools");
  return t("{count} of {total} tools", { count: plugin.tools.length - off, total: plugin.tools.length });
}

// MARK: - The Runner's plugins, and saving

/// Each bot's plugins and their tools as its Runner last answered; `failed` when it could not.
export const useAccessStore = create<{ byBot: Record<string, AccessPlugin[] | "failed"> }>(() => ({ byBot: {} }));

/// Asks the bot's Runner for its plugins and their tools. One that cannot be asked keeps what was
/// shown, or marks the list as failed when there was none.
export async function loadAccessCatalog(botId: string): Promise<void> {
  try {
    const { connections } = await core.request<{ connections: AccessPlugin[] }>("bots.permissions", { id: botId });
    useAccessStore.setState((s) => ({ byBot: { ...s.byBot, [botId]: connections.map((plugin) => ({ ...plugin, tools: plugin.tools ?? [] })) } }));
  } catch (error) {
    console.warn("loading the bot's plugins", error instanceof Error ? error.message : error);
    useAccessStore.setState((s) => (Array.isArray(s.byBot[botId]) ? s : { byBot: { ...s.byBot, [botId]: "failed" } }));
  }
}

/// Saves the bot's Access, shown at once; the CLI also dismisses the access requests the bot
/// left. A save that fails puts the old Access back and says why.
export async function setBotPermissions(botId: string, policy: BotPermissions): Promise<void> {
  const before = useStore.getState().bots.find((bot) => bot.id === botId)?.permissions;
  const put = (permissions: BotPermissions | undefined) =>
    useStore.setState((s) => ({ bots: s.bots.map((bot) => (bot.id === botId ? { ...bot, permissions } : bot)) }));
  put(policy);
  try {
    await core.request("bots.update", { id: botId, permissions: policy });
  } catch (error) {
    put(before);
    throw error;
  }
}
