// Where the phone's time goes, kept in Lorca Dev only: how long a launch takes to draw the chat
// list, how long a chat takes to show after a tap, how late a reply arrives, and how many frames
// the JS thread misses while a transcript scrolls. Settings shows the figures and copies them,
// so a change can be measured before and after on the same account.

// Imports nothing, so the store's tests load it: the app's root layout turns it on for Lorca Dev.
export let timingEnabled = false;

export function enableTiming() {
  timingEnabled = true;
}

/// A launch's phases, in ms since the JS bundle started.
export interface Launch {
  /// The startup cache drew the account, before the core had started.
  cacheApplied?: number;
  coreStarted?: number;
  snapshotApplied?: number;
  listDrawn?: number;
}

export type Series = "chatOpen" | "messageDelay" | "scrollDroppedFrames";

export interface Summary {
  count: number;
  median: number;
  p90: number;
  last: number;
}

/// A frame the JS thread took longer than this to give is a dropped one (60 Hz with slack).
const FRAME_BUDGET_MS = 1000 / 60 * 1.5;
const KEEP = 200;

const bundleStart = performance.now();
const launch: Launch = {};
const series: Record<Series, number[]> = { chatOpen: [], messageDelay: [], scrollDroppedFrames: [] };
let openingSince: number | null = null;
let scroll: { frame: number; last: number; dropped: number } | null = null;

function record(name: Series, value: number) {
  const list = series[name];
  list.push(value);
  if (list.length > KEEP) list.shift();
}

/// Marks a launch phase the first time it happens.
export function markLaunch(phase: keyof Launch) {
  if (!timingEnabled || launch[phase] !== undefined) return;
  launch[phase] = Math.round(performance.now() - bundleStart);
}

/// A chat row was tapped; `chatShown` ends the measure.
export function chatOpening() {
  if (timingEnabled) openingSince = performance.now();
}

export function chatShown() {
  if (openingSince === null) return;
  record("chatOpen", Math.round(performance.now() - openingSince));
  openingSince = null;
}

/// A new message reached the store: how long after its author's Device wrote it. Both clocks
/// are this Mac's on the simulator; on a phone it includes the clocks' difference.
export function messageArrived(createdAtSecs: number) {
  if (!timingEnabled || !createdAtSecs) return;
  const delay = Date.now() - createdAtSecs * 1000;
  // A message from a backlog is old news, not a delivery to measure.
  if (delay >= 0 && delay < 120_000) record("messageDelay", Math.round(delay));
}

/// Counts the JS thread's missed frames from a drag's start to the scroll's rest.
export function scrollBegan() {
  if (!timingEnabled || scroll) return;
  const state = { frame: 0, last: performance.now(), dropped: 0 };
  const tick = (now: number) => {
    if (scroll !== state) return;
    const gap = now - state.last;
    if (gap > FRAME_BUDGET_MS) state.dropped += Math.round(gap / (1000 / 60)) - 1;
    state.last = now;
    state.frame = requestAnimationFrame(tick);
  };
  scroll = state;
  state.frame = requestAnimationFrame(tick);
}

export function scrollEnded() {
  if (!scroll) return;
  cancelAnimationFrame(scroll.frame);
  record("scrollDroppedFrames", scroll.dropped);
  scroll = null;
}

export function summarize(values: number[]): Summary | null {
  if (!values.length) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const at = (q: number) => sorted[Math.min(sorted.length - 1, Math.floor(q * sorted.length))];
  return { count: values.length, median: at(0.5), p90: at(0.9), last: values[values.length - 1] };
}

export function timingReport(): { launch: Launch } & Record<Series, Summary | null> {
  return {
    launch: { ...launch },
    chatOpen: summarize(series.chatOpen),
    messageDelay: summarize(series.messageDelay),
    scrollDroppedFrames: summarize(series.scrollDroppedFrames),
  };
}

export function resetTiming() {
  for (const list of Object.values(series)) list.length = 0;
}
