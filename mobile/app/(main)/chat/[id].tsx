import { LegendList, type LegendListRef } from "@legendapp/list/react-native";
import { LinearGradient } from "expo-linear-gradient";
import { Stack, useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { useHeaderHeight } from "expo-router/react-navigation";
import {
  memo,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Platform, type ScrollViewProps, StyleSheet, Text, View } from "react-native";
import { Pressable } from "../../../src/ui/Pressable";
import {
  KeyboardChatScrollView,
  KeyboardController,
  useKeyboardHandler,
} from "react-native-keyboard-controller";
import Animated, {
  runOnJS,
  useAnimatedReaction,
  useDerivedValue,
  useSharedValue,
  ZoomIn,
  ZoomOut,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { SoftScrollEdgeView } from "../../../modules/lorca-core/SoftScrollEdgeView";
import { chatTitle, engine, type PickedFile } from "../../../src/core/engine";
import { isLive, showsSpeakers, type AgentRun, type Bot, type Message } from "../../../src/core/model";
import { showsSpeakersIn } from "../../../src/core/participants";
import { agentAskCard, chatAgents, mentionTargets } from "../../../src/core/participants";
import {
  chatById,
  useBotMap,
  useChat,
  useIsWorking,
  useRunningTasks,
  useStore,
  useWorkingBots,
} from "../../../src/core/store";
import { chatShown, scrollBegan, scrollEnded } from "../../../src/core/timing";
import { t, useLanguage } from "../../../src/i18n";
import { ParticipantCluster } from "../../../src/ui/Avatar";
import { Composer } from "../../../src/ui/Composer";
import { MessageReader } from "../../../src/ui/MessageReader";
import type { MessageFrame } from "../../../src/ui/longMessage";
import { RunningAgents } from "../../../src/ui/RunningAgents";
import { ParticipantsSheet } from "../../../src/ui/ParticipantsSheet";
import { AgentActionsSheet } from "../../../src/ui/AgentActionsSheet";
import { AgentAskSheet } from "../../../src/ui/AgentAskSheet";
import { afterDismiss } from "../../../src/ui/Sheet";
import { CommandSheet, MessageSheet, QuestionSheet, TasksSheet } from "../../../src/ui/DetailSheets";
import { CapsuleButton, Float, RoundButton } from "../../../src/ui/chrome";
import { KeyboardFoot } from "../../../src/ui/KeyboardFoot";
import { useWide } from "../../../src/ui/layout";
import { Symbol } from "../../../src/ui/Symbol";
import { usePalette, withAlpha } from "../../../src/ui/theme";
import { haptic } from "../../../src/ui/haptics";
import { quoteText } from "../../../src/ui/format";
import { AnswerSheet } from "../../../src/ui/AnswerSheet";
import { SecretSheet } from "../../../src/ui/SecretSheet";
import { alert } from "../../../src/ui/alert";
import {
  buildRows,
  shareRows,
  DayRow,
  MarkerRow,
  MessageRow,
  messageAuthorName,
  quoteAuthorName,
  NoticeRow,
  PermissionRow,
  AgentRow,
  DraftRow,
  CommandRow,
  StatusRow,
  WorkingRow,
  type Row,
} from "../../../src/ui/transcript";

/// Breathing room between the last message and the composer, as on the Mac.
const COMPOSER_GAP = 14;
/// How much taller the composer gets when it expands on focus, until it has been seen to.
const FOCUS_GROWTH_GUESS = 36;
/// How far from the end the transcript is scrolled before the jump-to-bottom disc shows.
const JUMP_DISTANCE = 160;
const JUMP_DISC = 36;
/** How far past the viewport, in points, the transcript keeps its rows drawn. A screen and a
 * half is enough for a flick, and each row kept is a native text view and a gesture handler. */
const DRAW_DISTANCE = 600;
/** A typical row's height before it is measured: a bubble of a line or two. */
const ESTIMATED_ROW = 72;
/// The transcript follows its end while the reader rests within this share of the screen of it.
const FOLLOW_THRESHOLD = 0.1;
const SCROLL_OWNS_ANCHOR_SPACE = { anchoredEndSpaceOwnerInternal: "scroll" } as object;
/// The list's stable props: a new object or callback on every streamed chunk re-renders its cells.
const KEEP_VISIBLE_POSITION = { data: true };
const FOLLOW_END = { animated: true };
const rowKey = (row: Row) => row.key;
const rowType = (row: Row) => row.type;
const ANDROID_BAR_HEIGHT = 56;
const ANDROID_FADE_HEIGHT = 24;

export default function ChatScreen() {
  const { language } = useLanguage();
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const p = usePalette();
  const wide = useWide();
  const insets = useSafeAreaInsets();
  const headerHeight = useHeaderHeight();
  const visibleHeaderHeight = Platform.OS === "android" ? insets.top + ANDROID_BAR_HEIGHT : headerHeight;
  const chat = useChat(id);
  const bots = useBotMap();
  const workingBotIds = useWorkingBots(id);
  const isWorking = useIsWorking(id);
  const hasTasks = useRunningTasks(id).length > 0;
  const status = useStore((s) => s.statuses[id] ?? null);
  // A long message read whole, from the place its bubble stands.
  const [reader, setReader] = useState<{ message: Message; frame: MessageFrame } | null>(null);
  const openReader = useCallback((message: Message, frame: MessageFrame) => setReader({ message, frame }), []);
  const listRef = useRef<LegendListRef>(null);
  // The composer floats over the transcript and rides the keyboard (KeyboardFoot). The
  // list is never resized: the chat scroll view keeps a bottom inset for the composer and adds
  // the keyboard's height to it frame by frame, lifting the last messages with the keys.
  // `blankSpace` is the inset with the keyboard closed (the composer with its home-indicator
  // padding, or the space under an anchored message); `extraContentPadding` is the composer's
  // height above the keys once it is open.
  // Composer bar (46) + its wrap padding (8) + home-indicator padding + the gap, as in onLayout.
  const composerGuess = 54 + COMPOSER_GAP + Math.max(insets.bottom, 8);
  const composerSpace = useSharedValue(composerGuess);
  const composerExtra = useSharedValue(composerGuess - insets.bottom);
  // Focusing the composer makes it taller just as the keyboard starts to move. The scroll view
  // answers a change of `extraContentPadding` by scrolling from the offset it last saw, which at
  // that moment is the one from before the keyboard's lift, so the lift would be lost. A new
  // composer height therefore waits until the keyboard has come to rest. For the lift to cover
  // the expanded composer in one motion, the padding already counts the expanded height while
  // the keyboard is down: the compact composer's height plus the growth seen on earlier focuses.
  const composerExtraTarget = useSharedValue(
    composerGuess - insets.bottom + FOCUS_GROWTH_GUESS,
  );
  const composerActual = useRef(composerGuess - insets.bottom);
  const composerCompact = useRef(Number.POSITIVE_INFINITY);
  const focusGrowth = useRef(FOCUS_GROWTH_GUESS);
  const compactAtKeyboardStart = useRef(false);
  const publishComposerExtra = useCallback(() => {
    const actual = composerActual.current;
    const compact = composerCompact.current;
    // Until the composer's first layout there is no compact height, and the guess stands. A
    // keyboard that comes to rest before then (one dismissed on the way into the chat) would
    // otherwise publish an infinite inset, and UIKit throws on the NaN scroll bar it makes.
    if (!Number.isFinite(compact)) return;
    const expanded = actual > compact + 1;
    composerExtraTarget.value = expanded
      ? actual
      : compact + focusGrowth.current;
  }, [composerExtraTarget]);
  const keyboardMoving = useSharedValue(false);
  // What the composer grew by on a focus is learned once the keyboard has come to rest.
  const keyboardEndRef = useRef<(height: number) => void>(() => {});
  const keyboardEnded = useCallback(
    (height: number) => keyboardEndRef.current(height),
    [],
  );
  const keyboardStartRef = useRef<(height: number) => void>(() => {});
  const keyboardStarted = useCallback(
    (height: number) => keyboardStartRef.current(height),
    [],
  );
  useKeyboardHandler(
    {
      onStart: (e) => {
        "worklet";
        keyboardMoving.value = true;
        runOnJS(keyboardStarted)(e.height);
      },
      onEnd: (e) => {
        "worklet";
        keyboardMoving.value = false;
        composerExtra.value = composerExtraTarget.value;
        runOnJS(keyboardEnded)(e.height);
      },
    },
    [keyboardEnded, keyboardStarted],
  );
  useAnimatedReaction(
    () => composerExtraTarget.value,
    (extra) => {
      if (!keyboardMoving.value) composerExtra.value = extra;
    },
  );
  // Where the rows start and where a sent message comes to rest: under the header on iOS, below
  // Android's chat bar (which floats over the list) with a little room. The space is the list's
  // own top padding, so a transcript shorter than the screen is not scrollable by it.
  const topPad = Platform.OS === "ios" ? headerHeight : visibleHeaderHeight + 8;
  // Sending a message anchors it under the header: LegendList keeps space below the rows from that
  // message on until they fill the viewport (`anchoredEndSpace`), and the space gives way as the
  // reply comes in, so nothing moves while it lands. Once the reply outgrows the viewport the
  // space is gone and the list follows its end. The space reaches the scroll view as its
  // `blankSpace`, a floor under the keyboard's lift: a keyboard that fits in it lifts nothing.
  const [anchorKey, setAnchorKey] = useState<string | null>(null);
  const anchorReady = useRef<string | null>(null);
  const anchorSpace = useSharedValue(0);
  const blankSpace = useDerivedValue(() => Math.max(anchorSpace.value, composerSpace.value));
  useEffect(() => {
    setAnchorKey(null);
    anchorReady.current = null;
    anchorSpace.value = 0;
  }, [id, anchorSpace]);
  // The jump-to-bottom disc floats over the composer while the end is out of reach.
  const [awayFromEnd, setAwayFromEnd] = useState(false);
  // Older pages load only once the chat has opened at its end.
  const loaded = useRef(false);
  useEffect(() => {
    setAwayFromEnd(false);
    loaded.current = false;
  }, [id]);
  const [composerHeight, setComposerHeight] = useState(
    composerGuess - COMPOSER_GAP,
  );
  // The end is checked while the finger moves, at most every 100 ms: reading the list's position
  // on every frame put a native call on the JS thread during the scroll it was measuring.
  const lastMeasure = useRef(0);
  const measureEnd = useCallback(() => {
    const now = Date.now();
    if (now - lastMeasure.current < 100) return;
    lastMeasure.current = now;
    const state = listRef.current?.getState();
    if (!state || state.scrollLength === 0) return;
    setAwayFromEnd(state.contentLength - state.scrollLength - state.scroll > JUMP_DISTANCE);
  }, []);
  // Lorca Dev counts missed frames from a drag's start to the scroll's rest (src/core/timing.ts):
  // a fling's momentum starts right after the drag ends, so a drag without one rests a beat later.
  const restTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const onScrollBeginDrag = useCallback(() => {
    if (restTimer.current) clearTimeout(restTimer.current);
    restTimer.current = null;
    scrollBegan();
  }, []);
  const onScrollEndDrag = useCallback(() => {
    restTimer.current = setTimeout(() => {
      restTimer.current = null;
      scrollEnded();
      // The last look at a scroll the throttle skipped still decides the disc.
      lastMeasure.current = 0;
      measureEnd();
    }, 50);
  }, [measureEnd]);
  const onMomentumScrollBegin = useCallback(() => {
    if (restTimer.current) clearTimeout(restTimer.current);
    restTimer.current = null;
  }, []);
  useEffect(() => () => {
    if (restTimer.current) clearTimeout(restTimer.current);
  }, []);
  const jumpToEnd = useCallback(() => {
    void listRef.current?.scrollToEnd({ animated: true });
  }, []);
  // Stable list callbacks, so a streamed chunk does not hand the list new functions.
  const onLoad = useCallback(() => {
    loaded.current = true;
    chatShown();
  }, []);
  const onStartReached = useCallback(() => {
    if (loaded.current) void engine.loadOlder(id);
  }, [id]);
  keyboardEndRef.current = (height) => {
    // The composer has expanded by now: what it grew by is what the next focus will need.
    const grown = composerActual.current - composerCompact.current;
    if (height > 0 && compactAtKeyboardStart.current && grown > 1)
      focusGrowth.current = grown;
    publishComposerExtra();
  };
  keyboardStartRef.current = () => {
    compactAtKeyboardStart.current =
      composerActual.current <= composerCompact.current + 1;
  };
  // The transcript's scroll view is the keyboard-controller's chat scroll view, as in LegendList's
  // own KeyboardAwareLegendList, with the composer's resting space under the anchor's as its floor.
  // Its insets are reported back to the list, which counts them in where its end is.
  const reportInset = useCallback((inset: { top: number; bottom: number; left: number; right: number }) => {
    listRef.current?.reportContentInset(inset);
  }, []);
  const ChatScroll = useCallback(
    (props: ScrollViewProps) => (
      <KeyboardChatScrollView
        {...props}
        blankSpace={blankSpace}
        extraContentPadding={composerExtra}
        onContentInsetChange={reportInset}
        applyWorkaroundForContentInsetHitTestBug
      />
    ),
    [blankSpace, composerExtra, reportInset],
  );

  // The scroll view ends the bar's track where its bottom padding starts, which includes the
  // gap kept above the composer; the bar itself runs down to the composer.
  const scrollIndicatorInsets = useMemo(
    () => ({ top: Platform.OS === "ios" ? headerHeight : 0, bottom: -COMPOSER_GAP }),
    [headerHeight],
  );
  const contentContainerStyle = useMemo(() => ({ paddingTop: topPad }), [topPad]);

  useFocusEffect(useCallback(() => {
    // Coming back from an agent page or a sheet, the chat's composer starts with the keyboard
    // down: a keyboard the screen left open behind it never gets its foot back down.
    void KeyboardController.dismiss();
    useStore.setState({ openChatId: id });
    return () => {
      // A sheet or the agent page takes the screen while the composer has the keyboard: leave it
      // down rather than letting the chat come back with the foot still lifted.
      void KeyboardController.dismiss();
      if (useStore.getState().openChatId === id)
        useStore.setState({ openChatId: null });
    };
  }, [id]));

  // The rows carry words, so a new language builds them again.
  // Rows that did not change keep their identity, and so does the list when none did: LegendList
  // diffs its data on every new array.
  const sharedRows = useRef<Row[]>([]);
  const rowsRef = useRef<Row[]>([]);
  const rows = useMemo(() => {
    const next = chat ? buildRows(chat, bots, workingBotIds, isWorking, status) : [];
    return (sharedRows.current = shareRows(sharedRows.current, next));
  }, [chat, bots, workingBotIds, isWorking, status, language]);
  rowsRef.current = rows;
  const anchorIndex = anchorKey ? rows.findIndex((row) => row.key === anchorKey) : -1;
  const anchoredEndSpace = useMemo(
    () =>
      anchorIndex < 0
        ? undefined
        : {
            anchorIndex,
            anchorOffset: topPad,
            onSizeChanged: (size: number) => {
              anchorSpace.value = size;
            },
            // Once the sent message and everything after it are measured, the list goes to the
            // end its space makes, which puts the message under the header.
            onReady: ({ anchorKey: key }: { anchorKey: string | undefined }) => {
              if (!key || anchorReady.current === key) return;
              anchorReady.current = key;
              void listRef.current?.scrollToEnd({ animated: true });
            },
          },
    [anchorIndex, anchorSpace, topPad],
  );
  // Rows that arrive or grow move the end: the disc shows once it is far enough below the reader.
  useEffect(() => {
    const frame = requestAnimationFrame(measureEnd);
    return () => cancelAnimationFrame(frame);
  }, [rows, measureEnd]);
  // Keyed on the member list, not the chat, which changes with every streamed piece of a reply.
  const botIds = chat?.bot_ids;
  const members = useMemo(
    () =>
      (botIds ?? [])
        .map((b) => bots.get(b))
        .filter((b): b is Bot => !!b),
    [botIds, bots],
  );
  const isGroup = chat?.kind === "group";
  // A channel's conversation shows its speakers by name, as a group does.
  const speakers = chat ? showsSpeakersIn(chat) : false;
  const title = chat ? chatTitle(chat) : t("Chat");
  // The agent card changes rarely; keep the composer's target list from changing with every
  // streamed word of a reply, or its memo would re-render on each one.
  const devices = useStore((s) => s.devices);
  const runnerSupportsAgents = useMemo(() => {
    const runner = members[0] ? devices.find((device) => device.id === members[0].runner_id) : undefined;
    return !!runner?.agent_messages;
  }, [members, devices]);
  const agentRosterKey = chat
    ? [chat.bot_ids.join(","), ...chatAgents(chat).map(({ agent }) => `${agent.id}:${agent.name ?? ""}:${agent.kind}:${agent.state}`)].join("|")
    : "";
  const targets = useMemo(() => (chat ? mentionTargets(chat, bots, runnerSupportsAgents) : []), [agentRosterKey, bots, runnerSupportsAgents]);
  const agentMentionable = targets.some((target) => target.kind === "agent");
  // The header and participants sheet read the open agents, not every streamed word.
  const agents = useMemo(() => (chat ? chatAgents(chat) : []), [agentRosterKey]);
  // The header's array keeps its identity while the agents do, so its memo holds through a stream.
  const agentRuns = useMemo(() => agents.map(({ agent }) => agent), [agents]);
  const [participantsOpen, setParticipantsOpen] = useState(false);
  const openParticipants = useCallback(() => setParticipantsOpen(true), []);
  const [actionsFor, setActionsFor] = useState<Message | null>(null);
  const [askFor, setAskFor] = useState<Message | null>(null);
  const [messageFor, setMessageFor] = useState<Message | null>(null);
  const [commandFor, setCommandFor] = useState<Message | null>(null);
  const [tasksOpen, setTasksOpen] = useState(false);
  const openTasks = useCallback(() => setTasksOpen(true), []);
  const [questionFor, setQuestionFor] = useState<Message | null>(null);
  const openAgentActions = useCallback((message: Message) => {
    void KeyboardController.dismiss();
    haptic.open();
    setActionsFor(message);
  }, []);
  const openAsk = useCallback((message: Message) => {
    void KeyboardController.dismiss();
    setAskFor(message);
  }, []);
  const openCommand = useCallback((message: Message) => {
    void KeyboardController.dismiss();
    setCommandFor(message);
  }, []);
  const openQuestion = useCallback((message: Message) => {
    void KeyboardController.dismiss();
    setQuestionFor(message);
  }, []);
  const questionRow =
    questionFor?.body.kind === "permission"
      ? { key: questionFor.id, type: "permission" as const, message: questionFor, body: questionFor.body, bot: questionFor.author.kind === "bot" ? bots.get(questionFor.author.bot_id) : undefined, groupStart: false }
      : undefined;
  const actionsAgent = actionsFor?.body.kind === "tool" ? actionsFor.body.agent : undefined;
  const askRow =
    askFor?.body.kind === "permission"
      ? { key: askFor.id, type: "permission" as const, message: askFor, body: askFor.body, bot: askFor.author.kind === "bot" ? bots.get(askFor.author.bot_id) : undefined, groupStart: false }
      : undefined;
  // After the Mac app: "Message Chef", or the group's title with a hint that @ addresses one.
  const placeholder =
    !isGroup && members[0]
      ? agentMentionable
        ? t("Message {name} — @ to address an agent", { name: members[0].name })
        : t("Ask {name}", { name: members[0].name })
      : targets.length > 1
        ? t("Message {title} — @ to address a bot or agent", { title })
        : t("Message {name}", { name: title });

  /// The command whose answer sheet is up. The sheet reads the live row, so it goes when the
  /// command ends and its card leaves.
  const [answeringId, setAnsweringId] = useState<string | null>(null);
  const answering = answeringId ? chat?.messages.find((m) => m.id === answeringId) : undefined;
  const answeringRun = answering?.body.kind === "tool" ? answering.body.run : undefined;
  /// The secret request whose sheet is up, while it still asks.
  const [fillingId, setFillingId] = useState<string | null>(null);
  const filling = fillingId ? chat?.messages.find((m) => m.id === fillingId) : undefined;
  const fillingAsk = filling?.body.kind === "permission" && filling.body.decision === "pending" ? filling.body.secret : undefined;

  /// The message the draft answers, from a swipe on its bubble; another chat starts without one.
  const [replying, setReplying] = useState<{ messageID: string; name: string; text: string } | null>(null);
  useEffect(() => setReplying(null), [id]);
  const startReply = useCallback(
    (message: Message) => {
      if (message.body.kind !== "text") return;
      const words = quoteText(message.body.text) || (message.body.attachments ?? []).map((file) => file.name).join(", ");
      setReplying({ messageID: message.id, name: messageAuthorName(message, bots), text: words });
    },
    [bots],
  );
  const cancelReply = useCallback(() => setReplying(null), []);
  /// The user's message being edited, from Edit under it; sending runs it again with the new words.
  const [editing, setEditing] = useState<{ id: string; text: string } | null>(null);
  useEffect(() => setEditing(null), [id]);
  const startEdit = useCallback((message: Message) => {
    if (message.body.kind !== "text") return;
    setReplying(null);
    setEditing({ id: message.id, text: message.body.text });
  }, []);
  const cancelEdit = useCallback(() => setEditing(null), []);
  const regenerate = useCallback(
    () => engine.regenerate(id).catch((error) => alert(t("Couldn't regenerate"), error instanceof Error ? error.message : String(error))),
    [id],
  );
  const stopResponding = useCallback(() => engine.stopChat(id), [id]);
  // Stable while the reply target is, so the memoized composer skips the renders a streaming
  // reply causes.
  const send = useCallback(
    (text: string, files: PickedFile[], mentions: string[], agents: string[] = []) => {
      // The anchor is measured against the screen without the keyboard.
      void KeyboardController.dismiss();
      if (editing) {
        const messageId = editing.id;
        setEditing(null);
        engine.rerun(id, messageId, text).catch((error) => alert(t("Couldn't send the edit"), error instanceof Error ? error.message : String(error)));
        return;
      }
      const replyTo = replying?.messageID;
      setReplying(null);
      engine
        .sendMessage(id, text, files, mentions, agents, replyTo)
        .then((message) => setAnchorKey(message.id))
        .catch((error) => {
          alert(
            t("Could not send"),
            error instanceof Error ? error.message : String(error),
          );
        });
    },
    [editing, id, replying],
  );
  /// The message a reply's quote names, brought into view with its bubble pulsing. One on a page
  /// not loaded yet stays where it is.
  const [flashId, setFlashId] = useState<string | null>(null);
  const revealQuoted = useCallback((messageID: string) => {
    const index = rowsRef.current.findIndex((row) => row.type === "message" && row.message.id === messageID);
    if (index < 0) return;
    listRef.current?.scrollToIndex({ index, animated: true, viewPosition: 0.4 });
    setFlashId(messageID);
    setTimeout(() => setFlashId((current) => (current === messageID ? null : current)), 1400);
  }, []);

  /// The whole message behind a "Messaged ◉ Name" marker, as a sheet.
  const openMarker = useCallback(
    (row: Extract<Row, { type: "marker" }>) => {
      // Read at press time, so the callback stays the same through every streamed chunk of the chat.
      const message = chatById(id)?.messages.find((each) => each.id === row.key);
      if (message) setMessageFor(message);
    },
    [id],
  );

  /// Answers a card. One whose answer cannot reach the bot's Runner asks again, and says why.
  const answerCard = useCallback((message: Message, decision: "allow" | "always" | "deny") => {
    engine.answerPermission(message.chat_id, message.id, decision).catch((error) => {
      alert(t("Could not answer"), error instanceof Error ? error.message : String(error));
    });
  }, []);

  const answerCommand = useCallback((message: Message) => setAnsweringId(message.id), []);
  const fillSecret = useCallback((message: Message) => setFillingId(message.id), []);
  const stopCommand = useCallback((message: Message) => engine.stopCommand(message.chat_id, message.id), []);
  const stopAgent = useCallback((message: Message) => engine.stopAgent(message.chat_id, message.id), []);

  const renderItem = useCallback(
    ({ item }: { item: Row }) => {
      switch (item.type) {
        case "day":
          return <DayRow at={item.at} />;
        case "message":
          return (
            <MessageRow
              row={item}
              bots={bots}
              speakers={speakers}
              onReply={startReply}
              onQuotePress={revealQuoted}
              onEdit={startEdit}
              onRegenerate={regenerate}
              onOpenMessage={openReader}
              flashing={flashId === item.message.id}
            />
          );
        case "marker":
          return <MarkerRow row={item} onPress={openMarker} />;
        case "notice":
          return <NoticeRow row={item} />;
        case "permission":
          return (
            <PermissionRow
              row={item}
              isGroup={speakers}
              onDecide={answerCard}
              onFill={fillSecret}
              onOpenQuestion={openQuestion}
              onOpenAsk={openAsk}
              onOpenCommand={openCommand}
            />
          );
        case "draft":
          return <DraftRow row={item} isGroup={isGroup} />;
        case "command":
          return (
            <CommandRow
              row={item}
              isGroup={speakers}
              onDecide={answerCard}
              onAnswer={answerCommand}
              onStop={stopCommand}
              onOpenCommand={openCommand}
            />
          );
        case "agent":
          return (
            <AgentRow
              row={item}
              isGroup={isGroup}
              onDecide={answerCard}
              onStop={stopAgent}
              onActions={openAgentActions}
              onOpenAsk={openAsk}
              onOpenCommand={openCommand}
            />
          );
        case "working":
          return <WorkingRow chatId={id} bots={item.bots} isGroup={isGroup} />;
        case "status":
          return <StatusRow text={item.text} />;
      }
    },
    [answerCard, answerCommand, fillSecret, bots, id, isGroup, speakers, openMarker, startReply, revealQuoted, flashId, stopCommand, startEdit, regenerate, openAgentActions, openAsk, openCommand, openQuestion, openReader],
  );

  if (!chat) {
    return (
      <View style={styles.missing}>
        <Stack.Screen options={{ title: t("Chat") }} />
        <Text style={{ color: p.secondaryLabel }}>
          {t("This chat is no longer on the roster.")}
        </Text>
      </View>
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: p.background }}>
      <ChatHeader
        id={id}
        title={title}
        members={members}
        agents={agentRuns}
        onOpenParticipants={openParticipants}
        service={chat?.channel?.service}
        working={isWorking}
        hasTasks={hasTasks}
        onTasks={openTasks}
        wide={wide}
        top={insets.top}
      />
      <View style={{ flex: 1 }}>
        <SoftScrollEdgeView style={styles.transcript}>
          <LegendList
            // A chat of its own: another chat's measured rows and position say nothing here.
            key={id}
            ref={listRef}
            renderScrollComponent={ChatScroll}
            data={rows}
            renderItem={renderItem}
            // The rows keep their identity while they are unchanged (shareRows), so the data is
            // what tells the list to draw again. The quote's flash is not in a row, so it is the
            // one thing the list has to hear about separately.
            extraData={flashId}
            keyExtractor={rowKey}
            getItemType={rowType}
            estimatedItemSize={ESTIMATED_ROW}
            recycleItems={false}
            // The chat opens at its last message, with no scroll to get there, and a transcript
            // shorter than the screen sits at its bottom, above the composer.
            initialScrollAtEnd
            alignItemsAtEnd
            // Arriving rows and rows that grow keep a reader at the end there, never under a
            // finger on the list; one who has scrolled up stays put and gets the disc.
            maintainScrollAtEnd={FOLLOW_END}
            maintainScrollAtEndThreshold={FOLLOW_THRESHOLD}
            // A page loaded above keeps the rows on screen where they are.
            maintainVisibleContentPosition={KEEP_VISIBLE_POSITION}
            anchoredEndSpace={anchoredEndSpace}
            // The space is the scroll view's inset (`blankSpace`), not padding in the list, as in
            // LegendList's KeyboardAwareLegendList; the prop is LegendList's internal one for it.
            {...SCROLL_OWNS_ANCHOR_SPACE}
            contentInsetAdjustmentBehavior="never"
            contentContainerStyle={contentContainerStyle}
            // The insets here already account for the header and the composer; iOS would add the
            // safe area to the scroll bar's track again and keep it short of both ends.
            automaticallyAdjustsScrollIndicatorInsets={false}
            scrollIndicatorInsets={scrollIndicatorInsets}
            // Rows are laid out ahead of the viewport so a quick flick finds them drawn: the
            // default 250 leaves bare background at the edge when the rows' heights are still
            // being measured. LegendList never detaches offscreen views, which would blank a
            // row until it is attached again.
            drawDistance={DRAW_DISTANCE}
            keyboardDismissMode="interactive"
            onLoad={onLoad}
            // Nearing the first message: the page before it, once the chat has opened at its end.
            onStartReached={onStartReached}
            onStartReachedThreshold={1}
            onScroll={measureEnd}
            scrollEventThrottle={16}
            onScrollBeginDrag={onScrollBeginDrag}
            onScrollEndDrag={onScrollEndDrag}
            onMomentumScrollBegin={onMomentumScrollBegin}
            onMomentumScrollEnd={scrollEnded}
          />
        </SoftScrollEdgeView>
        <KeyboardFoot
          style={[styles.jump, { bottom: composerHeight + 10 }]}
          tuck={insets.bottom}
        >
          {awayFromEnd && (
            <Animated.View
              entering={ZoomIn.duration(160)}
              exiting={ZoomOut.duration(160)}
            >
              <Pressable
                onPress={jumpToEnd}
                hitSlop={8}
                accessibilityRole="button"
                accessibilityLabel={t("Jump to bottom")}
              >
                <Float style={styles.jumpDisc} radius={JUMP_DISC / 2}>
                  <Symbol name="arrow.down" size={16} color={p.label} />
                </Float>
              </Pressable>
            </Animated.View>
          )}
        </KeyboardFoot>
        <KeyboardFoot
          style={[
            styles.composer,
            { paddingBottom: Math.max(insets.bottom, 8) },
          ]}
          // Open, the composer's home-indicator padding is not needed: it sits on the keys.
          tuck={insets.bottom}
          onLayout={(e) => {
            setComposerHeight(e.nativeEvent.layout.height);
            const blank = e.nativeEvent.layout.height + COMPOSER_GAP;
            composerSpace.value = blank;
            const extra = blank - insets.bottom;
            composerActual.current = extra;
            composerCompact.current = Math.min(composerCompact.current, extra);
            publishComposerExtra();
          }}
        >
          <RunningAgents onActions={openAgentActions} />
          <Composer
            mentionTargets={targets}
            placeholder={placeholder}
            reply={replying}
            onCancelReply={cancelReply}
            editing={editing}
            onCancelEdit={cancelEdit}
            responding={isWorking}
            onStop={stopResponding}
            onSend={send}
          />
        </KeyboardFoot>
      </View>
      {answering && answeringRun && isLive(answeringRun) ? (
        <AnswerSheet
          run={answeringRun}
          onDismiss={() => setAnsweringId(null)}
          onSend={(text) => engine.answerCommand(answering.chat_id, answering.id, text)}
        />
      ) : null}
      {filling && fillingAsk ? (
        <SecretSheet
          fields={fillingAsk.fields}
          onDismiss={() => setFillingId(null)}
          onSave={(values) => engine.answerSecret(filling.chat_id, filling.id, values)}
        />
      ) : null}
      <AgentActionsSheet
        visible={!!actionsFor}
        agent={actionsAgent}
        onClose={() => setActionsFor(null)}
        onOpenTranscript={() => {
          const message = actionsFor;
          setActionsFor(null);
          if (message) afterDismiss(() => router.push({ pathname: "/agent/[id]", params: { id: message.id, chat: message.chat_id } }));
        }}
        onAnswer={
          actionsAgent?.asking
            ? () => {
                const message = chat && actionsAgent ? agentAskCard(chat, actionsAgent) : undefined;
                setActionsFor(null);
                if (message) afterDismiss(() => setAskFor(message));
              }
            : undefined
        }
        onStop={() => {
          if (actionsFor) void engine.stopAgent(actionsFor.chat_id, actionsFor.id);
        }}
      />
      <AgentAskSheet visible={!!askFor} row={askRow} ask={askRow?.body.agent_ask} onClose={() => setAskFor(null)} />
      <MessageSheet visible={!!messageFor} message={messageFor ?? undefined} onClose={() => setMessageFor(null)} />
      <CommandSheet visible={!!commandFor} message={commandFor ?? undefined} onClose={() => setCommandFor(null)} />
      <TasksSheet
        visible={tasksOpen}
        chatId={id}
        onClose={() => setTasksOpen(false)}
        onOpenCommand={(message) => {
          setTasksOpen(false);
          afterDismiss(() => router.push({ pathname: "/tasks/command/[id]", params: { id: message.id, chat: id } }));
        }}
      />
      <QuestionSheet
        visible={!!questionFor}
        row={questionRow}
        ask={questionRow?.body.ask}
        onClose={() => setQuestionFor(null)}
        onDecide={(message, decision) => {
          setQuestionFor(null);
          answerCard(message, decision);
        }}
      />
      <ParticipantsSheet
        visible={participantsOpen}
        title={title}
        bots={members}
        agents={agents.map(({ message, agent }) => ({ messageId: message.id, agent }))}
        onClose={() => setParticipantsOpen(false)}
        onOpenAgent={(messageId) => {
          setParticipantsOpen(false);
          // The native sheet dismisses first; a route pushed mid-dismiss can be swallowed.
          afterDismiss(() => router.push({ pathname: "/agent/[id]", params: { id: messageId, chat: id } }));
        }}
        onStopAgent={(messageId) => engine.stopAgent(id, messageId)}
      />
      {reader ? <MessageReader message={reader.message} frame={reader.frame} onClose={() => setReader(null)} /> : null}
    </View>
  );
}

/// The chat's bar, after the reference's: Back and a capsule of the members and title (a tap opens
/// Details) at the start, and one round button at the end, Running tasks while there are any and
/// Details' ⋯ otherwise. iOS draws the bar's glass discs itself; Android's bar is the app's own, the
/// surface color fading into the transcript under round buttons. Memoized, and its options held:
/// the screen renders again with every event in the chat, and new header options re-apply the
/// native bar.
const ChatHeader = memo(function ChatHeader({
  id,
  title,
  members,
  agents,
  onOpenParticipants,
  service,
  working,
  hasTasks,
  onTasks,
  wide,
  top,
}: {
  id: string;
  title: string;
  members: Bot[];
  agents: AgentRun[];
  onOpenParticipants: () => void;
  service?: string;
  working: boolean;
  hasTasks: boolean;
  onTasks: () => void;
  wide: boolean;
  top: number;
}) {
  useLanguage();
  const router = useRouter();
  const p = usePalette();
  const options = useMemo(() => ({ title, headerTitle: "" }), [title]);
  const veil = useMemo(() => {
    const surface = p.background as string;
    return [withAlpha(surface, 0.97), withAlpha(surface, 0.86), withAlpha(surface, 0.68), withAlpha(surface, 0)] as const;
  }, [p]);
  const visibleHeaderHeight = top + ANDROID_BAR_HEIGHT;
  const androidHeaderHeight = visibleHeaderHeight + ANDROID_FADE_HEIGHT;
  const androidHeaderStop = visibleHeaderHeight / androidHeaderHeight;
  const info = () => router.push(`/chat-info/${id}`);
  const tasks = onTasks;
  const label = t("{title}, participants", { title });
  const who = (
    <>
      <ParticipantCluster bots={members} agents={agents} size={30} working={working} service={service} />
      <Text style={[styles.titleText, { color: p.label }]} numberOfLines={1}>
        {title}
      </Text>
    </>
  );
  return (
    <>
      <Stack.Screen options={options} />
      {Platform.OS === "ios" ? (
        <>
          <Stack.Toolbar placement="left">
            <Stack.Toolbar.View>
              <Pressable onPress={onOpenParticipants} style={styles.titleView} accessibilityRole="button" accessibilityLabel={label}>
                {who}
              </Pressable>
            </Stack.Toolbar.View>
          </Stack.Toolbar>
          <Stack.Toolbar placement="right">
            {/* The commands the chat's bots are running, while there are any. */}
            <Stack.Toolbar.Button hidden={!hasTasks} icon="desktopcomputer" accessibilityLabel={t("Running tasks")} onPress={tasks} />
            <Stack.Toolbar.Button hidden={hasTasks} icon="ellipsis" accessibilityLabel={t("Chat info")} onPress={info} />
          </Stack.Toolbar>
        </>
      ) : (
        <View pointerEvents="box-none" style={[styles.androidHeader, { height: androidHeaderHeight }]}>
          <LinearGradient
            pointerEvents="none"
            colors={veil}
            locations={[0, androidHeaderStop * 0.55, androidHeaderStop, 1]}
            start={{ x: 0.5, y: 0 }}
            end={{ x: 0.5, y: 1 }}
            style={StyleSheet.absoluteFill}
          />
          <View pointerEvents="box-none" style={[styles.androidHeaderControls, { height: visibleHeaderHeight, paddingTop: top }]}>
            {/* Beside the sidebar there is nothing to go back to. */}
            {wide ? null : <RoundButton icon="chevron.left" label={t("Back")} onPress={() => router.back()} />}
            <CapsuleButton label={label} onPress={onOpenParticipants}>
              {who}
            </CapsuleButton>
            <View style={{ flex: 1 }} />
            {hasTasks ? <RoundButton icon="desktopcomputer" label={t("Running tasks")} onPress={tasks} /> : <RoundButton icon="ellipsis" label={t("Chat info")} onPress={info} />}
          </View>
        </View>
      )}
    </>
  );
});

const styles = StyleSheet.create({
  transcript: { flex: 1 },
  missing: { flex: 1, alignItems: "center", justifyContent: "center" },
  androidHeader: { position: "absolute", top: 0, left: 0, right: 0, zIndex: 100 },
  androidHeaderControls: { flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 16 },
  composer: { position: "absolute", left: 0, right: 0, bottom: 0 },
  jump: { position: "absolute", right: 12 },
  jumpDisc: {
    width: JUMP_DISC,
    height: JUMP_DISC,
    borderRadius: JUMP_DISC / 2,
    alignItems: "center",
    justifyContent: "center",
  },
  titleView: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    maxWidth: 230,
    paddingLeft: 4,
    paddingRight: 10,
    height: 36,
  },
  titleText: { fontSize: 17, fontWeight: "600", flexShrink: 1, letterSpacing: -0.2 },
});
