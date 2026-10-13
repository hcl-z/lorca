// A message body as a native text view. The core parses the Markdown; the view renders it
// selectable in place (UITextView on iOS, TextView on Android), sizes itself to its text within
// `maxWidth`, and opens links.
import { requireNativeModule, requireNativeViewManager } from "expo-modules-core";
import { PixelRatio, type ProcessedColorValue, type StyleProp, type ViewStyle } from "react-native";

export interface MarkdownViewProps {
  markdown: string;
  /// The widest the text may run, in points; the view claims the width it actually uses.
  maxWidth: number;
  fontSize: number;
  codeFontSize: number;
  color: ProcessedColorValue;
  linkColor: ProcessedColorValue;
  codeBackground: ProcessedColorValue;
  quoteColor: ProcessedColorValue;
  /// Table rules.
  border: ProcessedColorValue;
  /// Selection handles and highlight.
  tint: ProcessedColorValue;
  /// Web links as source chips (a bot's message), on this fill and hairline.
  sourceChips: boolean;
  chipColor: ProcessedColorValue;
  chipBorder: ProcessedColorValue;
  style?: StyleProp<ViewStyle>;
}

const native = requireNativeModule<{
  measure(markdown: string, maxWidth: number, fontSize: number, codeFontSize: number, sourceChips: boolean): { width: number; height: number };
  sourceHosts(markdown: string): string[];
  setFavicon(host: string, data: string | null): void;
}>("MarkdownView");

/// The sites a message's source chips name, each once.
export const sourceHosts = (markdown: string): string[] => native.sourceHosts(markdown);

/// Hands a site's icon (base64 image bytes) to every chip of it.
export const setFavicon = (host: string, data: string | null) => native.setFavicon(host, data);

/// Sizes already measured, by text and width, most recent last. A chat opened again, or a row
/// FlashList recycles while scrolling, asks for the same sizes; measuring parses the Markdown and
/// lays its text out.
const measured = new Map<string, { width: number; height: number }>();
const MEASURED_LIMIT = 600;

/// The size the text takes within `maxWidth`, measured natively before the view renders. The
/// view is sized from this, so a transcript's rows have their heights in their first layout.
export function measureMarkdown(markdown: string, maxWidth: number, fontSize: number, codeFontSize: number, sourceChips: boolean) {
  // Android sizes the text in sp, so the system's font scale is part of the answer.
  const key = `${maxWidth}|${fontSize}|${codeFontSize}|${PixelRatio.getFontScale()}|${sourceChips ? 1 : 0}|${markdown}`;
  const hit = measured.get(key);
  if (hit) {
    measured.delete(key);
    measured.set(key, hit);
    return hit;
  }
  const size = native.measure(markdown, maxWidth, fontSize, codeFontSize, sourceChips);
  measured.set(key, size);
  if (measured.size > MEASURED_LIMIT) measured.delete(measured.keys().next().value!);
  return size;
}

export const MarkdownView = requireNativeViewManager<MarkdownViewProps>("MarkdownView");
