// Colors and type, by the screens' own names for them, drawn from the tokens (`tokens.ts`): an
// ivory or near-black surface, near-white cards with a hairline, neutral text in three steps, ink
// (the inverted call-to-action color) for the primary button and a switch that is on, and blue
// only for links and the caret. Bots keep their accents.

import { getMaterialColors, type MaterialColors, type RgbaHex } from "@expo/ui/jetpack-compose";
import { Platform, useColorScheme, type ColorValue, type ViewStyle } from "react-native";
import type { Accent } from "../core/model";
import { darkColors, lightColors, typeScale } from "./tokens";

export interface Palette {
  label: ColorValue;
  secondaryLabel: ColorValue;
  tertiaryLabel: ColorValue;
  separator: ColorValue;
  /// A screen's own background: the chat list and the transcript.
  background: ColorValue;
  /// Behind a form's grouped cards: settings, Details, and the sheets.
  groupedBackground: ColorValue;
  cell: ColorValue;
  /// A neutral control fill: a secondary button, the Dictate disc, a pressed row.
  fill: ColorValue;
  secondaryFill: ColorValue;
  /// What the app's own controls are drawn in: ink.
  tint: ColorValue;
  ink: string;
  /// Text and glyphs on ink.
  inkText: string;
  userBubble: ColorValue;
  userBubbleText: ColorValue;
  botBubble: ColorValue;
  botBubbleText: ColorValue;
  /// The card inside a gray card in the transcript (a draft's body, a command), lighter than it.
  innerCard: ColorValue;
  /// A card's hairline.
  cardBorder: ColorValue;
  /// The floating round buttons and capsules of the bars and the composer.
  chrome: string;
  chromeBorder: ColorValue;
  shadow: string;
  /// A published output's card in the transcript, apart from the bubbles.
  outputCard: ColorValue;
  code: ColorValue;
  /// `code` over `cell` as one solid color, then that color clear: the ends of the fade at a
  /// scrolling code block's edge. iOS only; Android fades a scroll view's edges itself.
  codeFade?: readonly [string, string];
  green: ColorValue;
  red: ColorValue;
  link: ColorValue;
  /// A web link's source chip: its fill and hairline, so it stands out of a bubble's gray.
  chip: string;
  chipBorder: string;
  dark: boolean;
}

/// One palette per appearance, built on first use; the same object each time keeps memoized work
/// that depends on it from running again.
const palettes: { light?: Palette; dark?: Palette } = {};
const materials: { light?: MaterialColors; dark?: MaterialColors } = {};

export function usePalette(): Palette {
  const dark = useColorScheme() === "dark";
  return (palettes[dark ? "dark" : "light"] ??= makePalette(dark));
}

/// Android's Material 3 palette for the Compose views (`@expo/ui`), given to them once from the
/// root: a neutral scheme with ink as its primary, so menus, sheets, and dialogs match the rest.
export function useMaterialPalette(): MaterialColors | null {
  const dark = useColorScheme() === "dark";
  if (Platform.OS !== "android") return null;
  const scheme = dark ? "dark" : "light";
  return (materials[scheme] ??= makeMaterial(dark));
}

function makePalette(dark: boolean): Palette {
  const c = dark ? darkColors : lightColors;
  return {
    label: c.textPrimary,
    secondaryLabel: c.textSecondary,
    tertiaryLabel: c.textTertiary,
    separator: c.border,
    background: c.surface,
    groupedBackground: c.surface,
    cell: c.surfaceElevated,
    fill: c.surfaceChip,
    secondaryFill: c.borderTranslucent,
    tint: c.cta,
    ink: c.cta,
    inkText: c.ctaText,
    userBubble: c.surfaceElevated,
    userBubbleText: c.textPrimary,
    botBubble: c.surfaceChip,
    botBubbleText: c.textPrimary,
    innerCard: c.surface,
    cardBorder: c.border,
    chrome: c.surfaceElevated,
    chromeBorder: c.border,
    shadow: "#000000",
    outputCard: c.surfaceElevated,
    code: c.chatCodeSurface,
    // `code` is solid: the fade runs from it to itself clear.
    codeFade: [c.chatCodeSurface, withAlpha(c.chatCodeSurface, 0)],
    green: c.statusDone,
    red: c.statusError,
    link: c.inputCaret,
    chip: c.surfaceChip,
    chipBorder: c.border,
    dark,
  };
}

/// `#rrggbb` as Compose's `#RRGGBBAA`.
function rgba(hex: string, alpha = 1): RgbaHex {
  const a = Math.round(alpha * 255).toString(16).padStart(2, "0");
  return `#${hex.slice(1, 7)}${a}`.toUpperCase() as RgbaHex;
}

