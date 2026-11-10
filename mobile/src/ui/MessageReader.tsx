// A message read whole, over the chat: a white page on black text, the same in either appearance,
// grows out of the bubble. The bubble's own rect becomes the window the page is drawn in — its four
// edges move together, its corner rounds off, and the page's fill goes solid over the bubble while
// the page's text comes up inside it. The page is laid out once at the screen's size and clipped by
// that window, so its text never stretches and never reflows; the full message replaces the head
// only once the growth is done, so its height does not stall the motion. Nothing dims behind it.
//
// It closes by folding back into the bubble rather than fading where it stands: the window lands on
// the bubble's rect with its fill going clear again, so the chat comes back around it and the
// bubble it grew out of is what is left at the end. The motion an open and a close share is the
// panel's rect; the two ends differ in how long they take and in how the fill and the text trade
// places, both of which are measured from the reference.

import { useEffect, useState } from "react";
import { Modal, Pressable, ScrollView, StyleSheet, useWindowDimensions, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { StatusBar } from "expo-status-bar";
import Animated, { Easing, useAnimatedStyle, useReducedMotion, useSharedValue, withTiming } from "react-native-reanimated";
import { scheduleOnRN } from "react-native-worklets";
import { t, useLanguage } from "../i18n";
import type { Message } from "../core/model";
import { collapsedPrefix, type MessageFrame } from "./longMessage";
import { Markdown } from "./Markdown";
import { pageFillOpacity, pageOpacity, panelFrame } from "./readerTransition";
import { Symbol } from "./Symbol";
import { lightPalette } from "./theme";

/// The growth and the fold back, with the curves the reference uses: an ease-in-out onto the page,
/// and a snappier one back, since a reader putting a message away is done with it.
const OPEN_MS = 333;
const CLOSE_MS = 292;
const OPEN_EASE = Easing.bezier(0.64, 0, 0.3, 0.8);
const CLOSE_EASE = Easing.bezier(0.84, 0.44, 0.04, 0.76);

export function MessageReader({ message, frame, onClose }: { message: Message; frame: MessageFrame; onClose: () => void }) {
  useLanguage();
  const p = lightPalette();
  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const reduceMotion = useReducedMotion();
  const text = message.body.kind === "text" ? message.body.text : "";
  // The head is what the bubble already laid out; the whole waits for the panel to finish growing.
  const [whole, setWhole] = useState(false);
  /// How far the panel has grown into the page: 0 on the bubble, 1 the screen. The same value runs
  /// back on the way out.
  const at = useSharedValue(0);
  const closing = useSharedValue(0);

  useEffect(() => {
    if (reduceMotion) {
      at.set(1);
      setWhole(true);
      return;
    }
    at.set(withTiming(1, { duration: OPEN_MS, easing: OPEN_EASE }, (done) => {
      if (done) scheduleOnRN(setWhole, true);
    }));
  }, [at, reduceMotion]);

  const close = () => {
    if (reduceMotion) {
      onClose();
      return;
    }
    closing.set(1);
    at.set(withTiming(0, { duration: CLOSE_MS, easing: CLOSE_EASE }, (done) => {
      if (done) scheduleOnRN(onClose);
    }));
  };

  // The window: the bubble's rect growing to the screen, the corner it started with gone by the end.
  const panel = useAnimatedStyle(() => {
    const rect = panelFrame(frame, width, height, at.get());
    return { left: rect.left, top: rect.top, width: rect.width, height: rect.height, borderRadius: rect.radius };
  });
  // The page's fill over the bubble: clear on the bubble's own view, solid by the opening's third.
  const fill = useAnimatedStyle(() => ({ opacity: pageFillOpacity(at.get()) }));
  // The page's text, behind the bubble's own text in time: it comes up as the fill takes the bubble,
  // and leaves ahead of it on the way back so the bubble is what is showing when the window lands.
  const page = useAnimatedStyle(() => ({ opacity: pageOpacity(at.get(), closing.get() === 1) }));

  return (
    <Modal transparent visible animationType="none" onRequestClose={close} statusBarTranslucent>
    {/* The page is white whatever the appearance, so its status bar is dark too. */}
    <StatusBar style="dark" />
    <View style={StyleSheet.absoluteFill} accessibilityViewIsModal onAccessibilityEscape={close}>
      {/* The window is the only thing clipped: the page inside it keeps the screen's size, pinned to
          the window's top left corner, so it travels with the edge that grows. */}
      <Animated.View style={[styles.panel, panel]}>
        <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: p.background }, fill]} pointerEvents="none" />
        <Animated.View style={[{ width, height }, page]}>
          <ScrollView contentContainerStyle={{ paddingTop: insets.top + 56, paddingHorizontal: 20, paddingBottom: insets.bottom + 40 }}>
            <Markdown text={whole ? text : collapsedPrefix(text)} color={p.label} maxWidth={Math.min(width - 40, 620)} sources={message.author.kind === "bot"} />
          </ScrollView>
          <View style={[styles.close, { top: insets.top + 8 }]}>
            <Pressable onPress={close} accessibilityRole="button" accessibilityLabel={t("Close")} hitSlop={8} style={({ pressed }) => [{ backgroundColor: p.code }, styles.disc, pressed && { opacity: 0.6 }]}>
              <Symbol name="xmark" size={17} color={p.label} weight="semibold" />
            </Pressable>
          </View>
        </Animated.View>
      </Animated.View>
    </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  panel: { position: "absolute", overflow: "hidden" },
  close: { position: "absolute", left: 12 },
  disc: { width: 40, height: 40, borderRadius: 20, alignItems: "center", justifyContent: "center" },
});
