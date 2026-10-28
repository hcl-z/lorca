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
import Animated, { FadeIn, LinearTransition, runOnJS, useAnimatedStyle, useReducedMotion, useSharedValue, withSequence, withSpring, withTiming } from "react-native-reanimated";
import { LinearGradient } from "expo-linear-gradient";
import { useRouter } from "expo-router";
import { ShimmerView } from "../../modules/lorca-core/ShimmerView";
import { agentIsRunning, agentKindName, agentName, agentStarted, assignedTo, canBeQuoted, isLive, isSentMessage, showsCard, showsSpeakers, type AgentRun, type AskQuestion, type Author, type Body, type Bot, type Chat, type CommandRun, type Message } from "../core/model";
import { engine } from "../core/engine";
import { canRegenerate, rerunnable } from "../core/rerun";
import { answerWords, askAnswer, sendsOnPick, togglePick } from "../core/ask";
import * as agentAsk from "../core/agentAsk";
import { agentBusy, agentSince, agentTail } from "../core/agentKit";
import { AgentHeader, AgentTile, Callout, ConsoleTail, PullRequestButton, SpecStrip, StopButton, agentBrand, alpha, useNow } from "./agentKit";
import { useStore } from "../core/store";
import { language, t, tc, useLanguage } from "../i18n";
import { AttachmentBlock } from "./attachments";
import { OutputCard } from "./outputs";
import { BotAvatar, ContactAvatar } from "./Avatar";
import { daySeparator, firstLine, messageAuthorName, quoteAuthorName, workingActivity } from "./format";
export { messageAuthorName, quoteAuthorName } from "./format";
import { Markdown } from "./Markdown";
import { FoldedText } from "./FoldedText";
import type { MessageFrame } from "./longMessage";
import { Button } from "./Button";
import { Symbol } from "./Symbol";
import { usePaneWidth } from "./layout";
import { accentColor, Font, usePalette } from "./theme";
import { alert } from "./alert";
import { draftStateWord, draftTitle, fileSize, sendable, type DraftBody } from "./drafts";
import { secretCaption, secretTitle } from "./secrets";

const AVATAR = 28;
const GUTTER = 8;
/// A bubble stops growing here in a wide pane.
const BUBBLE_COLUMN_MAX = 620;
const INSET = 12;
/// A message that hands nothing to anyone, shared so its row compares equal from build to build.
const NO_BOTS: Bot[] = [];
/// A notice's line, and its icon, which centers on the first line as on the Mac.
const NOTICE_LINE = 17;
const NOTICE_ICON = 14;

import { buildRows, SEPARATOR_GAP_SECS, type Row } from "./transcriptRows";
export { buildRows, SEPARATOR_GAP_SECS, type Row } from "./transcriptRows";

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

