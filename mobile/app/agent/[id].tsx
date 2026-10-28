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
// changed, and keeps to the end unless the user scrolled up to read.

import { Stack, useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { memo, useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { FlashList, type FlashListRef } from "@shopify/flash-list";
import { ActivityIndicator, Platform, Pressable, ScrollView, StyleSheet, Text, useWindowDimensions, View } from "react-native";
import { type AgentEntry, type AgentTool, mergeAgentEntries, toolFailed, toolRunning } from "../../src/core/agentEntries";
import { agentBusy, agentSince, latestPlan, outputSpans, type OutputSpan } from "../../src/core/agentKit";
import { engine } from "../../src/core/engine";
import { agentIsRunning, agentName, type AgentRun, type Message } from "../../src/core/model";
import { useStore } from "../../src/core/store";
import { t, useLanguage } from "../../src/i18n";
import { AgentHeader, AgentTile, Callout, PullRequestButton, SpecStrip, StopButton, alpha, mono, useNow } from "../../src/ui/agentKit";
import { agentSteps, agentToolText, exploredText, thoughtTitle, type AgentStep } from "../../src/ui/agentSteps";
import { alert } from "../../src/ui/alert";
import { Markdown } from "../../src/ui/Markdown";
import { CloseToolbar } from "../../src/ui/navigation";
import { Symbol } from "../../src/ui/Symbol";
import { AgentAskRow, CardButton, DECISION_ICONS } from "../../src/ui/transcript";
import { accentColor, usePalette } from "../../src/ui/theme";

/// How often the transcript is asked for again while it is on screen.
const REFRESH_MS = 5000;
const NODE = 24;

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
  const scroll = useRef<FlashListRef<AgentStep>>(null);
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

  const who = bot?.name ?? t("The bot");
  const steps = useMemo(() => agentSteps(entries), [entries]);
  let lastToolSeq = 0;
  for (const entry of entries) if (entry.tool && hasBody(entry.tool)) lastToolSeq = entry.seq;
  const isOpen = (entry: AgentEntry) => open[entry.seq] ?? (!!entry.tool && (toolRunning(entry.tool) || toolFailed(entry.tool) || entry.seq === lastToolSeq));
  const column = Math.min(width, 760) - 32;
  const toggle = useCallback((entry: AgentEntry, shown: boolean) => setOpen((was) => ({ ...was, [entry.seq]: !shown })), []);
  const stop = () => {
    if (!message || stopping) return;
    setStopping(true);
    engine
      .stopAgent(chatId, message.id)
      .catch((e) => alert(t("Could not stop {agent}", { agent: agent ? agentName(agent) : "" }), e instanceof Error ? e.message : String(e)))
      .finally(() => setStopping(false));
  };

  return (
    <View style={styles.screen}>
      <Stack.Screen options={{ title: agent ? agentName(agent) : t("Transcript") }} />
      <CloseToolbar label={Platform.OS === "android" ? t("Close") : t("Done")} onClose={() => router.dismiss()} />
      {/* A long run's transcript holds hundreds of entries: the list draws those near the screen,
          the overview over them and what the agent asks under them. */}
      <FlashList
        ref={scroll}
        data={steps}
        keyExtractor={(step) => String(step.entry.seq)}
        getItemType={(step) => (step.explored ? "explored" : step.entry.kind)}
        extraData={open}
        drawDistance={800}
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
            {agent && message ? <Overview agent={agent} message={message} entries={entries} stopping={stopping} onStop={stop} /> : null}
            <View style={{ height: 6 }} />
          </>
        }
        renderItem={({ item: step }) => (
          <StepRow
            step={step}
            kind={agent?.kind ?? ""}
            who={who}
            open={step.explored ? !!open[step.entry.seq] : isOpen(step.entry)}
            width={column}
            onToggle={toggle}
          />
        )}
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
    </View>
  );
}

