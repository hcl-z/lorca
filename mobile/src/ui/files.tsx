// The pieces every file on the phone is drawn with: its icon (Untitled UI's solid set), the start
// of its bytes as text, a picture to pinch, and the page a lone output's card shows of it.

import { File } from "expo-file-system";
import { Image } from "expo-image";
import { useEffect, useState } from "react";
import { Platform, ScrollView, StyleSheet, Text, View } from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, { useAnimatedStyle, useSharedValue, withTiming } from "react-native-reanimated";
import { decodeHead, fileIconName, peekOf, type FilePeek, type PreviewKind } from "../core/files";
import { FILE_ICONS } from "./fileIconAssets";
import { usePalette } from "./theme";

const ICON_NAMES = new Set(Object.keys(FILE_ICONS));

/// The icon a file wears, and the color of its page.
export function fileIcon(name: string, mime: string) {
  return FILE_ICONS[fileIconName(name, mime, ICON_NAMES)] ?? FILE_ICONS.empty;
}

/// A file's icon, `size` points square, in its own colors.
export function FileIcon({ name, mime, size }: { name: string; mime: string; size: number }) {
  return <Image source={fileIcon(name, mime).source} style={{ width: size, height: size }} contentFit="contain" accessible={false} />;
}

/// The start of a file: at most `limit` bytes as text, whether it goes on past them, or undefined
/// when it is not UTF-8 text.
export function readHead(uri: string, limit: number): { text: string; truncated: boolean } | undefined {
  const file = new File(uri);
  const handle = file.open();
  try {
    const size = handle.size ?? limit + 1;
    const bytes = handle.readBytes(Math.min(size, limit + 1));
    const truncated = bytes.length > limit;
    const text = decodeHead(truncated ? bytes.subarray(0, limit) : bytes);
    return text === undefined ? undefined : { text, truncated };
  } finally {
    handle.close();
  }
}