function makeMaterial(dark: boolean): MaterialColors {
  // A gray seed gives neutral tonal roles; the ones the app's controls show are set to the tokens.
  const c = dark ? darkColors : lightColors;
  const base = getMaterialColors({ scheme: dark ? "dark" : "light", seedColor: "#808080" });
  return {
    ...base,
    primary: rgba(c.cta),
    onPrimary: rgba(c.ctaText),
    primaryContainer: rgba(c.surfaceElevated),
    onPrimaryContainer: rgba(c.textPrimary),
    secondary: rgba(c.cta),
    onSecondary: rgba(c.ctaText),
    secondaryContainer: rgba(c.surfaceChip),
    onSecondaryContainer: rgba(c.textPrimary),
    tertiary: rgba(c.cta),
    onTertiary: rgba(c.ctaText),
    surfaceTint: rgba(c.surface, 0),
    background: rgba(c.surface),
    onBackground: rgba(c.textPrimary),
    surface: rgba(c.surface),
    onSurface: rgba(c.textPrimary),
    surfaceContainerLowest: rgba(c.surface),
    surfaceContainerLow: rgba(c.surfaceElevated),
    surfaceContainer: rgba(c.surfaceElevated),
    surfaceContainerHigh: rgba(c.surfaceElevated),
    surfaceContainerHighest: rgba(c.surfaceChip),
    outline: rgba(c.borderStrong),
    outlineVariant: rgba(c.border),
    error: rgba(c.statusError),
  };
}

/// A `#rrggbb` color at an opacity.
export function withAlpha(hex: string, alpha: number): string {
  const value = parseInt(hex.slice(1, 7), 16);
  return `rgba(${(value >> 16) & 255},${(value >> 8) & 255},${value & 255},${alpha})`;
}

/// The soft shadow under a floating round button or capsule, as in the reference's white discs.
/// It belongs on a view that does not clip (`overflow: "hidden"` removes an iOS shadow) and that
/// has a background of its own, since the shadow takes the view's shape. On Android a dark surface
/// shows no elevation, so there the hairline edge alone separates the disc, and a light one gets a
/// short, tinted shadow rather than the default's gray smudge.
export function floatShadow(p: Palette): ViewStyle {
  if (Platform.OS === "ios") return { shadowColor: p.shadow, shadowOpacity: p.dark ? 0.35 : 0.1, shadowRadius: 8, shadowOffset: { width: 0, height: 2 } };
  return p.dark ? { elevation: 0 } : { elevation: 3, shadowColor: "rgba(0,0,0,0.45)" };
}

/// The shadow under a popup menu: larger and softer than a button's, as it floats over everything.
export function menuShadow(p: Palette): ViewStyle {
  if (Platform.OS === "ios") return { shadowColor: p.shadow, shadowOpacity: p.dark ? 0.5 : 0.18, shadowRadius: 24, shadowOffset: { width: 0, height: 8 } };
  return { elevation: p.dark ? 0 : 12, shadowColor: "rgba(0,0,0,0.5)" };
}

/// The eight bot accents, as the Mac app's system colors, with a lifted twin for gradients.
export const ACCENTS: Record<Accent, { light: [string, string]; dark: [string, string] }> = {
  indigo: { light: ["#8583F5", "#5856D6"], dark: ["#8B89F7", "#5E5CE6"] },
  blue: { light: ["#4DA3FF", "#007AFF"], dark: ["#4FA8FF", "#0A84FF"] },
  teal: { light: ["#5FC5D9", "#30B0C7"], dark: ["#6CD3E6", "#40C8E0"] },
  green: { light: ["#6ADB85", "#34C759"], dark: ["#66E183", "#30D158"] },
  orange: { light: ["#FFB04D", "#FF9500"], dark: ["#FFB454", "#FF9F0A"] },
  pink: { light: ["#FF6B8D", "#FF2D55"], dark: ["#FF6F8F", "#FF375F"] },
  purple: { light: ["#C58CF0", "#AF52DE"], dark: ["#CB94F5", "#BF5AF2"] },
  red: { light: ["#FF7069", "#FF3B30"], dark: ["#FF7A72", "#FF453A"] },
};

export function accentColors(accent: string, dark: boolean): [string, string] {
  const entry = ACCENTS[(accent as Accent) in ACCENTS ? (accent as Accent) : "indigo"];
  return dark ? entry.dark : entry.light;
}

export function accentColor(accent: string, dark: boolean): string {
  return accentColors(accent, dark)[1];
}

/// The type sizes the screens name, in the tokens' steps: a row's title and a field, a message's
/// text, a caption, an author's name and a row's subtitle, and code.
export const Font = {
  body: typeScale.body,
  message: typeScale.bodyLarge,
  caption: typeScale.caption,
  author: typeScale.footnote,
  small: typeScale.footnote,
  code: typeScale.bodySmall - 1,
} as const;
