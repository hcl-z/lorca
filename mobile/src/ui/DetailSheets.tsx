// The chat's short details as bottom sheets: a message behind a marker, a whole command, the
// commands a chat is running, and a bot's question. Each keeps its route as the deep-link
// fallback; the chat presents the sheet directly.

import * as Clipboard from "expo-clipboard";
import { useRouter } from "expo-router";
import { Platform, StyleSheet, Text, View } from "react-native";
import { hasEnded, type Message } from "../core/model";
import { useBotMap, useChat, useRunningTasks } from "../core/store";
import { t } from "../i18n";
import { firstLine } from "./format";
import { Row, Section } from "./forms";
import { Markdown } from "./Markdown";
import { QuestionRow, type Row as TranscriptRow } from "./transcript";
import { Sheet, SheetActionRow } from "./Sheet";
import { taskState, useNow } from "./tasks";
import { usePalette } from "./theme";
import type { AskQuestion } from "../core/model";

export function MessageSheet({ visible, message, onClose }: { visible: boolean; message: Message | undefined; onClose: () => void }) {
  const p = usePalette();
  const body = message?.body;
  const text = body?.kind === "tool" ? body.detail : body?.kind === "handoff" ? body.reason : "";
  return (
    <Sheet visible={visible} onClose={onClose} title={t("Message")} scroll>
      <View style={styles.content}>
        {text ? <Markdown text={text} color={p.label} maxWidth={360} /> : <Text style={{ color: p.secondaryLabel }}>{t("This message is no longer available.")}</Text>}
      </View>
    </Sheet>
  );
}

export function CommandSheet({ visible, message, onClose }: { visible: boolean; message: Message | undefined; onClose: () => void }) {
  const p = usePalette();
  const body = message?.body;
  const command =
    body?.kind === "permission" ? (body.command ?? body.summary.replace(/^\$ /, "")) : body?.kind === "tool" ? (body.run?.command ?? body.agent?.question?.command ?? "") : "";
  const copy = () => {
    void Clipboard.setStringAsync(command);
  };
  return (
    <Sheet visible={visible} onClose={onClose} title={t("Command")} scroll>
      {command ? (
        <>
          <SheetActionRow icon="doc.on.doc" title={t("Copy")} onPress={copy} />
          <View style={[styles.code, { backgroundColor: p.code }]}>
            <Text selectable style={[styles.mono, { color: p.label }]}>
              {command}
            </Text>
          </View>
        </>
      ) : (
        <View style={styles.content}>
          <Text style={{ color: p.secondaryLabel }}>{t("This message is no longer available.")}</Text>
        </View>
      )}
    </Sheet>
  );
}

export function TasksSheet({ visible, chatId, onClose, onOpenCommand }: { visible: boolean; chatId: string; onClose: () => void; onOpenCommand: (message: Message) => void }) {
  const p = usePalette();
  const chat = useChat(chatId);
  const bots = useBotMap();
  const running = useRunningTasks(chatId);
  const now = useNow();
  const tasks = (chat?.messages ?? []).filter((message) => {
    const run = message.body.kind === "tool" ? message.body.run : undefined;
    return running.includes(message) || (!!run && hasEnded(run));
  });
  return (
    <Sheet visible={visible} onClose={onClose} title={t("Running tasks")} scroll>
      {tasks.length > 0 ? (
        <Section>
          {tasks.map((message) => {
            const body = message.body.kind === "tool" ? message.body : undefined;
            if (!body?.run) return null;
            const bot = message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined;
            const state = taskState(body.run, message.created_at, now);
            return (
              <Row
                key={message.id}
                icon="terminal"
                title={body.description ?? firstLine(body.run.command)}
                subtitle={chat?.kind === "group" ? `${bot?.name ?? t("The bot")} · ${state}` : state}
                chevron
                onPress={() => {
                  onClose();
                  setTimeout(() => onOpenCommand(message), 260);
                }}
              />
            );
          })}
        </Section>
      ) : (
        <Text style={[styles.empty, { color: p.secondaryLabel }]}>{t("No commands are running.")}</Text>
      )}
    </Sheet>
  );
}

export function QuestionSheet({
  visible,
  row,
  ask,
  onClose,
  onDecide,
}: {
  visible: boolean;
  row: Extract<TranscriptRow, { type: "permission" }> | undefined;
  ask: AskQuestion | undefined;
  onClose: () => void;
  onDecide: (message: Message, decision: "allow" | "always" | "deny") => void;
}) {
  return (
    <Sheet visible={visible && !!row && !!ask} onClose={onClose} title={t("Question")} scroll>
      {row && ask ? <QuestionRow row={row} ask={ask} isGroup={false} onDecide={onDecide} /> : null}
    </Sheet>
  );
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: 16, paddingBottom: 8 },
  code: { marginHorizontal: 16, marginTop: 4, borderRadius: 10, padding: 12 },
  mono: { fontFamily: Platform.OS === "ios" ? "Menlo" : "monospace", fontSize: 13, lineHeight: 18 },
  empty: { textAlign: "center", marginTop: 24, fontSize: 15 },
});
