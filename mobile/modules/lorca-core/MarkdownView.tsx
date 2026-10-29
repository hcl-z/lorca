// A message body as a native text view. The core parses the Markdown; the view renders it
// selectable in place (UITextView on iOS, TextView on Android), sizes itself to its text within
// `maxWidth`, and opens links.
import * as Application from "expo-application";
import { File, Paths } from "expo-file-system";
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

type Size = { width: number; height: number };

/// Sizes already measured, by a hash of the text and width, most recent last. A chat opened again,
/// or a row FlashList recycles while scrolling, asks for the same sizes; measuring parses the
/// Markdown and lays its text out on the JS thread. The sizes outlive a launch in the app's caches
/// folder, so the first chat opened after one measures only what is new; a build of its own keeps
/// its own file, since a new renderer can lay the same text out differently.
let measured: Map<string, Size> | null = null;
const MEASURED_LIMIT = 3000;
const SAVE_DELAY_MS = 2000;
let saveTimer: ReturnType<typeof setTimeout> | null = null;

function sizesFile(): File {
  return new File(Paths.cache, `markdown-sizes-${Application.nativeApplicationVersion ?? "0"}-${Application.nativeBuildVersion ?? "0"}.json`);
}

function sizes(): Map<string, Size> {
  if (measured) return measured;
  measured = new Map();
  try {
    const file = sizesFile();
    if (file.exists) for (const [key, width, height] of JSON.parse(file.textSync()) as [string, number, number][]) measured.set(key, { width, height });
  } catch {
    // A file from a run that ended mid-write: measured again.
  }
  return measured;
}

function saveSoon() {
  if (saveTimer) return;
  saveTimer = setTimeout(() => {
    saveTimer = null;
    try {
      sizesFile().write(JSON.stringify([...sizes()].map(([key, size]) => [key, size.width, size.height])));
    } catch (error) {
      console.warn("saving Markdown sizes", error);
    }
  }, SAVE_DELAY_MS);
}

/// FNV-1a over the key's UTF-16 units in two 32-bit lanes: a key the file holds without the text.
export function sizeKey(text: string): string {
  let a = 0x811c9dc5;
  let b = 0x01000193;
  for (let i = 0; i < text.length; i++) {
    const unit = text.charCodeAt(i);
    a = Math.imul(a ^ unit, 0x01000193);
    b = Math.imul(b ^ unit, 0x5bd1e995) ^ (b >>> 15);
  }
  return `${(a >>> 0).toString(36)}${(b >>> 0).toString(36)}${text.length.toString(36)}`;
}

/// The size the text takes within `maxWidth`, measured natively before the view renders. The
/// view is sized from this, so a transcript's rows have their heights in their first layout.
/// `transient` measures without keeping the answer: a reply still streaming would fill the cache
/// and its file with every flush's text.
export function measureMarkdown(markdown: string, maxWidth: number, fontSize: number, codeFontSize: number, sourceChips: boolean, transient = false) {
  // Android sizes the text in sp, so the system's font scale is part of the answer.
  const key = sizeKey(`${maxWidth}|${fontSize}|${codeFontSize}|${PixelRatio.getFontScale()}|${sourceChips ? 1 : 0}|${markdown}`);
  const cache = sizes();
  const hit = cache.get(key);
  if (hit) {
    cache.delete(key);
    cache.set(key, hit);
    return hit;
  }
  const size = native.measure(markdown, maxWidth, fontSize, codeFontSize, sourceChips);
  if (transient) return size;
  cache.set(key, size);
  if (cache.size > MEASURED_LIMIT) cache.delete(cache.keys().next().value!);
  saveSoon();
  return size;
}

export const MarkdownView = requireNativeViewManager<MarkdownViewProps>("MarkdownView");
