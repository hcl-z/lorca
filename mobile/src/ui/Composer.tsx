// The message field, after the reference's phone app: a round "+" button floating over the
// transcript, which drops down a native menu of attachment sources, and beside it a pill holding
// a multiline input that grows to five lines and, inside the pill's right edge, a disc: Dictate
// (gray) while the field is empty, Send (ink) once there is something to send, and Stop (ink)
// while a turn runs and the field is empty; with something typed during a turn, Stop sits beside
// Send, which steers the turn, as on the desktop. A reply or an edit shows a line above the text,
// and attached files wrap as chips above it. @-mention chips in a group. Where liquid glass is not
// available (older iOS, Android) the pieces are filled. An image pasted into the field attaches
// like a picked photo.

import { MenuView, type MenuAction, type MenuComponentRef } from "@expo/ui/community/menu";
import * as DocumentPicker from "expo-document-picker";
import { haptic } from "./haptics";
import { Image } from "expo-image";
import * as ImagePicker from "expo-image-picker";
import { ExpoSpeechRecognitionModule, useSpeechRecognitionEvent } from "expo-speech-recognition";
import { memo, useEffect, useMemo, useRef, useState } from "react";
import { Linking, Platform, ScrollView, StyleSheet, Text, TextInput, View, type ColorValue, type ImageSourcePropType, type StyleProp, type ViewStyle } from "react-native";
import { Pressable } from "./Pressable";
import { ImagePasteView, type PastedImage } from "../../modules/lorca-core/ImagePasteView";
import type { PickedFile } from "../core/engine";
import { fileSize, MAX_ATTACHMENT_BYTES, MAX_ATTACHMENTS } from "../core/model";
import { BotAvatar } from "./Avatar";
import { AgentTile } from "./agentKit";
import { AttachSheet, type AttachSource } from "./AttachSheet";
import { insertMention as insertMentionText, mentionTrigger, pickMentions, type MentionPick } from "../core/mentions";
import type { MentionTarget } from "../core/participants";
import { automaticLanguage, languageName, pickDictationLanguage, setDictationLanguage, useDictationLanguage, useSupportedLanguages } from "./dictation";
import { t, useLanguage } from "../i18n";
import { joinDictation } from "./format";
import { Symbol } from "./Symbol";
import { floatShadow, Font, usePalette } from "./theme";
import { Float, ROUND, RoundButton } from "./chrome";
import { AndroidIcons } from "./navigation";
import { alert } from "./alert";

const MAX_LINES = 5;
const CHIP = 56;
const FILE_CHIP = 176;
const DISC = 34;

