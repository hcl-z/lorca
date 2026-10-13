// Transcript rows, after the Mac app: bubbles (a group shows the bot's name above and its
// avatar beside the bubble's bottom edge; a DM shows neither), "Today 4:13 AM" separators after
// fifteen minutes of silence, the "is working" row, "Chef stopped without replying", and the
// centered "Message from ◉ Name" / "Messaged ◉ Name" markers. Tool calls never render, except a
// command, which shows as its card while it needs the user.

import { memo, useEffect, useMemo, useRef, useState } from "react";
import { Linking, Platform, ScrollView, StyleSheet, Text, TextInput, View } from "react-native";
import { Pressable } from "./Pressable";
import * as Clipboard from "expo-clipboard";
import { haptic } from "./haptics";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, { runOnJS, useAnimatedStyle, useSharedValue, withSequence, withSpring, withTiming } from "react-native-reanimated";
import { LinearGradient } from "expo-linear-gradient";
import { useRouter } from "expo-router";
import { ShimmerView } from "../../modules/lorca-core/ShimmerView";
import { agentIsOpen, agentIsRunning, agentName, agentPlace, agentStarted, canBeQuoted, isLive, isSentMessage, showsCard, showsSpeakers, type AgentRun, type AskQuestion, type Author, type Body, type Bot, type Chat, type CommandRun, type Message } from "../core/model";
import { engine } from "../core/engine";
import { canRegenerate, rerunnable } from "../core/rerun";
import { answerWords, askAnswer, sendsOnPick, togglePick } from "../core/ask";
import { useStore } from "../core/store";
import { language, t, tc, useLanguage } from "../i18n";
import { AttachmentBlock } from "./attachments";
import { OutputCard } from "./outputs";
import { BotAvatar, ContactAvatar } from "./Avatar";
import { daySeparator, firstLine, workingActivity } from "./format";
import { Markdown } from "./Markdown";
import { Symbol } from "./Symbol";
import { usePaneWidth } from "./layout";
import { accentColor, Font, usePalette } from "./theme";
import { alert } from "./alert";
import { draftStateWord, draftTitle, fileSize, sendable, type DraftBody } from "./drafts";
import { secretCaption, secretTitle } from "./secrets";

export const SEPARATOR_GAP_SECS = 15 * 60;
const AVATAR = 28;
const GUTTER = 8;
/// A bubble stops growing here in a wide pane.
const BUBBLE_COLUMN_MAX = 620;
const INSET = 12;
/// A notice's line, and its icon, which centers on the first line as on the Mac.
const NOTICE_LINE = 17;
const NOTICE_ICON = 14;

export type Row =
  | { key: string; type: "day"; at: number }
  | { key: string; type: "message"; message: Message; groupStart: boolean; groupEnd: boolean; showsName: boolean; copyable: boolean; regenerable: boolean; editable: boolean }
  | { key: string; type: "marker"; text: string; bot: Bot | undefined; tooltip?: string; groupStart: boolean }
  | { key: string; type: "notice"; text: string; groupStart: boolean }
  | { key: string; type: "permission"; message: Message; body: Extract<Body, { kind: "permission" }>; bot: Bot | undefined; groupStart: boolean }
  | { key: string; type: "command"; message: Message; run: CommandRun; bot: Bot | undefined; groupStart: boolean }
  | { key: string; type: "agent"; message: Message; agent: AgentRun; bot: Bot | undefined; groupStart: boolean }
  | { key: string; type: "draft"; message: Message; body: DraftBody; bot: Bot | undefined; groupStart: boolean }
  | { key: string; type: "working"; bots: Bot[] }
  | { key: string; type: "status"; text: string };

/// Builds the rows for a chat, the way the Mac app's ChatViewController does.
export function buildRows(chat: Chat, bots: Map<string, Bot>, workingBotIds: string[], isWorking: boolean, status: string | null): Row[] {
  const rows: Row[] = [];
  let previous: Message | null = null;
  let previousAuthorKey: string | null = null;
  const shown = chat.messages.filter((m) => m.body.kind !== "tool" || !!m.body.agent || showsCard(m.body) || isSentMessage(m.body));
  // The latest exchange's actions, under its messages: Copy and Regenerate under the bot's last
  // reply, Edit under the user's message (`src/core/rerun.ts`).
  const run = rerunnable(chat, isWorking);
  const last = shown[shown.length - 1];
  const latestReply = !isWorking && last?.author.kind === "bot" && last.body.kind === "text" && last.state.kind === "complete" && !last.output ? last.id : undefined;
  for (let i = 0; i < shown.length; i++) {
    const message = shown[i];
    const separated = !previous || message.created_at - previous.created_at >= SEPARATOR_GAP_SECS;
    if (separated) {
      rows.push({ key: `day-${message.id}`, type: "day", at: message.created_at });
      previousAuthorKey = null;
    }
    const authorKey = keyOf(message.author);
    const groupStart = separated || authorKey !== previousAuthorKey;
    switch (message.body.kind) {
      case "text": {
        const next = shown[i + 1];
        const nextKey = next ? keyOf(next.author) : null;
        const nextSeparated = next ? next.created_at - message.created_at >= SEPARATOR_GAP_SECS : true;
        const groupEnd = nextSeparated || nextKey !== authorKey || next?.body.kind !== "text";
        rows.push({
          key: message.id,
          type: "message",
          message,
          groupStart,
          groupEnd,
          showsName: showsSpeakers(chat) && (message.author.kind === "bot" || message.author.kind === "contact") && groupStart,
          copyable: message.id === latestReply && message.body.text.trim().length > 0,
          regenerable: message.id === latestReply && canRegenerate(chat, isWorking, message),
          editable: run?.asked.id === message.id,
        });
        previousAuthorKey = authorKey;
        break;
      }
      case "tool":
        if (message.body.agent) {
          const bot = message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined;
          rows.push({ key: message.id, type: "agent", message, agent: message.body.agent, bot, groupStart });
        } else if (message.body.run) {
          const bot = message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined;
          rows.push({ key: message.id, type: "command", message, run: message.body.run, bot, groupStart });
        } else {
          rows.push({ key: message.id, type: "marker", text: t("Messaged"), bot: bots.get(message.body.target_bot_id ?? ""), tooltip: message.body.detail, groupStart });
        }
        previousAuthorKey = null;
        break;
      case "handoff": {
        // From a bot outside the chat, as a DM's request or a handoff's report: a message.
        const incoming = chat.bot_ids.includes(message.body.to) && !chat.bot_ids.includes(message.body.from);
        rows.push({
          key: message.id,
          type: "marker",
          text: incoming ? t("Message from") : t("Handed off to"),
          bot: bots.get(incoming ? message.body.from : message.body.to),
          tooltip: message.body.reason,
          groupStart,
        });
        previousAuthorKey = null;
        break;
      }
      case "notice":
        rows.push({ key: message.id, type: "notice", text: message.body.text, groupStart });
        previousAuthorKey = null;
        break;
      case "permission":
        rows.push({ key: message.id, type: "permission", message, body: message.body, bot: message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined, groupStart });
        previousAuthorKey = null;
        break;
      case "draft":
        rows.push({ key: message.id, type: "draft", message, body: message.body, bot: message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined, groupStart });
        previousAuthorKey = null;
        break;
    }
    previous = message;
  }
  if (isWorking) rows.push({ key: "working", type: "working", bots: workingBotIds.map((id) => bots.get(id)).filter((b): b is Bot => !!b) });
  else if (status) rows.push({ key: "status", type: "status", text: status });
  return rows;
}

/// `next` with every row that says what its predecessor of the same key said replaced by that
/// predecessor, and `previous` itself when nothing changed. Rows are rebuilt on every event in
/// the chat, most of which change one row or none (a tool call the transcript does not show).
export function shareRows(previous: Row[], next: Row[]): Row[] {
  if (previous.length === 0) return next;
  const byKey = new Map(previous.map((row) => [row.key, row]));
  let changed = next.length !== previous.length;
  const shared = next.map((row, index) => {
    const old = byKey.get(row.key);
    const kept = old && sameRow(old, row) ? old : row;
    if (kept !== previous[index]) changed = true;
    return kept;
  });
  return changed ? shared : previous;
}

function sameRow(a: Row, b: Row): boolean {
  const x = a as Record<string, unknown>;
  const y = b as Record<string, unknown>;
  for (const key in y) {
    if (x[key] === y[key]) continue;
    const left = x[key];
    const right = y[key];
    // The working row's bots: the same bots in a new list.
    if (Array.isArray(left) && Array.isArray(right) && left.length === right.length && left.every((item, i) => item === right[i])) continue;
    return false;
  }
  return Object.keys(x).length === Object.keys(y).length;
}

