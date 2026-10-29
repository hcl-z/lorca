// A coding agent, opened from its card or the bar over the composer, as a sheet: the phone's
// version of the desktop's full view. At the top its tile, its name over its session's title, how it
// stands, and Stop; then what the desktop's column beside the transcript holds: what it works on,
// where (branch, folder, Runner, since when), its session as the card's strip of cells, its plan
// with its steps done, and its pull request. Then its transcript as a timeline: each turn opens with
// what the bot sent, and each entry sits on a rail beside a node of its own, the agent's tile for
// what it said, a tool's state, a thought, the plan, a compaction, or a notice in its level's tint.
// A tool's line opens to its command's output in its colors, its edits as a diff, or what it gave
// back; a tool that runs, one that failed, and the last one are open, and a tap opens or closes
// any. What the agent asks, its own question's card or the card's Allow once and Deny, sits under
// the transcript, answered here as in the chat. It asks the Runner again every five seconds for what
// changed, and keeps to the end unless the user scrolled up to read. It opens on the newest page
// of entries and asks for the page before it when the top comes into view, so a long run mounts
// a hundred entries, not a thousand.

import { Stack, useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { memo, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { FlashList, type FlashListRef } from "@shopify/flash-list";
import { ActivityIndicator, Platform, Pressable, ScrollView, StyleSheet, Text, useWindowDimensions, View } from "react-native";
import { type AgentEntry, type AgentTool, hasOlderEntries, mergeAgentEntries, toolFailed, toolRunning, type AgentTranscriptPage } from "../../src/core/agentEntries";
import { agentSince, latestPlan, outputLines, spansForLines, type OutputSpan } from "../../src/core/agentKit";
import { engine } from "../../src/core/engine";
import { agentIsRunning, agentName, type AgentRun, type Message } from "../../src/core/model";
import { useStore } from "../../src/core/store";
import { t, useLanguage } from "../../src/i18n";
import { AgentHeader, AgentTile, Callout, PullRequestButton, SpecStrip, StopButton, alpha, mono } from "../../src/ui/agentKit";
import { alert } from "../../src/ui/alert";
import { Markdown } from "../../src/ui/Markdown";
import { FoldedText } from "../../src/ui/FoldedText";
import { CloseToolbar } from "../../src/ui/navigation";
import { Symbol } from "../../src/ui/Symbol";
import { AgentAskRow, CardButton, DECISION_ICONS } from "../../src/ui/transcript";
import { AgentActionsSheet } from "../../src/ui/AgentActionsSheet";
import { AgentAskSheet } from "../../src/ui/AgentAskSheet";
import { accentColor, usePalette } from "../../src/ui/theme";

/// How often the transcript is asked for again while it is on screen.
const REFRESH_MS = 5000;
/// How many entries one page carries: a couple of screens, so the top loads with little wait.
const AGENT_PAGE = 20;
/// The lines of a command's output a folded view lays out, its last.
const OUTPUT_LINES = 200;
/// The lines of an edit's diff a folded view lays out.
const DIFF_LINES = 200;
const NODE = 24;
/// Older entries land above the reader without moving what is on screen.
const KEEP_POSITION = { disabled: false };

export default function AgentScreen() {
  useLanguage();
  const { id, chat: chatId } = useLocalSearchParams<{ id: string; chat: string }>();
  const router = useRouter();
  const p = usePalette();
  const { width } = useWindowDimensions();
  const message = useStore((s) => s.chats.find((chat) => chat.id === chatId)?.messages.find((each) => each.id === id));
  const agent = message?.body.kind === "tool" ? message.body.agent : undefined;
  const bot = useStore((s) => (message?.author.kind === "bot" ? s.bots.find((each) => each.id === (message.author as { bot_id: string }).bot_id) : undefined));
  const askCard = useStore((s) => (agent?.asking ? s.chats.find((chat) => chat.id === chatId)?.messages.find((each) => each.id === agent.asking) : undefined));
  const [entries, setEntries] = useState<AgentEntry[]>([]);
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState<Record<number, boolean>>({});
  const [stopping, setStopping] = useState(false);
  const [actionsOpen, setActionsOpen] = useState(false);
  const [askOpen, setAskOpen] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const scroll = useRef<FlashListRef<AgentEntry>>(null);
  const atEnd = useRef(true);
  // What the list holds now, for the older page the top asks for while a refresh may be in
  // flight: both merges read and write these, never a page behind.
  const shown = useRef<AgentEntry[]>([]);
  // The first entry the Runner keeps: what says whether a page before the oldest is left.
  const firstKept = useRef<number | undefined>(undefined);
  const olderLoading = useRef(false);
  // A fresh run starts a new generation of pages: a page asked for before it lands nowhere.
  const generation = useRef(0);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  /// A page put into the list: what changed, or the newest page of an older one, and the first
  /// entry the Runner keeps from it.
  const applyPage = useCallback((page: AgentTranscriptPage, fresh: boolean) => {
    if (fresh) generation.current += 1;
    const next = mergeAgentEntries(fresh ? [] : shown.current, page);
    shown.current = next;
    firstKept.current = page.first;
    setEntries(next);
    setText(page.text);
    setError(null);
  }, []);

  useFocusEffect(
    useCallback(() => {
      let live = true;
      let timer: ReturnType<typeof setTimeout> | undefined;
      let rev: number | undefined;
      const load = async () => {
        try {
          // The newest page first; every refresh after it asks only for what changed.
          const page = await engine.agentTranscript(chatId, id, rev, undefined, rev === undefined ? AGENT_PAGE : undefined);
          if (!live) return;
          // A Runner that counts afresh, as after a restart that kept no entries: all of them again.
          const fresh = rev !== undefined && (page.rev ?? 0) < rev;
          applyPage(page, fresh);
          rev = fresh ? undefined : page.rev;
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
    }, [applyPage, chatId, id]),
  );

  /// The page before the oldest entry, when the reader scrolls up to the top. A reader at the end
  /// is not asking for history: the sheet opens at the end, and the initial scroll is not a pull.
  const loadOlder = useCallback(() => {
    const oldest = shown.current[0]?.seq;
    if (atEnd.current || olderLoading.current || oldest === undefined || !hasOlderEntries(shown.current, firstKept.current)) return;
    olderLoading.current = true;
    setLoadingOlder(true);
    const gen = generation.current;
    engine
      .agentTranscript(chatId, id, undefined, oldest, AGENT_PAGE)
      .then((page) => {
        // A fresh run replaced what this page was asked for: its entries are not the run's.
        if (!mounted.current || gen !== generation.current) return;
        const next = mergeAgentEntries(shown.current, page);
        shown.current = next;
        firstKept.current = page.first;
        setEntries(next);
      })
      .catch(() => {
        // What was shown stays; the top asks again when it comes into view.
      })
      .finally(() => {
        olderLoading.current = false;
        if (mounted.current) setLoadingOlder(false);
      });
  }, [chatId, id]);

  const who = bot?.name ?? t("The bot");
  const lastTool = useMemo(() => entries.reduce((last, entry, at) => (entry.tool && hasBody(entry.tool) ? at : last), -1), [entries]);
  const isOpen = useCallback((entry: AgentEntry, at: number) => open[entry.seq] ?? (!!entry.tool && (toolRunning(entry.tool) || toolFailed(entry.tool) || at === lastTool)), [open, lastTool]);
  const column = Math.min(width, 760) - 32;
  const toggle = useCallback((entry: AgentEntry, shown: boolean) => setOpen((was) => ({ ...was, [entry.seq]: !shown })), []);
  const askRow =
    askCard?.body.kind === "permission" && askCard.body.agent_ask
      ? { key: askCard.id, type: "permission" as const, message: askCard, body: askCard.body, bot, groupStart: false }
      : undefined;
  const stop = () => {
    if (!message || stopping) return;
    setStopping(true);
    engine
      .stopAgent(chatId, message.id)
      .catch((e) => alert(t("Could not stop {agent}", { agent: agent ? agentName(agent) : "" }), e instanceof Error ? e.message : String(e)))
      .finally(() => setStopping(false));
  };
  const renderEntry = useCallback(
    ({ item: entry, index: at }: { item: AgentEntry; index: number }) => (
      <EntryRow
        entry={entry}
        kind={agent?.kind ?? ""}
        who={who}
        // The rail runs on to the next entry with a node.
        rail={at + 1 < entries.length && entries[at + 1].kind !== "sent" && entries[at + 1].kind !== "log"}
        open={isOpen(entry, at)}
        width={column - NODE - 12}
        onToggle={toggle}
      />
    ),
    [agent?.kind, who, entries, isOpen, column, toggle],
  );

  return (
    <View style={styles.screen}>
      <Stack.Screen options={{ title: agent ? agentName(agent) : t("Transcript") }} />
      <CloseToolbar label={Platform.OS === "android" ? t("Close") : t("Done")} onClose={() => router.dismiss()} />
      {/* A long run's transcript holds hundreds of entries: the list draws those near the screen,
          the overview over them and what the agent asks under them. */}
      <FlashList
        ref={scroll}
        data={entries}
        keyExtractor={(entry) => String(entry.seq)}
        getItemType={(entry) => (entry.tool ? "tool" : entry.kind)}
        extraData={open}
        drawDistance={800}
        // The page before the oldest entry, asked for when the top comes into view; the list
        // keeps the reader's place as it lands.
        maintainVisibleContentPosition={KEEP_POSITION}
        onStartReached={loadOlder}
        onStartReachedThreshold={1}
        contentContainerStyle={{ ...styles.content, paddingHorizontal: 16 + (width - column - 32) / 2 }}
        contentInsetAdjustmentBehavior="automatic"
        scrollEventThrottle={64}
        onScroll={(e) => {
          const { contentOffset, contentSize, layoutMeasurement } = e.nativeEvent;
          atEnd.current = contentOffset.y + layoutMeasurement.height >= contentSize.height - 24;
        }}
        onContentSizeChange={() => {
          if (atEnd.current) scroll.current?.scrollToEnd({ animated: false });
        }}
        ListHeaderComponent={
          <>
            {agent && message ? <Overview agent={agent} message={message} entries={entries} stopping={stopping} onStop={stop} onActions={() => setActionsOpen(true)} /> : null}
            {loadingOlder ? <Text style={{ color: p.secondaryLabel, fontSize: 13, paddingVertical: 8 }}>{t("Loading earlier…")}</Text> : null}
            <View style={{ height: 6 }} />
          </>
        }
        renderItem={renderEntry}
        ListEmptyComponent={
          text ? (
            <View style={[styles.code, { backgroundColor: p.code }]}>
              <Text selectable style={[styles.mono, { color: p.label }]}>
                {text}
              </Text>
            </View>
          ) : (
            <Text style={{ color: p.secondaryLabel }}>{error ?? (text === "" ? t("Nothing yet") : t("Loading…"))}</Text>
          )
        }
        ListFooterComponent={
          <>
            {askCard?.body.kind === "permission" && askCard.body.agent_ask ? (
              <View style={{ marginTop: 8, marginHorizontal: -12 }}>
                <AgentAskRow row={{ key: askCard.id, type: "permission", message: askCard, body: askCard.body, bot, groupStart: false }} ask={askCard.body.agent_ask} isGroup={false} />
              </View>
            ) : null}
            {agent && message ? <CardQuestion agent={agent} message={message} who={who} /> : null}
          </>
        }
      />
      <AgentActionsSheet
        visible={actionsOpen}
        agent={agent}
        showTranscript={false}
        onClose={() => setActionsOpen(false)}
        onOpenTranscript={() => setActionsOpen(false)}
        onAnswer={
          agent?.asking
            ? () => {
                setActionsOpen(false);
                setTimeout(() => setAskOpen(true), 260);
              }
            : undefined
        }
        onStop={stop}
      />
      <AgentAskSheet visible={askOpen && !!askRow} row={askRow} ask={askRow?.body.agent_ask} onClose={() => setAskOpen(false)} />
    </View>
  );
}

/// The top of the sheet: the agent's header with Stop, then what it works on, where, its session,
/// its plan, and its pull request.
function Overview({ agent, message, entries, stopping, onStop, onActions }: { agent: AgentRun; message: Message; entries: AgentEntry[]; stopping: boolean; onStop: () => void; onActions: () => void }) {
  const p = usePalette();
  // The overview refreshes with the polls; its "Started 4m ago" needs minutes, not another clock.
  const since = agentSince(agent, Date.now());
  const plan = useMemo(() => latestPlan(entries), [entries]);
  const green = accentColor("green", p.dark);
  const fact = (icon: string, words: string, monospaced?: boolean) => (
    <View style={styles.fact}>
      <Symbol name={icon} size={13} color={p.tertiaryLabel} />
      <Text style={[styles.factText, { color: p.label }, monospaced && { fontFamily: mono, fontSize: 12.5 }]} numberOfLines={1}>
        {words}
      </Text>
    </View>
  );
  return (
    <View style={{ gap: 18 }}>
      <AgentHeader
        agent={agent}
        tile={40}
        size={18}
        sub={agent.title || agent.task}
        trailing={
          <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
            {agentIsRunning(agent) ? <StopButton busy={stopping} onPress={onStop} size={34} /> : null}
            <Pressable onPress={onActions} hitSlop={8} accessibilityRole="button" accessibilityLabel={t("Agent actions")} style={({ pressed }) => ({ opacity: pressed ? 0.6 : 1 })}>
              <Symbol name="ellipsis" size={20} color={p.secondaryLabel} />
            </Pressable>
          </View>
        }
      />
      {agent.task ? (
        <Section title={t("Task")}>
          <Text selectable style={{ color: p.label, fontSize: 15, lineHeight: 21 }}>
            {agent.task}
          </Text>
        </Section>
      ) : null}
      {agent.branch || agent.folder || agent.device || since ? (
        <Section title={t("Workspace")}>
          <View style={[styles.facts, { backgroundColor: p.code }]}>
            {agent.branch ? fact("arrow.triangle.branch", agent.branch, true) : null}
            {agent.folder ? fact("folder", agent.folder, true) : null}
            {agent.device ? fact("desktopcomputer", agent.device) : null}
            {since ? fact("clock", t("Started {time} ago", { time: since })) : null}
          </View>
        </Section>
      ) : null}
      {(agent.settings?.length ?? 0) > 0 && !agent.question ? (
        <Section title={t("Session")}>
          <SpecStrip agent={agent} chatId={message.chat_id} messageId={message.id} />
        </Section>
      ) : null}
      {plan ? (
        <Section title={t("Plan · {done} of {total}", { done: String(plan.done), total: String(plan.steps.length) })}>
          <View style={[styles.meter, { backgroundColor: p.separator }]}>
            <View style={{ height: 4, borderRadius: 2, width: `${(plan.done * 100) / plan.steps.length}%`, backgroundColor: green }} />
          </View>
          <View style={{ gap: 7 }}>
            {plan.steps.map((step, at) => (
              <View key={at} style={styles.step}>
                <Symbol name={step.done ? "checkmark.circle.fill" : "circle"} size={15} color={step.done ? green : p.tertiaryLabel} style={{ marginTop: 2 }} />
                <Text style={{ flex: 1, color: step.done ? p.secondaryLabel : p.label, fontSize: 14, lineHeight: 19 }}>{step.text}</Text>
              </View>
            ))}
          </View>
        </Section>
      ) : null}
      {agent.pull_request ? (
        <Section title={t("Pull request")}>
          <PullRequestButton link={agent.pull_request} name={agent.pull_request_name} />
        </Section>
      ) : null}
      <View style={[styles.rule, { backgroundColor: p.separator }]} />
    </View>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  const p = usePalette();
  return (
    <View style={{ gap: 8 }}>
      <Text style={[styles.sectionTitle, { color: p.tertiaryLabel }]}>{title}</Text>
      {children}
    </View>
  );
}

/// The card's own question, before the agent starts or runs a command, answered here as on it.
function CardQuestion({ agent, message, who }: { agent: AgentRun; message: Message; who: string }) {
  const p = usePalette();
  const q = agent.question;
  if (!q || (q.kind !== "start" && q.kind !== "command")) return null;
  const title = q.kind === "command" ? t("{agent} wants to run a command", { agent: agentName(agent) }) : agent.device ? t("{who} wants to start {agent} on {device}", { who, agent: agentName(agent), device: agent.device }) : t("{who} wants to start {agent}", { who, agent: agentName(agent) });
  const decide = (decision: "allow" | "always" | "deny") => engine.answerPermission(message.chat_id, message.id, decision).catch((e) => alert(t("Could not answer"), e instanceof Error ? e.message : String(e)));
  const decisions: [string, "allow" | "always" | "deny"][] = q.rule ? [[t("Allow once"), "allow"], [t("Always allow"), "always"], [t("Deny"), "deny"]] : [[t("Allow once"), "allow"], [t("Deny"), "deny"]];
  return (
    <View style={{ marginTop: 8 }}>
      <Callout tint={accentColor("orange", p.dark)} icon="hand.raised" title={title}>
        {q.command ? (
          <View style={[styles.code, { backgroundColor: p.code, paddingVertical: 9 }]}>
            <Text style={[styles.mono, { color: p.label }]} numberOfLines={3}>{`$ ${q.command}`}</Text>
          </View>
        ) : null}
        {q.reason ? <Text style={{ color: p.secondaryLabel, fontSize: 13, lineHeight: 18 }}>{q.reason}</Text> : null}
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
          {decisions.map(([label, decision], index) => (
            <CardButton key={decision} label={label} icon={DECISION_ICONS[decision]} primary={index === 0} onPress={() => void decide(decision)} />
          ))}
        </View>
        {q.rule ? <Text style={{ color: p.secondaryLabel, fontSize: 12, lineHeight: 16 }}>{t("Always allow adds the rule “{rule}”.", { rule: q.rule })}</Text> : null}
      </Callout>
    </View>
  );
}

function hasBody(tool: AgentTool): boolean {
  return !!tool.output || (tool.diffs?.length ?? 0) > 0 || !!tool.result;
}

/// One entry of the transcript: on the rail beside its node, or, for what was sent, a turn's start.
/// Drawn again only when its entry, its place, or whether it is open changes: the sheet refreshes
/// every few seconds, and most refreshes change one entry or none.
const EntryRow = memo(function EntryRow({ entry, kind, who, rail, open, width, onToggle }: { entry: AgentEntry; kind: string; who: string; rail: boolean; open: boolean; width: number; onToggle: (entry: AgentEntry, open: boolean) => void }) {
  const p = usePalette();
  const blue = accentColor("blue", p.dark);
  if (entry.kind === "sent") {
    return (
      <View style={[styles.sent, { backgroundColor: alpha(blue, 0.07), borderColor: alpha(blue, 0.2) }]}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 5 }}>
          <Symbol name="paperplane" size={12} color={blue} />
          <Text style={{ color: blue, fontSize: 12, fontWeight: "600" }} numberOfLines={1}>
            {t("{who} asked", { who })}
          </Text>
        </View>
        <Text selectable style={{ color: p.label, fontSize: 15, lineHeight: 21 }}>
          {entry.text}
        </Text>
      </View>
    );
  }
  if (entry.kind === "log") {
    return (
      <View style={[styles.code, { backgroundColor: p.code, marginBottom: 14 }]}>
        <Text selectable style={[styles.mono, { color: p.label }]}>
          {entry.text}
        </Text>
      </View>
    );
  }
  return (
    <View style={styles.entry}>
      <View style={styles.gutter}>
        {rail ? <View style={[styles.rail, { backgroundColor: p.separator }]} /> : null}
        <Node entry={entry} kind={kind} />
      </View>
      <View style={styles.entryContent}>
        <EntryContent entry={entry} open={open} width={width} onToggle={() => onToggle(entry, open)} />
      </View>
    </View>
  );
});

/// An entry's place on the rail: the agent's tile for what it said, a tool's state, or a quiet mark
/// of its kind.
function Node({ entry, kind }: { entry: AgentEntry; kind: string }) {
  const p = usePalette();
  const ring = (tint: string, child: ReactNode) => <View style={[styles.node, { backgroundColor: alpha(tint, 0.11), borderColor: alpha(tint, 0.3) }]}>{child}</View>;
  const mark = (name: string, tint: string) => <Symbol name={name} size={12} color={tint} weight="semibold" />;
  const spin = (tint: string) => <ActivityIndicator size="small" color={tint} style={{ transform: [{ scale: 0.6 }] }} />;
  const gray = p.dark ? "#AEAEB2" : "#6E6E73";
  switch (entry.kind) {
    case "said":
      return <AgentTile kind={kind} size={NODE} round />;
    case "tool": {
      const tool = entry.tool;
      if (!tool) return null;
      if (toolRunning(tool)) return ring(accentColor("blue", p.dark), spin(accentColor("blue", p.dark)));
      if (toolFailed(tool)) return ring(accentColor("red", p.dark), mark("xmark", accentColor("red", p.dark)));
      return ring(gray, mark(toolSymbol(tool.kind), gray));
    }
    case "thought":
      return ring(accentColor("purple", p.dark), mark("sparkles", accentColor("purple", p.dark)));
    case "plan":
      return ring(accentColor("green", p.dark), mark("checklist", accentColor("green", p.dark)));
    case "compaction": {
      const teal = accentColor("teal", p.dark);
      return ring(teal, !entry.status || entry.status === "in_progress" ? spin(teal) : mark("arrow.down.right.and.arrow.up.left", teal));
    }
    case "notice": {
      const tint = entry.level === "error" ? accentColor("red", p.dark) : entry.level === "warning" ? accentColor("orange", p.dark) : gray;
      return ring(tint, mark(entry.level === "error" ? "xmark.circle" : entry.level === "warning" ? "exclamationmark.triangle" : "info.circle", tint));
    }
    default:
      return (
        <View style={[styles.node, { borderWidth: 0 }]}>
          <View style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: p.tertiaryLabel }} />
        </View>
      );
  }
}

/// The mark of a tool that ended well, by its kind.
function toolSymbol(kind?: string): string {
  switch (kind) {
    case "execute":
      return "terminal";
    case "edit":
    case "delete":
    case "move":
      return "pencil";
    case "read":
      return "doc.text";
    case "search":
      return "magnifyingglass";
    case "fetch":
      return "globe";
    case "think":
      return "sparkles";
  }
  return "checkmark";
}

function EntryContent({ entry, open, width, onToggle }: { entry: AgentEntry; open: boolean; width: number; onToggle: () => void }) {
  const p = usePalette();
  switch (entry.kind) {
    case "said":
      return <FoldedText text={entry.text ?? ""} color={p.label} maxWidth={width} size={15} background={p.background} actionColor={p.link} expanded={open} onToggle={onToggle} />;
    case "plan": {
      const steps = entry.steps ?? [];
      const green = accentColor("green", p.dark);
      return (
        <View style={{ gap: 7 }}>
          <View style={styles.head}>
            <Text style={[styles.title, { color: p.label, fontWeight: "600" }]}>{t("Plan")}</Text>
            <Text style={{ color: p.tertiaryLabel, fontSize: 13, fontVariant: ["tabular-nums"] }}>{t("{done} of {total}", { done: String(steps.filter((step) => step.done).length), total: String(steps.length) })}</Text>
          </View>
          <View style={[styles.code, { backgroundColor: p.code, gap: 6 }]}>
            {steps.map((step, at) => (
              <View key={at} style={styles.step}>
                <Symbol name={step.done ? "checkmark.circle.fill" : "circle"} size={14} color={step.done ? green : p.tertiaryLabel} style={{ marginTop: 2 }} />
                <Text style={{ flex: 1, color: step.done ? p.secondaryLabel : p.label, fontSize: 14, lineHeight: 19 }}>{step.text}</Text>
              </View>
            ))}
          </View>
        </View>
      );
    }
    case "tool":
      return entry.tool ? <ToolView tool={entry.tool} open={open} onToggle={onToggle} /> : null;
    case "thought":
      return <Folded title={t("Thought")} text={entry.text ?? ""} open={open} onToggle={onToggle} width={width} />;
    case "compaction": {
      const title = { completed: t("Compacted the context"), failed: t("Could not compact the context"), cancelled: t("Stopped compacting the context") }[entry.status ?? ""] ?? t("Compacting the context…");
      return <Folded title={title} text={entry.text ?? ""} open={open} onToggle={onToggle} width={width} />;
    }
    case "notice":
      return (
        <Text selectable style={[styles.note, { color: entry.level === "error" ? p.red : entry.level === "warning" ? p.label : p.secondaryLabel }]}>
          {entry.text}
        </Text>
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
function Folded({ title, text, open, onToggle, width }: { title: string; text: string; open: boolean; onToggle: () => void; width: number }) {
  const p = usePalette();
  const body = text.trim() !== "";
  const shown = body && open;
  return (
    <View style={{ gap: 6 }}>
      <Pressable disabled={!body} onPress={onToggle} accessibilityRole="button" accessibilityState={{ expanded: shown }} style={styles.head}>
        <Text style={[styles.title, { color: p.secondaryLabel }]}>{title}</Text>
        {!shown && body ? (
          <Text numberOfLines={1} style={[styles.preview, { color: p.tertiaryLabel }]}>
            {text.trim().split("\n")[0]}
          </Text>
        ) : (
          <View style={{ flex: 1 }} />
        )}
        {body ? <Symbol name={shown ? "chevron.down" : "chevron.right"} size={11} color={p.tertiaryLabel} weight="semibold" /> : null}
      </Pressable>
      {shown ? (
        <View style={[styles.code, { backgroundColor: p.code, paddingVertical: 4 }]}>
          <Markdown text={text} color={p.secondaryLabel} maxWidth={width - 24} size={14} />
        </View>
      ) : null}
    </View>
  );
}

function ToolView({ tool, open, onToggle }: { tool: AgentTool; open: boolean; onToggle: () => void }) {
  const p = usePalette();
  const body = hasBody(tool);
  const shown = body && open;
  const diffs = tool.diffs ?? [];
  const added = diffs.reduce((sum, diff) => sum + diff.added, 0);
  const removed = diffs.reduce((sum, diff) => sum + diff.removed, 0);
  const green = accentColor("green", p.dark);
  const red = accentColor("red", p.dark);
  // A diff of hundreds of lines is laid out to its head until the reader asks for all of it.
  const [allDiffs, setAllDiffs] = useState(false);
  const truncated = diffs.some((diff) => diff.lines.length > DIFF_LINES);
  return (
    <View style={{ gap: 7 }}>
      <Pressable disabled={!body} onPress={onToggle} accessibilityRole="button" accessibilityState={{ expanded: shown }} style={styles.head}>
        <Text numberOfLines={1} style={[styles.title, { color: p.label }]}>
          {tool.title}
        </Text>
        {tool.detail ? (
          <Text numberOfLines={1} style={[styles.detail, { color: p.secondaryLabel }]}>
            {tool.detail}
          </Text>
        ) : null}
        <View style={{ flex: 1 }} />
        {added + removed > 0 ? <Text style={[styles.small, { color: green }]}>+{added}</Text> : null}
        {added + removed > 0 ? <Text style={[styles.small, { color: red }]}>−{removed}</Text> : null}
        {tool.exit_code ? <Text style={[styles.small, { color: red }]}>{t("exit {code}", { code: String(tool.exit_code) })}</Text> : null}
        {body ? <Symbol name={shown ? "chevron.down" : "chevron.right"} size={11} color={p.tertiaryLabel} weight="semibold" /> : null}
      </Pressable>
      {shown && tool.output ? <OutputView output={tool.output} /> : null}
      {shown
        ? diffs.map((diff) => (
            <View key={diff.path} style={[styles.code, { backgroundColor: p.code, paddingHorizontal: 0, paddingVertical: 0, overflow: "hidden" }]}>
              <Text numberOfLines={1} style={[styles.mono, styles.diffLine, { color: p.secondaryLabel, paddingVertical: 6 }]}>
                {diff.path}
              </Text>
              <View style={{ paddingBottom: 6 }}>
                {(allDiffs ? diff.lines : diff.lines.slice(0, DIFF_LINES)).map((line, at) => {
                  const plus = line.startsWith("+");
                  const minus = line.startsWith("-");
                  return (
                    <Text key={at} selectable style={[styles.mono, styles.diffLine, { color: plus ? green : minus ? red : line.startsWith("@@") ? p.tertiaryLabel : p.secondaryLabel, backgroundColor: plus ? alpha(green, 0.1) : minus ? alpha(red, 0.1) : undefined }]}>
                      {line}
                    </Text>
                  );
                })}
                {!allDiffs && diff.lines.length > DIFF_LINES ? (
                  <Text style={[styles.small, styles.diffLine, { color: p.tertiaryLabel, paddingVertical: 4 }]}>{t("… {count} more lines", { count: String(diff.lines.length - DIFF_LINES) })}</Text>
                ) : null}
              </View>
            </View>
          ))
        : null}
      {shown && truncated ? (
        <Pressable onPress={() => setAllDiffs(!allDiffs)} accessibilityRole="button" style={{ paddingVertical: 4 }}>
          <Text style={[styles.small, { color: p.link }]}>{allDiffs ? t("Show less") : t("Show all")}</Text>
        </Pressable>
      ) : null}
      {shown && !tool.output && diffs.length === 0 && tool.result ? (
        <View style={[styles.code, { backgroundColor: p.code }]}>
          <Text selectable style={[styles.mono, { color: p.label }]}>
            {tool.result}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

/// The 16 colors of a terminal, for each appearance, readable on the code block's fill.
const ANSI = {
  light: ["#1d1d1f", "#c4332b", "#2f8a3a", "#a5730b", "#2a5bd7", "#9c3fb8", "#167f8f", "#6e6e73", "#6e6e73", "#e0443c", "#38a447", "#c48a0e", "#3c72f0", "#b456d1", "#1c98ab", "#1d1d1f"],
  dark: ["#8e8e93", "#ff6b61", "#5fd36f", "#f2c14e", "#6fa8ff", "#d68cf2", "#5fd0e0", "#e5e5ea", "#8e8e93", "#ff8a80", "#7ee38b", "#ffd76a", "#8fbcff", "#e2a6f7", "#7fe0ee", "#ffffff"],
};

function spanColor(span: OutputSpan, dark: boolean): string | undefined {
  if (span.rgb) return span.rgb;
  const n = span.color;
  if (n === undefined) return undefined;
  if (n < 16) return ANSI[dark ? "dark" : "light"][n];
  if (n >= 232) {
    const v = 8 + (n - 232) * 10;
    return `rgb(${v},${v},${v})`;
  }
  const at = n - 16;
  const level = (x: number) => (x === 0 ? 0 : 55 + x * 40);
  return `rgb(${level(Math.floor(at / 36))},${level(Math.floor(at / 6) % 6)},${level(at % 6)})`;
}

/// A command's output in its colors, its last lines in view while it grows. The lines before them
/// are laid out only when the reader asks for all of it, so a command that printed thousands of
/// lines costs its tail.
function OutputView({ output }: { output: string }) {
  const p = usePalette();
  const [all, setAll] = useState(false);
  const lines = useMemo(() => outputLines(output), [output]);
  const shownLines = useMemo(() => (all ? lines : lines.slice(-OUTPUT_LINES)), [lines, all]);
  const spans = useMemo(() => spansForLines(shownLines), [shownLines]);
  const hidden = lines.length - shownLines.length;
  const scroll = useRef<ScrollView>(null);
  return (
    <View style={[styles.code, { backgroundColor: p.code }]}>
      {lines.length > OUTPUT_LINES ? (
        <Pressable onPress={() => setAll(!all)} accessibilityRole="button" style={{ paddingBottom: 6 }}>
          <Text style={[styles.small, { color: p.link }]}>{all ? t("Show less") : t("… {count} more lines", { count: String(hidden) })}</Text>
        </Pressable>
      ) : null}
      <ScrollView ref={scroll} style={styles.output} nestedScrollEnabled onContentSizeChange={() => scroll.current?.scrollToEnd({ animated: false })}>
        <Text selectable style={[styles.mono, { color: p.label }]}>
          {spans.map((line, at) => (
            <Text key={at}>
              {line.map((span, i) => (
                <Text key={i} style={{ color: spanColor(span, p.dark), fontWeight: span.bold ? "700" : undefined, opacity: span.dim ? 0.6 : undefined }}>
                  {span.text}
                </Text>
              ))}
              {at < spans.length - 1 ? "\n" : ""}
            </Text>
          ))}
        </Text>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  content: { padding: 16, paddingBottom: 48, gap: 0 },
  sectionTitle: { fontSize: 12.5, fontWeight: "600" },
  facts: { borderRadius: 12, paddingVertical: 10, paddingHorizontal: 12, gap: 8 },
  fact: { flexDirection: "row", alignItems: "center", gap: 8, minWidth: 0 },
  factText: { fontSize: 14, flexShrink: 1 },
  meter: { height: 4, borderRadius: 2, overflow: "hidden" },
  step: { flexDirection: "row", gap: 8, alignItems: "flex-start" },
  rule: { height: StyleSheet.hairlineWidth, marginTop: 2, marginBottom: 8 },
  code: { borderRadius: 10, padding: 12 },
  output: { maxHeight: 300 },
  mono: { fontFamily: mono, fontSize: 12, lineHeight: 17 },
  diffLine: { paddingHorizontal: 12 },
  sent: { borderRadius: 14, borderWidth: StyleSheet.hairlineWidth, paddingTop: 10, paddingBottom: 12, paddingHorizontal: 13, gap: 6, marginTop: 6, marginBottom: 18 },
  entry: { flexDirection: "row", gap: 12 },
  gutter: { width: NODE, alignItems: "center" },
  rail: { position: "absolute", top: NODE, bottom: 0, width: StyleSheet.hairlineWidth * 2, left: NODE / 2 - StyleSheet.hairlineWidth },
  node: { width: NODE, height: NODE, borderRadius: NODE / 2, borderWidth: StyleSheet.hairlineWidth, alignItems: "center", justifyContent: "center", overflow: "hidden" },
  entryContent: { flex: 1, minWidth: 0, paddingTop: 1, paddingBottom: 18 },
  note: { fontSize: 14, lineHeight: 19, marginTop: 2 },
  head: { flexDirection: "row", alignItems: "center", gap: 8, minHeight: NODE },
  title: { fontSize: 15, fontWeight: "500", flexShrink: 1 },
  detail: { fontFamily: mono, fontSize: 12, flexShrink: 2 },
  preview: { fontSize: 13, flex: 1 },
  small: { fontFamily: mono, fontSize: 12 },
});