/// A bot's published output, which shows as a card of its own.
function isOutputCard(message: Message): boolean {
  return message.output !== undefined && message.author.kind !== "you";
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
  speakers,
  onReply,
  onQuotePress,
  onEdit,
  onRegenerate,
  onOpenMessage,
  flashing,
}: {
  row: Extract<Row, { type: "message" }>;
  bots: Map<string, Bot>;
  speakers: boolean;
  onReply?: (message: Message) => void;
  onQuotePress?: (messageID: string) => void;
  onEdit?: (message: Message) => void;
  onRegenerate?: (message: Message) => void;
  /// Reads the whole of a long message, from the bubble's place in the window.
  onOpenMessage?: (message: Message, frame: MessageFrame) => void;
  flashing?: boolean;
}) {
  const held = row.message.queued === true;
  const reply = useMemo(() => (onReply && canBeQuoted(row.message) ? () => onReply(row.message) : undefined), [onReply, row.message]);
  useLanguage();
  const p = usePalette();
  const paneWidth = usePaneWidth();
  const { message, groupStart, groupEnd, showsName, stackAbove, stackBelow } = row;
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
  const showsAvatar = speakers && !isYou;
  // A bot's published output is a card of its own, without a bubble.
  const isOutput = isOutputCard(message);
  // The bubble column is 80% of the pane, up to a readable line; the bubble pads 13 a side.
  // Attachments and the text both fit that width.
  const columnWidth = Math.min(Math.floor(paneWidth * 0.8), BUBBLE_COLUMN_MAX);
  const attachmentWidth = columnWidth - 26 - (showsAvatar ? AVATAR + GUTTER : 0);
  const bubble = useRef<View>(null);
  // The fill behind the text: a folded message's fade runs into it.
  const bubbleBackground = isYou ? p.userBubble : failed ? "rgba(255,59,48,0.14)" : p.botBubble;
  const openWhole = useMemo(() => {
    if (!onOpenMessage) return undefined;
    return () => bubble.current?.measureInWindow((x, y, w, h) => onOpenMessage(message, { x, y, w, h }));
  }, [message, onOpenMessage]);
  const quoteName = quote ? quoteAuthorName(quote, bots) : "";
  const agent = message.agent;
  const agentLabel = agent ? agent.name || agentKindName(agent.kind) : "";
  const reduceMotion = useReducedMotion();
  const appear = reduceMotion ? undefined : FadeIn.duration(180);
  const settle = reduceMotion ? undefined : LinearTransition;
  const swipe = useSwipeToReply(reply);
  return (
    <View>
    <Animated.View style={[styles.replyArrow, swipe.arrow]} pointerEvents="none">
      <Symbol name="arrowshape.turn.up.left.fill" size={16} color={p.secondaryLabel} />
    </Animated.View>
    <Animated.View style={swipe.follow}>
    <View style={[styles.messageRow, { paddingTop: stackAbove ? 0 : groupStart ? 14 : 3 }, isYou ? styles.messageRowYou : styles.messageRowBot]}>
      {showsAvatar && (
        <Animated.View entering={appear} layout={settle} style={{ width: AVATAR + GUTTER, alignSelf: "flex-end" }}>
          {groupEnd &&
            (agent ? <AgentTile kind={agent.kind} size={AVATAR} round /> : contact !== undefined ? <ContactAvatar name={contact} size={AVATAR} /> : <BotAvatar bot={bot} size={AVATAR} />)}
        </Animated.View>
      )}
      <View style={[styles.bubbleColumn, { maxWidth: columnWidth }, isYou && styles.bubbleColumnYou]}>
        {showsName && (
          <Animated.View entering={appear} layout={settle}>
            <Text style={[styles.author, { color: agent ? agentBrand(agent.kind, p.dark) : p.secondaryLabel }]} numberOfLines={1}>
              {agent ? agentLabel : (contact ?? bot?.name ?? t("Bot"))}
            </Text>
          </Animated.View>
        )}
        {quote && (
          <Pressable
            onPress={() => onQuotePress?.(quote.message_id)}
            hitSlop={6}
            style={[styles.quote, isYou && styles.quoteYou]}
            accessibilityRole="button"
            accessibilityLabel={t("In reply to {name}: {text}", { name: quoteName, text: quote.text })}
          >
            {quote.agent ? <AgentTile kind={quote.agent.kind} size={12} round /> : <Symbol name="arrowshape.turn.up.left.fill" size={10} color={p.tertiaryLabel} />}
            <Text style={[styles.quoteText, { color: p.secondaryLabel }]} numberOfLines={1}>
              <Text style={[styles.quoteName, quote.agent ? { color: agentBrand(quote.agent.kind, p.dark) } : null]}>{quoteName}:</Text> {quote.text}
            </Text>
          </Pressable>
        )}
        <GestureDetector gesture={swipe.pan}>
        {isOutput ? (
          <Animated.View style={pulsing}>
            <OutputCard message={message} maxWidth={columnWidth - (showsAvatar ? AVATAR + GUTTER : 0)} stack={{ above: stackAbove, below: stackBelow }} />
          </Animated.View>
        ) : (
        <Animated.View
          ref={bubble}
          style={[
            styles.bubble,
            isYou ? [styles.bubbleYou, { backgroundColor: bubbleBackground }] : { backgroundColor: bubbleBackground },
            pulsing,
            held && styles.held,
          ]}
        >
          {attachments.length > 0 && <AttachmentBlock attachments={attachments} onUserBubble={isYou} maxWidth={attachmentWidth} chatId={message.chat_id} messageId={message.id} />}
          {text.length > 0 && (
            <FoldedText
              text={text}
              color={isYou ? p.userBubbleText : p.botBubbleText}
              maxWidth={attachmentWidth}
              sources={message.author.kind === "bot"}
              background={bubbleBackground}
              actionColor={isYou ? p.userBubbleText : p.link}
              onToggle={openWhole}
            />
          )}
          {failed && (
            <View style={styles.bubbleFooter}>
              <Symbol name="exclamationmark.triangle.fill" size={11} color={p.red} />
            </View>
          )}
        </Animated.View>
        )}
        </GestureDetector>
        {row.assigned.length > 0 && (
          <View style={styles.assignedLine} accessible accessibilityLabel={t("Handed to {names}", { names: row.assigned.map((b) => b.name).join(", ") })}>
            <Symbol name="arrow.right" size={10} color={p.tertiaryLabel} />
            {row.assigned.map((b) => (
              <View key={b.id} style={styles.assignedBot}>
                <BotAvatar bot={b} size={14} />
                <Text style={[styles.caption, { color: p.secondaryLabel, fontWeight: "600" }]}>{b.name}</Text>
              </View>
            ))}
          </View>
        )}
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
  a.row.assigned.length === b.row.assigned.length &&
  a.row.assigned.every((bot, i) => bot === b.row.assigned[i]) &&
  a.onEdit === b.onEdit &&
  a.onRegenerate === b.onRegenerate &&
  a.bots === b.bots &&
  a.speakers === b.speakers &&
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
/// The icon of each decision a card offers.
export const DECISION_ICONS: Record<string, string> = { allow: "checkmark", always: "checkmark.seal", deny: "xmark", access: "key.fill" };

export function CardButton({ label, icon, primary = false, disabled = false, onPress }: { label: string; icon?: string; primary?: boolean; disabled?: boolean; onPress: () => void }) {
  return <Button title={label} icon={icon} kind={primary ? "primary" : "secondary"} disabled={disabled} onPress={onPress} style={styles.cardButton} />;
}

export const PermissionRow = memo(function PermissionRow({ row, isGroup, onDecide, onFill, onOpenQuestion, onOpenAsk, onOpenCommand }: { row: Extract<Row, { type: "permission" }>; isGroup: boolean; onDecide: (message: Message, decision: "allow" | "always" | "deny") => void; onFill: (message: Message) => void; onOpenQuestion?: (message: Message) => void; onOpenAsk?: (message: Message) => void; onOpenCommand?: (message: Message) => void }) {
  if (row.body.tool === "secret" && row.body.secret) return <SecretRow row={row} isGroup={isGroup} onDecide={onDecide} onFill={onFill} />;
  if (row.body.tool === "ask" && row.body.ask) return <QuestionRow row={row} ask={row.body.ask} isGroup={isGroup} onDecide={onDecide} onOpen={onOpenQuestion} compact />;
  if (row.body.tool === "agent_ask" && row.body.agent_ask) return <AgentAskRow row={row} ask={row.body.agent_ask} isGroup={isGroup} compact onOpen={onOpenAsk} />;
  return <AskRow row={row} isGroup={isGroup} onDecide={onDecide} onOpenCommand={onOpenCommand} />;
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
              <CardButton label={t("Fill In")} icon="key.fill" primary onPress={() => onFill(row.message)} />
              <CardButton label={t("Not now")} onPress={() => onDecide(row.message, "deny")} />
            </View>
            <Text style={[styles.ruleNote, { color: p.secondaryLabel }]}>{t("Saved on {runner}. {who} never sees it.", { runner, who })}</Text>
          </>
        ) : null}
      </View>
    </View>
  );
}

