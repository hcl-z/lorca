import { describe, expect, test } from "bun:test";
import { BUBBLE_RADIUS, pageFillOpacity, pageOpacity, panelFrame } from "./readerTransition";

const frame = { x: 40, y: 500, w: 280, h: 120 };
const width = 400;
const height = 800;

describe("panelFrame", () => {
  test("starts exactly where the bubble stands, with its corner", () => {
    const rect = panelFrame(frame, width, height, 0);
    expect(rect.left).toBe(frame.x);
    expect(rect.top).toBe(frame.y);
    expect(rect.width).toBe(frame.w);
    expect(rect.height).toBe(frame.h);
    expect(rect.radius).toBe(BUBBLE_RADIUS);
  });

  test("ends as the page itself, with no corner", () => {
    const rect = panelFrame(frame, width, height, 1);
    expect(rect.left).toBe(0);
    expect(rect.top).toBe(0);
    expect(rect.width).toBe(width);
    expect(rect.height).toBe(height);
    expect(rect.radius).toBe(0);
  });

  test("moves all four edges, never only the near ones", () => {
    // The reference's panel is a rectangle growing, not a sheet scaled from its center: the far
    // edges travel too, and each one travels its own distance.
    const half = panelFrame(frame, width, height, 0.5);
    expect(half.left).toBeCloseTo(frame.x / 2);
    expect(half.top).toBeCloseTo(frame.y / 2);
    expect(half.left + half.width).toBeCloseTo((frame.x + frame.w + width) / 2);
    expect(half.top + half.height).toBeCloseTo((frame.y + frame.h + height) / 2);
  });

  test("always holds the bubble it grew out of, however far along it is", () => {
    // The panel is what the bubble's view is seen through, so it may never be the smaller of the two.
    for (const at of [0, 0.1, 0.25, 0.5, 0.75, 0.9, 1]) {
      const rect = panelFrame(frame, width, height, at);
      expect(rect.left).toBeLessThanOrEqual(frame.x);
      expect(rect.top).toBeLessThanOrEqual(frame.y);
      expect(rect.left + rect.width).toBeGreaterThanOrEqual(frame.x + frame.w);
      expect(rect.top + rect.height).toBeGreaterThanOrEqual(frame.y + frame.h);
    }
  });
});

describe("pageFillOpacity", () => {
  test("starts clear over the bubble's own view, and is solid a third of the way in", () => {
    expect(pageFillOpacity(0)).toBe(0);
    expect(pageFillOpacity(0.36)).toBe(1);
    expect(pageFillOpacity(1)).toBe(1);
  });

  test("holds the first frames before it goes, so the bubble's text reads as it leaves", () => {
    // A fill rising evenly to solid over the same stretch is already a fifth of the way there at 5%
    // of the growth, which takes the text out in two frames. The reference holds it: this one is a
    // twentieth of the way there.
    expect(pageFillOpacity(0.05)).toBeLessThan(0.1);
    for (const at of [0, 0.05, 0.1, 0.2, 0.3]) {
      expect(pageFillOpacity(at + 0.05)).toBeGreaterThan(pageFillOpacity(at));
    }
  });

  test("matches the recording's card text, which is 22%, 72% and 87% gone at 42, 83 and 125 ms", () => {
    // Of a 333 ms growth. The fill is what leaves that text, so its alpha follows the same curve.
    const of = (ms: number) => pageFillOpacity(ms / 333);
    expect(Math.abs(of(42) - 0.22)).toBeLessThan(0.15);
    expect(Math.abs(of(83) - 0.72)).toBeLessThan(0.15);
    expect(Math.abs(of(125) - 0.87)).toBeLessThan(0.15);
  });
});

describe("pageOpacity", () => {
  test("comes up from nothing as the page itself", () => {
    expect(pageOpacity(0, false)).toBe(0);
    expect(pageOpacity(1, false)).toBe(1);
    expect(pageOpacity(0.5, false)).toBeCloseTo(0.25);
  });

  test("holds longer on the way back, so the page is what shows while the panel folds", () => {
    expect(pageOpacity(0, true)).toBe(0);
    expect(pageOpacity(1, true)).toBe(1);
    expect(pageOpacity(0.5, true)).toBeCloseTo(Math.pow(0.5, 1.2));
    for (const at of [0.1, 0.25, 0.5, 0.75]) {
      expect(pageOpacity(at, true)).toBeGreaterThan(pageOpacity(at, false));
    }
  });
});
