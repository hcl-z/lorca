// The app's push button: a filled capsule in ink for the action a card or screen leads with, a
// quiet fill with a hairline for the others, and red words for one that removes something. An
// icon may lead its title. A press sinks it on a spring and taps the haptics; a disabled one is
// faded and takes no press.

import * as Haptics from "expo-haptics";
import type { ReactNode } from "react";
import { ActivityIndicator, Platform, Pressable, StyleSheet, Text, type StyleProp, type ViewStyle } from "react-native";
import Animated, { useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";
import { Symbol } from "./Symbol";
import { usePalette } from "./theme";

export type ButtonKind = "primary" | "secondary" | "destructive";

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);
const SPRING = { damping: 18, stiffness: 420, mass: 0.6 };

export function Button({
  title,
  icon,
  kind = "secondary",
  size = "regular",
  disabled,
  busy,
  onPress,
  style,
  children,
}: {
  title: string;
  icon?: string;
  kind?: ButtonKind;
  size?: "small" | "regular" | "large";
  disabled?: boolean;
  /// Shows a spinner in place of the icon, and takes no press.
  busy?: boolean;
  onPress: () => void;
  style?: StyleProp<ViewStyle>;
  children?: ReactNode;
}) {
  const p = usePalette();
  const scale = useSharedValue(1);
  const animated = useAnimatedStyle(() => ({ transform: [{ scale: scale.value }] }));
  const off = disabled || busy;
  const fill = kind === "primary" ? p.ink : p.fill;
  const ink = kind === "primary" ? p.inkText : kind === "destructive" ? p.red : p.label;
  const height = size === "small" ? 32 : size === "large" ? 50 : 42;
  return (
    <AnimatedPressable
      onPress={() => {
        if (Platform.OS === "ios") void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        onPress();
      }}
      onPressIn={() => (scale.value = withSpring(0.96, SPRING))}
      onPressOut={() => (scale.value = withSpring(1, SPRING))}
      disabled={off}
      accessibilityRole="button"
      accessibilityLabel={title}
      accessibilityState={{ disabled: !!off, busy: !!busy }}
      android_ripple={{ color: kind === "primary" ? "rgba(255,255,255,0.18)" : "rgba(0,0,0,0.08)", foreground: true }}
      style={[
        styles.button,
        { height, borderRadius: height / 2, backgroundColor: fill, paddingHorizontal: size === "small" ? 14 : 20, opacity: disabled ? 0.4 : 1 },
        kind !== "primary" && { borderWidth: StyleSheet.hairlineWidth, borderColor: p.cardBorder },
        animated,
        style,
      ]}
    >
      {busy ? <ActivityIndicator size="small" color={ink as string} /> : icon ? <Symbol name={icon} size={size === "small" ? 15 : 17} color={ink} weight="semibold" /> : null}
      <Text style={[styles.title, { color: ink, fontSize: size === "small" ? 14 : 16 }]} numberOfLines={1}>
        {title}
      </Text>
      {children}
    </AnimatedPressable>
  );
}

const styles = StyleSheet.create({
  button: { flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 7, overflow: "hidden" },
  title: { fontWeight: "600", letterSpacing: -0.1 },
});