/// Memoized: the chat renders again with every streamed piece of a reply, the composer only when
/// its own inputs change.
export const Composer = memo(function Composer({
  mentionTargets,
  placeholder,
  reply,
  onCancelReply,
  editing,
  onCancelEdit,
  responding = false,
  onStop,
  onSend,
}: {
  mentionTargets: MentionTarget[];
  placeholder: string;
  /** The message the draft answers, shown above the text until it is sent or dropped. */
  reply?: { name: string; text: string } | null;
  onCancelReply?: () => void;
  /** The user's message being edited: its words fill the field, and sending runs it again. */
  editing?: { id: string; text: string } | null;
  onCancelEdit?: () => void;
  /** A turn is in flight in the chat: the disc offers Stop. */
  responding?: boolean;
  onStop?: () => Promise<void>;
  /** The text, its files, the bots its `@Name`s picked, and the coding agents it addresses. */
  onSend: (text: string, attachments: PickedFile[], mentions: string[], agents: string[]) => void;
}) {
  const { language: appLanguage } = useLanguage();
  const p = usePalette();
  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<PickedFile[]>([]);
  const [listening, setListening] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [levels, setLevels] = useState<number[]>([0, 0, 0, 0, 0]);
  /// The transcript accumulates while the pill shows; it lands in the field when the user
  /// stops or sends, the way Grok Bot commits a recording.
  const transcript = useRef("");
  const pendingSend = useRef(false);
  const inputRef = useRef<TextInput>(null);
  /// The bots and agents picked from the `@` chips since the last send, in order. Two can share
  /// a name; the pick says which one the user meant.
  const pickedMentions = useRef<MentionTarget[]>([]);
  const [attachOpen, setAttachOpen] = useState(false);
  const dictationMenuRef = useRef<MenuComponentRef>(null);
  const { language, setting: dictationSetting } = useDictationLanguage();
  const dictationLanguages = useSupportedLanguages();
  const canSend = text.trim().length > 0 || (!editing && attachments.length > 0);
  // A swipe on a bubble starts a reply; the keyboard comes up for it.
  useEffect(() => {
    if (reply) inputRef.current?.focus();
  }, [reply]);
  // Editing takes the message's words into the field and keeps the draft that was there, which
  // comes back when the edit is sent or cancelled.
  const editingId = editing?.id;
  const kept = useRef<{ text: string; attachments: PickedFile[] } | null>(null);
  useEffect(() => {
    if (editing) {
      kept.current ??= { text, attachments };
      setText(editing.text);
      setAttachments([]);
      inputRef.current?.focus();
    } else if (kept.current) {
      setText(kept.current.text);
      setAttachments(kept.current.attachments);
      kept.current = null;
    }
    // Only a new edit or its end; the field's own changes are not a reason to start over.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editingId]);
  // Stop is pressed once; the disc waits for the turn to end.
  const [stopping, setStopping] = useState(false);
  useEffect(() => {
    if (!responding) setStopping(false);
  }, [responding]);
  function stop() {
    if (stopping || !onStop) return;
    haptic.send();
    setStopping(true);
    onStop().catch((error) => {
      setStopping(false);
      alert(t("Couldn't stop"), error instanceof Error ? error.message : String(error));
    });
  }
  const lineHeight = Font.body * 1.3;

  const trigger = useMemo(() => mentionTrigger(text), [text]);
  const mention = useMemo(() => {
    if (!trigger) return null;
    const prefix = trigger.query.toLowerCase();
    const matches = mentionTargets.filter((target) => !prefix || target.name.toLowerCase().startsWith(prefix));
    return matches.length ? { trigger, matches } : null;
  }, [trigger, mentionTargets]);

  function pickMention(target: MentionTarget) {
    // A same-name target replaces the pick; the id in the mention still says which one it was.
    pickedMentions.current = [...pickedMentions.current.filter((picked) => !(picked.kind === target.kind && picked.name === target.name)), target];
    const at = mentionTrigger(text);
    if (!at) return;
    const next = insertMentionText(text, at, target);
    setText(next.text);
    requestAnimationFrame(() => inputRef.current?.focus());
  }

  /// The picks whose `@Name` is still in the text, and a clean slate for the next message.
  function takeMentions(value: string): { mentions: string[]; agents: string[] } {
    const picks: MentionPick[] = pickedMentions.current.map((target) => ({ kind: target.kind, id: target.id, name: target.name }));
    const picked = pickMentions(value, picks);
    pickedMentions.current = [];
    return picked;
  }

  function send() {
    if (listening) {
      // The recording ends, its words land in the field, and the message goes.
      pendingSend.current = true;
      ExpoSpeechRecognitionModule.stop();
      return;
    }
    if (!canSend) return;
    haptic.send();
    const picked = takeMentions(text);
    onSend(text, attachments, picked.mentions, picked.agents);
    setText("");
    setAttachments([]);
  }

  // MARK: - Attachments

  function addFiles(files: PickedFile[]) {
    const problems: string[] = [];
    setAttachments((current) => {
      const next = [...current];
      for (const file of files) {
        if (next.length >= MAX_ATTACHMENTS) {
          problems.push(t("At most {count} files per message.", { count: MAX_ATTACHMENTS }));
          break;
        }
        if (file.size !== undefined && file.size > MAX_ATTACHMENT_BYTES) {
          problems.push(t("{name} is larger than {size} MB.", { name: file.name, size: MAX_ATTACHMENT_BYTES / 1024 / 1024 }));
          continue;
        }
        next.push(file);
      }
      return next;
    });
    if (problems.length) alert(t("Some files were not attached"), problems.join("\n"));
  }

  async function pickPhotos() {
    const result = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ["images"],
      allowsMultipleSelection: true,
      selectionLimit: Math.max(1, MAX_ATTACHMENTS - attachments.length),
      quality: 0.85,
    });
    if (result.canceled) return;
    addFiles(result.assets.map(imageAsset));
  }

  async function takePhoto() {
    const permission = await ImagePicker.requestCameraPermissionsAsync();
    if (!permission.granted) {
      alert(t("Camera access is off"), t("Allow the camera for Lorca in Settings to take a photo."), [
        { text: t("Settings"), onPress: () => void Linking.openSettings() },
        { text: t("OK"), style: "cancel" },
      ]);
      return;
    }
    const result = await ImagePicker.launchCameraAsync({ mediaTypes: ["images"], quality: 0.85 });
    if (result.canceled) return;
    addFiles(result.assets.map(imageAsset));
  }

  async function pickFiles() {
    const result = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true });
    if (result.canceled) return;
    addFiles(
      result.assets.map((asset) => ({
        uri: asset.uri,
        name: asset.name,
        mime: asset.mimeType ?? "application/octet-stream",
        size: asset.size ?? undefined,
      })),
    );
  }

  function pasteImages(images: PastedImage[]) {
    const stamp = timestamp();
    addFiles(
      images.map((image, index) => {
        const extension = image.mime === "image/jpeg" ? "jpg" : image.mime.slice("image/".length).split("+")[0] || "png";
        const suffix = images.length > 1 ? ` ${index + 1}` : "";
        return { ...image, name: `Pasted Image ${stamp}${suffix}.${extension}` };
      }),
    );
  }

  // MARK: - Dictation

  useEffect(() => {
    if (!listening) return;
    const started = Date.now();
    setElapsed(0);
    const timer = setInterval(() => setElapsed(Math.floor((Date.now() - started) / 1000)), 500);
    return () => clearInterval(timer);
  }, [listening]);

  useSpeechRecognitionEvent("result", (event) => {
    transcript.current = event.results[0]?.transcript ?? "";
  });
  useSpeechRecognitionEvent("volumechange", (event) => {
    // -2…10 from the recognizer; anything under 0 is silence.
    const level = Math.min(1, Math.max(0, event.value / 8));
    setLevels((current) => [...current.slice(1), level]);
  });
  useSpeechRecognitionEvent("end", () => finishDictation());
  useSpeechRecognitionEvent("error", (event) => {
    const problem = event.error === "aborted" || event.error === "no-speech" ? null : event.message;
    finishDictation(problem);
  });

  function finishDictation(problem: string | null = null) {
    setListening(false);
    setLevels([0, 0, 0, 0, 0]);
    const words = transcript.current.trim();
    transcript.current = "";
    const next = words ? joinDictation(text, words) : text;
    if (words) setText(next);
    if (problem) {
      pendingSend.current = false;
      alert(t("Dictation stopped"), problem);
      return;
    }
    if (pendingSend.current) {
      pendingSend.current = false;
      if (next.trim() || attachments.length) {
        haptic.send();
        const picked = takeMentions(next);
        onSend(next, attachments, picked.mentions, picked.agents);
        setText("");
        setAttachments([]);
          }
    }
  }

  async function dictate() {
    if (listening) {
      ExpoSpeechRecognitionModule.stop();
      return;
    }
    const permission = await ExpoSpeechRecognitionModule.requestPermissionsAsync();
    if (!permission.granted) {
      alert(t("Dictation needs the microphone"), t("Allow the microphone and speech recognition for Lorca in Settings."), [
        { text: t("Settings"), onPress: () => void Linking.openSettings() },
        { text: t("OK"), style: "cancel" },
      ]);
      return;
    }
    transcript.current = "";
    pendingSend.current = false;
    setListening(true);
    haptic.start();
    ExpoSpeechRecognitionModule.start({
      lang: language,
      interimResults: true,
      continuous: true,
      addsPunctuation: true,
      volumeChangeEventOptions: { enabled: true, intervalMillis: 100 },
    });
  }

  const primary = listening || canSend ? "send" : responding ? "stop" : "dictate";
  // An edit takes no new files.
  const plus = (
    <RoundButton
      icon="plus"
      label={t("Attach")}
      disabled={!!editing}
      onPress={() => {
        haptic.open();
        setAttachOpen(true);
      }}
    />
  );

  const recording = (
    <Pressable
      key="recording"
      onPress={() => ExpoSpeechRecognitionModule.stop()}
      style={({ pressed }) => [styles.recordingPill, { backgroundColor: p.fill, opacity: pressed ? 0.7 : 1 }]}
      accessibilityRole="button"
      accessibilityLabel={t("Stop recording, {seconds} seconds", { seconds: elapsed })}
    >
      <View style={styles.stop}>
        <Symbol name="stop.fill" size={11} color={p.label} weight="bold" />
      </View>
      <Text style={[styles.elapsed, { color: p.label }]}>{`${Math.floor(elapsed / 60)}:${String(elapsed % 60).padStart(2, "0")}`}</Text>
      <View style={styles.bars}>
        {levels.map((level, index) => (
          <View key={index} style={[styles.levelBar, { backgroundColor: p.label, height: 3 + level * 11 }]} />
        ))}
      </View>
    </Pressable>
  );

  // The field sizes itself to its text (Fabric measures a multiline input with no fixed height)
  // up to five lines, past which the text view scrolls. A content-size event is no use here:
  // Fabric emits it only when the layout changes, so a fixed height never learns of a wrapped
  // line. The measurement leaves out a trailing empty line, so the text's own line count is a
  // floor: Return at the end of the text opens a new line at once.
  const breaks = text.split("\n").length;
  const minHeight = Math.min(MAX_LINES, Math.max(1, breaks)) * lineHeight;
  const maxHeight = MAX_LINES * lineHeight;
  // The paste view carries the input's layout; the input fills it.
  const input = (
    <ImagePasteView
      key="input"
      onPasteImages={({ nativeEvent }) => pasteImages(nativeEvent.files)}
      style={styles.inputBox}
    >
      <TextInput
        ref={inputRef}
        value={text}
        onChangeText={setText}
        placeholder={editing ? t("Edit your message") : reply ? t("Reply…") : placeholder}
        placeholderTextColor={p.tertiaryLabel}
        multiline
        keyboardAppearance={p.dark ? "dark" : "light"}
        style={[styles.input, { color: p.label, lineHeight, minHeight, maxHeight }]}
        accessibilityLabel={t("Message")}
      />
    </ImagePasteView>
  );

  const stopButton = (
    <Pressable
      key="stop"
      onPress={stop}
      disabled={stopping}
      hitSlop={8}
      style={({ pressed }) => [styles.disc, { backgroundColor: primary === "stop" ? p.ink : p.fill, opacity: stopping ? 0.5 : pressed ? 0.7 : 1 }]}
      accessibilityRole="button"
      accessibilityLabel={t("Stop responding")}
    >
      <Symbol name="stop.fill" size={12} color={primary === "stop" ? p.inkText : p.label} weight="bold" />
    </Pressable>
  );
  const primaryButton = (
    <Pressable
      key="primary"
      onPress={primary === "send" ? send : () => void dictate()}
      onLongPress={
        primary !== "dictate"
          ? undefined
          : Platform.OS === "ios"
            ? () => void pickDictationLanguage()
            : () => dictationMenuRef.current?.show()
      }
      hitSlop={8}
      style={({ pressed }) => [styles.disc, { backgroundColor: primary === "send" ? p.ink : p.fill, opacity: pressed ? 0.7 : 1 }]}
      accessibilityRole="button"
      accessibilityLabel={primary === "send" ? t("Send") : t("Dictate")}
      accessibilityHint={primary === "dictate" ? t("Long press to choose the language") : undefined}
    >
      <Symbol name={primary === "send" ? "arrow.up" : "mic.fill"} size={16} color={primary === "send" ? p.inkText : p.secondaryLabel} weight="bold" />
    </Pressable>
  );
  const dictationActions = useMemo<MenuAction[]>(
    () => [
      {
        id: "automatic",
        title: t("Automatic ({language})", { language: languageName(automaticLanguage(dictationLanguages)) }),
        state: dictationSetting ? "off" : "on",
      },
      ...dictationLanguages.map((tag) => ({ id: tag, title: languageName(tag), state: dictationSetting === tag ? ("on" as const) : ("off" as const) })),
    ],
    // `appLanguage` names the languages.
    [dictationLanguages, dictationSetting, appLanguage],
  );
  const primaryDisc =
    Platform.OS === "android" && primary === "dictate" ? (
      <MenuView
        key="primary"
        ref={dictationMenuRef}
        actions={dictationActions}
        shouldOpenOnLongPress
        onPressAction={({ nativeEvent }) => setDictationLanguage(nativeEvent.event === "automatic" ? undefined : nativeEvent.event)}
        style={styles.androidDiscHost}
      >
        {primaryButton}
      </MenuView>
    ) : (
      primaryButton
    );

  function attach(source: AttachSource) {
    if (source === "photos") void pickPhotos();
    else if (source === "camera") void takePhoto();
    else void pickFiles();
  }

  return (
    <View style={styles.wrap} pointerEvents="box-none">
      {mention && (
        <ScrollView horizontal keyboardShouldPersistTaps="always" showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chips}>
          {mention.matches.map((target) => (
            <Pressable
              key={`${target.kind}:${target.id}`}
              onPress={() => pickMention(target)}
              style={({ pressed }) => [styles.chip, { backgroundColor: pressed ? p.secondaryFill : p.cell, opacity: pressed ? 0.8 : 1 }]}
            >
              {target.kind === "bot" ? <BotAvatar bot={target.bot} size={20} /> : <AgentTile kind={target.agent.kind} size={20} round />}
              <Text style={[styles.chipText, { color: p.label }]}>{target.name}</Text>
            </Pressable>
          ))}
        </ScrollView>
      )}
      <View style={styles.bar} pointerEvents="box-none">
        {plus}
        <Pressable onPress={() => inputRef.current?.focus()} accessible={false} ripple="none" style={styles.pillPress}>
          <Float style={styles.pill} radius={ROUND / 2}>
            {editing ? (
              <View style={styles.reply}>
                <Symbol name="pencil" size={12} color={p.secondaryLabel} />
                <Text style={[styles.replyText, { color: p.label, fontWeight: "600" }]} numberOfLines={1}>
                  {t("Editing your message")}
                </Text>
                <Pressable onPress={onCancelEdit} hitSlop={14} ripple="borderless" rippleRadius={18} accessibilityRole="button" accessibilityLabel={t("Cancel editing")}>
                  <Symbol name="xmark.circle.fill" size={17} color={p.tertiaryLabel} />
                </Pressable>
              </View>
            ) : reply ? (
              <View style={styles.reply}>
                <Symbol name="arrowshape.turn.up.left.fill" size={12} color={p.secondaryLabel} />
                <Text style={[styles.replyText, { color: p.secondaryLabel }]} numberOfLines={1}>
                  <Text style={{ color: p.label, fontWeight: "600" }}>{t("Replying to {name}", { name: reply.name })}</Text>
                  {"  "}
                  {reply.text}
                </Text>
                <Pressable onPress={onCancelReply} hitSlop={14} ripple="borderless" rippleRadius={18} accessibilityRole="button" accessibilityLabel={t("Cancel reply")}>
                  <Symbol name="xmark.circle.fill" size={17} color={p.tertiaryLabel} />
                </Pressable>
              </View>
            ) : null}
            {attachments.length > 0 && (
              <View style={styles.files}>
                {attachments.map((file, index) => (
                  <View key={`${file.uri}-${index}`} style={styles.fileChip}>
                    {file.mime.startsWith("image/") ? (
                      <Image source={{ uri: file.uri }} style={[styles.thumb, { backgroundColor: p.fill }]} contentFit="cover" />
                    ) : (
                      <View style={[styles.fileCard, { backgroundColor: p.fill }]}>
                        <Symbol name="doc.fill" size={18} color={p.secondaryLabel} />
                        <View style={{ flex: 1 }}>
                          <Text style={[styles.fileName, { color: p.label }]} numberOfLines={1}>
                            {file.name}
                          </Text>
                          {file.size !== undefined && <Text style={[styles.fileSize, { color: p.secondaryLabel }]}>{fileSize(file.size)}</Text>}
                        </View>
                      </View>
                    )}
                    <Pressable
                      onPress={() => setAttachments((current) => current.filter((_, i) => i !== index))}
                      hitSlop={15}
                      ripple="borderless"
                      rippleRadius={16}
                      style={styles.remove}
                      accessibilityLabel={t("Remove {name}", { name: file.name })}
                    >
                      <Symbol name="xmark" size={9} color="#FFFFFF" weight="bold" />
                    </Pressable>
                  </View>
                ))}
              </View>
            )}
            <View style={styles.row}>
              {listening ? recording : input}
              {responding && primary !== "stop" && !listening ? stopButton : null}
              {primary === "stop" ? stopButton : primaryDisc}
            </View>
          </Float>
        </Pressable>
      </View>
      <AttachSheet visible={attachOpen} onClose={() => setAttachOpen(false)} onPick={(source) => void attach(source)} />
    </View>
  );
});