export const DayRow = memo(function DayRow({ at }: { at: number }) {
  useLanguage();
  const p = usePalette();
  return (
    <View style={styles.dayRow}>
      <Text style={[styles.caption, { color: p.secondaryLabel }]}>{daySeparator(new Date(at * 1000))}</Text>
    </View>
  );
});

/// Swiping a bubble this far to the right makes the draft a reply to it.
const REPLY_SWIPE = 56;

/// Who wrote a quoted message, as a reply's quote names them.
export function quoteAuthorName(author: Author, bots: Map<string, Bot>): string {
  if (author.kind === "you") return t("You");
  if (author.kind === "bot") return bots.get(author.bot_id)?.name ?? t("Bot");
  if (author.kind === "contact") return author.name;
  return "Lorca";
}

/// Who a run of bubbles belongs to: the user, one bot, or one person outside Lorca.
function keyOf(author: Author): string {
  if (author.kind === "bot") return `bot:${author.bot_id}`;
  if (author.kind === "contact") return `contact:${author.name}`;
  return author.kind;
}

/// How far from the screen's left edge a drag stays the system's back gesture.
const BACK_EDGE = 28;

/// Swipe to reply, as in Messages and Google Messages: the bubble, dragged to the right, takes its
/// row along with a reply arrow fading in behind it; let go past `REPLY_SWIPE` and the draft
/// answers it. Only a drag that starts on the bubble, away from the left edge, is a reply: one
/// anywhere else stays the system's back gesture (iOS 26 takes it from the whole content), as does
/// a leftward one, and vertical ones stay with the transcript. The drag runs on the UI thread, so
/// the bubble follows the finger while JS is busy with a streaming reply.
function useSwipeToReply(onReply?: () => void) {
  const offset = useSharedValue(0);
  const armed = useSharedValue(false);
  const pan = useMemo(() => {
    const reply = () => onReply?.();
    const tick = haptic.threshold;
    return Gesture.Pan()
      .enabled(!!onReply)
      .activeOffsetX(14)
      .failOffsetX(-10)
      .failOffsetY([-10, 10])
      .onTouchesDown((event, manager) => {
        if ((event.allTouches[0]?.absoluteX ?? 0) < BACK_EDGE) manager.fail();
      })
      .onUpdate((event) => {
        offset.value = Math.max(0, Math.min(REPLY_SWIPE * 1.4, event.translationX));
        const past = offset.value >= REPLY_SWIPE;
        if (past !== armed.value) {
          armed.value = past;
          if (past) runOnJS(tick)();
        }
      })
      .onEnd(() => {
        if (armed.value) runOnJS(reply)();
      })
      .onFinalize(() => {
        armed.value = false;
        offset.value = withSpring(0, { damping: 22, stiffness: 260 });
      });
  }, [onReply, offset, armed]);
  const follow = useAnimatedStyle(() => ({ transform: [{ translateX: offset.value }] }));
  const arrow = useAnimatedStyle(() => {
    const progress = Math.min(1, offset.value / REPLY_SWIPE);
    return { opacity: progress, transform: [{ scale: 0.6 + 0.4 * progress }] };
  });
  return { pan, follow, arrow };
}

/// `onReply` makes the draft a reply to this message (a swipe to the left), when it can be quoted;
/// `onQuotePress` brings the message a reply answers into view; `flashing` pulses the bubble once
/// it is there. Rows are built anew on every change to the chat, but their messages keep their
/// identity until they change, so a bubble renders again only when its own message or place
/// in the run does: a streaming reply re-renders its own bubble, not the screenful above it.
export const MessageRow = memo(function MessageRow({
  row,
  bots,
  isGroup,
  onReply,
  onQuotePress,
  onEdit,
  onRegenerate,
  flashing,
}: {
  row: Extract<Row, { type: "message" }>;
  bots: Map<string, Bot>;
  isGroup: boolean;
  onReply?: (message: Message) => void;
  onQuotePress?: (messageID: string) => void;
  onEdit?: (message: Message) => void;
  onRegenerate?: (message: Message) => void;
  flashing?: boolean;
}) {
  const held = row.message.queued === true;
  const reply = useMemo(() => (onReply && canBeQuoted(row.message) ? () => onReply(row.message) : undefined), [onReply, row.message]);
  useLanguage();
  const p = usePalette();
  const paneWidth = usePaneWidth();
  const { message, groupStart, groupEnd, showsName } = row;
  const quote = message.body.kind === "text" ? message.body.reply_to : undefined;
  const pulse = useSharedValue(1);
  useEffect(() => {
    if (!flashing) return;
    pulse.value = withSequence(withTiming(0.35, { duration: 260 }), withTiming(1, { duration: 260 }), withTiming(0.35, { duration: 260 }), withTiming(1, { duration: 260 }));
  }, [flashing, pulse]);
  const pulsing = useAnimatedStyle(() => ({ opacity: pulse.value }));
  const isYou = message.author.kind === "you";
  const bot = message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined;
  const contact = message.author.kind === "contact" ? message.author.name : undefined;
  const text = message.body.kind === "text" ? message.body.text : "";
  const attachments = message.body.kind === "text" ? (message.body.attachments ?? []) : [];
  const failed = message.state.kind === "failed";
  const showsAvatar = isGroup && !isYou;
  // A bot's published output is a card of its own, without a bubble.
  const isOutput = message.output !== undefined && !isYou;
  // The bubble column is 80% of the pane, up to a readable line; the bubble pads 13 a side.
  // Attachments and the text both fit that width.
  const columnWidth = Math.min(Math.floor(paneWidth * 0.8), BUBBLE_COLUMN_MAX);
  const attachmentWidth = columnWidth - 26 - (showsAvatar ? AVATAR + GUTTER : 0);
  const quoteName = quote ? quoteAuthorName(quote.author, bots) : "";
  const swipe = useSwipeToReply(reply);
  return (
    <View>
    <Animated.View style={[styles.replyArrow, swipe.arrow]} pointerEvents="none">
      <Symbol name="arrowshape.turn.up.left.fill" size={16} color={p.secondaryLabel} />
    </Animated.View>
    <Animated.View style={swipe.follow}>
    <View style={[styles.messageRow, { paddingTop: groupStart ? 14 : 3 }, isYou ? styles.messageRowYou : styles.messageRowBot]}>
      {showsAvatar && (
        <View style={{ width: AVATAR + GUTTER, alignSelf: "flex-end" }}>
          {groupEnd && (contact !== undefined ? <ContactAvatar name={contact} size={AVATAR} /> : <BotAvatar bot={bot} size={AVATAR} />)}
        </View>
      )}
      <View style={[styles.bubbleColumn, { maxWidth: columnWidth }, isYou && styles.bubbleColumnYou]}>
        {showsName && (
          <Text style={[styles.author, { color: p.secondaryLabel }]} numberOfLines={1}>
            {contact ?? bot?.name ?? t("Bot")}
          </Text>
        )}
        {quote && (
          <Pressable
            onPress={() => onQuotePress?.(quote.message_id)}
            hitSlop={6}
            style={[styles.quote, isYou && styles.quoteYou]}
            accessibilityRole="button"
            accessibilityLabel={t("In reply to {name}: {text}", { name: quoteName, text: quote.text })}
          >
            <Symbol name="arrowshape.turn.up.left.fill" size={10} color={p.tertiaryLabel} />
            <Text style={[styles.quoteText, { color: p.secondaryLabel }]} numberOfLines={1}>
              <Text style={styles.quoteName}>{quoteName}:</Text> {quote.text}
            </Text>
          </Pressable>
        )}
        <GestureDetector gesture={swipe.pan}>
        {isOutput ? (
          <Animated.View style={pulsing}>
            <OutputCard message={message} maxWidth={columnWidth - (showsAvatar ? AVATAR + GUTTER : 0)} />
          </Animated.View>
        ) : (
        <Animated.View
          style={[
            styles.bubble,
            isYou ? [styles.bubbleYou, { backgroundColor: p.userBubble }] : { backgroundColor: failed ? "rgba(255,59,48,0.14)" : p.botBubble },
            pulsing,
            held && styles.held,
          ]}
        >
          {attachments.length > 0 && <AttachmentBlock attachments={attachments} onUserBubble={isYou} maxWidth={attachmentWidth} />}
          {text.length > 0 && <Markdown text={text} color={isYou ? p.userBubbleText : p.botBubbleText} maxWidth={attachmentWidth} sources={message.author.kind === "bot"} />}
          {failed && (
            <View style={styles.bubbleFooter}>
              <Symbol name="exclamationmark.triangle.fill" size={11} color={p.red} />
            </View>
          )}
        </Animated.View>
        )}
        </GestureDetector>
        {(row.copyable || row.regenerable || row.editable || message.edited_at) && !held ? (
          <MessageActions row={row} isYou={isYou} text={text} onEdit={onEdit} onRegenerate={onRegenerate} />
        ) : null}
        {held && (
          <Pressable
            onPress={() => engine.sendNow(message.chat_id, message.id).catch((error) => alert(t("Could not send now"), error instanceof Error ? error.message : String(error)))}
            hitSlop={8}
            style={styles.sendNow}
            accessibilityRole="button"
            accessibilityHint={t("Have the bot read this now. A command it is running moves to the background.")}
          >
            <Text style={[styles.sendNowText, { color: p.tint }]}>{t("Send now")}</Text>
          </Pressable>
        )}
      </View>
    </View>
    </Animated.View>
    </View>
  );
}, (a, b) =>
  a.row.message === b.row.message &&
  a.row.groupStart === b.row.groupStart &&
  a.row.groupEnd === b.row.groupEnd &&
  a.row.showsName === b.row.showsName &&
  a.row.copyable === b.row.copyable &&
  a.row.regenerable === b.row.regenerable &&
  a.row.editable === b.row.editable &&
  a.onEdit === b.onEdit &&
  a.onRegenerate === b.onRegenerate &&
  a.bots === b.bots &&
  a.isGroup === b.isGroup &&
  a.onReply === b.onReply &&
  a.onQuotePress === b.onQuotePress &&
  a.flashing === b.flashing,
);

