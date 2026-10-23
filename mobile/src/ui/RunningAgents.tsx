// The coding agents at work in every chat, over the composer: the phone's version of the desktop's
// floating windows. Each is a small card on the app's floating glass: its tile, its name, how it
// stands, what it does now (or that it asks, and what), Stop, and Close, which hides it until it
// stops; a hairline along its foot says how full its context is. A tap opens its sheet
// (`app/agent/[id].tsx`), its transcript with its question to answer, from whichever chat is open.
// Several scroll sideways, the latest started first; an agent that is done gives its card up.

import { useRouter } from "expo-router";
import { memo, useMemo, useState } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { agentNow, runningAgents } from "../core/agentKit";
import { engine } from "../core/engine";
import { agentName, type AgentRun, type Chat, type Message } from "../core/model";
import { useShallow } from "zustand/react/shallow";
import { useStore } from "../core/store";
import { t, useLanguage } from "../i18n";
import { AgentPill, AgentTile, ContextHairline, StopButton, alpha } from "./agentKit";
import { alert } from "./alert";
import { Float } from "./chrome";
import { firstLine } from "./format";
import { usePaneWidth } from "./layout";
import { Pressable } from "./Pressable";
import { Symbol } from "./Symbol";
import { accentColor, usePalette } from "./theme";

const CARD_MAX = 320;

export const RunningAgents = memo(function RunningAgents() {
  useLanguage();
  // Re-rendered only when the agents at work change: a message elsewhere leaves the cards alone.
  const running = useStore(useShallow((s) => runningAgents(s.chats).flatMap((each) => [each.chat, each.message])));
  const [hidden, setHidden] = useState<Record<string, true>>({});
  const pane = usePaneWidth();
  const found = useMemo(() => runningAgents(chatsOf(running)).filter((each) => !hidden[each.message.id]), [running, hidden]);
  if (found.length === 0) return null;
  const width = found.length === 1 ? Math.min(pane - 24, CARD_MAX * 1.4) : Math.min(pane - 56, CARD_MAX);
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.strip} style={{ flexGrow: 0 }} keyboardShouldPersistTaps="handled">
      {found.map(({ chat, message, agent }) => (
        <AgentFloat key={message.id} chat={chat} message={message} agent={agent} width={width} onClose={() => setHidden((was) => ({ ...was, [message.id]: true }))} />
      ))}
    </ScrollView>
  );
});

/// The chats of the agents at work, each once, from the pairs the selector keeps.
function chatsOf(running: (Chat | Message)[]): Chat[] {
  const chats = new Set<Chat>();
  for (let i = 0; i < running.length; i += 2) chats.add(running[i] as Chat);
  return [...chats];
}

function AgentFloat({ chat, message, agent, width, onClose }: { chat: Chat; message: Message; agent: AgentRun; width: number; onClose: () => void }) {
  const p = usePalette();
  const router = useRouter();
  const [stopping, setStopping] = useState(false);
  const asks = !!agent.question || !!agent.asking;
  const askCard = asks && agent.asking ? chat.messages.find((each) => each.id === agent.asking) : undefined;
  const askText = askCard?.body.kind === "permission" ? askCard.body.agent_ask?.message : undefined;
  const now = asks ? firstLine(askText ?? "") || t("Asks you in the chat") : agentNow(agent) || agent.task;
  const orange = accentColor("orange", p.dark);
  const stop = () => {
    setStopping(true);
    engine
      .stopAgent(message.chat_id, message.id)
      .catch((e) => alert(t("Could not stop {agent}", { agent: agentName(agent) }), e instanceof Error ? e.message : String(e)))
      .finally(() => setStopping(false));
  };
  return (
    <Float radius={18} style={{ width, overflow: "hidden" }}>
      <Pressable
        onPress={() => router.push({ pathname: "/agent/[id]", params: { id: message.id, chat: message.chat_id } })}
        accessibilityRole="button"
        accessibilityLabel={`${agentName(agent)}: ${now}`}
        accessibilityHint={t("Show the transcript")}
        style={({ pressed }) => [styles.card, { opacity: pressed ? 0.75 : 1 }]}
      >
        <AgentTile kind={agent.kind} size={30} />
        <View style={{ flex: 1, minWidth: 0, gap: 3 }}>
          <View style={styles.top}>
            <Text style={[styles.name, { color: p.label }]} numberOfLines={1}>
              {agentName(agent)}
            </Text>
            <AgentPill agent={agent} />
          </View>
          <View style={styles.top}>
            {asks ? <Symbol name="questionmark.circle" size={12} color={orange} /> : null}
            <Text style={[styles.now, { color: asks ? p.label : p.secondaryLabel }]} numberOfLines={1}>
              {now}
            </Text>
          </View>
        </View>
        <StopButton busy={stopping} onPress={stop} size={28} />
        <Pressable onPress={onClose} hitSlop={8} accessibilityRole="button" accessibilityLabel={t("Close")} style={({ pressed }) => [styles.close, { backgroundColor: alpha(p.dark ? "#ffffff" : "#000000", pressed ? 0.12 : 0.05) }]}>
          <Symbol name="xmark" size={10} color={p.secondaryLabel} weight="semibold" />
        </Pressable>
      </Pressable>
      {asks ? <View style={{ height: 2, backgroundColor: orange }} /> : <ContextHairline agent={agent} />}
    </Float>
  );
}

const styles = StyleSheet.create({
  strip: { gap: 8, paddingHorizontal: 12, paddingBottom: 8 },
  card: { flexDirection: "row", alignItems: "center", gap: 10, paddingVertical: 10, paddingLeft: 10, paddingRight: 10 },
  top: { flexDirection: "row", alignItems: "center", gap: 6, minWidth: 0 },
  name: { fontSize: 14, fontWeight: "600", flexShrink: 1 },
  now: { fontSize: 13, flexShrink: 1 },
  close: { width: 22, height: 22, borderRadius: 11, alignItems: "center", justifyContent: "center" },
});
