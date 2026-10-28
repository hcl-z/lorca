// The agent's question as a bottom sheet: the card's form, with the sheet's keyboard and drag.
// The chat's card keeps its summary and a button that opens this; the full transcript's footer
// uses the same form in place.

import type { AgentAsk } from "../core/agentAsk";
import type { Message } from "../core/model";
import { t } from "../i18n";
import { AgentAskRow, type Row } from "./transcript";
import { Sheet } from "./Sheet";

export function AgentAskSheet({
  visible,
  row,
  ask,
  onClose,
}: {
  visible: boolean;
  row: Extract<Row, { type: "permission" }> | undefined;
  ask: AgentAsk | undefined;
  onClose: () => void;
}) {
  return (
    <Sheet visible={visible && !!row && !!ask} onClose={onClose} title={ask ? t("{agent} asks", { agent: ask.name }) : t("Coding agent")} scroll>
      {row && ask ? <AgentAskRow row={row} ask={ask} isGroup={false} onAnswered={onClose} /> : null}
    </Sheet>
  );
}
