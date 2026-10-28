// The quick actions for one coding agent: open its full transcript, answer what it asks, or stop
// it where it runs. An action list, so it is the same bottom sheet on both platforms.

import { agentIsRunning, agentName, type AgentRun } from "../core/model";
import { t } from "../i18n";
import { ActionSheet, type SheetAction } from "./Sheet";

export function AgentActionsSheet({
  visible,
  agent,
  onClose,
  onOpenTranscript,
  onAnswer,
  onStop,
  showTranscript = true,
}: {
  visible: boolean;
  agent: AgentRun | undefined;
  onClose: () => void;
  onOpenTranscript: () => void;
  onAnswer?: () => void;
  onStop: () => void;
  showTranscript?: boolean;
}) {
  const actions: SheetAction[] = showTranscript ? [{ label: t("Open transcript"), icon: "doc.text.magnifyingglass", onPress: onOpenTranscript }] : [];
  if (agent?.asking && onAnswer) actions.push({ label: t("Answer question"), icon: "questionmark.circle", onPress: onAnswer });
  if (agent && agentIsRunning(agent)) actions.push({ label: t("Stop {agent}", { agent: agentName(agent) }), icon: "stop.fill", destructive: true, onPress: onStop });
  return <ActionSheet visible={visible} title={agent ? agentName(agent) : t("Coding agent")} actions={actions} onClose={onClose} />;
}
