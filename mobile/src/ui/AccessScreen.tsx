// A bot's Access, slid in from its Access row in Details, or opened from a "needs more access"
// card: the plugins on its Runner, each a switch with a screen of its own for its tools, whether
// its messages wait as drafts, then the bot's own files and shell commands under the Runner's
// name. Each change saves at once. The desktop apps' Access sheet holds the same.

import { Stack, useLocalSearchParams, useRouter } from "expo-router";
import { useEffect } from "react";
import { ScrollView, StyleSheet } from "react-native";
import {
  browserOn,
  draftsOn,
  loadAccessCatalog,
  pluginOn,
  readState,
  shellState,
  stateTitle,
  TOOL_STATES,
  toolDoes,
  toolState,
  toolsSummary,
  setBotPermissions,
  useAccessStore,
  withBrowser,
  withDrafts,
  withOwnTool,
  withPlugin,
  withTool,
  writeState,
  type AccessPlugin,
  type ToolState,
} from "../core/access";
import { BROWSER_PLUGIN_ID, type BotPermissions } from "../core/model";
import { useStore } from "../core/store";
import { t, useLanguage } from "../i18n";
import { alert } from "./alert";
import { CheckRow, Row, Section, ToggleRow } from "./forms";
import { CloseToolbar } from "./navigation";

/// The bot, its Runner, and the plugins to show: the Runner's answer once it has one, its own
/// list of plugins until then.
function useAccess(botId: string) {
  const bot = useStore((s) => s.bots.find((each) => each.id === botId));
  const runner = useStore((s) => s.devices.find((device) => device.id === bot?.runner_id));
  const catalog = useAccessStore((s) => s.byBot[botId]);
  // Browser is a switch of its own, not one of the plugins.
  const plugins: AccessPlugin[] = (Array.isArray(catalog) ? catalog : (runner?.plugins ?? []).map((plugin) => ({ id: plugin.id, name: plugin.name, tools: [] }))).filter((plugin) => plugin.id !== BROWSER_PLUGIN_ID);
  const save = (policy: BotPermissions) =>
    setBotPermissions(botId, policy).catch((error) => alert(t("Could not update the bot"), error instanceof Error ? error.message : String(error)));
  return { bot, runner, plugins, failed: catalog === "failed", save };
}

export default function AccessScreen() {
  useLanguage();
  const { id, close } = useLocalSearchParams<{ id: string; close?: string }>();
  const router = useRouter();
  const { bot, runner, plugins, failed, save } = useAccess(id);
  // What each plugin offers is the Runner's to say; asked whenever the screen opens.
  useEffect(() => {
    void loadAccessCatalog(id);
  }, [id]);
  if (!bot) return null;
  const policy = bot.permissions;
  const runnerName = runner?.name ?? t("its Runner");
  const icons = new Map((runner?.plugins ?? []).map((plugin) => [plugin.id, plugin.icon]));

  return (
    <>
      <Stack.Screen options={{ title: t("Access") }} />
      {/* Opened from a card, it is the sheet's first screen and closes it. */}
      {close ? <CloseToolbar label={t("Done")} onClose={() => router.back()} /> : null}
      <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
        {plugins.length > 0 && (
          <Section title={t("Plugins")} footer={failed ? t("Couldn't get the tools from {runner}.", { runner: runnerName }) : undefined}>
            {plugins.map((plugin) => (
              <Row
                key={plugin.id}
                title={plugin.name}
                subtitle={toolsSummary(policy, plugin)}
                icon={icons.get(plugin.id) || "puzzlepiece.extension"}
                accessory={
                  <ToggleRow
                    title={plugin.name}
                    value={pluginOn(policy, plugin.id)}
                    onValueChange={(on) => void save(withPlugin(policy, plugins, plugin.id, on))}
                  />
                }
                chevron={false}
                onPress={() => router.push({ pathname: "/chat-info/access-plugin", params: { bot: bot.id, plugin: plugin.id } })}
              />
            ))}
          </Section>
        )}
        <Section title={t("Messages")} footer={t("Emails and Slack messages wait in the chat for you to send. Off, {name} sends them itself where the service can.", { name: bot.name })}>
          <ToggleRow title={t("Draft first")} value={draftsOn(policy)} onValueChange={(on) => void save(withDrafts(policy, on))} />
        </Section>
        {/* The bot's own tools are the Runner's, so its section goes by the Runner's name. */}
        <Section title={runner?.name ?? t("Runner")} footer={t("Shell commands run as you on {runner} and can reach anything you can there.", { runner: runnerName })}>
          <Row title={t("Read files")} menu={stateMenu("read", readState(policy), (state) => void save(withOwnTool(policy, "read", state)))} />
          <Row title={t("Change files")} menu={stateMenu("write", writeState(policy), (state) => void save(withOwnTool(policy, "write", state)))} />
          <Row title={t("Shell commands")} menu={stateMenu("shell", shellState(policy), (state) => void save(withOwnTool(policy, "shell", state)))} />
          <ToggleRow title={t("Browser")} value={browserOn(policy)} onValueChange={(on) => void save(withBrowser(policy, plugins, on))} />
        </Section>
      </ScrollView>
    </>
  );
}

/// A row's menu of the three states, with the one in force ticked.
function stateMenu(key: string, current: ToolState, pick: (state: ToolState) => void) {
  return {
    value: stateTitle(current),
    title: key,
    choices: TOOL_STATES.map((state) => ({ title: stateTitle(state), selected: current === state, onPress: () => pick(state) })),
  };
}

/// One plugin's Access, slid in from its row: its switch, then its tools, each on, asking first,
/// or off.
export function AccessPluginScreen() {
  useLanguage();
  const { bot: botId, plugin: pluginId } = useLocalSearchParams<{ bot: string; plugin: string }>();
  const { bot, plugins, save } = useAccess(botId);
  const plugin = plugins.find((each) => each.id === pluginId);
  if (!bot || !plugin) return null;
  const policy = bot.permissions;
  const on = pluginOn(policy, plugin.id);

  return (
    <>
      <Stack.Screen options={{ title: plugin.name }} />
      <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
        <Section footer={t("A tool set to ask waits for you every time, whatever the bot's review mode says.")}>
          <ToggleRow title={t("Use this plugin")} value={on} onValueChange={(next) => void save(withPlugin(policy, plugins, plugin.id, next))} />
        </Section>
        {plugin.tools.length > 0 && on && (
          <Section title={t("Tools")}>
            {plugin.tools.map((tool) => (
              <Row
                key={tool.name}
                title={tool.title || tool.name}
                subtitle={toolDoes(tool.capability)}
                menu={stateMenu(tool.name, toolState(policy, plugin.id, tool.name), (state) => void save(withTool(policy, plugins, plugin, tool.name, state)))}
              />
            ))}
          </Section>
        )}
      </ScrollView>
    </>
  );
}

const styles = StyleSheet.create({
  content: { paddingBottom: 40 },
});
