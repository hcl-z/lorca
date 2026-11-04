// The phone's quick sheets, on the native bottom sheet Expo SDK 57 ships
// (`@expo/ui/community/bottom-sheet`): a content-sized action list, a picker, a one-line form, or
// a short scroll. The native sheet owns the keyboard, the drag indicator, the corners, and the
// scrim; we give it Cindy's sheet surface and the content: a 44 pt header with the title centered
// at 16/22 semibold, and 52 pt rows on hairlines.
//
// One sheet at a time per screen. To open another after this one, pass the next step to
// `onDismiss` (or `dismissThen`), never present both at once: iOS dismisses a sheet and presents
// the next only after the animation.

import { useEffect, useRef, useState, type ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import {
  BottomSheetFlatList,
  BottomSheetModal,
  BottomSheetScrollView,
  BottomSheetTextInput,
  BottomSheetView,
  type BottomSheetMethods,
} from "@expo/ui/community/bottom-sheet";
import { Pressable } from "./Pressable";
import { Symbol } from "./Symbol";
import { usePalette } from "./theme";
import { useTheme } from "./ThemeProvider";
import { fontWeight, iconSize, lineHeight, radius, spacing, typeScale } from "./tokens";
import { haptic } from "./haptics";
import { SHEET_PRESETS, orderActions } from "./sheetPresets";
import { t, useLanguage } from "../i18n";

export type SheetRef = BottomSheetMethods;

/// How long the native sheet takes to animate out; a caller that must present another sheet or
/// push a route waits this long after setting `visible` false.
export const SHEET_DISMISS_MS = 260;

export function afterDismiss(next: () => void) {
  setTimeout(next, SHEET_DISMISS_MS);
}

export interface SheetProps {
  visible: boolean;
  onClose: () => void;
  title?: string;
  subtitle?: string;
  snapPoints?: (string | number)[];
  scroll?: boolean;
  children: ReactNode;
}

export function Sheet({ visible, onClose, title, subtitle, snapPoints, scroll, children }: SheetProps) {
  const { colors } = useTheme();
  const insets = useSafeAreaInsets();
  const ref = useRef<SheetRef>(null);
  useEffect(() => {
    if (visible) ref.current?.present();
    else ref.current?.dismiss();
  }, [visible]);
  const body = (
    <>
      {title ? <SheetHeader title={title} subtitle={subtitle} onClose={onClose} /> : null}
      {children}
    </>
  );
  return (
    <BottomSheetModal
      ref={ref}
      snapPoints={snapPoints}
      enablePanDownToClose
      onDismiss={onClose}
      backgroundStyle={{ backgroundColor: colors.sheetSurface }}
    >
      {scroll ? (
        <BottomSheetScrollView
          style={{ flex: 1 }}
          contentContainerStyle={{ paddingBottom: insets.bottom + spacing.md }}
          keyboardShouldPersistTaps="handled"
        >
          {body}
        </BottomSheetScrollView>
      ) : (
        <BottomSheetView style={{ paddingBottom: insets.bottom + spacing.md }}>{body}</BottomSheetView>
      )}
    </BottomSheetModal>
  );
}

export function SheetHeader({ title, subtitle, onClose, closeLabel }: { title: string; subtitle?: string; onClose?: () => void; closeLabel?: string }) {
  useLanguage();
  const p = usePalette();
  return (
    <View style={styles.header}>
      <View style={styles.headerBar}>
        <Text accessibilityRole="header" style={[styles.title, { color: p.label }]} numberOfLines={1}>
          {title}
        </Text>
        {onClose ? (
          <Pressable accessibilityRole="button" accessibilityLabel={closeLabel ?? t("Close")} onPress={onClose} hitSlop={12} style={[styles.close, { backgroundColor: p.fill }]}>
            <Symbol name="xmark" size={iconSize.sm} color={p.secondaryLabel} weight="semibold" />
          </Pressable>
        ) : null}
      </View>
      {subtitle ? <Text style={[styles.subtitle, { color: p.secondaryLabel }]}>{subtitle}</Text> : null}
    </View>
  );
}

export interface SheetAction {
  label: string;
  icon?: string;
  destructive?: boolean;
  disabled?: boolean;
  onPress: () => void;
}

export function SheetActionRow({
  icon,
  tile,
  title,
  subtitle,
  trailing,
  onPress,
  accessibilityLabel,
  destructive,
  disabled,
}: {
  icon?: string;
  tile?: ReactNode;
  title: string;
  subtitle?: string;
  trailing?: ReactNode;
  onPress?: () => void;
  accessibilityLabel?: string;
  destructive?: boolean;
  disabled?: boolean;
}) {
  const p = usePalette();
  const tint = destructive ? p.red : p.label;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? title}
      accessibilityState={{ disabled: !!disabled }}
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [styles.row, { borderBottomColor: p.separator, opacity: disabled ? 0.45 : 1, backgroundColor: pressed ? p.fill : "transparent" }]}
    >
      {tile ? <View style={styles.tile}>{tile}</View> : icon ? <Symbol name={icon} size={iconSize.action} color={destructive ? p.red : p.secondaryLabel} /> : null}
      <View style={styles.rowText}>
        <Text style={[styles.rowTitle, { color: tint }]} numberOfLines={1}>
          {title}
        </Text>
        {subtitle ? (
          <Text style={[styles.rowSubtitle, { color: p.secondaryLabel }]} numberOfLines={2}>
            {subtitle}
          </Text>
        ) : null}
      </View>
      {trailing}
    </Pressable>
  );
}