/// The small round actions under the latest exchange, in the message's own words when they say
/// something (Edited): Copy and Regenerate under the bot's reply, Edit under the user's message.
/// Not a long press: that selects the message's text in place.
function MessageActions({ row, isYou, text, onEdit, onRegenerate }: { row: Extract<Row, { type: "message" }>; isYou: boolean; text: string; onEdit?: (message: Message) => void; onRegenerate?: (message: Message) => void }) {
  useLanguage();
  const p = usePalette();
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  const action = (icon: string, label: string, onPress: () => void) => (
    <Pressable key={label} onPress={onPress} hitSlop={6} ripple="borderless" rippleRadius={16} style={({ pressed }) => [styles.action, { opacity: pressed ? 0.5 : 1 }]} accessibilityRole="button" accessibilityLabel={label}>
      <Symbol name={icon} size={15} color={p.secondaryLabel} />
    </Pressable>
  );
  return (
    <View style={[styles.actions, isYou && styles.actionsYou]}>
      {isYou && row.message.edited_at ? <Text style={[styles.edited, { color: p.tertiaryLabel }]}>{t("Edited")}</Text> : null}
      {row.copyable
        ? action(copied ? "checkmark" : "doc.on.doc", copied ? t("Copied") : t("Copy message"), () => {
            void Clipboard.setStringAsync(text);
            haptic.success();
            setCopied(true);
          })
        : null}
      {row.regenerable && onRegenerate ? action("arrow.triangle.2.circlepath", t("Regenerate"), () => onRegenerate(row.message)) : null}
      {row.editable && onEdit ? action("pencil", t("Edit"), () => onEdit(row.message)) : null}
    </View>
  );
}

