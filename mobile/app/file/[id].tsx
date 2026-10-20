// A file from a chat or a bot's output, full screen, after the desktop app's viewer: the file's icon,
// name, and what it is in the bar, and the file edge to edge under it. A picture or an SVG pinches
// and zooms; Markdown is drawn as a page; a CSV or TSV file is a grid with its header row kept in
// view; code and data run unwrapped beside line numbers; other text wraps. Markdown, HTML, tables,
// and SVG switch to their source. An HTML page, a PDF, a recording, or any other file is a card that
// opens it in Quick Look on iOS (the share sheet on Android). The bar's menu opens, shares, and
// copies the file, and puts an output's other versions on screen.

import * as Clipboard from "expo-clipboard";
import { Stack, useLocalSearchParams, useRouter } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import { ActivityIndicator, FlatList, Platform, ScrollView, StyleSheet, Text, useWindowDimensions, View } from "react-native";
import { canPreviewFiles } from "../../modules/lorca-core";
import { engine } from "../../src/core/engine";
import { extensionOf, isCode, layOutJSON, lineNumbers, parseTable, peekOf, previewKind, type PreviewKind } from "../../src/core/files";
import { fileSize, type Attachment, type Message } from "../../src/core/model";
import { useOutputs, useStore } from "../../src/core/store";
import { t, useLanguage } from "../../src/i18n";
import { FileIcon, FilePeekView, fileIcon, useFileHead, ZoomablePicture } from "../../src/ui/files";
import { daySeparator } from "../../src/ui/format";
import { Markdown } from "../../src/ui/Markdown";
import { AndroidIcons, IconCloseToolbar } from "../../src/ui/navigation";
import { openFile, outputAttachment, shareFile } from "../../src/ui/outputs";
import { Pressable } from "../../src/ui/Pressable";
import { Font, usePalette } from "../../src/ui/theme";

const TEXT_LIMIT = 256 << 10;
const DOCUMENT_LIMIT = 1 << 20;
const TABLE_ROWS = 1000;
const MONO = Platform.OS === "ios" ? "Menlo" : "monospace";

/// The message a viewer shows, from the chat's loaded messages or its listed outputs.
function useMessage(chatId: string, messageId: string): Message | undefined {
  return useStore((s) => s.chats.find((c) => c.id === chatId)?.messages.find((m) => m.id === messageId) ?? s.outputs[chatId]?.find((m) => m.id === messageId));
}

