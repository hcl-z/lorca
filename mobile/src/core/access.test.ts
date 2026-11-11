import { expect, mock, test } from "bun:test";

const sent: { method: string; params: unknown }[] = [];
let failNext = false;
mock.module("./prefs", () => ({ loadPrefs: () => ({}), savePrefs: () => {}, coreHome: () => "/unused", pathOf: (p: string) => p, wipePrefs: () => {} }));
mock.module("expo-localization", () => ({ getLocales: () => [{ languageCode: "en", languageTag: "en-US" }] }));
mock.module("../../modules/lorca-core", () => ({
  request: async (method: string, params: unknown) => {
    sent.push({ method, params });
    if (failNext) {
      failNext = false;
      throw new Error("The Runner did not answer.");
    }
    if (method === "bots.permissions") return { connections: [{ id: "github", name: "GitHub", tools: [{ name: "search_issues", capability: "read" }] }] };
    return {};
  },
}));
const access = await import("./access");
const { useStore } = await import("./store");

const github = {
  id: "github",
  name: "GitHub",
  tools: [
    { name: "search_issues", capability: "read" },
    { name: "create_pull_request_review", capability: "draft" },
    { name: "create_issue", capability: "write" },
  ],
};
const linear = { id: "linear", name: "Linear", tools: [] };
const plugins = [github, linear];

test("no policy is full access, and a plugin's tools follow its own switch", () => {
  expect(access.isFullAccess(undefined)).toBe(true);
  expect(access.accessSummary(undefined)).toBe("Full access");
  expect(access.pluginOn(undefined, "github")).toBe(true);
  expect(access.toolState(undefined, "github", "search_issues")).toBe("on");
  expect(access.accessSummary({ tools: { shell: { enabled: false } } })).toBe("Limited");
});

test("a plugin is on or off, and its switch lists it or leaves it out", () => {
  const off = access.withPlugin(undefined, plugins, "github", false);
  expect(access.pluginOn(off, "github")).toBe(false);
  expect(access.pluginOn(off, "linear"), "the other plugins stay on").toBe(true);
  expect(access.pluginOn(off, "playwright"), "Browser keeps its own place").toBe(true);
  const back = access.withPlugin(off, plugins, "github", true);
  expect(access.pluginOn(back, "github")).toBe(true);
});

test("a tool is on, asks first, or off, and a tool back at its plugin's state drops its entry", () => {
  const asking = access.withTool(undefined, plugins, github, "create_issue", "ask");
  expect(asking.connections?.github?.tools).toEqual({ create_issue: { enabled: true, ask: true } });
  expect(access.toolState(asking, "github", "create_issue")).toBe("ask");
  expect(access.toolState(asking, "github", "search_issues")).toBe("on");
  const off = access.withTool(asking, plugins, github, "create_issue", "off");
  expect(access.toolState(off, "github", "create_issue")).toBe("off");
  expect(access.toolsSummary(off, github)).toBe("2 of 3 tools");
  const back = access.withTool(off, plugins, github, "create_issue", "on");
  expect(access.toolState(back, "github", "create_issue")).toBe("on");
  expect(back.connections?.github?.tools).toBeUndefined();
  expect(access.toolsSummary(back, github)).toBe("All tools");
  expect(access.toolsSummary(back, linear)).toBeUndefined();
});

test("the bot's own tools are each on, asking, or off", () => {
  const policy = access.withOwnTool(undefined, "shell", "off");
  expect(access.shellState(policy)).toBe("off");
  expect(access.readState(policy)).toBe("on");
  const asking = access.withOwnTool(policy, "write", "ask");
  expect(access.writeState(asking)).toBe("ask");
  expect(access.isFullAccess(asking)).toBe(false);
});

test("a save shows at once, goes to the core, and a failed one puts the old Access back", async () => {
  useStore.setState({ bots: [{ id: "b", name: "Writer", description: "", symbol_name: "pencil", accent: "pink", runner_id: "r", provider: "deepseek", created_at: 0 }] });
  await access.setBotPermissions("b", { tools: { shell: { enabled: false } } });
  expect(useStore.getState().bots[0].permissions).toEqual({ tools: { shell: { enabled: false } } });
  expect(sent.at(-1)).toEqual({ method: "bots.update", params: { id: "b", permissions: { tools: { shell: { enabled: false } } } } });
  failNext = true;
  await expect(access.setBotPermissions("b", { tools: { read: { enabled: false } } })).rejects.toThrow("did not answer");
  expect(useStore.getState().bots[0].permissions).toEqual({ tools: { shell: { enabled: false } } });
  await access.loadAccessCatalog("b");
  expect(access.useAccessStore.getState().byBot.b).toEqual([{ id: "github", name: "GitHub", tools: [{ name: "search_issues", capability: "read" }] }]);
  failNext = true;
  await access.loadAccessCatalog("b");
  expect(Array.isArray(access.useAccessStore.getState().byBot.b)).toBe(true);
});

test("Browser is one switch that the listed plugins keep their hands off", () => {
  expect(access.browserOn(undefined)).toBe(true);
  const off = access.withBrowser(undefined, plugins, false);
  expect(access.browserOn(off)).toBe(false);
  expect(off.connections?.playwright).toBeUndefined();
  const narrowed = access.withPlugin(off, plugins, "github", false);
  expect(access.browserOn(narrowed), "turning a plugin off leaves Browser off").toBe(false);
  const on = access.withBrowser(access.withPlugin(narrowed, plugins, "github", true), plugins, true);
  expect(access.browserOn(on)).toBe(true);
});
