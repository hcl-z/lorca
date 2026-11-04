// The token colors of the system's appearance, and style sheets built from them. A style factory
// is a module-level `(colors) => ({…})`, compiled once per appearance and cached by its identity,
// so a list row that renders again only looks its styles up.

import { useMemo } from "react";
import { StyleSheet, useColorScheme } from "react-native";
import { palettes, type ThemeColors, type ThemeMode } from "./tokens";

export function useTheme(): { mode: ThemeMode; colors: ThemeColors } {
  const mode: ThemeMode = useColorScheme() === "dark" ? "dark" : "light";
  return useMemo(() => ({ mode, colors: palettes[mode] }), [mode]);
}

type StyleFactory<T> = (colors: ThemeColors) => T;

const sheets = new WeakMap<StyleFactory<unknown>, Partial<Record<ThemeMode, unknown>>>();

/// `make` must be a module-level constant: a factory made in a render misses the cache every time.
export function useThemedStyles<T extends StyleSheet.NamedStyles<T>>(make: StyleFactory<T>): T {
  const { mode, colors } = useTheme();
  return useMemo(() => {
    const factory = make as StyleFactory<unknown>;
    let byMode = sheets.get(factory);
    if (!byMode) sheets.set(factory, (byMode = {}));
    return (byMode[mode] ??= StyleSheet.create(make(colors))) as T;
  }, [make, mode, colors]);
}
