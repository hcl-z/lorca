// The app's icons: Lucide's, the set the desktop app draws, by the SF Symbol names the Mac app,
// the models, and bot profiles use (`lucideIcons.ts`, written from desktop/icons_lucide.go). Each
// is a stroked 24-unit SVG drawn by react-native-svg in the text's color, the same on iOS and
// Android. A name with no icon draws sparkles.

import { memo, type ReactElement } from "react";
import type { ColorValue, StyleProp, ViewStyle } from "react-native";
import Svg, { Circle, Ellipse, Line, Path, Polygon, Polyline, Rect } from "react-native-svg";
import { FILLED_SYMBOLS, LUCIDE_SHAPES, SYMBOL_ICONS } from "./lucideIcons";

/// SF Symbols' weights, as the stroke the icons draw with in their 24-unit box.
export type SymbolWeight = "ultraLight" | "thin" | "light" | "regular" | "medium" | "semibold" | "bold" | "heavy" | "black";

const STROKES: Record<SymbolWeight, number> = {
  ultraLight: 1.25,
  thin: 1.4,
  light: 1.6,
  regular: 1.8,
  medium: 1.9,
  semibold: 2.2,
  bold: 2.45,
  heavy: 2.7,
  black: 2.9,
};

const ELEMENTS = { path: Path, circle: Circle, rect: Rect, line: Line, polyline: Polyline, polygon: Polygon, ellipse: Ellipse } as const;
type Shape = { kind: keyof typeof ELEMENTS; props: Record<string, string> };

const parsed = new Map<string, Shape[]>();

/// An icon's elements and their attributes, read once.
function shapesOf(icon: string): Shape[] {
  let shapes = parsed.get(icon);
  if (!shapes) {
    shapes = [];
    for (const match of (LUCIDE_SHAPES[icon] ?? "").matchAll(/<(\w+)((?:\s+[\w-]+="[^"]*")*)\s*\/>/g)) {
      const kind = match[1] as Shape["kind"];
      if (!(kind in ELEMENTS)) continue;
      const props: Record<string, string> = {};
      for (const attr of match[2].matchAll(/([\w-]+)="([^"]*)"/g)) props[attr[1].replace(/-(\w)/g, (_, c: string) => c.toUpperCase())] = attr[2];
      shapes.push({ kind, props });
    }
    parsed.set(icon, shapes);
  }
  return shapes;
}

/// Whether a name draws an icon of its own rather than the fallback.
export function hasSymbol(name: string): boolean {
  return !!SYMBOL_ICONS[name];
}

export interface SymbolProps {
  name: string;
  size?: number;
  color?: ColorValue;
  weight?: SymbolWeight;
  style?: StyleProp<ViewStyle>;
}

export const Symbol = memo(function Symbol({ name, size = 17, color, weight = "medium", style }: SymbolProps) {
  const icon = SYMBOL_ICONS[name] ?? "Sparkles";
  const filled = FILLED_SYMBOLS.has(name);
  // Lucide's stroke is drawn in a 24-unit box: a small icon keeps it a little heavier, so it does
  // not thin out next to its words.
  const stroke = STROKES[weight] * (size < 16 ? 1.12 : 1);
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" style={style} color={color as string} accessible={false} pointerEvents="none">
      {shapesOf(icon).map(({ kind, props }, index): ReactElement => {
        const Element = ELEMENTS[kind] as React.ComponentType<Record<string, unknown>>;
        return (
          <Element
            key={index}
            {...props}
            fill={filled ? "currentColor" : "none"}
            stroke="currentColor"
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        );
      })}
    </Svg>
  );
});

/// Symbols offered when creating a bot, a subset the Mac app also knows.
export const BOT_SYMBOLS = [
  "sparkles",
  "wand.and.stars",
  "hammer.fill",
  "book.fill",
  "paintbrush.fill",
  "chart.bar.fill",
  "terminal.fill",
  "globe",
  "brain.head.profile",
  "magnifyingglass",
  "envelope.fill",
  "calendar",
  "flask.fill",
  "bolt.fill",
  "leaf.fill",
  "shield.fill",
  "binoculars.fill",
  "chevron.left.forwardslash.chevron.right",
  "pencil.and.scribble",
  "bolt.horizontal.fill",
  "flame.fill",
];
