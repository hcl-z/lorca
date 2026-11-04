// The chat list's search: the bar's round Search button opens this capsule (Cindy's search pill: a
// card-colored capsule with a hairline) at the top of the list,
// focused, with Cancel beside it; Cancel or an empty field closed by the button puts the list back.
// The same on a phone, in a tablet's sidebar, and on Android.

import { useEffect, useRef } from "react";
import { StyleSheet, View, type TextInput as NativeTextInput } from "react-native";
import { Text, TextInput } from "./AppText";
import { Pressable } from "./Pressable";
import { t, useLanguage } from "../i18n";
import { Symbol } from "./Symbol";
import { usePalette } from "./theme";
import { fontWeight, iconSize, lineHeight, radius, spacing, typeScale } from "./tokens";

export function SearchField({ value, onChangeText, onCancel }: { value: string; onChangeText: (value: string) => void; onCancel: () => void }) {
  useLanguage();
  const p = usePalette();
  const input = useRef<NativeTextInput>(null);
  useEffect(() => {
    const timer = setTimeout(() => input.current?.focus(), 50);
    return () => clearTimeout(timer);
  }, []);
  return (
    <View style={styles.wrap}>
      <View style={[styles.field, { backgroundColor: p.cell, borderColor: p.separator }]}>
        <Symbol name="magnifyingglass" size={iconSize.md} color={p.tertiaryLabel} />
        <TextInput
          ref={input}
          style={[styles.input, { color: p.label }]}
          // Uncontrolled: the list re-renders on every keystroke, and a controlled field would
          // drop the characters typed meanwhile.
          defaultValue={value}
          onChangeText={onChangeText}
          placeholder={t("Search")}
          placeholderTextColor={p.tertiaryLabel as any}
          autoCapitalize="none"
          autoCorrect={false}
          returnKeyType="search"
          clearButtonMode="never"
          keyboardAppearance={p.dark ? "dark" : "light"}
          accessibilityLabel={t("Search")}
        />
        {value ? (
          <Pressable
            onPress={() => {
              input.current?.clear();
              onChangeText("");
            }}
            hitSlop={10}
            ripple="borderless"
            rippleRadius={16}
            accessibilityRole="button"
            accessibilityLabel={t("Clear")}
          >
            <Symbol name="xmark.circle.fill" size={iconSize.md} color={p.tertiaryLabel} />
          </Pressable>
        ) : null}
      </View>
      <Pressable onPress={onCancel} hitSlop={8} ripple="borderless" rippleRadius={30} accessibilityRole="button">
        <Text style={[styles.cancel, { color: p.label }]}>{t("Cancel")}</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { flexDirection: "row", alignItems: "center", gap: spacing.md, paddingHorizontal: spacing.lg, paddingTop: spacing.xs, paddingBottom: spacing.md },
  field: { flex: 1, height: 40, flexDirection: "row", alignItems: "center", gap: spacing.sm, paddingHorizontal: spacing.md, borderRadius: radius.pill, borderWidth: StyleSheet.hairlineWidth },
  input: { flex: 1, fontSize: typeScale.bodySmall, paddingVertical: 0 },
  cancel: { fontSize: typeScale.body, lineHeight: lineHeight.body, fontWeight: fontWeight.medium },
});
