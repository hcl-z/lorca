// A message read whole grows out of its bubble: the panel the page is drawn on starts as the
// bubble's own rect, widens to the screen with all four edges moving at once, and rounds its corner
// off as it goes, while the page's text comes up inside it and its fill goes from clear to solid.
// The page is laid out once at the screen's size and clipped by the panel, so its text never
// stretches and never reflows while the panel moves: the panel is a window onto a page already
// drawn, and the edge that grows carries the page's top left corner with it.
//
// The fill is what leaves the bubble. It starts clear over the bubble's own view, which the chat
// still draws below the reader, and turns solid within the opening's first third, so the bubble's
// text fades into the page rather than being wiped by an edge arriving over it. Nothing dims behind
// the panel and nothing is scaled: the panel is a rectangle growing, which is the reference's
// motion, measured from it frame by frame.

import type { MessageFrame } from "./longMessage";

/// The bubble's corner. The panel starts with it and rounds off from it; the transcript's bubble
/// style takes it too, so the panel's first frame and the bubble it grows out of cannot disagree.
export const BUBBLE_RADIUS = 20;

/// The share of the growth the panel's fill takes to go from clear to solid: the reference's card
/// text is down to 13% of itself by 125 ms of a 333 ms growth.
const FILL = 0.36;

/// The panel's frame at `at` of the growth: 0 is the bubble's rect on screen, 1 the page's. Every
/// edge moves at once, and the corner the panel starts with is gone by the time it is a page.
export function panelFrame(frame: MessageFrame, width: number, height: number, at: number) {
  "worklet";
  const back = 1 - at;
  return {
    left: frame.x * back,
    top: frame.y * back,
    width: frame.w + (width - frame.w) * at,
    height: frame.h + (height - frame.h) * at,
    radius: BUBBLE_RADIUS * back,
  };
}

/// How solid the page's fill is over the bubble at `at`: clear at the bubble, solid once the panel
/// has grown a third of the way, then solid for the rest. It eases in rather than rising evenly, so
/// the bubble's text reads for a frame or two before it goes.
export function pageFillOpacity(at: number): number {
  "worklet";
  const rise = Math.min(1, Math.max(0, at / FILL));
  return rise * rise * (3 - 2 * rise);
}

/// How visible the page's text is at `at`: it comes up behind the bubble's own text, so the reader
/// sees the page rising out of the bubble rather than a blank sheet. On the way back it holds
/// longer than it did coming in — half way back it is still at 44% — since it is what the panel
/// shows while it folds down onto the bubble.
export function pageOpacity(at: number, closing: boolean): number {
  "worklet";
  const v = Math.min(1, Math.max(0, at));
  return Math.pow(v, closing ? 1.2 : 2);
}