export default function FileScreen() {
  useLanguage();
  const { id, chat, message: messageId } = useLocalSearchParams<{ id: string; chat: string; message: string }>();
  const router = useRouter();
  const p = usePalette();
  const opened = useMessage(chat, messageId);
  const series = useOutputs(opened?.output ? chat : undefined).find((each) => each.id === opened?.output?.id);
  // An output's viewer shows the version picked in its menu, the one opened to start with.
  const [picked, setPicked] = useState(messageId);
  const message = series?.versions.find((version) => version.id === picked) ?? opened;
  const attachment: Attachment | undefined = message?.output
    ? outputAttachment(message)
    : message?.body.kind === "text"
      ? message.body.attachments?.find((each) => each.id === id)
      : undefined;
  const uri = useStore((s) => (attachment ? s.files[attachment.id] : undefined));
  const error = useStore((s) => (attachment ? s.fileErrors[attachment.id] : undefined));
  useEffect(() => {
    if (attachment && !uri) void engine.fetchFile(attachment);
  }, [uri, attachment]);
  const [source, setSource] = useState(false);
  const kind = attachment ? previewKind(attachment.name, attachment.mime) : "none";
  const limit = kind === "markdown" || kind === "html" || kind === "svg" ? DOCUMENT_LIMIT : TEXT_LIMIT;
  const wantsText = kind === "markdown" || kind === "html" || kind === "text" || kind === "table" || kind === "svg";
  const head = useFileHead(wantsText ? uri : undefined, limit);
  if (!attachment || !message) return null;

  const text = head.text !== undefined ? layOutJSON(head.text, attachment.name) : undefined;
  const switchable = text !== undefined && (kind === "markdown" || kind === "html" || kind === "table" || kind === "svg");
  const detail = [extensionOf(attachment.name).toUpperCase(), attachment.width && attachment.height ? `${attachment.width} × ${attachment.height}` : "", fileSize(attachment.size)]
    .filter(Boolean)
    .join(" · ");
  const versions = series && series.versions.length > 1 ? series.versions : undefined;

  let body: React.ReactNode;
  if (error) {
    body = (
      <Centered>
        <Text style={[styles.cardTitle, { color: p.label }]}>{t("This file couldn't be downloaded.")}</Text>
        <Text style={[styles.cardDetail, { color: p.secondaryLabel }]}>{error.charAt(0).toUpperCase() + error.slice(1)}</Text>
        <PillButton title={t("Try Again")} onPress={() => engine.retryFile(attachment)} />
      </Centered>
    );
  } else if (!uri || head.loading) {
    body = (
      <Centered>
        <ActivityIndicator />
        {!uri && <Text style={[styles.cardDetail, { color: p.secondaryLabel }]}>{t("Downloading…")}</Text>}
      </Centered>
    );
  } else if (kind === "image" || (kind === "svg" && !source)) {
    body = (
      <View style={[styles.fill, { backgroundColor: p.groupedBackground }]}>
        <ZoomablePicture uri={uri} />
      </View>
    );
  } else if (kind === "markdown" && text !== undefined && !source) {
    body = <DocumentView text={text} truncated={head.truncated} />;
  } else if (kind === "table" && text !== undefined && !source) {
    const rows = parseTable(text, extensionOf(attachment.name) === "tsv", head.truncated, TABLE_ROWS);
    body = rows.length ? <TableView rows={rows} more={rows.length === TABLE_ROWS || head.truncated} /> : <CodeView text={text} truncated={head.truncated} />;
  } else if (kind === "html" && text !== undefined && !source) {
    body = (
      <Centered>
        <View style={styles.pagePeek}>
          <FilePeekView peek={peekOf(text, "html")} kind="html" name={attachment.name} tint={fileIcon(attachment.name, attachment.mime).tint} />
        </View>
        <OpenButton attachment={attachment} />
      </Centered>
    );
  } else if (text !== undefined) {
    body = isCode(attachment.name) || source ? <CodeView text={text} truncated={head.truncated} /> : <ProseView text={text} truncated={head.truncated} />;
  } else {
    body = (
      <Centered>
        <FileIcon name={attachment.name} mime={attachment.mime} size={88} />
        <Text style={[styles.cardTitle, { color: p.label }]} numberOfLines={2}>
          {attachment.name}
        </Text>
        <Text style={[styles.cardDetail, { color: p.secondaryLabel }]}>{detail}</Text>
        {kind === "none" && <Text style={[styles.cardDetail, { color: p.tertiaryLabel }]}>{t("No preview for this kind of file.")}</Text>}
        <OpenButton attachment={attachment} />
      </Centered>
    );
  }

  const ios = Platform.OS === "ios";
  return (
    <View style={[styles.fill, { backgroundColor: p.background }]}>
      <Stack.Screen
        options={{
          title: attachment.name,
          headerTitle: () => (
            <View style={styles.title}>
              <FileIcon name={attachment.name} mime={attachment.mime} size={26} />
              <View style={styles.titleText}>
                <Text style={[styles.titleName, { color: p.label }]} numberOfLines={1} ellipsizeMode="middle">
                  {attachment.name}
                </Text>
                <Text style={[styles.titleDetail, { color: p.secondaryLabel }]} numberOfLines={1}>
                  {message.output ? `${daySeparator(new Date(message.created_at * 1000))} · ${fileSize(attachment.size)}` : detail}
                </Text>
              </View>
            </View>
          ),
        }}
      />
      <IconCloseToolbar label={t("Close")} onClose={() => router.back()} />
      <Stack.Toolbar placement="right">
        <Stack.Toolbar.Menu icon={ios ? "ellipsis" : AndroidIcons.more} accessibilityLabel={t("Options")}>
          <Stack.Toolbar.MenuAction icon={ios ? "eye" : undefined} disabled={!uri} onPress={() => void openFile(attachment)}>
            {canPreviewFiles ? t("Open") : t("Open With…")}
          </Stack.Toolbar.MenuAction>
          <Stack.Toolbar.MenuAction icon={ios ? "square.and.arrow.up" : undefined} disabled={!uri} onPress={() => void shareFile(attachment)}>
            {ios ? t("Share…") : t("Share")}
          </Stack.Toolbar.MenuAction>
          {text !== undefined ? (
            <Stack.Toolbar.MenuAction icon={ios ? "doc.on.doc" : undefined} onPress={() => void Clipboard.setStringAsync(head.text ?? "")}>
              {t("Copy Text")}
            </Stack.Toolbar.MenuAction>
          ) : null}
          {versions ? (
            <Stack.Toolbar.Menu inline title={t("Versions")}>
              {versions.map((version) => (
                <Stack.Toolbar.MenuAction key={version.id} isOn={version.id === message.id} onPress={() => setPicked(version.id)}>
                  {t("Version {number}", { number: version.output!.version })}
                </Stack.Toolbar.MenuAction>
              ))}
            </Stack.Toolbar.Menu>
          ) : null}
        </Stack.Toolbar.Menu>
      </Stack.Toolbar>
      {switchable ? <ModeSwitch source={source} onChange={setSource} /> : null}
      {body}
    </View>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <View style={styles.centered}>{children}</View>;
}

/// The file in Quick Look on iOS, the share sheet's apps on Android.
function OpenButton({ attachment }: { attachment: Attachment }) {
  return <PillButton title={canPreviewFiles ? t("Open") : t("Open With…")} primary onPress={() => void openFile(attachment)} />;
}

function PillButton({ title, primary, onPress }: { title: string; primary?: boolean; onPress: () => void }) {
  const p = usePalette();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      style={({ pressed }) => [styles.pill, { backgroundColor: primary ? p.ink : p.fill, opacity: pressed ? 0.75 : 1 }]}
    >
      <Text style={[styles.pillText, { color: primary ? p.inkText : p.label }]}>{title}</Text>
    </Pressable>
  );
}

