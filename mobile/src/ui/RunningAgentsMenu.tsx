// The coding agents at work, at the chat's top right: their tiles stacked, newest first, and a
// count past three. A tap opens the custom dropdown that lists every agent; a row opens that
// agent's transcript. The cards that used to float over the composer are gone.

import { useRouter } from "expo-router";
import { useHeaderHeight } from "expo-router/react-navigation";
import { useMemo } from "react";
import { Modal, Platform, Pressable as RNPressable, ScrollView, StyleSheet, Text, useWindowDimensions, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useShallow } from "zustand/react/shallow";
import { runningAgents } from "../core/agentKit";
import { agentName, type AgentRun, type Chat, type Message } from "../core/model";
import { useStore } from "../core/store";
import { t, useLanguage } from "../i18n";
import { AgentPill, AgentTile } from "./agentKit";
import { Pressable } from "./Pressable";
import { Symbol } from "./Symbol";
import { usePalette, withAlpha } from "./theme";

type Running = { chat: Chat; message: Message; agent: AgentRun };

/// The agents at work in every chat, the latest started first.
function useRunning(): Running[] {
  // A chat gets a new object only when it changed, so the pairs compare by their own objects.
  const pairs = useStore(useShallow((s) => runningAgents(s.chats).flatMap((found) => [found.chat, found.message])));
  return useMemo(() => {
    const chats = new Map<string, Chat>();
    for (let i = 0; i < pairs.length; i += 2) {
      const chat = pairs[i] as Chat;
      chats.set(chat.id, chat);
    }
    return runningAgents([...chats.values()]);
  }, [pairs]);
}

/// The capsule in the chat's top right: the agents' tiles stacked, and how many there are.
export function RunningAgentsCapsule({ onPress }: { onPress: () => void }) {
  const p = usePalette();
  useLanguage();
  const running = useRunning();
  if (running.length === 0) return null;
  const shown = running.slice(0, 3);
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={t("Running agents ({count})", { count: running.length })}
      hitSlop={6}
      style={({ pressed }) => [styles.capsule, { backgroundColor: p.chrome, borderColor: p.chromeBorder, opacity: pressed ? 0.7 : 1 }]}
    >
      <View style={styles.stack}>
        {shown.map(({ agent }, i) => (
          <View key={agent.id} style={[styles.tile, { marginLeft: i ? -7 : 0, backgroundColor: p.background, borderColor: p.chromeBorder }]}>
            <AgentTile kind={agent.kind} size={22} round />
          </View>
        ))}
      </View>
      {running.length > 1 ? (
        <View style={[styles.count, { backgroundColor: p.ink }]}>
          <Text style={[styles.countText, { color: p.inkText }]}>{running.length}</Text>
        </View>
      ) : null}
    </Pressable>
  );
}

/// The dropdown: every agent at work, a row each, under the chat's top-right bar.
export function RunningAgentsMenu({ visible, onClose }: { visible: boolean; onClose: () => void }) {
  const p = usePalette();
  useLanguage();
  const router = useRouter();
  const running = useRunning();
  const insets = useSafeAreaInsets();
  const header = useHeaderHeight();
  const { width, height } = useWindowDimensions();
  const top = Platform.OS === "ios" ? header + 4 : insets.top + 56;
  const open = (found: Running) => {
    onClose();
    router.push({ pathname: "/agent/[id]", params: { id: found.message.id, chat: found.chat.id } });
  };
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <RNPressable style={[StyleSheet.absoluteFill, { backgroundColor: withAlpha(p.label as string, 0.14) }]} onPress={onClose} />
      <View style={[styles.panel, { top, right: 12, width: Math.min(width - 24, 360), maxHeight: Math.min(height * 0.6, 460), backgroundColor: p.cell, borderColor: p.cardBorder, shadowColor: p.shadow }]}>
        <Text style={[styles.heading, { color: p.tertiaryLabel }]}>{t("Running agents")}</Text>
        <ScrollView keyboardShouldPersistTaps="handled" showsVerticalScrollIndicator={false}>
          {running.map((found) => (
            <Pressable
              key={found.message.id}
              onPress={() => open(found)}
              accessibilityRole="button"
              accessibilityLabel={agentName(found.agent)}
              style={({ pressed }) => [styles.row, pressed && { backgroundColor: p.fill }]}
            >
              <AgentTile kind={found.agent.kind} size={30} round />
              <View style={styles.rowText}>
                <View style={styles.rowTop}>
                  <Text numberOfLines={1} style={[styles.name, { color: p.label }]}>
                    {agentName(found.agent)}
                  </Text>
                  <AgentPill agent={found.agent} />
                </View>
                <Text numberOfLines={1} style={[styles.task, { color: p.secondaryLabel }]}>
                  {found.agent.title || found.agent.task}
                </Text>
              </View>
              <Symbol name="chevron.right" size={11} color={p.tertiaryLabel} weight="semibold" />
            </Pressable>
          ))}
        </ScrollView>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  capsule: { height: 34, flexDirection: "row", alignItems: "center", gap: 6, paddingLeft: 3, paddingRight: 8, borderRadius: 17, borderWidth: 1 },
  stack: { flexDirection: "row", alignItems: "center" },
  tile: { width: 26, height: 26, borderRadius: 13, borderWidth: 1, alignItems: "center", justifyContent: "center" },
  count: { minWidth: 16, height: 16, borderRadius: 8, paddingHorizontal: 4, alignItems: "center", justifyContent: "center" },
  countText: { fontSize: 10, fontWeight: "700", fontVariant: ["tabular-nums"] },
  panel: { position: "absolute", borderRadius: 16, borderWidth: 1, paddingVertical: 6, overflow: "hidden", shadowOpacity: 0.3, shadowRadius: 24, shadowOffset: { width: 0, height: 10 }, elevation: 12 },
  heading: { fontSize: 11, fontWeight: "600", letterSpacing: 0.2, paddingHorizontal: 16, paddingTop: 8, paddingBottom: 6 },
  row: { flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 12, paddingVertical: 9 },
  rowText: { flex: 1, minWidth: 0, gap: 2 },
  rowTop: { flexDirection: "row", alignItems: "center", gap: 8 },
  name: { fontSize: 15, fontWeight: "600", flexShrink: 1 },
  task: { fontSize: 12.5 },
});
