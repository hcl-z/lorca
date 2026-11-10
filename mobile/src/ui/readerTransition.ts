// A message read whole grows out of its bubble: the page's own fill starts as the bubble's rect
// and expands to the screen, while the page's text fades in over the growing sheet. Only the
// sheet's plain fill is transformed — the text is laid out once, at its page size, so it never
// stretches, and the bubble fades under the sheet instead of jumping to the page's layout.

import type { MessageFrame } from "./longMessage";

/// The sheet's transform at `at` of the opening: 0 is the bubble's rect on screen, 1 the page.
export function sheetPose(frame: MessageFrame, width: number, height: number, at: number) {
  "worklet";
  const back = 1 - at;
  const scaleX = frame.w > 0 ? frame.w / width : 1;
  const scaleY = frame.h > 0 ? frame.h / height : 1;
  return {
    translateX: back * (frame.x + frame.w / 2 - width / 2),
    translateY: back * (frame.y + frame.h / 2 - height / 2),
    scaleX: 1 + back * (scaleX - 1),
    scaleY: 1 + back * (scaleY - 1),
  };
}

/// How visible the page's text is at `at`: the text has no clip of its own, so it waits until the
/// sheet covers the screen — its padding hides the last sliver of growth — and then fades in as
/// the sheet settles. Every frame shows either the bubble filling the screen or the page.
export function pageReveal(at: number): number {
  "worklet";
  const reveal = (at - 0.72) / 0.23;
  return Math.min(1, Math.max(0, reveal));
}