/// The start of a file once it is read, read after the first frame so the screen opens at once.
export function useFileHead(uri: string | undefined, limit: number): { text?: string; truncated: boolean; loading: boolean } {
  const [read, setRead] = useState<{ uri: string; head?: { text: string; truncated: boolean } }>();
  useEffect(() => {
    if (!uri) return;
    let live = true;
    const timer = setTimeout(() => {
      let head: { text: string; truncated: boolean } | undefined;
      try {
        head = readHead(uri, limit);
      } catch {}
      if (live) setRead({ uri, head });
    }, 0);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [uri, limit]);
  if (!uri || read?.uri !== uri) return { truncated: false, loading: !!uri };
  return { text: read.head?.text, truncated: read.head?.truncated ?? false, loading: false };
}

const PEEK_BYTES = 8 << 10;
const peeks = new Map<string, FilePeek>();

/// What a card shows of the file at `uri`, read once per file.
export function useFilePeek(uri: string | undefined, kind: PreviewKind, name: string): FilePeek | undefined {
  const [, redraw] = useState(0);
  const cached = uri ? peeks.get(uri) : undefined;
  useEffect(() => {
    if (!uri || peeks.has(uri)) return;
    const timer = setTimeout(() => {
      let head: { text: string } | undefined;
      try {
        head = readHead(uri, PEEK_BYTES);
      } catch {}
      peeks.set(uri, head ? peekOf(head.text, kind, name) : {});
      redraw((n) => n + 1);
    }, 0);
    return () => clearTimeout(timer);
  }, [uri, kind, name]);
  return cached;
}

export const PEEK_HEIGHT = 96;

/// The start of a file on its card: a page on a ground tinted for the file, rising from below the
/// card's row and fading out before it.
export function FilePeekView({ peek, kind, name, tint }: { peek: FilePeek; kind: PreviewKind; name: string; tint: string }) {
  const p = usePalette();
  const mono = Platform.OS === "ios" ? "Menlo" : "monospace";
  let page: React.ReactNode;
  if (kind === "html") {
    page = (
      <>
        <View style={[styles.browserBar, { backgroundColor: p.fill }]}>
          {["#FF5F57", "#FEBC2E", "#28C840"].map((dot) => (
            <View key={dot} style={[styles.dot, { backgroundColor: dot }]} />
          ))}
          <View style={[styles.address, { backgroundColor: p.separator }]} />
        </View>
        <View style={styles.pageText}>
          <Text style={[styles.peekTitle, { color: p.label }]} numberOfLines={1}>
            {peek.title ?? name}
          </Text>
          {peek.body ? (
            <Text style={[styles.peekBody, { color: p.secondaryLabel }]} numberOfLines={1}>
              {peek.body}
            </Text>
          ) : null}
        </View>
      </>
    );
  } else if (kind === "markdown") {
    page = (
      <View style={styles.pageText}>
        {peek.title ? (
          <Text style={[styles.peekTitle, { color: p.label }]} numberOfLines={1}>
            {peek.title}
          </Text>
        ) : null}
        {peek.body ? (
          <Text style={[styles.peekBody, { color: p.secondaryLabel }]} numberOfLines={3}>
            {peek.body}
          </Text>
        ) : null}
      </View>
    );
  } else if (kind === "table" && peek.rows?.length) {
    page = peek.rows.map((row, i) => (
      <View key={i} style={[styles.peekRow, { borderBottomColor: p.separator }, i === 0 && { backgroundColor: p.fill }]}>
        {row.map((cell, j) => (
          <Text key={j} style={[styles.peekCell, { color: i === 0 ? p.label : p.secondaryLabel }, i === 0 && styles.peekHeader]} numberOfLines={1}>
            {cell}
          </Text>
        ))}
      </View>
    ));
  } else {
    page = (
      <Text style={[styles.peekLines, { color: p.secondaryLabel, fontFamily: mono }]} numberOfLines={7}>
        {(peek.lines ?? []).join("\n")}
      </Text>
    );
  }
  return (
    <View style={[styles.peek, { backgroundColor: tint + "14", borderBottomColor: p.separator }]}>
      <View style={[styles.paper, { backgroundColor: p.background, borderColor: p.separator }]}>{page}</View>
    </View>
  );
}

const MAX_ZOOM = 4;
const DOUBLE_TAP_ZOOM = 2.5;

/// A picture fitted to its box: pinch or double-tap to zoom, drag to look around. iOS zooms in a
/// UIScrollView, with its bounce; Android follows the fingers on the UI thread.
export function ZoomablePicture({ uri }: { uri: string }) {
  return Platform.OS === "ios" ? <ScrollZoom uri={uri} /> : <GestureZoom uri={uri} />;
}

function ScrollZoom({ uri }: { uri: string }) {
  return (
    <ScrollView
      style={styles.fill}
      contentContainerStyle={styles.fill}
      maximumZoomScale={MAX_ZOOM}
      minimumZoomScale={1}
      centerContent
      showsHorizontalScrollIndicator={false}
      showsVerticalScrollIndicator={false}
      contentInsetAdjustmentBehavior="never"
    >
      <Image source={{ uri }} style={styles.fill} contentFit="contain" />
    </ScrollView>
  );
}

function GestureZoom({ uri }: { uri: string }) {
  // The viewer's own size: the picture is centered in it, under the bar.
  const size = useSharedValue({ width: 0, height: 0 });
  const scale = useSharedValue(1);
  const startScale = useSharedValue(1);
  const x = useSharedValue(0);
  const y = useSharedValue(0);
  const startX = useSharedValue(0);
  const startY = useSharedValue(0);

  // How far the zoomed picture may move before its edge leaves the screen's.
  const clamp = (value: number, zoom: number, size: number) => {
    "worklet";
    const room = (size * (zoom - 1)) / 2;
    return Math.min(room, Math.max(-room, value));
  };
  const settle = () => {
    "worklet";
    if (scale.value <= 1) {
      scale.value = withTiming(1);
      x.value = withTiming(0);
      y.value = withTiming(0);
      return;
    }
    x.value = withTiming(clamp(x.value, scale.value, size.value.width));
    y.value = withTiming(clamp(y.value, scale.value, size.value.height));
  };

  const pinch = Gesture.Pinch()
    .onStart(() => {
      startScale.value = scale.value;
      startX.value = x.value;
      startY.value = y.value;
    })
    // The point under the fingers stays under them.
    .onUpdate((event) => {
      const next = Math.min(MAX_ZOOM, Math.max(0.8, startScale.value * event.scale));
      const ratio = next / startScale.value;
      const fx = event.focalX - size.value.width / 2;
      const fy = event.focalY - size.value.height / 2;
      x.value = fx - (fx - startX.value) * ratio;
      y.value = fy - (fy - startY.value) * ratio;
      scale.value = next;
    })
    .onEnd(settle);
  const pan = Gesture.Pan()
    .minPointers(1)
    .averageTouches(true)
    .onStart(() => {
      startX.value = x.value;
      startY.value = y.value;
    })
    .onUpdate((event) => {
      if (scale.value <= 1) return;
      x.value = startX.value + event.translationX;
      y.value = startY.value + event.translationY;
    })
    .onEnd(settle);
  const doubleTap = Gesture.Tap()
    .numberOfTaps(2)
    .onEnd((event) => {
      if (scale.value > 1) {
        scale.value = withTiming(1);
        x.value = withTiming(0);
        y.value = withTiming(0);
        return;
      }
      const fx = event.x - size.value.width / 2;
      const fy = event.y - size.value.height / 2;
      scale.value = withTiming(DOUBLE_TAP_ZOOM);
      x.value = withTiming(clamp(-fx * (DOUBLE_TAP_ZOOM - 1), DOUBLE_TAP_ZOOM, size.value.width));
      y.value = withTiming(clamp(-fy * (DOUBLE_TAP_ZOOM - 1), DOUBLE_TAP_ZOOM, size.value.height));
    });

  const style = useAnimatedStyle(() => ({ transform: [{ translateX: x.value }, { translateY: y.value }, { scale: scale.value }] }));
  return (
    <GestureDetector gesture={Gesture.Simultaneous(pinch, pan, doubleTap)}>
      <View style={styles.fill} onLayout={(e) => (size.value = { width: e.nativeEvent.layout.width, height: e.nativeEvent.layout.height })}>
        <Animated.View style={[styles.fill, style]}>
          <Image source={{ uri }} style={styles.fill} contentFit="contain" />
        </Animated.View>
      </View>
    </GestureDetector>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  peek: { height: PEEK_HEIGHT, overflow: "hidden", borderBottomWidth: StyleSheet.hairlineWidth },
  paper: { position: "absolute", left: 18, right: 18, top: 14, height: PEEK_HEIGHT, borderRadius: 8, borderWidth: StyleSheet.hairlineWidth, overflow: "hidden" },
  browserBar: { flexDirection: "row", alignItems: "center", gap: 4, height: 18, paddingHorizontal: 8 },
  dot: { width: 6, height: 6, borderRadius: 3 },
  address: { flex: 1, height: 9, borderRadius: 4.5, marginLeft: 8, opacity: 0.6 },
  pageText: { paddingHorizontal: 12, paddingVertical: 9, gap: 3 },
  peekTitle: { fontSize: 13, fontWeight: "700" },
  peekBody: { fontSize: 11, lineHeight: 15 },
  peekRow: { flexDirection: "row", height: 16, alignItems: "center", paddingHorizontal: 8, borderBottomWidth: StyleSheet.hairlineWidth },
  peekCell: { flex: 1, fontSize: 9.5, fontVariant: ["tabular-nums"] },
  peekHeader: { fontWeight: "700" },
  peekLines: { fontSize: 9.5, lineHeight: 14, paddingHorizontal: 12, paddingVertical: 9 },
});

