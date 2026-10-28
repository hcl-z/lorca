// A bot's memory, pushed from Details: its MEMORY.md against the budget a turn loads, the topic
// files beside it, capture history with per-batch Undo, the profile its Runner shares
// with every bot there (About you), and the prompt its next turn sends. All of it lives on the
// bot's Runner and comes through the core (`src/core/memory.ts`).

import { Stack, useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { useCallback, useState } from "react";
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from "react-native";
import { fileSize } from "../../../src/core/model";
import { botMemory, captureHistory, gauge, undoCapture, undoTidy, type BotMemory, type CaptureRun, type TidyRun } from "../../../src/core/memory";
import { useBotMap } from "../../../src/core/store";
import { t, useLanguage } from "../../../src/i18n";
import { alert } from "../../../src/ui/alert";
import { stamp } from "../../../src/ui/format";
import { Row, Section } from "../../../src/ui/forms";
import { accentColor, Font, usePalette } from "../../../src/ui/theme";

/// What a housekeeping pass did, in a line: how many lines it moved where.
function tidySummary(run: TidyRun): string {
  const parts: string[] = [];
  if (run.archived.length) parts.push(t("{count} unused archived", { count: run.archived.length }));
  if (run.history.length) parts.push(t("{count} replaced moved to history", { count: run.history.length }));
  return parts.join(" · ");
}

export default function BotMemoryScreen() {
  useLanguage();
  const { id, chat } = useLocalSearchParams<{ id: string; chat: string }>();
  const router = useRouter();
  const p = usePalette();
  const bot = useBotMap().get(id);
  const [memory, setMemory] = useState<BotMemory>();
  const [captures, setCaptures] = useState<CaptureRun[]>([]);
  const [error, setError] = useState<string>();
  const [undoing, setUndoing] = useState(false);

  const load = useCallback(() => {
    setError(undefined);
    Promise.all([botMemory(id), captureHistory(id)])
      .then(([overview, history]) => {
        setMemory(overview);
        setCaptures(history.runs);
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, [id]);
  // Again on each return: the editors below write the files.
  useFocusEffect(load);

  if (!bot) return <Stack.Screen options={{ title: t("Memory") }} />;
  const name = bot.name;

  function undoTidied(run: TidyRun) {
    alert(t("Put back what was tidied?"), [...run.archived, ...run.history].map((line) => `• ${line}`).join("\n"), [
      { text: t("Cancel"), style: "cancel" },
      {
        text: t("Put Back"),
        onPress: () => {
          setUndoing(true);
          undoTidy(id, run.id)
            .then(load)
            .catch((e) => alert(t("Couldn't undo it"), e instanceof Error ? e.message : String(e)))
            .finally(() => setUndoing(false));
        },
      },
    ]);
  }

  function undo(run: NonNullable<BotMemory["capture"]>) {
    alert(t("Forget what {name} remembered?", { name }), run.facts.map((fact) => `• ${fact.text} · ${fact.target === "profile" ? t("Shared profile") : t("Bot memory")}`).join("\n"), [
      { text: t("Cancel"), style: "cancel" },
      {
        text: t("Forget"),
        style: "destructive",
        onPress: () => {
          setUndoing(true);
          undoCapture(id, run.id)
            .then(load)
            .catch((e) => alert(t("Couldn't undo it"), e instanceof Error ? e.message : String(e)))
            .finally(() => setUndoing(false));
        },
      },
    ]);
  }

  const index = memory?.index;
  const measure = index ? gauge(index.text, index) : undefined;
  const tone = measure?.tone === "red" ? accentColor("red", p.dark) : measure?.tone === "orange" ? accentColor("orange", p.dark) : undefined;
  return (
    <>
      <Stack.Screen options={{ title: t("Memory") }} />
      <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
        {!memory ? (
          <View style={styles.center}>
            {error ? (
              <>
                <Text style={[styles.note, { color: p.secondaryLabel }]}>{error}</Text>
                <Section style={{ alignSelf: "stretch" }}>
                  <Row title={t("Retry")} action onPress={load} />
                </Section>
              </>
            ) : (
              <ActivityIndicator />
            )}
          </View>
        ) : (
          <>
            <Section
              title={t("Memory")}
              footer={t("MEMORY.md opens at the start of every turn: the first {lines} lines or {size}, whichever cuts first. Longer notes belong in memory/<topic>.md files the bot reads on demand.", {
                lines: memory.index.max_lines,
                size: fileSize(memory.index.max_bytes),
              })}
            >
              <Row
                title="MEMORY.md"
                subtitle={memory.index.text.trim() ? memory.index.text.trim().split("\n").slice(0, 3).join("\n") : t("Nothing yet")}
                subtitleLines={3}
                accessory={tone ? <Text style={{ color: tone, fontSize: Font.small }}>{t("{count} of {max} lines", { count: measure!.lines, max: memory.index.max_lines })}</Text> : undefined}
                detail={tone ? undefined : t("{count} of {max} lines", { count: measure!.lines, max: memory.index.max_lines })}
                chevron
                onPress={() => router.push({ pathname: "/chat-info/memory/edit", params: { id } })}
              />
              {memory.topics.map((topic) => (
                <Row key={topic} title={topic} icon="doc.text" />
              ))}
            </Section>

            {memory.tidy ? (
              <Section
                title={t("Tidied")}
                footer={`${stamp(new Date(memory.tidy.at * 1000))} · ${t("Moved to memory/history.md and memory/archive.md, where recall still finds them.")}`}
              >
                <Row title={tidySummary(memory.tidy)} titleLines={2} />
                <Row title={t("Undo…")} destructive disabled={undoing} onPress={() => undoTidied(memory.tidy!)} />
              </Section>
            ) : null}

            {captures.length || memory.captures?.length || memory.capture ? (
              <Section title={t("Automatic memory history")}>
                {(captures.length ? captures : memory.captures?.length ? memory.captures : memory.capture ? [memory.capture] : []).map((run) => (
                  <View key={run.id} style={styles.captureRun}>
                    <Text style={[styles.note, { color: p.secondaryLabel }]}>{stamp(new Date(run.at * 1000))}{run.undone ? ` · ${t("Undone")}` : ""}</Text>
                    {run.facts.map((fact, at) => (
                      <Row key={`${run.id}-${at}`} title={fact.text} detail={fact.target === "profile" ? t("Shared profile") : t("Bot memory")} titleLines={3} />
                    ))}
                    {!run.undone ? <Row title={t("Undo…")} destructive disabled={undoing} onPress={() => undo(run)} /> : null}
                  </View>
                ))}
              </Section>
            ) : null}

            <Section footer={t("What {runner} knows about you, shared by every bot on it.", { runner: memory.runner })}>
              <Row
                title={t("About You")}
                detail={memory.profile.text.trim() ? undefined : t("Nothing yet")}
                subtitle={memory.profile.text.trim() ? memory.profile.text.trim().split("\n")[0] : undefined}
                chevron
                onPress={() => router.push({ pathname: "/chat-info/profile/[id]", params: { id: bot.runner_id, bot: id } })}
              />
            </Section>

            {chat ? (
              <Section footer={t("What {name}'s next turn sends in this chat.", { name })}>
                <Row title={t("Prompt")} chevron onPress={() => router.push({ pathname: "/chat-info/prompt/[id]", params: { id, chat } })} />
              </Section>
            ) : null}
          </>
        )}
      </ScrollView>
    </>
  );
}

const styles = StyleSheet.create({
  content: { paddingBottom: 40 },
  center: { alignItems: "center", justifyContent: "center", gap: 12, padding: 24, paddingTop: 80 },
  note: { fontSize: Font.body, textAlign: "center" },
  captureRun: { paddingBottom: 8 },
});
