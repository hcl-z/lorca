// The chat's participants in a bottom sheet: its bots, then the coding agents its bots run while
// they are open. An agent opens its full transcript; Stop ends it where it runs.

import { StyleSheet, Text, View } from "react-native";
import { agentIsRunning, agentName, agentPlace, type AgentRun, type Bot } from "../core/model";
import { agentBusy } from "../core/agentKit";
import { useStore } from "../core/store";
import { t } from "../i18n";
import { AgentPill, AgentTile, StopButton } from "./agentKit";
import { BotAvatar } from "./Avatar";
import { Sheet, SheetActionRow } from "./Sheet";
import { usePalette } from "./theme";

export interface ParticipantAgent {
  messageId: string;
  agent: AgentRun;
}

export function ParticipantsSheet({
  visible,
  title,
  bots,
  agents,
  onClose,
  onOpenAgent,
  onStopAgent,
}: {
  visible: boolean;
  title: string;
  bots: Bot[];
  agents: ParticipantAgent[];
  onClose: () => void;
  onOpenAgent: (messageId: string) => void;
  onStopAgent: (messageId: string) => Promise<void>;
}) {
  const p = usePalette();
  const devices = useStore((s) => s.devices);
  const runnerName = (id: string) => devices.find((device) => device.id === id)?.name;
  return (
    <Sheet visible={visible} onClose={onClose} title={t("{title}, participants", { title })} scroll>
      <Text style={[styles.section, { color: p.secondaryLabel }]}>{t("Bots")}</Text>
      {bots.map((bot) => (
        <SheetActionRow key={bot.id} tile={<BotAvatar bot={bot} size={28} />} title={bot.name} subtitle={runnerName(bot.runner_id)} />
      ))}
      <Text style={[styles.section, { color: p.secondaryLabel }]}>{t("Coding agents")}</Text>
      {agents.length === 0 ? (
        <Text style={[styles.empty, { color: p.secondaryLabel }]}>{t("No coding agents here right now.")}</Text>
      ) : (
        agents.map(({ messageId, agent }) => (
          <SheetActionRow
            key={messageId}
            tile={<AgentTile kind={agent.kind} size={28} round />}
            title={agentName(agent)}
            subtitle={agent.task || agentPlace(agent)}
            onPress={() => onOpenAgent(messageId)}
            trailing={
              <View style={styles.trailing}>
                <AgentPill agent={agent} />
                {agentIsRunning(agent) ? <StopButton busy={agentBusy(agent)} size={28} label={t("Stop {agent}", { agent: agentName(agent) })} onPress={() => void onStopAgent(messageId)} /> : null}
              </View>
            }
          />
        ))
      )}
    </Sheet>
  );
}

const styles = StyleSheet.create({
  section: { fontSize: 12, fontWeight: "600", textTransform: "uppercase", letterSpacing: 0.4, paddingHorizontal: 16, paddingTop: 12, paddingBottom: 6 },
  empty: { fontSize: 14, paddingHorizontal: 16, paddingVertical: 12 },
  trailing: { flexDirection: "row", alignItems: "center", gap: 8 },
});