const AskRow = memo(function AskRow({ row, isGroup, onDecide, onOpenCommand }: { row: Extract<Row, { type: "permission" }>; isGroup: boolean; onDecide: (message: Message, decision: "allow" | "always" | "deny") => void; onOpenCommand?: (message: Message) => void }) {
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
            onPress={() => (onOpenCommand ? onOpenCommand(row.message) : router.push({ pathname: "/command/[id]", params: { id: row.message.id, chat: row.message.chat_id, title } }))}
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
                icon={DECISION_ICONS[decision]}
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
export const QuestionRow = memo(function QuestionRow({ row, ask, isGroup, onDecide, onOpen, compact }: { row: Extract<Row, { type: "permission" }>; ask: AskQuestion; isGroup: boolean; onDecide: (message: Message, decision: "allow" | "always" | "deny") => void; onOpen?: (message: Message) => void; compact?: boolean }) {
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
        {pending && compact && onOpen ? (
          <View style={styles.cardButtons}>
            <CardButton label={t("Answer")} icon="questionmark.circle" primary onPress={() => onOpen(row.message)} />
          </View>
        ) : pending ? (
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
              {ask.multiple || writing ? <CardButton label={t("Submit")} icon="paperplane" primary disabled={busy || !canSubmit} onPress={() => send(ask.multiple ? picked : [], writing ? other : "")} /> : null}
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

/// A coding agent's question, as the desktop's card: "Claude Code asks", its message, then its form.
/// A pick is a row with a radio, or a box where it takes several, and Other… for the user's own
/// words; yes or no is a pair of rows; text and numbers are fields. A form that asks for one pick
/// sends on its tap; the rest send with Submit once each required field has an answer. Decline
/// leaves the agent to go on without one. A refusal (offline, it no longer asks) shows why under the
/// card. Answered, it keeps the question and says who answered and what, or why there is no answer.
export const AgentAskRow = memo(function AgentAskRow({ row, ask, isGroup, onAnswered, compact, onOpen }: { row: Extract<Row, { type: "permission" }>; ask: agentAsk.AgentAsk; isGroup: boolean; onAnswered?: () => void; compact?: boolean; onOpen?: (message: Message) => void }) {
  useLanguage();
  const p = usePalette();
  const [draft, setDraft] = useState<agentAsk.AgentAskDraft>(agentAsk.emptyDraft);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const pending = row.body.decision === "pending";
  const who = row.bot?.name ?? t("The bot");
  const width = Math.min(usePaneWidth() - 2 * INSET, 420) - 2 * 14;
  const filled = (next: agentAsk.AgentAskDraft) => agentAsk.formValues(ask, next, t("Yes"), t("No"), t(", "));
  const act = (work: () => Promise<void>) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    haptic.send();
    work()
      .then(() => onAnswered?.())
      .catch((error) => setProblem(error instanceof Error ? error.message : String(error)))
      .finally(() => setBusy(false));
  };
  const send = (next: agentAsk.AgentAskDraft) => {
    const { values, ready } = filled(next);
    if (ready) act(() => engine.answerAgentAsk(row.message.chat_id, ask.row_id, values));
  };
  const pick = (field: agentAsk.AgentAskField, index: number) => {
    if (busy) return;
    const next = { ...draft, picks: { ...draft.picks, [field.key]: agentAsk.togglePick(field, draft.picks[field.key] ?? [], index) }, other: field.kind === "many" ? draft.other : { ...draft.other, [field.key]: undefined } };
    setDraft(next);
    if (agentAsk.sendsOnPick(ask, next)) send(next);
  };
  const ready = filled(draft).ready;
  const outcome =
    row.body.decision === "allowed"
      ? t("Answered by {who}", { who })
      : row.body.decision === "denied"
        ? ask.by === "bot" ? t("{who} declined it", { who }) : t("You declined it")
        : row.body.decision === "expired"
          ? t("No answer in time")
          : t("{agent} stopped before it was answered", { agent: ask.name });
  return (
    <View style={[styles.messageRow, { paddingTop: row.groupStart ? 14 : 6 }]}>
      <View style={[styles.permission, { backgroundColor: p.botBubble }]}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 10 }}>
          <AgentTile kind={ask.kind} size={32} />
          <View style={{ flex: 1, minWidth: 0, gap: 1 }}>
            <Text style={[styles.permissionTitle, { color: p.label }]} numberOfLines={1}>
              {t("{agent} asks", { agent: ask.name })}
            </Text>
            <Text style={{ color: p.tertiaryLabel, fontSize: 12 }} numberOfLines={1}>
              {t("For {who} to answer, or you", { who })}
            </Text>
          </View>
          {pending ? (
            <View style={[styles.waitBadge, { backgroundColor: alpha(accentColor("orange", p.dark), 0.14) }]}>
              <Text style={{ color: accentColor("orange", p.dark), fontSize: 11.5, fontWeight: "600" }} numberOfLines={1}>
                {t("Waiting for your answer")}
              </Text>
            </View>
          ) : null}
        </View>
        {ask.message ? <Markdown text={ask.message} color={p.label} maxWidth={width} size={15} /> : null}
        {pending && compact && onOpen ? (
          <View style={styles.cardButtons}>
            <CardButton label={t("Answer")} icon="questionmark.circle" primary onPress={() => onOpen(row.message)} />
          </View>
        ) : pending ? (
          <>
            {ask.fields.map((field) => {
              const choices = agentAsk.fieldChoices(field, t("Yes"), t("No"));
              const picks = draft.picks[field.key] ?? [];
              const writing = draft.other[field.key] !== undefined;
              const label = field.required || field.kind === "flag" ? field.label : t("{field} (optional)", { field: field.label });
              return (
                <View key={field.key} style={{ gap: 6 }}>
                  {ask.fields.length > 1 || field.label.trim() !== ask.message.trim() ? <Text style={[styles.optionLabel, { color: p.label }]}>{label}</Text> : null}
                  {choices.length > 0 ? (
                    <View style={[styles.innerCard, styles.options, { backgroundColor: p.innerCard }]}>
                      {choices.map((choice, index) => {
                        const on = picks.includes(index);
                        return (
                          <Pressable
                            key={`${index}-${choice.value}`}
                            onPress={() => pick(field, index)}
                            disabled={busy}
                            style={({ pressed }) => [styles.option, index > 0 && { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: p.separator }, { opacity: pressed ? 0.6 : 1 }]}
                            accessibilityRole={field.kind === "many" ? "checkbox" : "radio"}
                            accessibilityState={{ checked: on, disabled: busy }}
                          >
                            <Symbol name={field.kind === "many" ? (on ? "checkmark.square.fill" : "square") : on ? "largecircle.fill.circle" : "circle"} size={19} color={on ? p.ink : p.tertiaryLabel} />
                            <View style={{ flex: 1, gap: 2 }}>
                              <Text style={[styles.optionLabel, { color: p.label }]}>{choice.title}</Text>
                              {choice.description ? <Text style={[styles.reasonText, { color: p.secondaryLabel }]}>{choice.description}</Text> : null}
                            </View>
                          </Pressable>
                        );
                      })}
                      {field.other ? (
                        <Pressable
                          onPress={() => setDraft({ ...draft, other: { ...draft.other, [field.key]: "" }, picks: field.kind === "many" ? draft.picks : { ...draft.picks, [field.key]: [] } })}
                          disabled={busy || writing}
                          style={[styles.option, { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: p.separator }]}
                          accessibilityRole="button"
                        >
                          <Symbol name="pencil" size={17} color={writing ? p.ink : p.tertiaryLabel} />
                          {writing ? (
                            <TextInput
                              autoFocus
                              value={draft.other[field.key]}
                              onChangeText={(text) => setDraft({ ...draft, other: { ...draft.other, [field.key]: text } })}
                              placeholder={t("Your own answer")}
                              placeholderTextColor={p.tertiaryLabel as any}
                              style={[styles.otherInput, { color: p.label }]}
                              keyboardAppearance={p.dark ? "dark" : "light"}
                            />
                          ) : (
                            <Text style={[styles.optionLabel, { color: p.secondaryLabel }]}>{t("Other…")}</Text>
                          )}
                        </Pressable>
                      ) : null}
                    </View>
                  ) : (
                    <View style={[styles.innerCard, { backgroundColor: p.innerCard }]}>
                      <TextInput
                        value={draft.words[field.key] ?? ""}
                        onChangeText={(text) => setDraft({ ...draft, words: { ...draft.words, [field.key]: text } })}
                        placeholder={field.kind === "integer" ? t("A whole number") : field.kind === "number" ? t("A number") : t("Your answer")}
                        placeholderTextColor={p.tertiaryLabel as any}
                        keyboardType={field.kind === "integer" ? "number-pad" : field.kind === "number" ? "decimal-pad" : "default"}
                        style={[styles.otherInput, { color: p.label }]}
                        keyboardAppearance={p.dark ? "dark" : "light"}
                        accessibilityLabel={field.label}
                      />
                    </View>
                  )}
                </View>
              );
            })}
            {problem ? <Text style={[styles.reasonText, { color: p.red }]}>{problem}</Text> : null}
            <View style={styles.cardButtons}>
              {!agentAsk.sendsOnPick(ask, draft) ? <CardButton label={t("Submit")} icon="paperplane" primary disabled={busy || !ready} onPress={() => send(draft)} /> : null}
              <CardButton label={t("Decline")} disabled={busy} onPress={() => act(() => engine.declineAgentAsk(row.message.chat_id, ask.row_id))} />
            </View>
          </>
        ) : (
          (ask.answers ?? []).length === 0 ? (
            <Text style={[styles.reasonText, { color: p.tertiaryLabel }]}>{outcome}</Text>
          ) : (
            // Answered: the answer as a reply beside a green bar, each field's label small over
            // its answer, and whose it is when the bot gave it.
            <View style={{ gap: 6, marginTop: 2 }}>
              <View style={{ flexDirection: "row", gap: 10 }}>
                <View style={{ width: 3, borderRadius: 1.5, backgroundColor: alpha(accentColor("green", p.dark), 0.75) }} />
                <View style={{ flex: 1, gap: 8, paddingVertical: 1 }}>
                  {(ask.answers ?? []).map((answer, index) => (
                    <View key={index} style={{ gap: 1 }}>
                      {(ask.answers ?? []).length > 1 || ask.fields.length > 1 ? <Text style={{ color: p.tertiaryLabel, fontSize: 12 }}>{answer.name}</Text> : null}
                      <Text selectable style={{ color: p.label, fontSize: 16, fontWeight: "600", lineHeight: 21 }}>
                        {answer.value}
                      </Text>
                    </View>
                  ))}
                </View>
              </View>
              {ask.by === "bot" ? <Text style={{ color: p.tertiaryLabel, fontSize: 12, marginLeft: 13 }}>{outcome}</Text> : null}
            </View>
          )
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
  onOpenCommand,
}: {
  row: Extract<Row, { type: "command" }>;
  isGroup: boolean;
  onDecide: (message: Message, decision: "allow" | "always" | "deny") => void;
  onAnswer: (message: Message) => void;
  onStop: (message: Message) => Promise<void>;
  onOpenCommand?: (message: Message) => void;
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
          onPress={() => (onOpenCommand ? onOpenCommand(row.message) : router.push({ pathname: "/command/[id]", params: { id: row.message.id, chat: row.message.chat_id, title: t("{who}'s command", { who }) } }))}
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
              <CardButton key={decision} label={label} icon={DECISION_ICONS[decision]} primary={index === 0} onPress={() => onDecide(row.message, decision)} />
            ))}
          </View>
        ) : null}
        {run.state === "asking" && run.rule ? <Text style={[styles.ruleNote, { color: p.secondaryLabel }]}>{t("Always allow adds the rule “{rule}”.", { rule: run.rule })}</Text> : null}
        {run.state === "waiting" && takesInput ? (
          <View style={{ flexDirection: "row", marginTop: 4 }}>
            <CardButton label={t("Answer")} icon="square.and.pencil" primary onPress={() => onAnswer(row.message)} />
          </View>
        ) : null}
      </View>
    </View>
  );
});