/// Preview and Source, for a file that has both.
function ModeSwitch({ source, onChange }: { source: boolean; onChange: (source: boolean) => void }) {
  const p = usePalette();
  return (
    <View style={[styles.switchBar, { borderBottomColor: p.separator }]}>
      <View style={[styles.switchTrack, { backgroundColor: p.fill }]} accessibilityRole="tablist">
        {[t("Preview"), t("Source")].map((title, i) => {
          const on = (i === 1) === source;
          return (
            <Pressable
              key={title}
              onPress={() => onChange(i === 1)}
              accessibilityRole="tab"
              accessibilityState={{ selected: on }}
              style={[styles.switchSegment, on && [styles.switchOn, { backgroundColor: p.background }]]}
            >
              <Text style={[styles.switchText, { color: on ? p.label : p.secondaryLabel }, on && styles.switchTextOn]}>{title}</Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

function Note({ truncated, text }: { truncated: boolean; text: string }) {
  const p = usePalette();
  if (!truncated) return null;
  return <Text style={[styles.note, { color: p.tertiaryLabel }]}>{t("Showing the first {size}", { size: fileSize(new TextEncoder().encode(text).length) })}</Text>;
}

/// Markdown drawn as a page to read.
function DocumentView({ text, truncated }: { text: string; truncated: boolean }) {
  const p = usePalette();
  const { width } = useWindowDimensions();
  const column = Math.min(width, 720) - 40;
  return (
    <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.document}>
      <Markdown text={text} color={p.label} maxWidth={column} size={Font.message + 0.5} />
      <Note truncated={truncated} text={text} />
    </ScrollView>
  );
}

/// Code and data: unwrapped lines that slide sideways beside their numbers, which stay.
function CodeView({ text, truncated }: { text: string; truncated: boolean }) {
  const p = usePalette();
  const numbers = useMemo(() => lineNumbers(text), [text]);
  return (
    <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.codeContent}>
      <View style={styles.codeRow}>
        <Text style={[styles.gutter, { color: p.tertiaryLabel, backgroundColor: p.groupedBackground, borderRightColor: p.separator }]}>{numbers}</Text>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.codeLines}>
          <Text style={[styles.code, { color: p.label }]} selectable>
            {text}
          </Text>
        </ScrollView>
      </View>
      <Note truncated={truncated} text={text} />
    </ScrollView>
  );
}

/// Prose and logs, wrapped.
function ProseView({ text, truncated }: { text: string; truncated: boolean }) {
  const p = usePalette();
  return (
    <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.prose}>
      <Text style={[styles.code, { color: p.label }]} selectable>
        {text}
      </Text>
      <Note truncated={truncated} text={text} />
    </ScrollView>
  );
}

/// A table's rows as a grid: the first row as its header, kept in view while the rows scroll, and a
/// number before each row. Columns are as wide as their words, up to a limit.
function TableView({ rows, more }: { rows: string[][]; more: boolean }) {
  const p = usePalette();
  const columns = Math.max(...rows.map((row) => row.length));
  const widths = useMemo(() => {
    const widths = Array.from({ length: columns }, () => 64);
    for (const row of rows.slice(0, 200)) row.forEach((cell, j) => (widths[j] = Math.min(Math.max(widths[j], cell.split("\n")[0].length * 7.6 + 28), 260)));
    return widths;
  }, [rows, columns]);
  const numberWidth = String(rows.length).length * 8 + 24;
  const total = numberWidth + widths.reduce((a, b) => a + b, 0);
  const line = (row: string[], index: number) => {
    const header = index === 0;
    return (
      <View style={[styles.tableRow, { width: total, borderBottomColor: p.separator, backgroundColor: header ? p.groupedBackground : index % 2 ? p.background : p.fill }]}>
        <Text style={[styles.tableNumber, { width: numberWidth, color: p.tertiaryLabel }]}>{header ? "" : index}</Text>
        {widths.map((width, j) => (
          <Text key={j} style={[styles.tableCell, { width, color: p.label }, header && styles.tableHeader]} numberOfLines={1}>
            {(row[j] ?? "").split("\n")[0]}
          </Text>
        ))}
      </View>
    );
  };
  return (
    <ScrollView horizontal style={styles.fill} contentContainerStyle={{ minWidth: "100%" }}>
      <FlatList
        data={rows}
        keyExtractor={(_, index) => String(index)}
        renderItem={({ item, index }) => line(item, index)}
        stickyHeaderIndices={[0]}
        contentInsetAdjustmentBehavior="automatic"
        initialNumToRender={40}
        ListFooterComponent={more ? <Text style={[styles.note, { color: p.tertiaryLabel, paddingLeft: numberWidth }]}>{t("Showing the first {count} rows", { count: rows.length })}</Text> : null}
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  title: { flexDirection: "row", alignItems: "center", gap: 8, maxWidth: 260 },
  titleText: { flexShrink: 1 },
  titleName: { fontSize: 15, fontWeight: "600" },
  titleDetail: { fontSize: 11, fontVariant: ["tabular-nums"] },
  centered: { flex: 1, alignItems: "center", justifyContent: "center", gap: 8, paddingHorizontal: 32 },
  cardTitle: { fontSize: Font.body, fontWeight: "600", textAlign: "center", marginTop: 8 },
  cardDetail: { fontSize: Font.small, textAlign: "center" },
  pagePeek: { width: 280, borderRadius: 16, overflow: "hidden", marginBottom: 8 },
  pill: { marginTop: 14, paddingHorizontal: 22, height: 40, borderRadius: 20, alignItems: "center", justifyContent: "center" },
  pillText: { fontSize: Font.body - 1, fontWeight: "600" },
  switchBar: { alignItems: "center", paddingVertical: 8, borderBottomWidth: StyleSheet.hairlineWidth },
  switchTrack: { flexDirection: "row", padding: 2, borderRadius: 9 },
  switchSegment: { paddingHorizontal: 18, height: 28, borderRadius: 7, alignItems: "center", justifyContent: "center" },
  switchOn: { shadowColor: "#000", shadowOpacity: 0.12, shadowRadius: 3, shadowOffset: { width: 0, height: 1 }, elevation: 1 },
  switchText: { fontSize: 13 },
  switchTextOn: { fontWeight: "600" },
  document: { alignSelf: "center", paddingHorizontal: 20, paddingTop: 20, paddingBottom: 56, gap: 20 },
  codeContent: { paddingBottom: 48 },
  codeRow: { flexDirection: "row" },
  gutter: { fontFamily: MONO, fontSize: 12, lineHeight: 19, textAlign: "right", paddingLeft: 12, paddingRight: 10, paddingTop: 14, borderRightWidth: StyleSheet.hairlineWidth },
  codeLines: { paddingLeft: 12, paddingRight: 24, paddingTop: 14 },
  code: { fontFamily: MONO, fontSize: 12, lineHeight: 19 },
  prose: { padding: 18, paddingBottom: 48, gap: 16 },
  note: { fontSize: Font.caption, paddingHorizontal: 16, paddingTop: 14 },
  tableRow: { flexDirection: "row", alignItems: "center", height: 36, borderBottomWidth: StyleSheet.hairlineWidth },
  tableNumber: { fontSize: 11, textAlign: "right", paddingRight: 12, fontVariant: ["tabular-nums"] },
  tableCell: { fontSize: 13, paddingHorizontal: 12, fontVariant: ["tabular-nums"] },
  tableHeader: { fontWeight: "600" },
});
