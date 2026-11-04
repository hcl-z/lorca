// A frosted backdrop: a blur with a translucent layer over it, so the glass keeps its color where
// Android's blur is weak. Without `overlayColor` it is a scrim, dark in both appearances; with one
// it is a surface (a bar, a sheet) whose blur follows the appearance. It only draws: touches pass
// through to what is above it.

import type { ReactNode } from "react";
import { BlurView } from "expo-blur";
import { StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { useTheme } from "./ThemeProvider";

/// Chrome that floats over scrolling content: the chat list's bar on Android.
export const FLOATING_CHROME_BLUR_INTENSITY = 55;

export function BlurBackdrop({ intensity = 50, overlayColor, children, style }: { intensity?: number; overlayColor?: string; children?: ReactNode; style?: StyleProp<ViewStyle> }) {
  const { mode, colors } = useTheme();
  const scrim = overlayColor === undefined;
  return (
    <View pointerEvents="none" style={[StyleSheet.absoluteFill, style]}>
      <BlurView intensity={intensity} tint={scrim || mode === "dark" ? "dark" : "light"} style={StyleSheet.absoluteFill} />
      <View style={[StyleSheet.absoluteFill, { backgroundColor: overlayColor ?? colors.overlay }]} />
      {children}
    </View>
  );
}
