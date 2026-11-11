// The composer's "+" menu, drawn as the desktop app draws its drawer: a rounded card floating
// above the composer, not a sheet sliding up from the bottom. A press on the scrim or a Back
// closes it. Each row carries its words on the left and what it does at the far end; the review
// mode's row is one tap a mode, so the mode is always one tap from the message being typed.

import { Modal, Pressable, ScrollView, StyleSheet, Text, useWindowDimensions, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { t, useLanguage } from "../i18n";
import type { ReviewMode } from "../core/model";
import { Symbol } from "./Symbol";
import { Font, menuShadow, usePalette } from "./theme";

export type AttachSource = "photos" | "camera" | "file";

/// What the drawer can do besides attach a file.
export interface ComposerActions {
  /** The bot's own review mode, and what one tap moves it to. */
  mode: ReviewMode;
  onNextMode: () => void;
  onAttach: (source: AttachSource) => void;
  onCompact: () => void;
  /** A coding agent that takes images and nothing else leaves out Choose File. */
  imagesOnly?: boolean;
}

/// The review mode, in the words the settings use.
export function reviewModeTitle(mode: ReviewMode): string {
  return mode === "auto" ? t("Automatic") : mode === "open" ? t("Off") : t("By Access");
}

type Item = { key: string; icon: string; title: string; detail?: string; onPress: () => void };

function groupHeading(text: string) {
  return { key: `heading:${text}`, heading: text };
}

export function ComposerMenu({ visible, onClose, bottom, actions }: { visible: boolean; onClose: () => void; bottom: number; actions: ComposerActions }) {
  useLanguage();
  const p = usePalette();
  const insets = useSafeAreaInsets();
  const window = useWindowDimensions();
  const { onAttach, imagesOnly } = actions;
  const items: (Item | { heading: string; key: string })[] = [
    groupHeading(t("Add")),
    { key: "files", icon: "paperclip", title: t("Photo Library"), onPress: () => onAttach("photos") },
    { key: "camera", icon: "camera", title: t("Take Photo"), onPress: () => onAttach("camera") },
    ...(imagesOnly ? [] : [{ key: "file", icon: "folder", title: t("Choose File"), onPress: () => onAttach("file") }]),
    groupHeading(t("Instructions")),
    { key: "mode", icon: "shield", title: t("Permission"), detail: reviewModeTitle(actions.mode), onPress: actions.onNextMode },
    { key: "compact", icon: "arrow.down.right.and.arrow.up.left", title: t("Compact"), detail: t("Summarize older messages"), onPress: actions.onCompact },
  ];
  return (
    <Modal transparent visible={visible} animationType="fade" onRequestClose={onClose} statusBarTranslucent>
      {/* A press anywhere but the card closes it, as the desktop's popover does. The card keeps
          the composer's width, inside the screen's own inset. */}
      <Pressable style={styles.scrim} onPress={onClose} accessibilityLabel={t("Close")}>
        <Pressable
          style={[styles.card, { backgroundColor: p.cell, borderColor: p.separator, bottom: insets.bottom + bottom + 14, left: insets.left + 12, right: insets.right + 12 }, menuShadow(p)]}
          onPress={() => {}}
        >
          <ScrollView bounces={false} style={{ maxHeight: 420 }} contentContainerStyle={{ paddingVertical: 6 }}>
            {items.map((item) =>
              "heading" in item ? (
                <Text key={item.key} style={[styles.heading, { color: p.secondaryLabel }]}>
                  {item.heading}
                </Text>
              ) : (
                <Pressable key={item.key} onPress={item.onPress} accessibilityRole="button" accessibilityLabel={item.title} style={({ pressed }) => [styles.row, pressed && { backgroundColor: p.fill }]}>
                  <Symbol name={item.icon} size={17} color={p.secondaryLabel} />
                  <Text style={[styles.title, { color: p.label }]} numberOfLines={1}>
                    {item.title}
                  </Text>
                  <View style={{ flex: 1 }} />
                  {item.detail ? (
                    <Text style={[styles.detail, { color: p.secondaryLabel }]} numberOfLines={1}>
                      {item.detail}
                    </Text>
                  ) : null}
                </Pressable>
              ),
            )}
          </ScrollView>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

const styles = StyleSheet.create({
  scrim: { flex: 1, backgroundColor: "transparent" },
  card: { position: "absolute", borderRadius: 16, borderWidth: StyleSheet.hairlineWidth, overflow: "hidden" },
  heading: { fontSize: 12, fontWeight: "600", paddingHorizontal: 14, paddingTop: 10, paddingBottom: 4 },
  row: { minHeight: 42, flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 14, paddingVertical: 8 },
  title: { fontSize: Font.body, flexShrink: 0 },
  detail: { fontSize: Font.small, maxWidth: "52%", marginLeft: 10 },
});
