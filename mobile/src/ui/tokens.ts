// Design tokens after Cindy's phone app (`apps/mobile/src/theme/tokens.ts`): the colors of each
// appearance as roles, and the steps every screen draws from for spacing, corners, type, icons,
// and motion. Plain data with no React Native import, so a test can read it as it is.

export type ThemeMode = "light" | "dark";

/// The colors of one appearance. Both appearances have the same keys.
export interface ThemeColors {
  /// A screen's own background.
  surface: string;
  /// A card, a cell, a bubble, an input: one step above the screen.
  surfaceElevated: string;
  /// A bar over scrolling content, translucent rather than blurred.
  surfaceTranslucent: string;
  /// The chat's top bar glass and the line under it.
  chatHeaderSurface: string;
  chatHeaderDivider: string;
  /// A code block and its hairline.
  chatCodeSurface: string;
  chatCodeBorder: string;
  /// The caret, the selection, and links.
  inputCaret: string;
  /// A bottom sheet's glass, its action rows and their hairline, and its grabber.
  sheetSurface: string;
  sheetActionSurface: string;
  sheetActionBorder: string;
  sheetGrabber: string;
  /// A chip, a pill, a pressed or selected row.
  surfaceChip: string;
  /// Hairlines and separators.
  border: string;
  borderTranslucent: string;
  borderStrong: string;
  textPrimary: string;
  textSecondary: string;
  textTertiary: string;
  /// Only for a field's placeholder.
  textPlaceholder: string;
  /// The primary action's fill and the words on it: neutral, inverted.
  cta: string;
  ctaText: string;
  statusDone: string;
  statusError: string;
  statusAccent: string;
  /// A removal's words.
  destructive: string;
  /// A modal's scrim, dark in both appearances.
  overlay: string;
  /// A chat row's swipe actions and the glyphs on them.
  swipeActionPin: string;
  swipeActionRead: string;
  swipeActionNeutral: string;
  swipeActionText: string;
}

/// Light: an ivory screen, near-white cards with a hairline, neutral text.
export const lightColors: ThemeColors = {
  surface: "#F9F9F6",
  surfaceElevated: "#FFFFFC",
  surfaceTranslucent: "rgba(249, 249, 246, 0.78)",
  chatHeaderSurface: "rgba(249, 249, 246, 0.90)",
  chatHeaderDivider: "#CCCCC8",
  chatCodeSurface: "#F1F1EC",
  chatCodeBorder: "#CCCCC8",
  inputCaret: "#417CDD",
  sheetSurface: "rgba(249, 249, 246, 0.96)",
  sheetActionSurface: "#FFFFFC",
  sheetActionBorder: "#CCCCC8",
  sheetGrabber: "#C2C2BE",
  surfaceChip: "#EAEAE6",
  border: "#CCCCC8",
  borderTranslucent: "rgba(204, 204, 200, 0.62)",
  borderStrong: "#858581",
  textPrimary: "#0F0F0F",
  textSecondary: "#4D4D4A",
  textTertiary: "#686864",
  textPlaceholder: "#858581",
  cta: "#0F0F0F",
  ctaText: "#FFFFFF",
  statusDone: "#2AAE5B",
  statusError: "#D91F37",
  statusAccent: "#EA6B17",
  destructive: "#F43D3F",
  overlay: "rgba(38, 38, 38, 0.35)",
  swipeActionPin: "#EA6B17",
  swipeActionRead: "#008579",
  swipeActionNeutral: "#8E8E93",
  swipeActionText: "#FBFBFA",
};

