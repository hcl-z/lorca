import { describe, expect, test } from "bun:test";
import { pageReveal, sheetPose } from "./readerTransition";

const frame = { x: 40, y: 500, w: 280, h: 120 };
const width = 400;
const height = 800;

describe("sheetPose", () => {
  test("starts exactly where the bubble stands", () => {
    const pose = sheetPose(frame, width, height, 0);
    // The sheet's center lands on the bubble's center...
    expect(pose.translateX).toBe(frame.x + frame.w / 2 - width / 2);
    expect(pose.translateY).toBe(frame.y + frame.h / 2 - height / 2);
    // ...and its frame is the bubble's.
    expect(pose.scaleX).toBeCloseTo(frame.w / width);
    expect(pose.scaleY).toBeCloseTo(frame.h / height);
  });

  test("ends as the page itself", () => {
    const pose = sheetPose(frame, width, height, 1);
    expect(pose.translateX).toBeCloseTo(0);
    expect(pose.translateY).toBeCloseTo(0);
    expect(pose.scaleX).toBe(1);
    expect(pose.scaleY).toBe(1);
  });

  test("keeps the sheet on screen as it opens", () => {
    const pose = sheetPose({ x: -20, y: -40, w: 300, h: 100 }, width, height, 0.5);
    expect(Number.isFinite(pose.translateX)).toBe(true);
    expect(Number.isFinite(pose.scaleX)).toBe(true);
  });
});

describe("pageReveal", () => {
  test("keeps the page hidden until the sheet covers the screen", () => {
    expect(pageReveal(0)).toBe(0);
    expect(pageReveal(0.7)).toBe(0);
  });

  test("shows it as the sheet settles", () => {
    expect(pageReveal(0.96)).toBe(1);
    expect(pageReveal(1)).toBe(1);
  });
});
