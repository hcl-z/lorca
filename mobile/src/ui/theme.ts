// Colors and type. One monochrome palette on both platforms, the desktop app's (`desktop/theme.go`):
// neutral surfaces, ink (near-black in light, near-white in dark) for the primary button, the
// user's bubble, and a switch that is on; system blue only for links. Bots keep their accents.

import { getMaterialColors, type MaterialColors, type RgbaHex } from "@expo/ui/jetpack-compose";
import { Platform, useColorScheme, type ColorValue } from "react-native";
import type { Accent } from "../core/model";

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
  if (dark) {
    return {
      label: "#F2F2F2",
      secondaryLabel: "rgba(242,242,242,0.58)",
      tertiaryLabel: "rgba(242,242,242,0.34)",
      separator: "rgba(255,255,255,0.09)",
      background: "#0A0A0A",
      groupedBackground: "#000000",
      cell: "#1C1C1E",
      fill: "rgba(255,255,255,0.10)",
      secondaryFill: "rgba(255,255,255,0.07)",
      tint: "#F5F5F5",
      ink: "#F5F5F5",
      inkText: "#0A0A0A",
      userBubble: "#F5F5F5",
      userBubbleText: "#0A0A0A",
      botBubble: "#1C1C1E",
      botBubbleText: "#F2F2F2",
      innerCard: "#2C2C2E",
      cardBorder: "rgba(255,255,255,0.10)",
      chrome: "#1F1F21",
      chromeBorder: "rgba(255,255,255,0.10)",
      shadow: "#000000",
      outputCard: "#161618",
      code: "rgba(255,255,255,0.06)",
      // `cell` (#1C1C1E) under `code`.
      codeFade: ["rgb(42,42,44)", "rgba(42,42,44,0)"],
      green: "#30D158",
      red: "#FF453A",
      link: "#4EA1FF",
      chip: "#2A2A2C",
      chipBorder: "rgba(255,255,255,0.10)",
      dark,
    };
  }
  return {
    label: "#0A0A0A",
    secondaryLabel: "rgba(10,10,10,0.56)",
    tertiaryLabel: "rgba(10,10,10,0.34)",
    separator: "rgba(0,0,0,0.08)",
    background: "#FFFFFF",
    groupedBackground: "#F4F4F4",
    cell: "#FFFFFF",
    fill: "rgba(0,0,0,0.06)",
    secondaryFill: "rgba(0,0,0,0.04)",
    tint: "#0A0A0A",
    ink: "#0A0A0A",
    inkText: "#FFFFFF",
    userBubble: "#0A0A0A",
    userBubbleText: "#FFFFFF",
    botBubble: "#F2F2F2",
    botBubbleText: "#0A0A0A",
    innerCard: "#FFFFFF",
    cardBorder: "rgba(0,0,0,0.09)",
    chrome: "#FFFFFF",
    chromeBorder: "rgba(0,0,0,0.06)",
    shadow: "#000000",
    outputCard: "#FFFFFF",
    code: "rgba(0,0,0,0.05)",
    // `cell` (#FFFFFF) under `code`.
    codeFade: ["rgb(242,242,242)", "rgba(242,242,242,0)"],
    green: "#1F9E47",
    red: "#FF3B30",
    link: "#0068DA",
    chip: "#FFFFFF",
    chipBorder: "rgba(0,0,0,0.09)",
    dark,
  };
}

/// `#rrggbb` as Compose's `#RRGGBBAA`.
function rgba(hex: string, alpha = 1): RgbaHex {
  const a = Math.round(alpha * 255).toString(16).padStart(2, "0");
  return `#${hex.slice(1, 7)}${a}`.toUpperCase() as RgbaHex;
}

function makeMaterial(dark: boolean): MaterialColors {
  // A gray seed gives neutral tonal roles; the ones the app's controls show are set to the palette.
  const base = getMaterialColors({ scheme: dark ? "dark" : "light", seedColor: "#808080" });
  const ink = dark ? "#F5F5F5" : "#0A0A0A";
  const onInk = dark ? "#0A0A0A" : "#FFFFFF";
  const surface = dark ? "#0A0A0A" : "#FFFFFF";
  const container = dark ? "#1C1C1E" : "#F4F4F4";
  return {
    ...base,
    primary: rgba(ink),
    onPrimary: rgba(onInk),
    primaryContainer: rgba(container),
    onPrimaryContainer: rgba(ink),
    secondary: rgba(ink),
    onSecondary: rgba(onInk),
    secondaryContainer: rgba(dark ? "#2C2C2E" : "#EBEBEB"),
    onSecondaryContainer: rgba(ink),
    tertiary: rgba(ink),
    onTertiary: rgba(onInk),
    surfaceTint: rgba(surface, 0),
    background: rgba(surface),
    onBackground: rgba(ink),
    surface: rgba(surface),
    onSurface: rgba(ink),
    surfaceContainerLowest: rgba(surface),
    surfaceContainerLow: rgba(dark ? "#121214" : "#FAFAFA"),
    surfaceContainer: rgba(container),
    surfaceContainerHigh: rgba(dark ? "#222224" : "#EFEFEF"),
    surfaceContainerHighest: rgba(dark ? "#2C2C2E" : "#E8E8E8"),
    error: rgba(dark ? "#FF453A" : "#FF3B30"),
  };
}

/// A `#rrggbb` color at an opacity.
export function withAlpha(hex: string, alpha: number): string {
  const value = parseInt(hex.slice(1, 7), 16);
  return `rgba(${(value >> 16) & 255},${(value >> 8) & 255},${value & 255},${alpha})`;
}

/// The soft shadow under a floating round button or capsule, as in the reference's white discs.
export function floatShadow(p: Palette) {
  return Platform.select({
    ios: { shadowColor: p.shadow, shadowOpacity: p.dark ? 0.4 : 0.08, shadowRadius: 10, shadowOffset: { width: 0, height: 3 } },
    default: { elevation: 2 },
  });
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

export const Font = {
  body: Platform.OS === "android" ? 16 : 17,
  message: Platform.OS === "android" ? 16 : 16.5,
  caption: 12,
  author: Platform.OS === "android" ? 12 : 13,
  small: Platform.OS === "android" ? 12 : 13,
  code: 14,
} as const;