/// The top of the sheet: the agent's header with Stop, then what it works on, where, its session,
/// its plan, and its pull request.
function Overview({ agent, message, entries, stopping, onStop }: { agent: AgentRun; message: Message; entries: AgentEntry[]; stopping: boolean; onStop: () => void }) {
  const p = usePalette();
  const now = useNow(agentBusy(agent));
  const since = agentSince(agent, now);
  const plan = latestPlan(entries);
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
      <AgentHeader agent={agent} tile={40} size={18} sub={agent.title || agent.task} trailing={agentIsRunning(agent) ? <StopButton busy={stopping} onPress={onStop} size={34} /> : null} />
      {agent.task ? (
        <Section title={t("Task")}>
          <Text selectable style={{ color: p.label, fontSize: 15, lineHeight: 21 }}>
            {agent.task}
          </Text>
        </Section>
      ) : null}
      {agent.branch || agent.folder || agent.device || since || (agent.changes?.files ?? 0) > 0 ? (
        <Section title={t("Workspace")}>
          <View style={[styles.facts, { backgroundColor: p.code }]}>
            {agent.branch ? fact("arrow.triangle.branch", agent.branch, true) : null}
            {agent.folder ? fact("folder", agent.folder, true) : null}
            {agent.device ? fact("desktopcomputer", agent.device) : null}
            {since ? fact("clock", t("Started {time} ago", { time: since })) : null}
            {agent.changes && agent.changes.files > 0
              ? fact(
                  "plus.forwardslash.minus",
                  `+${agent.changes.added} −${agent.changes.removed} ${agent.changes.files === 1 ? t("in 1 file") : t("in {count} files", { count: agent.changes.files })}`,
                )
              : null}
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

/// One step of the transcript, after the desktop's: a folded run of reads and searches, a prompt
/// card, a thought line, a tool line, or what the agent said.
const StepRow = memo(function StepRow({ step, kind, who, open, width, onToggle }: { step: AgentStep; kind: string; who: string; open: boolean; width: number; onToggle: (entry: AgentEntry, open: boolean) => void }) {
  const p = usePalette();
  const entry = step.entry;
  if (step.explored) {
    return (
      <View style={{ paddingBottom: 10 }}>
        <Pressable onPress={() => onToggle(entry, open)} accessibilityRole="button" accessibilityState={{ expanded: open }} style={styles.stepLine}>
          <Text numberOfLines={1} style={[styles.stepTitle, { color: p.secondaryLabel }]}>{exploredText(step.explored)}</Text>
          <View style={{ flex: 1 }} />
          <Symbol name={open ? "chevron.down" : "chevron.right"} size={11} color={p.tertiaryLabel} weight="semibold" />
        </Pressable>
        {open ? (
          <View style={{ gap: 6, paddingLeft: 12, paddingTop: 4 }}>
            {step.explored.map((each) => (each.tool ? <ToolView key={each.seq} tool={each.tool} title={agentToolText(each.tool)} open={false} onToggle={() => {}} /> : null))}
          </View>
        ) : null}
      </View>
    );
  }
  switch (entry.kind) {
    case "sent":
      return <PromptCard entry={entry} who={who} open={open} onToggle={() => onToggle(entry, open)} />;
    case "said":
      return (
        <View style={{ paddingBottom: 10 }}>
          <Markdown text={entry.text ?? ""} color={p.label} maxWidth={width} size={15} />
        </View>
      );
    case "thought":
      return (
        <View style={{ paddingBottom: 6 }}>
          <Folded title={thoughtTitle(entry)} text={entry.text ?? ""} open={open} onToggle={() => onToggle(entry, open)} width={width} />
        </View>
      );
    case "tool":
      return entry.tool ? (
        <View style={{ paddingBottom: 10 }}>
          <ToolView tool={entry.tool} title={agentToolText(entry.tool)} open={open} onToggle={() => onToggle(entry, open)} />
        </View>
      ) : null;
    case "log":
      return (
        <View style={[styles.code, { backgroundColor: p.code, marginBottom: 10 }]}>
          <Text selectable style={[styles.mono, { color: p.label }]}>{entry.text}</Text>
        </View>
      );
    default:
      return (
        <View style={{ paddingBottom: 8 }}>
          <EntryContent entry={entry} open={open} width={width} onToggle={() => onToggle(entry, open)} />
        </View>
      );
  }
});

/// What the agent was sent, under who wrote it: "You" for the user, else the bot. A long brief
/// folds to two lines behind a chevron on its author line.
const PromptCard = memo(function PromptCard({ entry, who, open, onToggle }: { entry: AgentEntry; who: string; open: boolean; onToggle: () => void }) {
  const p = usePalette();
  const mine = entry.from === "user";
  const text = entry.text ?? "";
  const long = (text.match(/\n/g)?.length ?? 0) >= 2 || [...text].length > 200;
  const shown = long && open;
  return (
    <View style={[styles.prompt, { backgroundColor: mine ? alpha(p.ink, 0.09) : p.botBubble, borderColor: mine ? alpha(p.ink, 0.22) : p.cardBorder }]}>
      <Pressable disabled={!long} onPress={onToggle} accessibilityRole="button" accessibilityState={{ expanded: shown }} style={styles.promptHead}>
        <Text numberOfLines={1} style={[styles.promptAuthor, { color: p.tertiaryLabel }]}>{mine ? t("You") : who}</Text>
        <View style={{ flex: 1 }} />
        {long ? <Symbol name={shown ? "chevron.up" : "chevron.down"} size={10} color={p.tertiaryLabel} weight="semibold" /> : null}
      </Pressable>
      <Text selectable numberOfLines={long && !shown ? 2 : undefined} style={[styles.promptText, { color: p.label }]}>{text}</Text>
    </View>
  );
});

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
      return <Markdown text={entry.text ?? ""} color={p.label} maxWidth={width} size={15} />;
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

function ToolView({ tool, title, open, onToggle }: { tool: AgentTool; title?: string; open: boolean; onToggle: () => void }) {
  const p = usePalette();
  const body = hasBody(tool);
  const shown = body && open;
  const diffs = tool.diffs ?? [];
  const added = diffs.reduce((sum, diff) => sum + diff.added, 0);
  const removed = diffs.reduce((sum, diff) => sum + diff.removed, 0);
  const green = accentColor("green", p.dark);
  const red = accentColor("red", p.dark);
  return (
    <View style={{ gap: 7 }}>
      <Pressable disabled={!body} onPress={onToggle} accessibilityRole="button" accessibilityState={{ expanded: shown }} style={styles.head}>
        <Text numberOfLines={1} style={[styles.title, { color: toolFailed(tool) ? red : p.label }]}>
          {title ?? tool.title}
        </Text>
        {!title && tool.detail ? (
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
                {diff.lines.map((line, at) => {
                  const plus = line.startsWith("+");
                  const minus = line.startsWith("-");
                  return (
                    <Text key={at} selectable style={[styles.mono, styles.diffLine, { color: plus ? green : minus ? red : line.startsWith("@@") ? p.tertiaryLabel : p.secondaryLabel, backgroundColor: plus ? alpha(green, 0.1) : minus ? alpha(red, 0.1) : undefined }]}>
                      {line}
                    </Text>
                  );
                })}
              </View>
            </View>
          ))
        : null}
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

/// A command's output in its colors, its last lines in view while it grows.
function OutputView({ output }: { output: string }) {
  const p = usePalette();
  const lines = outputSpans(output);
  const scroll = useRef<ScrollView>(null);
  return (
    <ScrollView ref={scroll} style={[styles.code, styles.output, { backgroundColor: p.code }]} nestedScrollEnabled onContentSizeChange={() => scroll.current?.scrollToEnd({ animated: false })}>
      <Text selectable style={[styles.mono, { color: p.label }]}>
        {lines.map((spans, at) => (
          <Text key={at}>
            {spans.map((span, i) => (
              <Text key={i} style={{ color: spanColor(span, p.dark), fontWeight: span.bold ? "700" : undefined, opacity: span.dim ? 0.6 : undefined }}>
                {span.text}
              </Text>
            ))}
            {at < lines.length - 1 ? "\n" : ""}
          </Text>
        ))}
      </Text>
    </ScrollView>
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
  stepLine: { flexDirection: "row", alignItems: "center", gap: 8, minHeight: 24 },
  stepTitle: { fontSize: 14, fontWeight: "500", flexShrink: 1 },
  prompt: { borderRadius: 12, borderWidth: StyleSheet.hairlineWidth, paddingHorizontal: 12, paddingTop: 9, paddingBottom: 10, gap: 4, marginTop: 6, marginBottom: 14 },
  promptHead: { flexDirection: "row", alignItems: "center", gap: 6 },
  promptAuthor: { fontSize: 11.5, fontWeight: "600" },
  promptText: { fontSize: 14.5, lineHeight: 20 },
  title: { fontSize: 15, fontWeight: "500", flexShrink: 1 },
  detail: { fontFamily: mono, fontSize: 12, flexShrink: 2 },
  preview: { fontSize: 13, flex: 1 },
  small: { fontFamily: mono, fontSize: 12 },
});
