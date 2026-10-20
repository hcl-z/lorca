// A coding agent's transcript, opened from its card, as a sheet: what it was sent, beside a bar;
// what it said, as a message is; each tool it used as a line with its state, which opens to its
// command's output, its edits as a diff, or what it gave back; what it thought, and each time it
// compacted its context, a line that opens to it; its plan; its notices; and Lorca's notes. A tool
// that runs, one that failed, and the last one are open, and a tap opens or closes any. It asks the
// Runner again every five seconds for what changed, and keeps to the end unless the user scrolled
// up to read.

import { Stack, useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { useCallback, useRef, useState } from "react";
import { ActivityIndicator, Platform, Pressable, ScrollView, StyleSheet, Text, useWindowDimensions, View } from "react-native";
import { type AgentEntry, type AgentTool, mergeAgentEntries, outputText, toolFailed, toolRunning } from "../../src/core/agentEntries";
import { engine } from "../../src/core/engine";
import { t, useLanguage } from "../../src/i18n";
import { Markdown } from "../../src/ui/Markdown";
import { CloseToolbar } from "../../src/ui/navigation";
import { accentColor, usePalette } from "../../src/ui/theme";

/// How often the transcript is asked for again while it is on screen.
const REFRESH_MS = 5000;
const mono = Platform.OS === "ios" ? "Menlo" : "monospace";

export default function AgentTranscriptScreen() {
  useLanguage();
  const { id, chat: chatId, title } = useLocalSearchParams<{ id: string; chat: string; title?: string }>();
  const router = useRouter();
  const p = usePalette();
  const { width } = useWindowDimensions();
  const [entries, setEntries] = useState<AgentEntry[]>([]);
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState<Record<number, boolean>>({});
  const scroll = useRef<ScrollView>(null);
  const atEnd = useRef(true);

  useFocusEffect(
    useCallback(() => {
      let live = true;
      let timer: ReturnType<typeof setTimeout> | undefined;
      let rev: number | undefined;
      const load = async () => {
        try {
          const page = await engine.agentTranscript(chatId, id, rev);
          if (!live) return;
          // A Runner that counts afresh, as after a restart that kept no entries: all of them again.
          const fresh = rev !== undefined && (page.rev ?? 0) < rev;
          setEntries((have) => mergeAgentEntries(fresh ? [] : have, page));
          rev = fresh ? undefined : page.rev;
          setText(page.text);
          setError(null);
        } catch (e) {
          // What was shown stays; the next try may reach the Runner.
          if (live) setError(e instanceof Error ? e.message : String(e));
        }
        if (live) timer = setTimeout(load, REFRESH_MS);
      };
      void load();
      return () => {
        live = false;
        if (timer) clearTimeout(timer);
      };
    }, [chatId, id]),
  );

  const lastTool = entries.reduce((last, entry, at) => (entry.tool && hasBody(entry.tool) ? at : last), -1);
  const isOpen = (entry: AgentEntry, at: number) => open[entry.seq] ?? (!!entry.tool && (toolRunning(entry.tool) || toolFailed(entry.tool) || at === lastTool));

  return (
    <View style={styles.screen}>
      <Stack.Screen options={{ title: title ?? t("Transcript") }} />
      <CloseToolbar label={Platform.OS === "android" ? t("Close") : t("Done")} onClose={() => router.dismiss()} />
      <ScrollView
        ref={scroll}
        contentContainerStyle={styles.content}
        contentInsetAdjustmentBehavior="automatic"
        scrollEventThrottle={64}
        onScroll={(e) => {
          const { contentOffset, contentSize, layoutMeasurement } = e.nativeEvent;
          atEnd.current = contentOffset.y + layoutMeasurement.height >= contentSize.height - 24;
        }}
        onContentSizeChange={() => {
          if (atEnd.current) scroll.current?.scrollToEnd({ animated: false });
        }}
      >
        {entries.length > 0 ? (
          entries.map((entry, at) => (
            <EntryView key={entry.seq} entry={entry} open={isOpen(entry, at)} width={width - 32} onToggle={() => setOpen((was) => ({ ...was, [entry.seq]: !isOpen(entry, at) }))} />
          ))
        ) : text ? (
          <View style={[styles.code, { backgroundColor: p.code }]}>
            <Text selectable style={[styles.mono, { color: p.label }]}>
              {text}
            </Text>
          </View>
        ) : (
          <Text style={{ color: p.secondaryLabel }}>{error ?? (text === "" ? t("Nothing yet") : t("Loading…"))}</Text>
        )}
      </ScrollView>
    </View>
  );
}

function hasBody(tool: AgentTool): boolean {
  return !!tool.output || (tool.diffs?.length ?? 0) > 0 || !!tool.result;
}

function EntryView({ entry, open, width, onToggle }: { entry: AgentEntry; open: boolean; width: number; onToggle: () => void }) {
  const p = usePalette();
  switch (entry.kind) {
    case "sent":
      return (
        <View style={styles.sent}>
          <View style={[styles.bar, { backgroundColor: p.tint }]} />
          <Text selectable style={[styles.sentText, { color: p.secondaryLabel }]}>
            {entry.text}
          </Text>
        </View>
      );
    case "said":
      return <Markdown text={entry.text ?? ""} color={p.label} maxWidth={width} size={15} />;
    case "plan":
      return (
        <View style={[styles.code, { backgroundColor: p.code, gap: 4 }]}>
          <Text style={[styles.caption, { color: p.secondaryLabel }]}>{t("Plan")}</Text>
          {(entry.steps ?? []).map((step, at) => (
            <Text key={at} style={{ color: step.done ? p.secondaryLabel : p.label, fontSize: 14 }}>
              {step.done ? "☑ " : "☐ "}
              {step.text}
            </Text>
          ))}
        </View>
      );
    case "tool":
      return entry.tool ? <ToolView tool={entry.tool} open={open} onToggle={onToggle} /> : null;
    case "thought":
      return <Folded title={t("Thought")} text={entry.text ?? ""} open={open} onToggle={onToggle} width={width} />;
    case "compaction": {
      const title = { completed: t("Compacted the context"), failed: t("Could not compact the context"), cancelled: t("Stopped compacting the context") }[entry.status ?? ""] ?? t("Compacting the context…");
      return <Folded title={title} text={entry.text ?? ""} open={open} onToggle={onToggle} width={width} running={!entry.status || entry.status === "in_progress"} />;
    }
    case "notice": {
      const tint = entry.level === "error" ? p.red : entry.level === "warning" ? accentColor("orange", p.dark) : p.secondaryLabel;
      return (
        <Text selectable style={[styles.note, { color: tint }]}>
          {entry.level === "error" ? "✕ " : entry.level === "warning" ? "⚠ " : "ⓘ "}
          {entry.text}
        </Text>
      );
    }
    case "log":
      return (
        <View style={[styles.code, { backgroundColor: p.code }]}>
          <Text selectable style={[styles.mono, { color: p.label }]}>
            {entry.text}
          </Text>
        </View>
      );
    default:
      return (
        <Text selectable style={[styles.note, { color: p.tertiaryLabel }]}>
          {entry.text}
        </Text>
      );
  }
}

/// A line that opens to what an entry says: a thought, or what a compaction kept.
function Folded({ title, text, open, onToggle, width, running }: { title: string; text: string; open: boolean; onToggle: () => void; width: number; running?: boolean }) {
  const p = usePalette();
  const body = text.trim() !== "";
  return (
    <View style={{ gap: 6 }}>
      <Pressable disabled={!body} onPress={onToggle} accessibilityRole="button" accessibilityState={{ expanded: open }} style={styles.head}>
        {running ? <ActivityIndicator size="small" /> : null}
        <Text style={[styles.title, { color: p.secondaryLabel }]}>{title}</Text>
        {!open && body ? (
          <Text numberOfLines={1} style={[styles.preview, { color: p.tertiaryLabel }]}>
            {text.trim().split("\n")[0]}
          </Text>
        ) : (
          <View style={{ flex: 1 }} />
        )}
        {body ? <Text style={{ color: p.tertiaryLabel }}>{open ? "⌄" : "›"}</Text> : null}
      </Pressable>
      {open ? <Markdown text={text} color={p.secondaryLabel} maxWidth={width} size={14} /> : null}
    </View>
  );
}

function ToolView({ tool, open, onToggle }: { tool: AgentTool; open: boolean; onToggle: () => void }) {
  const p = usePalette();
  const body = hasBody(tool);
  const diffs = tool.diffs ?? [];
  const added = diffs.reduce((sum, diff) => sum + diff.added, 0);
  const removed = diffs.reduce((sum, diff) => sum + diff.removed, 0);
  const failed = toolFailed(tool);
  return (
    <View style={{ gap: 6 }}>
      <Pressable disabled={!body} onPress={onToggle} accessibilityRole="button" accessibilityState={{ expanded: open }} style={styles.head}>
        {toolRunning(tool) ? <ActivityIndicator size="small" /> : <Text style={{ color: failed ? p.red : p.secondaryLabel, width: 16 }}>{failed ? "✕" : "✓"}</Text>}
        <Text numberOfLines={1} style={[styles.title, { color: p.label }]}>
          {tool.title}
        </Text>
        {tool.detail ? (
          <Text numberOfLines={1} style={[styles.detail, { color: p.secondaryLabel }]}>
            {tool.detail}
          </Text>
        ) : null}
        <View style={{ flex: 1 }} />
        {added + removed > 0 ? <Text style={[styles.small, { color: p.green }]}>+{added}</Text> : null}
        {added + removed > 0 ? <Text style={[styles.small, { color: p.red }]}>−{removed}</Text> : null}
        {tool.exit_code ? <Text style={[styles.small, { color: p.red }]}>{t("exit {code}", { code: String(tool.exit_code) })}</Text> : null}
        {body ? <Text style={{ color: p.tertiaryLabel }}>{open ? "⌄" : "›"}</Text> : null}
      </Pressable>
      {open && tool.output ? (
        <ScrollView style={[styles.code, styles.output, { backgroundColor: p.code }]} nestedScrollEnabled>
          <Text selectable style={[styles.mono, { color: p.label }]}>
            {outputText(tool.output)}
          </Text>
        </ScrollView>
      ) : null}
      {open
        ? diffs.map((diff) => (
            <View key={diff.path} style={[styles.code, { backgroundColor: p.code, paddingHorizontal: 0 }]}>
              <Text style={[styles.mono, styles.diffLine, { color: p.secondaryLabel, marginBottom: 4 }]}>{diff.path}</Text>
              {diff.lines.map((line, at) => (
                <Text key={at} selectable style={[styles.mono, styles.diffLine, { color: line.startsWith("+") ? p.green : line.startsWith("-") ? p.red : line.startsWith("@@") ? p.tertiaryLabel : p.secondaryLabel }]}>
                  {line}
                </Text>
              ))}
            </View>
          ))
        : null}
      {open && !tool.output && diffs.length === 0 && tool.result ? (
        <View style={[styles.code, { backgroundColor: p.code }]}>
          <Text selectable style={[styles.mono, { color: p.label }]}>
            {tool.result}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  content: { padding: 16, paddingBottom: 40, gap: 12 },
  code: { borderRadius: 10, padding: 12 },
  output: { maxHeight: 280 },
  mono: { fontFamily: mono, fontSize: 12, lineHeight: 17 },
  diffLine: { paddingHorizontal: 12 },
  sent: { flexDirection: "row", gap: 10 },
  bar: { width: 3, borderRadius: 1.5 },
  sentText: { flex: 1, fontSize: 15, lineHeight: 21 },
  caption: { fontSize: 12, fontWeight: "600" },
  note: { fontSize: 13, lineHeight: 18 },
  head: { flexDirection: "row", alignItems: "center", gap: 8, minHeight: 28 },
  title: { fontSize: 15, fontWeight: "500", flexShrink: 1 },
  detail: { fontFamily: mono, fontSize: 12, flexShrink: 2 },
  preview: { fontSize: 13, flex: 1 },
  small: { fontFamily: mono, fontSize: 12 },
});
