// The chat list's search: the bar's round Search button opens this capsule at the top of the list,
// focused, with Cancel beside it; Cancel or an empty field closed by the button puts the list back.
// The same on a phone, in a tablet's sidebar, and on Android.

import { useEffect, useRef } from "react";
import { StyleSheet, Text, TextInput, View } from "react-native";
import { Pressable } from "./Pressable";
import { t, useLanguage } from "../i18n";
import { Float, ROUND } from "./chrome";
import { Symbol } from "./Symbol";
import { Font, usePalette } from "./theme";

export function SearchField({ value, onChangeText, onCancel }: { value: string; onChangeText: (value: string) => void; onCancel: () => void }) {
  useLanguage();
  const p = usePalette();
  const input = useRef<TextInput>(null);
  useEffect(() => {
    const timer = setTimeout(() => input.current?.focus(), 50);
    return () => clearTimeout(timer);
  }, []);
  return (
    <View style={styles.wrap}>
      <Float style={styles.field}>
        <Symbol name="magnifyingglass" size={17} color={p.secondaryLabel} />
        <TextInput
          ref={input}
          style={[styles.input, { color: p.label }]}
          // Uncontrolled: the list re-renders on every keystroke, and a controlled field would
          // drop the characters typed meanwhile.
          defaultValue={value}
          onChangeText={onChangeText}
          placeholder={t("Search")}
          placeholderTextColor={p.secondaryLabel as any}
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
            <Symbol name="xmark.circle.fill" size={17} color={p.secondaryLabel} />
          </Pressable>
        ) : null}
      </Float>
      <Pressable onPress={onCancel} hitSlop={8} ripple="borderless" rippleRadius={30} accessibilityRole="button">
        <Text style={[styles.cancel, { color: p.label }]}>{t("Cancel")}</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 16, paddingTop: 4, paddingBottom: 10 },
  field: { flex: 1, height: ROUND, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 14 },
  input: { flex: 1, fontSize: Font.body, paddingVertical: 0 },
  cancel: { fontSize: Font.body, fontWeight: "500" },
});
