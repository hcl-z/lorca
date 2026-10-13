// The floating pieces of the bars and the composer, after the reference's phone app: a round
// button (a white disc with a soft shadow, liquid glass where iOS has it) and a capsule of the
// same height. iOS's own bars draw their buttons as glass discs themselves; these are what the
// app draws where the bar is its own (Android's headers) and around the composer.

import { GlassView, isLiquidGlassAvailable } from "expo-glass-effect";
import { forwardRef, type ReactNode } from "react";
import { Platform, StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { Pressable } from "./Pressable";
import { Symbol } from "./Symbol";
import { floatShadow, usePalette } from "./theme";

export const ROUND = 44;
const GLASS = Platform.OS === "ios" && isLiquidGlassAvailable();

/// A disc or capsule surface: glass on iOS 26, else the chrome color with a hairline and shadow.
export function Float({ style, children, radius = ROUND / 2 }: { style?: StyleProp<ViewStyle>; children?: ReactNode; radius?: number }) {
  const p = usePalette();
  if (GLASS) {
    return (
      <View style={[{ borderRadius: radius }, style]}>
        <GlassView glassEffectStyle="regular" isInteractive style={[StyleSheet.absoluteFill, { borderRadius: radius }]} />
        {children}
      </View>
    );
  }
  return (
    <View style={[{ borderRadius: radius, backgroundColor: p.chrome, borderWidth: StyleSheet.hairlineWidth, borderColor: p.chromeBorder }, floatShadow(p), style]}>
      {children}
    </View>
  );
}

/// A round button with one symbol.
export const RoundButton = forwardRef<View, { icon: string; label: string; onPress?: () => void; onLongPress?: () => void; size?: number; iconSize?: number; disabled?: boolean }>(function RoundButton(
  { icon, label, onPress, onLongPress, size = ROUND, iconSize = 19, disabled },
  ref,
) {
  const p = usePalette();
  return (
    <Pressable
      ref={ref}
      onPress={onPress}
      onLongPress={onLongPress}
      disabled={disabled}
      ripple="borderless"
      rippleRadius={size / 2}
      hitSlop={4}
      accessibilityRole="button"
      accessibilityLabel={label}
      style={({ pressed }) => [{ opacity: disabled ? 0.4 : pressed ? 0.7 : 1, transform: [{ scale: pressed ? 0.96 : 1 }] }]}
    >
      <Float style={[styles.round, { width: size, height: size }]} radius={size / 2}>
        <Symbol name={icon} size={iconSize} color={p.label} weight="medium" />
      </Float>
    </Pressable>
  );
});

/// A capsule button as tall as a round one: the chat bar's members and name.
export function CapsuleButton({ children, label, onPress, style }: { children: ReactNode; label: string; onPress: () => void; style?: StyleProp<ViewStyle> }) {
  return (
    <Pressable onPress={onPress} ripple="borderless" rippleRadius={60} accessibilityRole="button" accessibilityLabel={label} style={({ pressed }) => [{ opacity: pressed ? 0.7 : 1, flexShrink: 1 }, style]}>
      <Float style={styles.capsule}>{children}</Float>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  round: { alignItems: "center", justifyContent: "center" },
  capsule: { height: ROUND, flexDirection: "row", alignItems: "center", gap: 8, paddingLeft: 7, paddingRight: 16, flexShrink: 1 },
});
