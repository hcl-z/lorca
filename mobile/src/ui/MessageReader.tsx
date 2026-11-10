// A message read whole, over the chat: a white page on black text, the same in either appearance,
// grows out of the bubble. The bubble's fill grows into the page, and the message fades in as the
// page covers the bubble. Only the page's plain fill is stretched; the text is laid out once at
// its reading width, so it never distorts. The full message is laid out once the growth is done,
// so the reader's own height does not stall the motion.
//
// It closes by fading out where it stands, so the chat and the bubble come back through it; folding
// the page back into the bubble would hide the message behind a blank card until it vanished.

import { useEffect, useState } from "react";
import { Modal, Pressable, ScrollView, StyleSheet, useWindowDimensions, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { StatusBar } from "expo-status-bar";
import Animated, { Easing, runOnJS, useAnimatedStyle, useReducedMotion, useSharedValue, withTiming } from "react-native-reanimated";
import { t, useLanguage } from "../i18n";
import type { Message } from "../core/model";
import { collapsedPrefix, type MessageFrame } from "./longMessage";
import { Markdown } from "./Markdown";
import { pageReveal, sheetFill, sheetPose } from "./readerTransition";
import { Symbol } from "./Symbol";
import { lightPalette } from "./theme";

/// How long the page takes to grow from the bubble, and to fade back into the chat.
const OPEN_MS = 280;
const CLOSE_MS = 180;
/// The bubble's corner, which the page's fill starts with.
const RADIUS = 20;

export function MessageReader({ message, frame, onClose }: { message: Message; frame: MessageFrame; onClose: () => void }) {
  useLanguage();
  const p = lightPalette();
  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const reduceMotion = useReducedMotion();
  const text = message.body.kind === "text" ? message.body.text : "";
  // The head is what the bubble already laid out; the whole waits for the page to finish growing.
  const [whole, setWhole] = useState(false);
  const progress = useSharedValue(0);
  const closing = useSharedValue(0);

  useEffect(() => {
    if (reduceMotion) {
      progress.value = 1;
      setWhole(true);
      return;
    }
    progress.value = withTiming(1, { duration: OPEN_MS, easing: Easing.out(Easing.cubic) }, (done) => {
      if (done) runOnJS(setWhole)(true);
    });
  }, [progress, reduceMotion]);

  const close = () => {
    if (reduceMotion) {
      onClose();
      return;
    }
    closing.value = withTiming(1, { duration: CLOSE_MS, easing: Easing.out(Easing.quad) }, (done) => {
      if (done) runOnJS(onClose)();
    });
  };

  // The chat dims, the page's fill fades in as it grows out of the bubble's rect, and its corner
  // rounds off.
  const backdrop = useAnimatedStyle(() => ({ opacity: progress.value * 0.4 * (1 - closing.value) }));
  const sheet = useAnimatedStyle(() => {
    const pose = sheetPose(frame, width, height, progress.value);
    return {
      transform: [
        { translateX: pose.translateX },
        { translateY: pose.translateY },
        { scaleX: pose.scaleX },
        { scaleY: pose.scaleY },
      ],
      borderRadius: RADIUS * (1 - progress.value),
      opacity: sheetFill(progress.value) * (1 - closing.value),
    };
  });
  // The page's text comes in as the fill covers the bubble, rising a little as it does.
  const page = useAnimatedStyle(() => {
    const reveal = pageReveal(progress.value);
    return { opacity: reveal * (1 - closing.value), transform: [{ translateY: (1 - reveal) * 12 }] };
  });

  return (
    <Modal transparent visible animationType="none" onRequestClose={close} statusBarTranslucent>
    {/* The page is white whatever the appearance, so its status bar is dark too. */}
    <StatusBar style="dark" />
    <View style={StyleSheet.absoluteFill} accessibilityViewIsModal onAccessibilityEscape={close}>
      <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: "#000" }, backdrop]} />
      <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: p.background }, sheet]} pointerEvents="none" />
      <Animated.View style={[StyleSheet.absoluteFill, page]}>
        <ScrollView contentContainerStyle={{ paddingTop: insets.top + 56, paddingHorizontal: 20, paddingBottom: insets.bottom + 40 }}>
          <Markdown text={whole ? text : collapsedPrefix(text)} color={p.label} maxWidth={Math.min(width - 40, 620)} sources={message.author.kind === "bot"} />
        </ScrollView>
      </Animated.View>
      <Animated.View style={[styles.close, { top: insets.top + 8 }, page]}>
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
