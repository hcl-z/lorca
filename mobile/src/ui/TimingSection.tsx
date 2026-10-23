// Lorca Dev's timing figures in Settings (src/core/timing.ts), with Copy for a pull request's
// before and after. A developer's tool, so its words are not translated.

import * as Clipboard from "expo-clipboard";
import { useState } from "react";
import { resetTiming, timingEnabled, timingReport, type Summary } from "../core/timing";
import { Row, Section } from "./forms";

function figure(summary: Summary | null, unit: string): string {
  return summary ? `${summary.median}${unit} median · ${summary.p90}${unit} p90 · ${summary.count}×` : "—";
}

export function TimingSection() {
  // Read on each render: Settings renders on every store change, often enough for a figure.
  const [, refresh] = useState(0);
  if (!timingEnabled) return null;
  const report = timingReport();
  const { cacheApplied, coreStarted, snapshotApplied, listDrawn } = report.launch;
  const ms = (value?: number) => (value === undefined ? "—" : `${value}ms`);
  return (
    <Section title="Timing (Lorca Dev)" footer="From this launch. Launch phases are ms since the JS bundle started.">
      <Row title="Cache applied" detail={ms(cacheApplied)} />
      <Row title="Core started" detail={ms(coreStarted)} />
      <Row title="Snapshot applied" detail={ms(snapshotApplied)} />
      <Row title="Chat list drawn" detail={ms(listDrawn)} />
      <Row title="Chat open" subtitle={figure(report.chatOpen, "ms")} />
      <Row title="Message delay" subtitle={figure(report.messageDelay, "ms")} />
      <Row title="Dropped frames per scroll" subtitle={figure(report.scrollDroppedFrames, "")} />
      <Row title="Copy as JSON" action onPress={() => void Clipboard.setStringAsync(JSON.stringify(timingReport(), null, 2))} />
      <Row
        title="Reset"
        action
        onPress={() => {
          resetTiming();
          refresh((n) => n + 1);
        }}
      />
    </Section>
  );
}
