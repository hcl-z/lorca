// The phone's quick sheets, on the native bottom sheet Expo SDK 57 ships
// (`@expo/ui/community/bottom-sheet`): a content-sized action list, a picker, a one-line form, or
// a short scroll. The native sheet owns the keyboard, the drag indicator, and the scrim; we give
// it the palette and the content.
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
  const p = usePalette();
  const insets = useSafeAreaInsets();
  const ref = useRef<SheetRef>(null);
  // A sheet stands only while it is up, and until its dismissal finishes: a screen that keeps
  // several closed sheets in its tree would mount a native modal for each on every open.
  const [mounted, setMounted] = useState(visible);
  useEffect(() => {
    if (visible) {
      setMounted(true);
      return;
    }
    const timer = setTimeout(() => setMounted(false), SHEET_DISMISS_MS);
    return () => clearTimeout(timer);
  }, [visible]);
  useEffect(() => {
    if (!mounted) return;
    if (visible) ref.current?.present();
    else ref.current?.dismiss();
  }, [visible, mounted]);
  if (!mounted) return null;
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
      backgroundStyle={{ backgroundColor: p.cell }}
    >
      {scroll ? (
        <BottomSheetScrollView
          style={{ flex: 1 }}
          contentContainerStyle={{ paddingBottom: insets.bottom + 12 }}
          keyboardShouldPersistTaps="handled"
        >
          {body}
        </BottomSheetScrollView>
      ) : (
        <BottomSheetView style={{ paddingBottom: insets.bottom + 12 }}>{body}</BottomSheetView>
      )}
    </BottomSheetModal>
  );
}

export function SheetHeader({ title, subtitle, onClose, closeLabel }: { title: string; subtitle?: string; onClose?: () => void; closeLabel?: string }) {
  useLanguage();
  const p = usePalette();
  return (
    <View style={styles.header}>
      <View style={styles.headerText}>
        <Text accessibilityRole="header" style={[styles.title, { color: p.label }]}>
          {title}
        </Text>
        {subtitle ? <Text style={[styles.subtitle, { color: p.secondaryLabel }]}>{subtitle}</Text> : null}
      </View>
      {onClose ? (
        <Pressable accessibilityRole="button" accessibilityLabel={closeLabel ?? t("Close")} onPress={onClose} hitSlop={12} style={styles.close}>
          <Symbol name="xmark" size={15} color={p.secondaryLabel} weight="semibold" />
        </Pressable>
      ) : null}
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
      {tile ? <View style={styles.tile}>{tile}</View> : icon ? <Symbol name={icon} size={20} color={destructive ? p.red : p.secondaryLabel} /> : null}
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
          style={[styles.search, { color: p.label, backgroundColor: p.fill }]}
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
            trailing={item.value === selected ? <Symbol name="checkmark" size={17} color={p.tint} weight="semibold" /> : null}
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

const styles = StyleSheet.create({
  header: { flexDirection: "row", alignItems: "flex-start", gap: 12, paddingHorizontal: 16, paddingTop: 6, paddingBottom: 10 },
  headerText: { flex: 1, gap: 2 },
  title: { fontSize: 18, fontWeight: "700", letterSpacing: -0.2 },
  subtitle: { fontSize: 13, lineHeight: 18 },
  close: { width: 28, height: 28, borderRadius: 14, alignItems: "center", justifyContent: "center" },
  row: { minHeight: 52, flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 16, paddingVertical: 10, borderBottomWidth: StyleSheet.hairlineWidth },
  tile: { width: 28, alignItems: "center", justifyContent: "center" },
  rowText: { flex: 1, gap: 2 },
  rowTitle: { fontSize: 16, fontWeight: "500" },
  rowSubtitle: { fontSize: 13, lineHeight: 17 },
  search: { height: 38, borderRadius: 10, paddingHorizontal: 12, fontSize: 16, marginHorizontal: 16, marginBottom: 8 },
});
