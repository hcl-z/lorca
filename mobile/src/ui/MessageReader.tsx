// A message read whole, over the chat: the bubble's folded text grows from where the bubble
// stands to a page of its own, and closes back into it. The full message is laid out once the
// growth is done, so the reader's own height does not stall the motion; the head it shows then is
// the head the bubble showed, so nothing at the top moves.

import { useEffect, useState } from "react";
import { Modal, Pressable, ScrollView, StyleSheet, useWindowDimensions, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import Animated, { Easing, runOnJS, useAnimatedStyle, useSharedValue, withTiming } from "react-native-reanimated";
import { t, useLanguage } from "../i18n";
import type { Message } from "../core/model";
import { collapsedPrefix, type MessageFrame } from "./longMessage";
import { Markdown } from "./Markdown";
import { Symbol } from "./Symbol";
import { usePalette } from "./theme";

/// How long the page takes to grow from the bubble, and to fold back into it.
const OPEN_MS = 260;
const CLOSE_MS = 200;

export function MessageReader({ message, frame, onClose }: { message: Message; frame: MessageFrame; onClose: () => void }) {
  useLanguage();
  const p = usePalette();
  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const text = message.body.kind === "text" ? message.body.text : "";
  // The head is what the bubble already laid out; the whole waits for the page to finish growing.
  const [whole, setWhole] = useState(false);
  const progress = useSharedValue(0);

  useEffect(() => {
    progress.value = withTiming(1, { duration: OPEN_MS, easing: Easing.out(Easing.cubic) }, (done) => {
      if (done) runOnJS(setWhole)(true);
    });
  }, [progress]);

  const close = () => {
    progress.value = withTiming(0, { duration: CLOSE_MS, easing: Easing.in(Easing.cubic) }, (done) => {
      if (done) runOnJS(onClose)();
    });
  };

  const backdrop = useAnimatedStyle(() => ({ opacity: progress.value }));
  const growing = useAnimatedStyle(() => {
    const at = progress.value;
    const back = 1 - at;
    const scaleX = Math.max(0.05, frame.w / width);
    const scaleY = Math.max(0.05, frame.h / height);
    return {
      transform: [
        { translateX: back * (frame.x + frame.w / 2 - width / 2) },
        { translateY: back * (frame.y + frame.h / 2 - height / 2) },
        { scaleX: 1 + back * (scaleX - 1) },
        { scaleY: 1 + back * (scaleY - 1) },
      ],
    };
  });
  const control = useAnimatedStyle(() => ({ opacity: progress.value }));

  return (
    <Modal transparent visible animationType="none" onRequestClose={close} statusBarTranslucent>
    <View style={StyleSheet.absoluteFill} accessibilityViewIsModal onAccessibilityEscape={close}>
      <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: p.background }, backdrop]} />
      <Animated.View style={[StyleSheet.absoluteFill, growing]}>
        <ScrollView contentContainerStyle={{ paddingTop: insets.top + 56, paddingHorizontal: 20, paddingBottom: insets.bottom + 40 }}>
          <Markdown text={whole ? text : collapsedPrefix(text)} color={p.label} maxWidth={Math.min(width - 40, 620)} sources={message.author.kind === "bot"} />
        </ScrollView>
      </Animated.View>
      <Animated.View style={[styles.close, { top: insets.top + 8 }, control]}>
        <Pressable onPress={close} accessibilityRole="button" accessibilityLabel={t("Close")} hitSlop={8} style={({ pressed }) => [{ backgroundColor: p.code }, styles.disc, pressed && { opacity: 0.6 }]}>
          <Symbol name="xmark" size={17} color={p.label} weight="semibold" />
        </Pressable>
      </Animated.View>
    </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  close: { position: "absolute", left: 12 },
  disc: { width: 40, height: 40, borderRadius: 20, alignItems: "center", justifyContent: "center" },
});
