import { expect, test } from "bun:test";
import * as timing from "./timing";

timing.enableTiming();

test("a summary reads the median, the 90th percentile, and the last value", () => {
  expect(timing.summarize([])).toBeNull();
  expect(timing.summarize([30, 10, 20])).toEqual({ count: 3, median: 20, p90: 30, last: 20 });
  const hundred = Array.from({ length: 100 }, (_, i) => i + 1);
  expect(timing.summarize(hundred)).toMatchObject({ median: 51, p90: 91, last: 100 });
});

test("a launch phase is kept from its first time", () => {
  timing.markLaunch("coreStarted");
  const first = timing.timingReport().launch.coreStarted;
  expect(first).toBeNumber();
  timing.markLaunch("coreStarted");
  expect(timing.timingReport().launch.coreStarted).toBe(first!);
});

test("a chat open is measured from the tap to the transcript showing, once", () => {
  timing.resetTiming();
  timing.chatShown();
  expect(timing.timingReport().chatOpen).toBeNull();
  timing.chatOpening();
  timing.chatShown();
  timing.chatShown();
  expect(timing.timingReport().chatOpen?.count).toBe(1);
});

test("a message's delay leaves out backlog and clocks that disagree", () => {
  timing.resetTiming();
  const now = Date.now() / 1000;
  timing.messageArrived(now - 1.5);
  timing.messageArrived(now - 3600);
  timing.messageArrived(now + 60);
  const delay = timing.timingReport().messageDelay!;
  expect(delay.count).toBe(1);
  expect(delay.last).toBeGreaterThanOrEqual(1400);
});