/// "Messaged ◉ Name" with the message's first line under it; a tap opens the whole message
/// in a sheet.
export const MarkerRow = memo(function MarkerRow({ row, onPress }: { row: Extract<Row, { type: "marker" }>; onPress?: (row: Extract<Row, { type: "marker" }>) => void }) {
  useLanguage();
  const p = usePalette();
  // Bold and code marks go, as the chat list's preview drops them.
  const preview = row.tooltip ? firstLine(row.tooltip).replace(/\*\*|`/g, "") : "";
  return (
    <Pressable
      style={({ pressed }) => [styles.centered, { paddingTop: row.groupStart ? 14 : 6, opacity: pressed ? 0.5 : 1 }]}
      onPress={preview ? () => onPress?.(row) : undefined}
      disabled={!preview}
      accessibilityRole={preview ? "button" : undefined}
      accessibilityLabel={`${row.text} ${row.bot?.name ?? t("a teammate")}${preview ? `: ${row.tooltip}` : ""}`}
    >
      <View style={styles.markerLine}>
        <Text style={[styles.caption, { color: p.secondaryLabel }]}>{row.text} </Text>
        <BotAvatar bot={row.bot} size={14} />
        <Text style={[styles.caption, { color: p.secondaryLabel, fontWeight: "600" }]}> {row.bot?.name ?? t("a teammate")}</Text>
      </View>
      {preview ? (
        <Text style={[styles.caption, { color: p.tertiaryLabel, marginTop: 3, maxWidth: 300, textAlign: "center" }]} numberOfLines={1}>
          {preview}
        </Text>
      ) : null}
    </Pressable>
  );
});

export const NoticeRow = memo(function NoticeRow({ row }: { row: Extract<Row, { type: "notice" }> }) {
  const p = usePalette();
  return (
    <View style={[styles.centered, { paddingTop: row.groupStart ? 14 : 6 }]}>
      <View style={[styles.notice, { backgroundColor: p.fill }]}>
        <Symbol name="info.circle.fill" size={NOTICE_ICON} color={p.secondaryLabel} style={styles.noticeIcon} />
        <Text style={[styles.noticeText, { color: p.secondaryLabel }]}>{row.text}</Text>
      </View>
    </View>
  );
});

/// A bot asking before a plugin tool runs, a shell command runs, or a plugin is installed. While
/// it waits: the question, the call (a shell command in a code block that opens the whole
/// command on tap), why Auto-review paused it, the answers, and under them the rule Always allow
/// adds. A shell command offers Always allow only with a rule. Once answered, the answer and the
/// call; an Always allow keeps its rule. A bot's Access refusing a call asks for more access: Edit
/// Access… opens the bot's Access, and Dismiss puts the request away; neither runs the call. In a
/// group the card sits in the bubbles' column, the bot's avatar beside its bottom edge.
/// One of a card's choices: ink for the first, gray for the rest.
function CardButton({ label, primary = false, disabled = false, onPress }: { label: string; primary?: boolean; disabled?: boolean; onPress: () => void }) {
  const p = usePalette();
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      style={({ pressed }) => [
        styles.cardButton,
        primary ? { backgroundColor: p.ink } : { backgroundColor: p.fill, borderWidth: StyleSheet.hairlineWidth, borderColor: p.cardBorder },
        { opacity: disabled ? 0.45 : pressed ? 0.75 : 1 },
      ]}
    >
      <Text style={[styles.cardButtonText, { color: primary ? p.inkText : p.label }]} numberOfLines={1}>
        {label}
      </Text>
    </Pressable>
  );
}

export const PermissionRow = memo(function PermissionRow({ row, isGroup, onDecide, onFill }: { row: Extract<Row, { type: "permission" }>; isGroup: boolean; onDecide: (message: Message, decision: "allow" | "always" | "deny") => void; onFill: (message: Message) => void }) {
  if (row.body.tool === "secret" && row.body.secret) return <SecretRow row={row} isGroup={isGroup} onDecide={onDecide} onFill={onFill} />;
  if (row.body.tool === "ask" && row.body.ask) return <QuestionRow row={row} ask={row.body.ask} isGroup={isGroup} onDecide={onDecide} />;
  return <AskRow row={row} isGroup={isGroup} onDecide={onDecide} />;
});

/// A bot asking for a secret, as the Mac's card: who asks and where it goes, why, Fill In, which
/// opens `SecretSheet` with a field for each value, and Not now, then that the value stays on the
/// Runner and the bot never sees it. Once answered, the answer and what was asked for: "Saved · npm
/// token". In a group the card sits in the bubbles' column, the bot's avatar beside its bottom edge.
function SecretRow({ row, isGroup, onDecide, onFill }: { row: Extract<Row, { type: "permission" }>; isGroup: boolean; onDecide: (message: Message, decision: "allow" | "always" | "deny") => void; onFill: (message: Message) => void }) {
  useLanguage();
  const p = usePalette();
  const runner = useStore((s) => s.devices.find((d) => d.id === row.bot?.runner_id)?.name) ?? t("its Runner");
  const showsAvatar = isGroup && row.message.author.kind === "bot";
  const body = row.body;
  const pending = body.decision === "pending";
  const who = row.bot?.name ?? t("The bot");
  const title = secretTitle(body, who);
  const caption = secretCaption(body);
  return (
    <View style={[styles.messageRow, { paddingTop: row.groupStart ? 14 : 6 }]}>
      {showsAvatar && (
        <View style={{ width: AVATAR + GUTTER, alignSelf: "flex-end" }}>
          <BotAvatar bot={row.bot} size={AVATAR} />
        </View>
      )}
      <View style={[styles.permission, { backgroundColor: p.botBubble }]}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Symbol name="key" size={16} color={pending ? p.tint : p.secondaryLabel} />
          <Text style={[styles.permissionTitle, { color: p.label }]} numberOfLines={2}>
            {title}
          </Text>
        </View>
        {caption ? (
          <Text style={[pending ? styles.reasonText : styles.caption, { color: p.secondaryLabel }]} numberOfLines={4}>
            {caption}
          </Text>
        ) : null}
        {pending ? (
          <>
            <View style={{ flexDirection: "row", gap: 8, marginTop: 4 }}>
              <CardButton label={t("Fill In")} primary onPress={() => onFill(row.message)} />
              <CardButton label={t("Not now")} onPress={() => onDecide(row.message, "deny")} />
            </View>
            <Text style={[styles.ruleNote, { color: p.secondaryLabel }]}>{t("Saved on {runner}. {who} never sees it.", { runner, who })}</Text>
          </>
        ) : null}
      </View>
    </View>
  );
}

const AskRow = memo(function AskRow({ row, isGroup, onDecide }: { row: Extract<Row, { type: "permission" }>; isGroup: boolean; onDecide: (message: Message, decision: "allow" | "always" | "deny") => void }) {
  useLanguage();
  const p = usePalette();
  const [copied, setCopied] = useState(false);
  const router = useRouter();
  const showsAvatar = isGroup && row.message.author.kind === "bot";
  const pending = row.body.decision === "pending";
  const connect = row.body.tool === "connect";
  const access = row.body.tool === "access";
  // A question with options: listed under it, answered by a reply in the chat, or skipped.
  const question = row.body.tool === "ask" ? row.body.ask : undefined;
  const shell = row.body.plugin_id === "computer" && !access && !question;
  const who = row.bot?.name ?? t("The bot");
  const plugin = row.body.plugin_name;
  const title = question
    ? t("{who} asks you to choose", { who })
    : access
    ? t("{who} needs more access", { who })
    : connect
    ? t("{who} needs a sign-in to {plugin}", { who, plugin })
    : row.body.tool === "install"
      ? t("{who} wants to install {plugin}", { who, plugin })
      : shell
        ? t("{who} wants to run a command on {plugin}", { who, plugin })
        : t("{who} wants to use {plugin}", { who, plugin });
  const command = row.body.command ?? row.body.summary.replace(/^\$ /, "");
  // What an access request names: a plugin's tool as it is, or what the bot wanted to do on its
  // Runner in the CLI's English.
  const local: Record<string, string> = { "Shell commands": t("Shell commands"), "Changing files": t("Changing files"), "Reading files": t("Reading files") };
  const summary = question
    ? [question.question, question.options.map((option, i) => `${i + 1}. ${option.label}`).join("  ")].join("\n")
    : access ? (local[row.body.summary] ?? row.body.summary) : row.body.summary;
  // What was picked on a computer: the options' labels, then the user's own words.
  const answered = question?.answer
    ? [...(question.answer.choices ?? []).map((i) => question.options[i]?.label).filter(Boolean), ...(question.answer.other ? [`“${question.answer.other}”`] : [])].join(t(", "))
    : "";
  const reason = question ? t("Reply in the chat with your choice.") : access ? t("Not allowed in this bot's Access settings.") : row.body.reason;
  const ruleNote = !row.body.rule
    ? undefined
    : pending
      ? t("Always allow adds the rule “{rule}”.", { rule: row.body.rule })
      : row.body.decision === "always"
        ? t("Added the rule “{rule}” to Auto-review.", { rule: row.body.rule })
        : undefined;
  const decided: Record<string, string> = question
    ? { allowed: t("Answered"), denied: t("Skipped"), expired: t("No answer in time"), dismissed: t("Answered in the chat instead") }
    : connect
    ? { allowed: t("Signing in"), denied: t("Not now"), dismissed: t("Dismissed"), connected: t("Signed in"), failed: t("Sign-in failed") }
    : { allowed: t("Allowed once"), always: t("Always allowed"), denied: t("Denied"), expired: t("No answer in time"), dismissed: t("Dismissed") };
  const choices: [string, "allow" | "always" | "deny" | "access"][] = question
    ? [[t("Skip"), "deny"]]
    : access
    ? [[t("Edit Access…"), "access"], [t("Dismiss"), "deny"]]
    : connect
    ? [[t("Sign in"), "allow"], [t("Not now"), "deny"]]
    : row.body.tool === "install"
      ? [[t("Allow"), "allow"], [t("Deny"), "deny"]]
      : shell && !row.body.rule
        ? [[t("Allow once"), "allow"], [t("Deny"), "deny"]]
        : [[t("Allow once"), "allow"], [t("Always allow"), "always"], [t("Deny"), "deny"]];
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  return (
    <View style={[styles.messageRow, { paddingTop: row.groupStart ? 14 : 6 }]}>
      {showsAvatar && (
        <View style={{ width: AVATAR + GUTTER, alignSelf: "flex-end" }}>
          <BotAvatar bot={row.bot} size={AVATAR} />
        </View>
      )}
      <View style={[styles.permission, { backgroundColor: p.botBubble }]}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Symbol name={connect ? "person.crop.circle.badge.checkmark" : row.body.tool === "install" ? "puzzlepiece.extension" : "hand.raised"} size={16} color={row.body.decision === "failed" ? p.red : row.body.decision === "connected" ? p.green : p.tint} />
          <Text style={[styles.permissionTitle, { color: p.label }]} numberOfLines={2}>
            {title}
          </Text>
        </View>
        {pending && shell ? (
          <Pressable
            onPress={() => router.push({ pathname: "/command/[id]", params: { id: row.message.id, chat: row.message.chat_id, title } })}
            style={({ pressed }) => [styles.command, { backgroundColor: p.innerCard, opacity: pressed ? 0.6 : 1 }]}
            accessibilityRole="button"
            accessibilityLabel={t("Show the full command")}
          >
            <Text style={[styles.commandText, { color: p.label }]} numberOfLines={2}>
              {command}
            </Text>
          </Pressable>
        ) : (
          <Text style={[styles.caption, { color: p.secondaryLabel }]} numberOfLines={3}>
            {pending ? summary : answered ? `${question?.question} · ${t("You chose {answer}", { answer: answered })}` : `${decided[row.body.decision] ?? row.body.decision} · ${summary}`}
          </Text>
        )}
        {pending && reason ? (
          <Text style={[styles.reasonText, { color: p.secondaryLabel }]}>{reason}</Text>
        ) : null}
        {row.body.decision === "allowed" && row.body.code ? (
          <View style={{ flexDirection: "row", alignItems: "center", gap: 10, marginTop: 4 }}>
            <Text selectable style={{ color: p.label, fontSize: 17, fontWeight: "700", fontFamily: "Menlo" }}>{row.body.code}</Text>
            <Pressable
              onPress={async () => {
                if (row.body.code) {
                  await Clipboard.setStringAsync(row.body.code);
                  setCopied(true);
                }
                if (row.body.link) void Linking.openURL(row.body.link);
              }}
              style={({ pressed }) => [styles.permissionButton, { backgroundColor: p.ink, opacity: pressed ? 0.8 : 1 }]}
              accessibilityRole="button"
              accessibilityLabel={copied ? t("Copied") : t("Copy code and open")}
            >
              <View style={{ flexDirection: "row", alignItems: "center", gap: 5 }}>
                {copied ? <Symbol name="checkmark" size={13} color={p.inkText} weight="semibold" /> : null}
                <Text style={{ color: p.inkText, fontSize: 13, fontWeight: "600" }}>
                  {copied ? t("Copied") : t("Copy code and open")}
                </Text>
              </View>
            </Pressable>
          </View>
        ) : null}
        {pending ? (
          <View style={{ flexDirection: "row", gap: 8, marginTop: 4 }}>
            {choices.map(([label, decision], index) => (
              <CardButton
                key={decision}
                label={label}
                primary={index === 0}
                onPress={() =>
                  decision === "access"
                    ? row.message.author.kind === "bot" && router.push({ pathname: "/chat-info/access/[id]", params: { id: row.message.author.bot_id, close: "1" } })
                    : onDecide(row.message, decision)
                }
              />
            ))}
          </View>
        ) : null}
        {ruleNote ? <Text style={[styles.ruleNote, { color: p.secondaryLabel }]}>{ruleNote}</Text> : null}
      </View>
    </View>
  );
});

/// A bot's question with options, as the desktop's card: the question, then each option as a row
/// with its number, description, and Recommended on the bot's pick, a radio for one pick or a box
/// for several, and Other… for the user's own words where the question takes them. One pick sends
/// on its tap; several, or Other, send with Submit. Skip leaves the choice to the bot. A refusal
/// from the Runner (offline, expired) puts the card back with why under it. Answered, it keeps the
/// question and says what was picked.
const QuestionRow = memo(function QuestionRow({ row, ask, isGroup, onDecide }: { row: Extract<Row, { type: "permission" }>; ask: AskQuestion; isGroup: boolean; onDecide: (message: Message, decision: "allow" | "always" | "deny") => void }) {
  useLanguage();
  const p = usePalette();
  const [picked, setPicked] = useState<number[]>([]);
  const [writing, setWriting] = useState(false);
  const [other, setOther] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const showsAvatar = isGroup && row.message.author.kind === "bot";
  const pending = row.body.decision === "pending";
  const who = row.bot?.name ?? t("The bot");
  const send = (choices: number[], words: string) => {
    const result = askAnswer(ask, choices, words);
    if ("error" in result) return;
    setBusy(true);
    setProblem(null);
    haptic.send();
    engine
      .answerPermission(row.message.chat_id, row.message.id, "allow", result.answer)
      .catch((error) => setProblem(error instanceof Error ? error.message : String(error)))
      .finally(() => setBusy(false));
  };
  const pick = (index: number) => {
    if (busy) return;
    const next = togglePick(ask, picked, index);
    setPicked(next);
    if (sendsOnPick(ask)) {
      setWriting(false);
      send(next, "");
    }
  };
  const canSubmit = "answer" in askAnswer(ask, ask.multiple ? picked : [], writing ? other : "");
  const decided: Record<string, string> = { allowed: t("Answered"), denied: t("Skipped"), expired: t("No answer in time"), dismissed: t("Answered in the chat instead") };
  const answered = answerWords(ask, t(", "));
  return (
    <View style={[styles.messageRow, { paddingTop: row.groupStart ? 14 : 6 }]}>
      {showsAvatar && (
        <View style={{ width: AVATAR + GUTTER, alignSelf: "flex-end" }}>
          <BotAvatar bot={row.bot} size={AVATAR} />
        </View>
      )}
      <View style={[styles.permission, { backgroundColor: p.botBubble }]}>
        <Text style={[styles.caption, { color: p.secondaryLabel }]}>{t("{who} asks you to choose", { who })}</Text>
        <Text style={[styles.questionText, { color: p.label }]}>{ask.question}</Text>
        {pending ? (
          <>
            <View style={[styles.innerCard, styles.options, { backgroundColor: p.innerCard }]}>
              {ask.options.map((option, index) => {
                const on = picked.includes(index);
                return (
                  <Pressable
                    key={`${index}-${option.label}`}
                    onPress={() => pick(index)}
                    disabled={busy}
                    style={({ pressed }) => [styles.option, index > 0 && { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: p.separator }, { opacity: pressed ? 0.6 : 1 }]}
                    accessibilityRole={ask.multiple ? "checkbox" : "radio"}
                    accessibilityState={{ checked: on, disabled: busy }}
                  >
                    <Symbol name={ask.multiple ? (on ? "checkmark.square.fill" : "square") : on ? "largecircle.fill.circle" : "circle"} size={19} color={on ? p.ink : p.tertiaryLabel} />
                    <View style={{ flex: 1, gap: 2 }}>
                      <View style={{ flexDirection: "row", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                        <Text style={[styles.optionLabel, { color: p.label }]}>{`${index + 1}. ${option.label}`}</Text>
                        {ask.recommended === index ? (
                          <View style={[styles.recommended, { backgroundColor: p.fill }]}>
                            <Text style={[styles.recommendedText, { color: p.secondaryLabel }]}>{t("Recommended")}</Text>
                          </View>
                        ) : null}
                      </View>
                      {option.description ? <Text style={[styles.reasonText, { color: p.secondaryLabel }]}>{option.description}</Text> : null}
                    </View>
                  </Pressable>
                );
              })}
              {ask.allow_other ? (
                <Pressable
                  onPress={() => {
                    setWriting(true);
                    if (!ask.multiple) setPicked([]);
                  }}
                  disabled={busy || writing}
                  style={[styles.option, { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: p.separator }]}
                  accessibilityRole="button"
                >
                  <Symbol name="pencil" size={17} color={writing ? p.ink : p.tertiaryLabel} />
                  {writing ? (
                    <TextInput
                      autoFocus
                      value={other}
                      onChangeText={setOther}
                      placeholder={t("Your own answer")}
                      placeholderTextColor={p.tertiaryLabel as any}
                      style={[styles.otherInput, { color: p.label }]}
                      keyboardAppearance={p.dark ? "dark" : "light"}
                      returnKeyType="send"
                      onSubmitEditing={() => canSubmit && send(ask.multiple ? picked : [], other)}
                    />
                  ) : (
                    <Text style={[styles.optionLabel, { color: p.secondaryLabel }]}>{t("Other…")}</Text>
                  )}
                </Pressable>
              ) : null}
            </View>
            {problem ? <Text style={[styles.reasonText, { color: p.red }]}>{problem}</Text> : null}
            <View style={styles.cardButtons}>
              {ask.multiple || writing ? <CardButton label={t("Submit")} primary disabled={busy || !canSubmit} onPress={() => send(ask.multiple ? picked : [], writing ? other : "")} /> : null}
              <CardButton label={t("Skip")} disabled={busy} onPress={() => onDecide(row.message, "deny")} />
            </View>
          </>
        ) : (
          <Text style={[styles.reasonText, { color: p.secondaryLabel }]}>
            {answered ? t("You chose {answer}", { answer: answered }) : (decided[row.body.decision] ?? row.body.decision)}
          </Text>
        )}
      </View>
    </View>
  );
});

/// An email or Slack message a bot wrote, as a card: who drafted it and the account it goes out
/// from, To and Cc, the subject, the start of the text, and the attachments; while it waits, Send,
/// Always Send on a Slack card (which also has the bot send its next messages directly), and
/// Discard. A tap on the card opens the draft to edit, as Mail's compose sheet. Once sent or
/// discarded, how it ended on the title's line. In a group the card sits in the bubbles' column,
/// the bot's avatar beside its bottom edge.
export const DraftRow = memo(function DraftRow({ row, isGroup }: { row: Extract<Row, { type: "draft" }>; isGroup: boolean }) {
  useLanguage();
  const p = usePalette();
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const showsAvatar = isGroup && row.message.author.kind === "bot";
  const card = row.body;
  const draft = card.draft;
  const pending = card.state === "pending";
  const state = draftStateWord(card.state);
  const title = draftTitle(draft, row.bot?.name ?? t("The bot"));
  const open = () => router.push({ pathname: "/draft/[id]", params: { id: row.message.id, chat: row.message.chat_id } });
  const act = async (failure: string, request: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    try {
      await request();
    } catch (error) {
      alert(failure, error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };
  const send = (always: boolean) => act(t("Couldn't send this draft"), () => engine.sendDraft(row.message.chat_id, row.message.id, draft, always));
  const header = (label: string, values?: string[]) =>
    values && values.length > 0 ? (
      <Text style={[styles.draftHeader, { color: p.secondaryLabel }]} numberOfLines={1}>
        {label} <Text style={{ color: p.label }}>{values.join(", ")}</Text>
      </Text>
    ) : null;
  const buttons: [string, () => void, boolean][] = [
    [draft.kind === "email" ? t("Send email") : t("Send"), () => void send(false), true],
    ...(card.direct ? [[t("Always Send"), () => void send(true), true] as [string, () => void, boolean]] : []),
    [t("Discard"), () => void act(t("Couldn't discard this draft"), () => engine.discardDraft(row.message.chat_id, row.message.id)), false],
  ];
  return (
    <View style={[styles.messageRow, { paddingTop: row.groupStart ? 14 : 6 }]}>
      {showsAvatar && (
        <View style={{ width: AVATAR + GUTTER, alignSelf: "flex-end" }}>
          <BotAvatar bot={row.bot} size={AVATAR} />
        </View>
      )}
      <Pressable
        onPress={open}
        accessibilityRole="button"
        accessibilityHint={pending ? t("Opens the draft to edit") : undefined}
        style={({ pressed }) => [styles.permission, { backgroundColor: p.botBubble, opacity: pressed ? 0.85 : 1 }]}
      >
        <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Text style={[styles.permissionTitle, { color: p.label, flex: 1 }]} numberOfLines={2}>
            {title}
          </Text>
          {state ? (
            <Text style={[styles.caption, { color: card.state === "failed" ? p.red : card.state === "uncertain" ? accentColor("orange", p.dark) : p.secondaryLabel }]}>{state}</Text>
          ) : null}
        </View>
        <View style={[styles.innerCard, { backgroundColor: p.innerCard }]}>
          <View style={{ gap: 2 }}>
            <Text style={[styles.draftHeader, { color: p.secondaryLabel }]} numberOfLines={1}>
              {card.account}
            </Text>
            {header(t("To"), draft.to)}
            {header(t("Cc"), draft.cc)}
            {header(t("Bcc"), draft.bcc)}
          </View>
          {draft.subject ? (
            <Text style={[styles.draftSubject, { color: p.label }]} numberOfLines={2}>
              {draft.subject}
            </Text>
          ) : null}
          <Text style={[styles.draftBody, { color: p.label }]} numberOfLines={10}>
            {draft.body}
          </Text>
          {(draft.attachments ?? []).map((file, index) => (
            <View key={`${file.name}-${index}`} style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
              <Symbol name="paperclip" size={13} color={p.secondaryLabel} />
              <Text style={[styles.reasonText, { color: p.label, flexShrink: 1 }]} numberOfLines={1}>
                {file.name}
              </Text>
              {file.size > 0 ? <Text style={[styles.caption, { color: p.secondaryLabel }]}>{fileSize(file.size)}</Text> : null}
            </View>
          ))}
        </View>
        {card.note ? <Text style={[styles.ruleNote, { color: card.state === "failed" ? p.red : accentColor("orange", p.dark) }]}>{card.note}</Text> : null}
        {pending ? (
          <View style={{ flexDirection: "row", gap: 8, marginTop: 4 }}>
            {buttons.map(([label, onPress, tinted], index) => (
              <CardButton key={label} label={label} primary={index === 0} disabled={busy || (tinted && !sendable(draft))} onPress={onPress} />
            ))}
          </View>
        ) : null}
        {pending && card.direct ? <Text style={[styles.ruleNote, { color: p.secondaryLabel }]}>{t("Always Send also sends this bot's next Slack messages directly.")}</Text> : null}
      </Pressable>
    </View>
  );
});

/// A command's card, while the command needs the user (`showsCard`). While Auto-review asks to run
/// it: who wants to, the command on one line in a code block that opens the whole command on tap,
/// why, the answers, and the rule Always allow adds. Once the bot handed the running command over:
/// Stop on the title's line, the command, and its last lines in a code block of their own that
/// scrolls; at a question, Answer, which opens `AnswerSheet`. In a group the card sits in the
/// bubbles' column, the bot's avatar beside its bottom edge.
export const CommandRow = memo(function CommandRow({
  row,
  isGroup,
  onDecide,
  onAnswer,
  onStop,
}: {
  row: Extract<Row, { type: "command" }>;
  isGroup: boolean;
  onDecide: (message: Message, decision: "allow" | "always" | "deny") => void;
  onAnswer: (message: Message) => void;
  onStop: (message: Message) => Promise<void>;
}) {
  useLanguage();
  const p = usePalette();
  const [stopping, setStopping] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();
  const { run } = row;
  const showsAvatar = isGroup && row.message.author.kind === "bot";
  const who = row.bot?.name ?? t("The bot");
  const command = firstLine(run.command);
  // A new question clears what the last answer or Stop said.
  useEffect(() => setError(null), [run.output, run.state]);
  const title =
    run.state === "asking"
      ? run.device
        ? t("{who} wants to run a command on {plugin}", { who, plugin: run.device })
        : t("{who}'s command", { who })
      : run.state === "running"
        ? t("{who}'s command is running", { who })
        : t("{who}'s command is waiting for input", { who });
  const caption = run.state === "asking" ? run.reason : undefined;
  const output = isLive(run) ? (run.output ?? "").split("\n").filter(Boolean).join("\n") : "";
  const takesInput = isLive(run) && !!run.session_id;
  const choices: [string, "allow" | "always" | "deny"][] = run.rule
    ? [[t("Allow once"), "allow"], [t("Always allow"), "always"], [t("Deny"), "deny"]]
    : [[t("Allow once"), "allow"], [t("Deny"), "deny"]];
  const stop = async () => {
    setStopping(true);
    setError(null);
    try {
      await onStop(row.message);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setStopping(false);
    }
  };
  return (
    <View style={[styles.messageRow, { paddingTop: row.groupStart ? 14 : 6 }]}>
      {showsAvatar && (
        <View style={{ width: AVATAR + GUTTER, alignSelf: "flex-end" }}>
          <BotAvatar bot={row.bot} size={AVATAR} />
        </View>
      )}
      <View style={[styles.permission, { backgroundColor: p.botBubble }]}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Symbol name="terminal" size={16} color={p.tint} />
          <Text style={[styles.permissionTitle, { color: p.label, flex: 1 }]} numberOfLines={2}>
            {title}
          </Text>
          {takesInput ? (
            <Pressable
              disabled={stopping}
              onPress={() => void stop()}
              hitSlop={6}
              style={({ pressed }) => [styles.headerButton, { backgroundColor: p.innerCard, opacity: stopping ? 0.5 : pressed ? 0.7 : 1 }]}
              accessibilityRole="button"
            >
              <Text style={{ color: p.label, fontSize: 13, fontWeight: "600" }}>{t("Stop")}</Text>
            </Pressable>
          ) : null}
        </View>
        <Pressable
          onPress={() => router.push({ pathname: "/command/[id]", params: { id: row.message.id, chat: row.message.chat_id, title: t("{who}'s command", { who }) } })}
          style={({ pressed }) => [styles.command, { backgroundColor: p.innerCard, opacity: pressed ? 0.6 : 1 }]}
          accessibilityRole="button"
          accessibilityLabel={t("Show the full command")}
        >
          <Text style={[styles.commandText, { color: p.label }]} numberOfLines={1}>
            {`$ ${command}`}
          </Text>
        </Pressable>
        {caption ? <Text style={[styles.reasonText, { color: p.secondaryLabel }]}>{caption}</Text> : null}
        {output ? <OutputBlock text={output} /> : null}
        {error ? <Text style={[styles.reasonText, { color: p.red }]}>{error}</Text> : null}
        {run.state === "asking" ? (
          <View style={{ flexDirection: "row", gap: 8, marginTop: 4 }}>
            {choices.map(([label, decision], index) => (
              <CardButton key={decision} label={label} primary={index === 0} onPress={() => onDecide(row.message, decision)} />
            ))}
          </View>
        ) : null}
        {run.state === "asking" && run.rule ? <Text style={[styles.ruleNote, { color: p.secondaryLabel }]}>{t("Always allow adds the rule “{rule}”.", { rule: run.rule })}</Text> : null}
        {run.state === "waiting" && takesInput ? (
          <View style={{ flexDirection: "row", marginTop: 4 }}>
            <CardButton label={t("Answer")} primary onPress={() => onAnswer(row.message)} />
          </View>
        ) : null}
      </View>
    </View>
  );
});

/// How a coding agent stands, in a word or two.
export function agentStatus(agent: AgentRun): string {
  switch (agent.state) {
    case "checking":
    case "starting":
      return tc("Starting", "coding agent");
    case "asking":
      return tc("Needs you", "coding agent");
    case "working":
      return agent.stalled ? tc("Quiet", "coding agent") : tc("Working", "coding agent");
    case "idle":
      return tc("Done", "coding agent");
    case "exited":
      return tc("Ended", "coding agent");
    case "failed":
      return tc("Failed", "coding agent");
    case "denied":
      return tc("Not allowed", "coding agent");
    case "expired":
      return tc("No answer", "coding agent");
    case "dismissed":
      return tc("Dismissed", "coding agent");
    default:
      return tc("Stopped", "coding agent");
  }
}

/// "Chef wants to start Claude Code on Workbench", "Claude Code wants to run a command", "Claude
/// Code asks", or the agent's name.
export function agentTitle(agent: AgentRun, who: string): string {
  const name = agentName(agent);
  switch (agent.question?.kind) {
    case "start":
      return agent.device ? t("{who} wants to start {agent} on {device}", { who, agent: name, device: agent.device }) : t("{who} wants to start {agent}", { who, agent: name });
    case "command":
      return t("{agent} wants to run a command", { agent: name });
    case "choices":
    case "text":
      return t("{agent} asks", { agent: name });
    default:
      return name;
  }
}

/// The line under the task: how it stands and where it works, or how it ended.
export function agentDetail(agent: AgentRun): string {
  if (agent.question) return "";
  let parts = [agentStatus(agent)];
  if (agent.state === "failed" && agent.outcome) parts = [t("Failed: {outcome}", { outcome: agent.outcome })];
  const place = agentPlace(agent);
  if (place && agent.state !== "denied") parts.push(place);
  const host = agent.host === "herdr" ? "Herdr" : agent.host === "luvus" ? "Luvus" : "";
  if (host && agentIsOpen(agent)) parts.push(t("in {host}", { host }));
  return parts.join(" · ");
}

/// A coding agent's card, on the row of the call that started it, from Auto-review's question to
/// how it ended: the agent's name with Stop while it runs, what it was asked, how it stands and
/// where it works, and while it works its last lines. When it asks: Allow once, Always allow, and
/// Deny before it starts or runs a command; a button per choice its pane offers; or Answer, which
/// opens the answer sheet. A tap on what it works on opens its transcript.
export const AgentRow = memo(function AgentRow({
  row,
  isGroup,
  onDecide,
  onChoose,
  onAnswer,
  onStop,
}: {
  row: Extract<Row, { type: "agent" }>;
  isGroup: boolean;
  onDecide: (message: Message, decision: "allow" | "always" | "deny") => void;
  onChoose: (message: Message, choice: number) => Promise<void>;
  onAnswer: (message: Message) => void;
  onStop: (message: Message) => Promise<void>;
}) {
  useLanguage();
  const p = usePalette();
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { agent } = row;
  const question = agent.question;
  const showsAvatar = isGroup && row.message.author.kind === "bot";
  const who = row.bot?.name ?? t("The bot");
  const name = agentName(agent);
  // A new question clears what the last answer or Stop said.
  useEffect(() => setError(null), [question, agent.state]);
  const run = async (work: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await work();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const detail = agentDetail(agent);
  const output = !question && (agent.state === "starting" || agent.state === "working") ? (agent.output ?? "").split("\n").filter(Boolean).join("\n") : "";
  const decisions: [string, "allow" | "always" | "deny"][] = question?.rule
    ? [[t("Allow once"), "allow"], [t("Always allow"), "always"], [t("Deny"), "deny"]]
    : [[t("Allow once"), "allow"], [t("Deny"), "deny"]];
  const openTranscript = () => {
    const place = agentPlace(agent);
    router.push({ pathname: "/agent/[id]", params: { id: row.message.id, chat: row.message.chat_id, title: place ? `${name} · ${place}` : name } });
  };
  const body = (
    <>
      {agent.task ? (
        <Text style={[styles.agentTask, { color: p.label }]} numberOfLines={2}>
          {agent.task}
        </Text>
      ) : null}
      {detail ? (
        <Text style={[styles.agentDetail, { color: p.secondaryLabel }]} numberOfLines={1}>
          {detail}
        </Text>
      ) : null}
      {output ? <OutputBlock text={output} /> : null}
    </>
  );
  return (
    <View style={[styles.messageRow, { paddingTop: row.groupStart ? 14 : 6 }]}>
      {showsAvatar && (
        <View style={{ width: AVATAR + GUTTER, alignSelf: "flex-end" }}>
          <BotAvatar bot={row.bot} size={AVATAR} />
        </View>
      )}
      <View style={[styles.permission, { backgroundColor: p.botBubble }]}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Symbol name="chevron.left.forwardslash.chevron.right" size={15} color={p.tint} />
          <Text style={[styles.permissionTitle, { color: p.label, flex: 1 }]} numberOfLines={2}>
            {agentTitle(agent, who)}
          </Text>
          {agentIsRunning(agent) ? (
            <Pressable
              disabled={busy}
              onPress={() => void run(() => onStop(row.message))}
              hitSlop={6}
              style={({ pressed }) => [styles.headerButton, { backgroundColor: p.innerCard, opacity: busy ? 0.5 : pressed ? 0.7 : 1 }]}
              accessibilityRole="button"
            >
              <Text style={{ color: p.label, fontSize: 13, fontWeight: "600" }}>{t("Stop")}</Text>
            </Pressable>
          ) : null}
        </View>
        {agentStarted(agent) ? (
          <Pressable onPress={openTranscript} style={({ pressed }) => ({ opacity: pressed ? 0.6 : 1 })} accessibilityRole="button" accessibilityHint={t("Show the transcript")}>
            {body}
          </Pressable>
        ) : (
          body
        )}
        {question?.kind === "command" && question.command ? (
          <Pressable
            onPress={() => router.push({ pathname: "/command/[id]", params: { id: row.message.id, chat: row.message.chat_id, title: t("{who}'s command", { who: name }) } })}
            style={({ pressed }) => [styles.command, { backgroundColor: p.innerCard, opacity: pressed ? 0.6 : 1 }]}
            accessibilityRole="button"
            accessibilityLabel={t("Show the full command")}
          >
            <Text style={[styles.commandText, { color: p.label }]} numberOfLines={1}>
              {`$ ${firstLine(question.command)}`}
            </Text>
          </Pressable>
        ) : null}
        {(question?.kind === "choices" || question?.kind === "text") && question.text ? <OutputBlock text={question.text} /> : null}
        {question && (question.kind === "start" || question.kind === "command") && question.reason ? <Text style={[styles.reasonText, { color: p.secondaryLabel }]}>{question.reason}</Text> : null}
        {error ? <Text style={[styles.reasonText, { color: p.red }]}>{error}</Text> : null}
        {question && (question.kind === "start" || question.kind === "command") ? (
          <View style={{ flexDirection: "row", gap: 8, marginTop: 4 }}>
            {decisions.map(([label, decision], index) => (
              <CardButton key={decision} label={label} primary={index === 0} onPress={() => onDecide(row.message, decision)} />
            ))}
          </View>
        ) : null}
        {question?.kind === "choices" ? (
          <View style={{ gap: 6, marginTop: 4 }}>
            {(question.choices ?? []).map((choice, index) => (
              <Pressable
                key={`${index}-${choice}`}
                disabled={busy}
                onPress={() => void run(() => onChoose(row.message, index))}
                style={({ pressed }) => [styles.choiceButton, { backgroundColor: p.innerCard, opacity: busy ? 0.5 : pressed ? 0.7 : 1 }]}
                accessibilityRole="button"
              >
                <Text style={{ color: p.label, fontSize: 14, fontWeight: "600" }} numberOfLines={2}>
                  {choice}
                </Text>
              </Pressable>
            ))}
          </View>
        ) : null}
        {question?.kind === "text" ? (
          <View style={{ flexDirection: "row", marginTop: 4 }}>
            <CardButton label={t("Answer")} primary onPress={() => onAnswer(row.message)} />
          </View>
        ) : null}
        {question && (question.kind === "start" || question.kind === "command") && question.rule ? <Text style={[styles.ruleNote, { color: p.secondaryLabel }]}>{t("Always allow adds the rule “{rule}”.", { rule: question.rule })}</Text> : null}
      </View>
    </View>
  );
});

/// A running command's last lines: a code block like the command's that grows to six lines and
/// then scrolls, the newest line in view. An edge with more lines past it fades out: Android
/// draws that itself, iOS gets a gradient in the block's color.
function OutputBlock({ text }: { text: string }) {
  const p = usePalette();
  const scroll = useRef<ScrollView>(null);
  const extent = useRef({ content: 0, frame: 0, offset: 0 });
  const [edges, setEdges] = useState({ above: false, below: false });
  const measured = () => {
    const { content, frame, offset } = extent.current;
    const above = offset > 1;
    const below = offset + frame < content - 1;
    setEdges((shown) => (shown.above === above && shown.below === below ? shown : { above, below }));
  };
  const fade = p.codeFade;
  return (
    <View style={[styles.output, { backgroundColor: p.code }]}>
      <ScrollView
        ref={scroll}
        nestedScrollEnabled
        fadingEdgeLength={16}
        style={styles.outputScroll}
        contentContainerStyle={styles.outputContent}
        scrollEventThrottle={32}
        onLayout={(e) => {
          extent.current.frame = e.nativeEvent.layout.height;
          measured();
        }}
        onContentSizeChange={(_, height) => {
          extent.current.content = height;
          scroll.current?.scrollToEnd({ animated: false });
          measured();
        }}
        onScroll={(e) => {
          extent.current.offset = e.nativeEvent.contentOffset.y;
          measured();
        }}
      >
        <Text style={[styles.commandText, { color: p.label }]}>{text}</Text>
      </ScrollView>
      {fade && edges.above ? <LinearGradient pointerEvents="none" colors={[fade[0], fade[1]]} style={[styles.outputFade, { top: 0 }]} /> : null}
      {fade && edges.below ? <LinearGradient pointerEvents="none" colors={[fade[1], fade[0]]} style={[styles.outputFade, { bottom: 0 }]} /> : null}
    </View>
  );
}

/// The whole command a permission card asks about or a command's card runs, to read or copy.
export const StatusRow = memo(function StatusRow({ text }: { text: string }) {
  const p = usePalette();
  return (
    <View style={[styles.centered, { paddingTop: 10, paddingBottom: 4 }]}>
      <Text style={[styles.caption, { color: p.tertiaryLabel }]}>{text}</Text>
    </View>
  );
});

/// "Working…" in a DM and "Chef is working…" in a group, or what the one bot at work is doing,
/// the words shimmering while a turn runs.
export const WorkingRow = memo(function WorkingRow({ chatId, bots, isGroup }: { chatId: string; bots: Bot[]; isGroup: boolean }) {
  useLanguage();
  const p = usePalette();
  const activity = useStore((s) => workingActivity(s, chatId));
  const names = bots.map((b) => b.name);
  const label =
    names.length > 1
      ? t("{names} and {last} are working…", { names: names.slice(0, -1).join(language === "zh" ? "、" : ", "), last: names[names.length - 1] })
      : (activity ?? (isGroup && names.length === 1 ? t("{name} is working…", { name: names[0] }) : t("Working…")));
  return (
    <View style={[styles.messageRow, styles.messageRowBot, { paddingTop: 14, alignItems: "center" }]}>
      <View style={{ width: AVATAR + GUTTER }}>{bots[0] && <BotAvatar bot={bots[0]} size={AVATAR} />}</View>
      <ShimmerView style={styles.working}>
        <Text style={[styles.caption, { color: p.label }]} numberOfLines={1}>
          {label}
        </Text>
      </ShimmerView>
    </View>
  );
});

const styles = StyleSheet.create({
  dayRow: { alignItems: "center", paddingTop: 18, paddingBottom: 2 },
  caption: { fontSize: Font.caption, fontWeight: "500" },
  messageRow: { flexDirection: "row", paddingHorizontal: INSET },
  messageRowYou: { justifyContent: "flex-end" },
  messageRowBot: { justifyContent: "flex-start" },
  bubbleColumn: { maxWidth: "80%", alignItems: "flex-start" },
  bubbleColumnYou: { alignItems: "flex-end" },
  author: { fontSize: Font.author, fontWeight: "600", marginLeft: 12, marginBottom: 2 },
  quote: { flexDirection: "row", alignItems: "center", gap: 4, maxWidth: "100%", marginHorizontal: 6, marginBottom: 3 },
  quoteYou: { alignSelf: "flex-end" },
  quoteText: { flexShrink: 1, fontSize: 12 },
  quoteName: { fontWeight: "600" },
  replyArrow: { position: "absolute", left: 18, top: 0, bottom: 0, justifyContent: "center" },
  // Held for the bot's next step: the bubble waits, dimmed, over Send now.
  held: { opacity: 0.55 },
  sendNow: { alignSelf: "flex-end", marginTop: 4, marginRight: 6 },
  sendNowText: { fontSize: 13, fontWeight: "600" },
  bubble: { borderRadius: 20, paddingHorizontal: 14, paddingVertical: 10, gap: 2 },
  // The user's bubble is a capsule while it holds one line, as the reference's.
  bubbleYou: { borderRadius: 22 },
  actions: { flexDirection: "row", alignItems: "center", gap: 2, marginTop: 4, marginLeft: 2 },
  actionsYou: { alignSelf: "flex-end", marginLeft: 0, marginRight: 2 },
  action: { width: 30, height: 28, alignItems: "center", justifyContent: "center", borderRadius: 14 },
  edited: { fontSize: Font.caption, marginRight: 4 },
  bubbleFooter: { flexDirection: "row", justifyContent: "flex-end", alignItems: "center" },
  centered: { alignItems: "center", paddingHorizontal: INSET },
  markerLine: { flexDirection: "row", alignItems: "center" },
  notice: { flexDirection: "row", alignItems: "flex-start", gap: 8, borderRadius: 10, paddingHorizontal: 10, paddingVertical: 8, maxWidth: 360 },
  // The card takes what the row leaves beside the avatar column, up to `maxWidth`.
  // A card in the transcript, after the reference's: a gray panel as the bot's bubbles are, what it
  // holds on white cards inside it, and its choices as equal buttons, the first in ink.
  permission: { flex: 1, borderRadius: 20, padding: 14, gap: 8, maxWidth: 420 },
  permissionTitle: { fontSize: 15, fontWeight: "600", flexShrink: 1 },
  permissionButton: { paddingHorizontal: 12, paddingVertical: 7, borderRadius: 10 },
  cardButtons: { flexDirection: "row", gap: 8, marginTop: 4 },
  questionText: { fontSize: Font.message, lineHeight: 22, fontWeight: "500" },
  options: { paddingVertical: 0, paddingHorizontal: 0, gap: 0, overflow: "hidden" },
  option: { flexDirection: "row", alignItems: "flex-start", gap: 10, paddingHorizontal: 14, paddingVertical: 12 },
  optionLabel: { fontSize: 15, fontWeight: "500", lineHeight: 20 },
  otherInput: { flex: 1, fontSize: 15, paddingVertical: 0, minHeight: 20 },
  recommended: { borderRadius: 6, paddingHorizontal: 6, paddingVertical: 1 },
  recommendedText: { fontSize: 11, fontWeight: "600" },
  cardButton: { flex: 1, minHeight: 40, borderRadius: 10, paddingHorizontal: 10, alignItems: "center", justifyContent: "center" },
  cardButtonText: { fontSize: 15, fontWeight: "600" },
  innerCard: { borderRadius: 12, paddingHorizontal: 14, paddingVertical: 12, gap: 6 },
  choiceButton: { paddingHorizontal: 12, paddingVertical: 9, borderRadius: 8 },
  agentTask: { fontSize: 14, lineHeight: 19, marginTop: 6 },
  agentDetail: { fontSize: 12, marginTop: 3 },
  draftHeader: { fontSize: 13, lineHeight: 18 },
  draftSubject: { fontSize: 15, fontWeight: "600", lineHeight: 20 },
  draftBody: { fontSize: 15.5, lineHeight: 22 },
  headerButton: { paddingHorizontal: 10, paddingVertical: 4, borderRadius: 7 },
  command: { borderRadius: 10, paddingHorizontal: 12, paddingVertical: 9, marginTop: 2 },
  commandText: { fontFamily: Platform.OS === "ios" ? "Menlo" : "monospace", fontSize: 12.5, lineHeight: 17 },
  output: { borderRadius: 8, marginTop: 2, overflow: "hidden" },
  // Six lines of `commandText`, then the block scrolls.
  outputScroll: { maxHeight: 6 * 17 + 14 },
  outputContent: { paddingHorizontal: 10, paddingVertical: 7 },
  outputFade: { position: "absolute", left: 0, right: 0, height: 16 },
  reasonText: { fontSize: 13, lineHeight: 18 },
  ruleNote: { fontSize: 12, lineHeight: 16 },
  noticeIcon: { marginTop: (NOTICE_LINE - NOTICE_ICON) / 2 },
  noticeText: { fontSize: 12.5, lineHeight: NOTICE_LINE, flexShrink: 1 },
  working: { flexShrink: 1 },
});
