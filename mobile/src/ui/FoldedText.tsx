// A message body that folds: a long text lays out only its head inside a fixed height, with a
// fade into the bubble's own fill and Show more under it. What the view lays out is the folded
// prefix itself, so a long message — and every flush of one still streaming — costs its head,
// not its whole length. The full text is laid out only once the reader asks for it.

import { LinearGradient } from "expo-linear-gradient";
import { Pressable, StyleSheet, Text, View, type ColorValue } from "react-native";
import { t, useLanguage } from "../i18n";
import { collapsedPrefix, COLLAPSED_HEIGHT, isLongMessage } from "./longMessage";
import { Markdown } from "./Markdown";
import { Font } from "./theme";

export function FoldedText({
  text,
  color,
  maxWidth,
  size = Font.message,
  sources = false,
  background,
  actionColor,
  expanded = false,
  onToggle,
  height = COLLAPSED_HEIGHT,
}: {
  text: string;
  color: ColorValue;
  maxWidth: number;
  size?: number;
  sources?: boolean;
  /// The fill behind the text, which the fade runs into.
  background: ColorValue;
  actionColor: ColorValue;
  /// Whether the full text shows: an entry of the agent log opens in place. The chat's reader is
  /// a page of its own, so a bubble is never expanded.
  expanded?: boolean;
  /// Show more and Show less. Without it the text never folds.
  onToggle?: () => void;
  height?: number;
}) {
  useLanguage();
  const long = !!onToggle && isLongMessage(text, maxWidth, size, height);
  const folded = long && !expanded;
  return (
    <View style={folded ? { maxHeight: height, overflow: "hidden" } : undefined}>
      <Markdown text={folded ? collapsedPrefix(text) : text} color={color} maxWidth={maxWidth} size={size} sources={sources} />
      {folded ? (
        <>
          <LinearGradient colors={["transparent", background]} style={styles.fade} pointerEvents="none" />
          <Pressable onPress={onToggle} accessibilityRole="button" accessibilityLabel={t("Show more")} style={[styles.action, { backgroundColor: background }]}>
            <Text style={[styles.actionText, { color: actionColor }]}>{t("Show more")}</Text>
          </Pressable>
        </>
      ) : null}
      {long && expanded ? (
        <Pressable onPress={onToggle} accessibilityRole="button" accessibilityLabel={t("Show less")} style={styles.less}>
          <Text style={[styles.actionText, { color: actionColor }]}>{t("Show less")}</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  fade: { position: "absolute", left: 0, right: 0, bottom: 32, height: 56 },
  action: { position: "absolute", left: 0, right: 0, bottom: 0, paddingTop: 8, paddingBottom: 2 },
  actionText: { fontSize: 14, fontWeight: "600" },
  less: { paddingTop: 8, paddingBottom: 2 },
});
