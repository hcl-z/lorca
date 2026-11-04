// A message's bubble, after Cindy's (`session/messageBubbleStyles.ts`): a 12 pt corner, a hairline
// edge, and 12 pt of padding; the user's bubble is the card color at the trailing edge, up to 86%
// of the pane.

import { StyleSheet, type ViewStyle } from "react-native";
import { radius, spacing, type ThemeColors } from "./tokens";

/// A bubble's padding on each side.
export const BUBBLE_PADDING = spacing.md;
/// The share of the pane a user's bubble may take; a bot's column keeps the row's 80%.
export const USER_BUBBLE_SHARE = 0.86;

export const messageBubbleBase: ViewStyle = {
  borderRadius: radius.container,
  borderWidth: StyleSheet.hairlineWidth,
  gap: spacing.xs,
  padding: BUBBLE_PADDING,
};

export const compactMessageBubble: ViewStyle = {
  gap: 6,
  paddingHorizontal: spacing.md,
  paddingVertical: spacing.sm,
};

export const userMessageBubble = (colors: ThemeColors): ViewStyle => ({
  alignSelf: "flex-end",
  backgroundColor: colors.surfaceElevated,
  borderColor: colors.border,
  maxWidth: "100%",
  minWidth: 0,
  overflow: "hidden",
});