/// "Chef wants to start Claude Code on Workbench", "Claude Code wants to run a command", "Claude
/// Code asks", or the agent's name.
export function agentTitle(agent: AgentRun, who: string): string {
  const name = agentName(agent);
  switch (agent.question?.kind) {
    case "start":
      return agent.device ? t("{who} wants to start {agent} on {device}", { who, agent: name, device: agent.device }) : t("{who} wants to start {agent}", { who, agent: name });
    case "command":
      return t("{agent} wants to run a command", { agent: name });
    default:
      return name;
  }
}

/// A coding agent's card, on the row of the call that started it, from Auto-review's question to
/// how it ended, drawn from the agent kit (`agentKit.tsx`) as the desktop's: its tile, its name
/// over where it works, how it stands as a pill, and Stop while it runs; what it was asked; while
/// it works, what it does on a console; its session as a strip of cells, the model and effort a
/// menu where they can change now, over a meter of its context and what it cost; the pull request
/// it opened; and a footer with when it started and its transcript. When it asks: a callout with
/// Allow once, Always allow, and Deny before it starts or runs a command. A question the agent
/// itself asks is a card of its own further down the chat (`AgentAskRow`), which a callout points
/// to. A tap on what it works on opens its transcript.
export const AgentRow = memo(function AgentRow({
  row,
  isGroup,
  onDecide,
  onStop,
  onActions,
  onOpenAsk,
  onOpenCommand,
}: {
  row: Extract<Row, { type: "agent" }>;
  isGroup: boolean;
  onDecide: (message: Message, decision: "allow" | "always" | "deny") => void;
  onStop: (message: Message) => Promise<void>;
  onActions?: (message: Message) => void;
  onOpenAsk?: (message: Message) => void;
  onOpenCommand?: (message: Message) => void;
}) {
  useLanguage();
  const p = usePalette();
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { agent } = row;
  const question = agent.question;
  const who = row.bot?.name ?? t("The bot");
  const name = agentName(agent);
  const asks = question && (question.kind === "start" || question.kind === "command") ? question : undefined;
  const askCard = useStore((s) => (agent.asking ? s.chats.find((chat) => chat.id === row.message.chat_id)?.messages.find((message) => message.id === agent.asking) : undefined));
  const askText = askCard?.body.kind === "permission" ? askCard.body.agent_ask?.message : undefined;
  const now = useNow(agentBusy(agent));
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
  const tail = agentTail(agent);
  const decisions: [string, "allow" | "always" | "deny"][] = asks?.rule
    ? [[t("Allow once"), "allow"], [t("Always allow"), "always"], [t("Deny"), "deny"]]
    : [[t("Allow once"), "allow"], [t("Deny"), "deny"]];
  const openTranscript = () => router.push({ pathname: "/agent/[id]", params: { id: row.message.id, chat: row.message.chat_id } });
  const since = agentSince(agent, now);
  const body = (
    <View style={{ gap: 10 }}>
      {agent.task ? (
        <Text style={[styles.agentTask, { color: p.label }]} numberOfLines={3}>
          {agent.task}
        </Text>
      ) : null}
      {tail.length > 0 ? <ConsoleTail lines={tail} live={agentBusy(agent)} /> : null}
    </View>
  );
  return (
    <View style={[styles.messageRow, { paddingTop: row.groupStart ? 14 : 6 }]}>
      <View style={[styles.permission, styles.agentCard, { backgroundColor: p.botBubble }]}>
        <AgentHeader
          agent={agent}
          trailing={
            <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
              {agentIsRunning(agent) ? <StopButton busy={busy} onPress={() => void run(() => onStop(row.message))} /> : null}
              {onActions ? (
                <Pressable onPress={() => onActions(row.message)} hitSlop={8} accessibilityRole="button" accessibilityLabel={t("Agent actions")} style={({ pressed }) => ({ opacity: pressed ? 0.6 : 1 })}>
                  <Symbol name="ellipsis" size={17} color={p.secondaryLabel} />
                </Pressable>
              ) : null}
            </View>
          }
        />
        {agentStarted(agent) ? (
          <Pressable onPress={openTranscript} style={({ pressed }) => ({ opacity: pressed ? 0.6 : 1 })} accessibilityRole="button" accessibilityHint={t("Show the transcript")}>
            {body}
          </Pressable>
        ) : (
          body
        )}
        <SpecStrip agent={agent} chatId={row.message.chat_id} messageId={row.message.id} />
        {asks ? (
          <Callout tint={accentColor("orange", p.dark)} icon="hand.raised" title={agentTitle(agent, who)}>
            {asks.kind === "command" && asks.command ? (
              <Pressable
                onPress={() => (onOpenCommand ? onOpenCommand(row.message) : router.push({ pathname: "/command/[id]", params: { id: row.message.id, chat: row.message.chat_id, title: t("{who}'s command", { who: name }) } }))}
                style={({ pressed }) => [styles.command, { backgroundColor: p.innerCard, opacity: pressed ? 0.6 : 1 }]}
                accessibilityRole="button"
                accessibilityLabel={t("Show the full command")}
              >
                <Text style={[styles.commandText, { color: p.label }]} numberOfLines={1}>
                  {`$ ${firstLine(asks.command)}`}
                </Text>
              </Pressable>
            ) : null}
            {asks.reason ? <Text style={[styles.reasonText, { color: p.secondaryLabel }]}>{asks.reason}</Text> : null}
            <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
              {decisions.map(([label, decision], index) => (
                <CardButton key={decision} label={label} icon={DECISION_ICONS[decision]} primary={index === 0} onPress={() => onDecide(row.message, decision)} />
              ))}
            </View>
            {asks.rule ? <Text style={[styles.ruleNote, { color: p.secondaryLabel }]}>{t("Always allow adds the rule “{rule}”.", { rule: asks.rule })}</Text> : null}
          </Callout>
        ) : null}
        {error ? <Text style={[styles.reasonText, { color: p.red }]}>{error}</Text> : null}
        {!asks && agent.asking ? (
          <Callout tint={accentColor("orange", p.dark)} icon="questionmark.circle" title={t("Asks you in the chat")}>
            {askText ? (
              <Text style={[styles.reasonText, { color: p.secondaryLabel }]} numberOfLines={2}>
                {firstLine(askText)}
              </Text>
            ) : null}
            {onOpenAsk && askCard ? <CardButton label={t("Answer")} icon="questionmark.circle" primary onPress={() => onOpenAsk(askCard)} /> : null}
          </Callout>
        ) : null}
        {!asks && agent.state === "failed" && agent.outcome ? (
          <Callout tint={accentColor("red", p.dark)} icon="xmark.circle.fill" title={tc("Failed", "coding agent")}>
            <Text selectable style={[styles.reasonText, { color: p.secondaryLabel }]} numberOfLines={4}>
              {agent.outcome}
            </Text>
          </Callout>
        ) : null}
        {agent.pull_request ? <PullRequestButton link={agent.pull_request} name={agent.pull_request_name} /> : null}
        {agentStarted(agent) ? (
          <View style={[styles.agentFoot, { borderTopColor: p.separator }]}>
            {since ? <Text style={[styles.agentFootText, { color: p.tertiaryLabel }]}>{t("Started {time} ago", { time: since })}</Text> : null}
            <View style={{ flex: 1 }} />
            <Pressable onPress={openTranscript} hitSlop={8} accessibilityRole="button" style={({ pressed }) => [styles.agentFootLink, { opacity: pressed ? 0.6 : 1 }]}>
              <Text style={[styles.agentFootText, { color: p.secondaryLabel, fontWeight: "500" }]}>{t("Transcript")}</Text>
              <Symbol name="chevron.right" size={10} color={p.secondaryLabel} weight="semibold" />
            </Pressable>
          </View>
        ) : null}
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
  assignedLine: { flexDirection: "row", alignItems: "center", flexWrap: "wrap", gap: 5, marginTop: 3, marginHorizontal: 6 },
  assignedBot: { flexDirection: "row", alignItems: "center", gap: 3 },
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
  cardButton: { flex: 1, paddingHorizontal: 10 },
  innerCard: { borderRadius: 12, paddingHorizontal: 14, paddingVertical: 12, gap: 6 },
  choiceButton: { paddingHorizontal: 12, paddingVertical: 9, borderRadius: 8 },
  agentCard: { gap: 12, paddingBottom: 10 },
  waitBadge: { height: 22, paddingHorizontal: 8, borderRadius: 11, justifyContent: "center", flexShrink: 1 },
  agentTask: { fontSize: 15, lineHeight: 21 },
  agentFoot: { flexDirection: "row", alignItems: "center", borderTopWidth: StyleSheet.hairlineWidth, paddingTop: 9, marginTop: -2 },
  agentFootText: { fontSize: 12, fontVariant: ["tabular-nums"] },
  agentFootLink: { flexDirection: "row", alignItems: "center", gap: 3 },
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