/// Dark: a neutral near-black screen, one step lighter for cards.
export const darkColors: ThemeColors = {
  surface: "#121212",
  surfaceElevated: "#1E1E1E",
  surfaceTranslucent: "rgba(18, 18, 18, 0.78)",
  chatHeaderSurface: "rgba(18, 18, 18, 0.80)",
  chatHeaderDivider: "rgba(255, 255, 255, 0.08)",
  chatCodeSurface: "#1A1A1A",
  chatCodeBorder: "#383838",
  inputCaret: "#417CDD",
  sheetSurface: "rgba(28, 28, 28, 0.96)",
  sheetActionSurface: "#262626",
  sheetActionBorder: "#383838",
  sheetGrabber: "#5C5C5C",
  surfaceChip: "#2A2A2A",
  border: "#383838",
  borderTranslucent: "rgba(56, 56, 56, 0.62)",
  borderStrong: "#8A8A8A",
  textPrimary: "#EDEDED",
  textSecondary: "#BDBDBD",
  textTertiary: "#999999",
  textPlaceholder: "#757575",
  cta: "#EDEDED",
  ctaText: "#121212",
  statusDone: "#2AAE5B",
  statusError: "#D91F37",
  statusAccent: "#EA6B17",
  destructive: "#F43D3F",
  overlay: "rgba(0, 0, 0, 0.45)",
  swipeActionPin: "#EA6B17",
  swipeActionRead: "#008579",
  swipeActionNeutral: "#636366",
  swipeActionText: "#FBFBFA",
};

export const palettes: Record<ThemeMode, ThemeColors> = { light: lightColors, dark: darkColors };

/// Spacing, on a base of 4.
export const spacing = { xs: 4, sm: 8, md: 12, lg: 16, xl: 24, xxl: 32 } as const;

/// Corners: a tiny chip, a control inside a card, a card or sheet or input, and a capsule.
export const radius = { micro: 4, control: 8, container: 12, pill: 9999 } as const;

/// Type sizes by role: caption for short metadata, footnote for a sentence of help, bodySmall for
/// previews and fields, body for row titles and buttons, bodyLarge for message text, subtitle for
/// list titles, title for a screen's or sheet's heading.
export const typeScale = {
  micro: 11,
  caption: 12,
  footnote: 13,
  bodySmall: 15,
  body: 16,
  bodyLarge: 17,
  subtitle: 18,
  title: 20,
  headline: 24,
  largeTitle: 30,
} as const;

/// Line heights paired with the sizes; `listTitle` is the chat list's 18/28 title line.
export const lineHeight = {
  micro: 16,
  caption: 18,
  bodySmall: 20,
  body: 22,
  bodyRelaxed: 24,
  bodyLarge: 26,
  title: 25,
  subtitle: 26,
  listTitle: 28,
  headline: 30,
  largeTitle: 36,
} as const;

export const fontWeight = { regular: "400", medium: "500", semibold: "600", bold: "700" } as const;

/// Each size with its line height, to spread into a style.
export const textStyles = {
  micro: { fontSize: typeScale.micro, lineHeight: lineHeight.micro },
  caption: { fontSize: typeScale.caption, lineHeight: lineHeight.caption },
  footnote: { fontSize: typeScale.footnote, lineHeight: lineHeight.caption },
  bodySmall: { fontSize: typeScale.bodySmall, lineHeight: lineHeight.bodySmall },
  body: { fontSize: typeScale.body, lineHeight: lineHeight.body },
  bodyLarge: { fontSize: typeScale.bodyLarge, lineHeight: lineHeight.bodyLarge },
  subtitle: { fontSize: typeScale.subtitle, lineHeight: lineHeight.subtitle },
  title: { fontSize: typeScale.title, lineHeight: lineHeight.title },
  headline: { fontSize: typeScale.headline, lineHeight: lineHeight.headline },
  largeTitle: { fontSize: typeScale.largeTitle, lineHeight: lineHeight.largeTitle },
} as const;

export const iconSize = { xs: 12, sm: 14, md: 16, lg: 18, action: 20, xl: 22, xxl: 26, display: 32, hero: 44 } as const;

export const iconStroke = { thin: 1.75, regular: 2, medium: 2.2, bold: 2.5 } as const;

/// Motion durations in milliseconds.
export const motionDuration = { instant: 80, fast: 150, base: 200, enter: 250, exit: 150 } as const;

/// Cubic-bezier control points, for `Easing.bezier(...motionEasing.out)`.
export const motionEasing = {
  out: [0.16, 1, 0.3, 1],
  in: [0.4, 0, 1, 1],
  move: [0.4, 0, 0.2, 1],
} as const;