function imageAsset(asset: ImagePicker.ImagePickerAsset): PickedFile {
  const name = asset.fileName ?? `Photo ${timestamp()}.jpg`;
  return {
    uri: asset.uri,
    name,
    mime: asset.mimeType ?? (name.toLowerCase().endsWith(".png") ? "image/png" : "image/jpeg"),
    size: asset.fileSize ?? undefined,
    width: asset.width,
    height: asset.height,
  };
}

function timestamp(): string {
  return new Date().toISOString().slice(0, 19).replace("T", " ").replace(/:/g, ".");
}

function deviceLanguage(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().locale || "en-US";
  } catch {
    return "en-US";
  }
}

const styles = StyleSheet.create({
  wrap: { paddingBottom: 8 },
  bar: { flexDirection: "row", alignItems: "flex-end", gap: 8, marginHorizontal: 12 },
  round: { width: ROUND, height: ROUND, borderRadius: ROUND / 2, overflow: "hidden" },
  plusShadow: { width: ROUND, height: ROUND, borderRadius: ROUND / 2 },
  disabled: { opacity: 0.4 },
  pillPress: { flex: 1 },
  // One line of text makes the pill as tall as the round button: 5 + 34 + 5.
  pill: { minHeight: ROUND, padding: 5 },
  row: { flexDirection: "row", alignItems: "flex-end", gap: 6 },
  // Chips wrap; the padding leaves room for the remove button hanging off a chip's corner.
  files: { flexDirection: "row", flexWrap: "wrap", gap: 12, paddingTop: 8, paddingHorizontal: 6, paddingBottom: 4 },
  reply: { flexDirection: "row", alignItems: "center", gap: 6, paddingTop: 6, paddingBottom: 2, paddingHorizontal: 9 },
  replyText: { flex: 1, fontSize: 13 },
  inputBox: { flex: 1, marginVertical: 7, marginLeft: 10, marginRight: 4 },
  input: { fontSize: Font.body, paddingTop: 0, paddingBottom: 0, margin: 0 },
  // At the right of the field, beside Send, where Grok Bot puts it.
  recordingPill: { flexDirection: "row", alignItems: "center", gap: 6, height: 28, borderRadius: 14, paddingLeft: 4, paddingRight: 10, marginLeft: "auto", marginBottom: 3, marginRight: 4 },
  stop: { width: 22, height: 22, alignItems: "center", justifyContent: "center" },
  elapsed: { fontSize: 14, fontVariant: ["tabular-nums"] },
  bars: { flexDirection: "row", alignItems: "center", gap: 2.5, height: 14 },
  levelBar: { width: 3, borderRadius: 1.5, opacity: 0.85 },
  disc: { width: DISC, height: DISC, borderRadius: DISC / 2, alignItems: "center", justifyContent: "center", overflow: "hidden" },
  androidDiscHost: { width: DISC + 1, height: DISC + 1, alignItems: "center", justifyContent: "center" },
  chips: { paddingHorizontal: 12, paddingBottom: 8, gap: 8 },
  chip: { flexDirection: "row", alignItems: "center", gap: 6, paddingLeft: 4, paddingRight: 10, paddingVertical: 4, borderRadius: 14 },
  chipText: { fontSize: 14, fontWeight: "500" },
  fileChip: { height: CHIP, maxWidth: "100%" },
  thumb: { width: CHIP, height: CHIP, borderRadius: 10 },
  fileCard: { height: CHIP, width: FILE_CHIP, maxWidth: "100%", borderRadius: 10, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 10 },
  fileName: { fontSize: 13, fontWeight: "500" },
  fileSize: { fontSize: Font.caption },
  remove: { position: "absolute", top: -6, right: -6, width: 18, height: 18, borderRadius: 9, backgroundColor: "rgba(0,0,0,0.7)", alignItems: "center", justifyContent: "center" },
});