export function ActionSheet({ visible, title, subtitle, actions, onClose, snapPoints }: { visible: boolean; title?: string; subtitle?: string; actions: SheetAction[]; onClose: () => void; snapPoints?: (string | number)[] }) {
  return (
    <Sheet visible={visible} onClose={onClose} title={title} subtitle={subtitle} snapPoints={snapPoints ?? SHEET_PRESETS.actions} scroll={actions.length > 6}>
      {orderActions(actions).map((action) => (
        <SheetActionRow
          key={action.label}
          icon={action.icon}
          title={action.label}
          destructive={action.destructive}
          disabled={action.disabled}
          onPress={() => {
            onClose();
            afterDismiss(action.onPress);
          }}
        />
      ))}
    </Sheet>
  );
}

export interface SheetPickerOption {
  value: string;
  label: string;
  description?: string;
}

export function SheetPicker({
  visible,
  title,
  options,
  selected,
  onPick,
  onClose,
  search,
}: {
  visible: boolean;
  title: string;
  options: SheetPickerOption[];
  selected?: string;
  onPick: (value: string) => void;
  onClose: () => void;
  search?: boolean;
}) {
  useLanguage();
  const p = usePalette();
  const [query, setQuery] = useState("");
  useEffect(() => {
    if (!visible) setQuery("");
  }, [visible]);
  const shown = query.trim() ? options.filter((option) => option.label.toLowerCase().includes(query.trim().toLowerCase())) : options;
  return (
    <Sheet visible={visible} onClose={onClose} title={title} snapPoints={SHEET_PRESETS.picker}>
      {search ? (
        <BottomSheetTextInput
          value={query}
          onChangeText={setQuery}
          placeholder={t("Search")}
          placeholderTextColor={p.tertiaryLabel as string}
          autoCorrect={false}
          style={[styles.search, { color: p.label, backgroundColor: p.cell, borderColor: p.separator }]}
        />
      ) : null}
      <BottomSheetFlatList
        data={shown}
        keyExtractor={(option) => option.value}
        keyboardShouldPersistTaps="handled"
        style={{ maxHeight: 460 }}
        renderItem={({ item }) => (
          <SheetActionRow
            title={item.label}
            subtitle={item.description}
            trailing={item.value === selected ? <Symbol name="checkmark" size={iconSize.md} color={p.tint} weight="semibold" /> : null}
            onPress={() => {
              onPick(item.value);
              onClose();
            }}
          />
        )}
      />
    </Sheet>
  );
}

/// The sheet header's bar, as tall as a navigation bar.
const HEADER = 44;
/// The close disc in the header's trailing corner.
const CLOSE = 30;

const styles = StyleSheet.create({
  header: { paddingHorizontal: spacing.lg, paddingBottom: spacing.sm, gap: 2 },
  headerBar: { height: HEADER, alignItems: "center", justifyContent: "center", paddingHorizontal: CLOSE + spacing.sm },
  title: { fontSize: typeScale.body, lineHeight: lineHeight.body, fontWeight: fontWeight.semibold, textAlign: "center" },
  subtitle: { fontSize: typeScale.footnote, lineHeight: lineHeight.caption, textAlign: "center" },
  close: { position: "absolute", right: 0, width: CLOSE, height: CLOSE, borderRadius: CLOSE / 2, alignItems: "center", justifyContent: "center" },
  row: { minHeight: 52, flexDirection: "row", alignItems: "center", gap: spacing.md, paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, borderBottomWidth: StyleSheet.hairlineWidth },
  tile: { width: 28, alignItems: "center", justifyContent: "center" },
  rowText: { flex: 1, minWidth: 0, gap: 2 },
  rowTitle: { fontSize: typeScale.body, lineHeight: lineHeight.body, fontWeight: fontWeight.medium },
  rowSubtitle: { fontSize: typeScale.footnote, lineHeight: lineHeight.caption },
  search: { height: 40, borderRadius: radius.container, borderWidth: StyleSheet.hairlineWidth, paddingHorizontal: spacing.md, fontSize: typeScale.bodySmall, marginHorizontal: spacing.lg, marginBottom: spacing.sm },
});
