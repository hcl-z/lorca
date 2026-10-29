// Message Markdown, rendered by the native text view: selectable in place, links tappable,
// sized to its text within the width the bubble allows. In a bot's message a web link is a
// source chip, its site's icon and its words on a rounded fill (`docs/architecture/links.md`):
// the view draws it, and this asks the CLI for each site's icon once per run.

import { useEffect, useMemo, useRef } from "react";
import { Platform, processColor, useWindowDimensions, type ColorValue } from "react-native";
import { MarkdownView, measureMarkdown, setFavicon, sourceHosts } from "../../modules/lorca-core/MarkdownView";
import { engine } from "../core/engine";
import { Font, usePalette } from "./theme";

/// The sites whose icons the views have been given, or are being given.
const handed = new Set<string>();

export function Markdown({ text, color, maxWidth, size = Font.message, sources = false, streaming = false }: { text: string; color: ColorValue; maxWidth: number; size?: number; sources?: boolean; streaming?: boolean }) {
  const p = usePalette();
  // The text size the user picked: Android's view sizes its text in sp, which follows it already;
  // UIKit's takes points, so iOS scales them here, measuring and drawing alike.
  const { fontScale } = useWindowDimensions();
  const scale = Platform.OS === "ios" ? fontScale : 1;
  const fontSize = size * scale;
  const codeFontSize = Font.code * scale;
  const link = color === p.userBubbleText ? color : p.link;
  // A reply still streaming measures at most four times a second, and keeps its answer out of
  // the cache: its text is new on every flush, and the row takes the newest size it has meanwhile.
  const last = useRef<{ at: number; size: ReturnType<typeof measureMarkdown> | undefined }>({ at: 0, size: undefined });
  const measured = useMemo(() => {
    const now = Date.now();
    if (!streaming || !last.current.size || now - last.current.at >= 250) {
      last.current = { at: now, size: measureMarkdown(text, maxWidth, fontSize, codeFontSize, sources, streaming) };
    }
    return last.current.size!;
  }, [text, maxWidth, fontSize, codeFontSize, sources, streaming]);
  useEffect(() => {
    if (streaming || !sources || !/https?:\/\/|www\./i.test(text)) return;
    for (const host of sourceHosts(text)) {
      if (handed.has(host)) continue;
      handed.add(host);
      void engine.favicon(host).then((data) => data && setFavicon(host, data));
    }
  }, [sources, text, streaming]);
  return (
    <MarkdownView
      style={measured}
      markdown={text}
      maxWidth={maxWidth}
      fontSize={fontSize}
      codeFontSize={codeFontSize}
      color={processColor(color)!}
      linkColor={processColor(link)!}
      codeBackground={processColor(p.code)!}
      quoteColor={processColor(p.tertiaryLabel)!}
      border={processColor(p.separator)!}
      tint={processColor(link)!}
      sourceChips={sources}
      chipColor={processColor(p.chip)!}
      chipBorder={processColor(p.chipBorder)!}
    />
  );
}
