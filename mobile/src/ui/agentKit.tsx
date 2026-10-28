// The pieces every coding agent surface is drawn from, as the desktop's agent kit, so its card in
// the chat, its bar over the composer, its transcript, and its question read as one object: its
// mark on a tile in its own color, how it stands as a pill whose dot breathes while it works, the
// facts of its session as a strip of cells over a meter of its context, what it does now on a
// console of its own, and callouts for what it asks and how it failed.

import { useEffect, useState, type ReactNode } from "react";
import { ActivityIndicator, Linking, Platform, StyleSheet, Text, View } from "react-native";
import Animated, { Easing, useAnimatedStyle, useReducedMotion, useSharedValue, withRepeat, withTiming, cancelAnimation } from "react-native-reanimated";
import { agentBusy, agentContext, agentSetting, agentSince, agentTone, type AgentTone } from "../core/agentKit";
import { engine } from "../core/engine";
import { agentName, agentPlace, type AgentRun } from "../core/model";
import { AGENT_MARK_VIEWBOX, agentBrandColor, agentMark } from "./agentMarks";
import Svg, { Path } from "react-native-svg";
import { t, tc } from "../i18n";
import { haptic } from "./haptics";
import { Pressable } from "./Pressable";
import { Sheet, SheetActionRow } from "./Sheet";
import { Symbol } from "./Symbol";
import { alert } from "./alert";
import { accentColor, usePalette } from "./theme";

export const mono = Platform.OS === "ios" ? "Menlo" : "monospace";

/// `#rrggbb` at `a` opacity.
export function alpha(hex: string, a: number): string {
  const n = parseInt(hex.slice(1, 7), 16);
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
}

/// An agent's own color: Claude's terracotta, OpenAI's ink for Codex, Gemini's blue, and blue for
/// the rest.
export function agentBrand(kind: string, dark: boolean): string {
  return agentBrandColor(kind, dark, accentColor("blue", dark));
}

/// The colors of the tones of a state, as hex so they take a wash.
export function toneColor(tone: AgentTone, dark: boolean): string {
  switch (tone) {
    case "ask":
      return accentColor("orange", dark);
    case "busy":
      return accentColor("blue", dark);
    case "done":
      return accentColor("green", dark);
    case "fail":
      return accentColor("red", dark);
    default:
      return dark ? "#8E8E93" : "#8A8A8E";
  }
}

/// The agent's mark on a rounded square tinted in its color.
/// An agent's mark from `agentMarks.ts`, in its brand color.
export function AgentMark({ kind, size, color }: { kind: string; size: number; color: string }) {
  return (
    <Svg width={size} height={size} viewBox={AGENT_MARK_VIEWBOX}>
      <Path d={agentMark(kind)} fill={color} />
    </Svg>
  );
}

export function AgentTile({ kind, size, round }: { kind: string; size: number; round?: boolean }) {
  const p = usePalette();
  const brand = agentBrand(kind, p.dark);
  const mark = size * 0.58;
  return (
    <View style={[styles.tile, { width: size, height: size, borderRadius: round ? size / 2 : size * 0.3, backgroundColor: alpha(brand, 0.13), borderColor: alpha(brand, 0.22) }]}>
      <AgentMark kind={kind} size={mark} color={brand} />
    </View>
  );
}

/// The time now, ticking each second while `live`.
export function useNow(live: boolean): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    setNow(Date.now());
    if (!live) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [live]);
  return now;
}

/// The words of its pill: how it stands, in a word or two, by the core's key for it
/// (`AgentRun::summarize`), "Needs you" while it asks.
export function agentWords(agent: AgentRun): string {
  switch (agent.status) {
    case "starting":
      return tc("Starting", "coding agent");
    case "needs_you":
      return tc("Needs you", "coding agent");
    case "working":
      return tc("Working", "coding agent");
    case "quiet":
      return tc("Quiet", "coding agent");
    case "done":
      return tc("Done", "coding agent");
    case "ended":
      return tc("Ended", "coding agent");
    case "failed":
      return tc("Failed", "coding agent");
    case "held":
      return tc("Held by Auto-review", "coding agent");
    case "not_allowed":
      return tc("Not allowed", "coding agent");
    case "no_answer":
      return tc("No answer", "coding agent");
    case "dismissed":
      return tc("Dismissed", "coding agent");
    default:
      return tc("Stopped", "coding agent");
  }
}

/// How it stands: a capsule in its state's tint, with a dot that breathes while it works, and how
/// long it has been at it.
export function AgentPill({ agent }: { agent: AgentRun }) {
  const p = usePalette();
  const busy = agentBusy(agent);
  const now = useNow(busy);
  const tint = toneColor(agentTone(agent), p.dark);
  const since = busy ? agentSince(agent, now) : "";
  const words = since ? `${agentWords(agent)} · ${since}` : agentWords(agent);
  const breath = useSharedValue(1);
  const reduceMotion = useReducedMotion();
  useEffect(() => {
    if (!busy || reduceMotion) {
      cancelAnimation(breath);
      breath.value = 1;
      return;
    }
    breath.value = 0.35;
    breath.value = withRepeat(withTiming(1, { duration: 800, easing: Easing.inOut(Easing.ease) }), -1, true);
    return () => cancelAnimation(breath);
  }, [busy, breath, reduceMotion]);
  const dot = useAnimatedStyle(() => ({ opacity: breath.value }));
  return (
    <View style={[styles.pill, { backgroundColor: alpha(tint, 0.13) }]} accessibilityLabel={words}>
      <Animated.View style={[styles.dot, { backgroundColor: tint }, dot]} />
      <Text style={[styles.pillText, { color: agentTone(agent) === "quiet" ? p.secondaryLabel : tint }]} numberOfLines={1}>
        {words}
      </Text>
    </View>
  );
}

/// The top of every agent surface: its tile, its name over where it works, how it stands, and
/// what goes at the end, such as Stop.
export function AgentHeader({ agent, tile = 32, size = 15, sub, trailing }: { agent: AgentRun; tile?: number; size?: number; sub?: string; trailing?: ReactNode }) {
  const p = usePalette();
  const place = sub ?? [agentPlace(agent), agent.device].filter(Boolean).join(" · ");
  return (
    <View style={styles.header}>
      <AgentTile kind={agent.kind} size={tile} />
      <View style={{ flex: 1, minWidth: 0, gap: 1 }}>
        <Text style={{ color: agentBrand(agent.kind, p.dark), fontSize: size, fontWeight: "600" }} numberOfLines={1}>
          {agentName(agent)}
        </Text>
        {place ? (
          <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
            {sub === undefined ? <Symbol name={agent.branch ? "arrow.triangle.branch" : "folder"} size={11} color={p.tertiaryLabel} /> : null}
            <Text style={{ color: p.tertiaryLabel, fontSize: 12, flexShrink: 1 }} numberOfLines={1}>
              {place}
            </Text>
          </View>
        ) : null}
      </View>
      <AgentPill agent={agent} />
      {trailing}
    </View>
  );
}

/// Stop: a small square on a round fill.
export function StopButton({ busy, onPress, size = 30, label }: { busy: boolean; onPress: () => void; size?: number; label?: string }) {
  const p = usePalette();
  return (
    <Pressable
      disabled={busy}
      onPress={onPress}
      hitSlop={8}
      accessibilityRole="button"
      accessibilityLabel={label ?? t("Stop")}
      style={({ pressed }) => [styles.stop, { width: size, height: size, borderRadius: size / 2, backgroundColor: pressed ? alpha(accentColor("red", p.dark), 0.18) : p.code, opacity: busy ? 0.5 : 1 }]}
    >
      {busy ? <ActivityIndicator size="small" /> : <Symbol name="stop.fill" size={size * 0.36} color={p.secondaryLabel} />}
    </Pressable>
  );
}

/// The console palette: a dark well in both appearances, as a terminal is.
export function consoleColors(dark: boolean) {
  return dark
    ? { bg: "#0d0d0f", edge: "rgba(255,255,255,0.08)", dim: "rgba(228,228,231,0.45)", mark: "#f4f4f5", prompt: "#7cb7ff" }
    : { bg: "#18181b", edge: "rgba(0,0,0,0.2)", dim: "rgba(228,228,231,0.5)", mark: "#fafafa", prompt: "#7cb7ff" };
}

/// What it did last, on the console: what was sent in blue, each tool's line bright, and what
/// came back dimmed, the oldest fading out at the top. With `live`, a spinner leads the line of
/// what it does now.
export function ConsoleTail({ lines, live }: { lines: string[]; live: boolean }) {
  const p = usePalette();
  const k = consoleColors(p.dark);
  const last = lines.reduce((at, line, i) => (line.startsWith("● ") ? i : at), -1);
  return (
    <View style={[styles.console, { backgroundColor: k.bg, borderColor: k.edge }]}>
      {lines.map((line, i) => {
        const spinning = live && i === last;
        const tool = spinning || line.startsWith("● ");
        const shown = spinning ? line.slice(2) : line;
        return (
          <View key={i} style={[styles.consoleLine, lines.length - 1 - i >= 3 && { opacity: 0.55 }]}>
            {spinning ? <ActivityIndicator size="small" color={k.mark} style={styles.consoleSpinner} /> : null}
            <Text numberOfLines={1} style={[styles.consoleText, { color: tool ? k.mark : line.startsWith("> ") ? k.prompt : k.dim, fontWeight: tool ? "600" : "400" }]}>
              {shown}
            </Text>
          </View>
        );
      })}
    </View>
  );
}

/// A panel that asks something of the user or says how it ended: an icon and a title in its tint
/// over a faint wash of it, then what goes under it.
export function Callout({ tint, icon, title, children }: { tint: string; icon: string; title: string; children?: ReactNode }) {
  const p = usePalette();
  return (
    <View style={[styles.callout, { backgroundColor: alpha(tint, 0.09), borderColor: alpha(tint, 0.25) }]}>
      <View style={{ flexDirection: "row", gap: 8, alignItems: "flex-start" }}>
        <Symbol name={icon} size={16} color={tint} style={{ marginTop: 1 }} />
        <Text style={[styles.calloutTitle, { color: p.label }]} numberOfLines={3}>
          {title}
        </Text>
      </View>
      {children ? <View style={{ gap: 8, paddingLeft: 24 }}>{children}</View> : null}
    </View>
  );
}

/// The tint of a context meter: green, orange past two thirds, red past nine tenths.
export function contextTint(share: number, dark: boolean): string {
  return accentColor(share > 0.9 ? "red" : share > 0.66 ? "orange" : "green", dark);
}

/// Its session as a strip of cells, each a label over its value, the model's the widest: a menu
/// where it can change now (`coding.set`); and under them a meter of the context it has used, with
/// what it cost. A card that asks keeps to its question, so it has none.
export function SpecStrip({ agent, chatId, messageId }: { agent: AgentRun; chatId: string; messageId: string }) {
  const p = usePalette();
  const [busy, setBusy] = useState(false);
  const [control, setControl] = useState<AgentRun["controls"] extends (infer C)[] | undefined ? C | null : never>(null);
  if (agent.question) return null;
  const cost = agentSetting(agent, "Cost");
  const context = agentContext(agent);
  const cells = (agent.settings ?? []).filter((setting) => setting.name !== "Context" && setting.name !== "Cost");
  if (cells.length === 0 && !context && !cost) return null;
  const change = (what: string, name: string, value: string) => {
    if (busy) return;
    setBusy(true);
    haptic.send();
    engine
      .setAgentOption(chatId, messageId, what, value)
      .catch((error) => alert(t("Could not change {setting}", { setting: name }), error instanceof Error ? error.message : String(error)))
      .finally(() => setBusy(false));
  };
  return (
    <View style={[styles.strip, { backgroundColor: p.code }]}>
      {cells.length > 0 ? (
        <View style={{ flexDirection: "row" }}>
          {cells.map((setting, i) => {
            const control = agent.controls?.find((each) => each.name === setting.name);
            const value = setting.name === "Model" ? (agent.model ?? setting.value) : setting.value;
            const cell = (
              <View style={styles.cellInner}>
                <Text style={[styles.cellLabel, { color: p.tertiaryLabel }]} numberOfLines={1}>
                  {setting.name}
                </Text>
                <View style={{ flexDirection: "row", alignItems: "center", gap: 3 }}>
                  <Text style={[styles.cellValue, { color: p.label }]} numberOfLines={1}>
                    {value}
                  </Text>
                  {control ? <Symbol name="chevron.up.chevron.down" size={10} color={p.tertiaryLabel} /> : null}
                </View>
              </View>
            );
            return (
              <View key={setting.name} style={[{ flexGrow: setting.name === "Model" ? 2 : 1, flexBasis: 0, minWidth: 0 }, i > 0 && { borderLeftWidth: StyleSheet.hairlineWidth, borderLeftColor: p.separator, marginVertical: 9 }]}>
                {control ? (
                  <Pressable onPress={() => setControl(control)} style={{ opacity: busy ? 0.5 : 1 }} accessibilityRole="button" accessibilityLabel={`${setting.name}: ${value}`}>
                    {cell}
                  </Pressable>
                ) : (
                  cell
                )}
              </View>
            );
          })}
        </View>
      ) : null}
      {context || cost ? (
        <View style={[styles.use, cells.length > 0 && { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: p.separator }]}>
          {context ? (
            <>
              <Text style={[styles.cellLabel, { color: p.tertiaryLabel }]}>{t("Context")}</Text>
              <View style={[styles.meter, { backgroundColor: p.separator }]}>
                <View style={{ height: 4, borderRadius: 2, width: `${Math.max(context.share * 100, 1.5)}%`, backgroundColor: contextTint(context.share, p.dark) }} />
              </View>
              <Text style={[styles.useText, { color: p.secondaryLabel }]} numberOfLines={1}>
                {`${context.words} · ${Math.round(context.share * 100)}%`}
              </Text>
            </>
          ) : (
            <View style={{ flex: 1 }} />
          )}
          {cost ? (
            <>
              {context ? <View style={{ width: StyleSheet.hairlineWidth, height: 12, backgroundColor: p.separator }} /> : null}
              <Text style={[styles.useText, { color: p.label, fontWeight: "600" }]} accessibilityLabel={t("Cost so far")}>
                {cost}
              </Text>
            </>
          ) : null}
        </View>
      ) : null}
      <Sheet visible={!!control} onClose={() => setControl(null)} title={control?.name ?? ""}>
        {control?.choices.map((choice) => (
          <SheetActionRow
            key={choice.value}
            title={choice.name}
            trailing={choice.value === control.value ? <Symbol name="checkmark" size={17} color={p.tint} weight="semibold" /> : null}
            onPress={() => {
              if (choice.value !== control.value) change(control.what, control.name, choice.value);
              setControl(null);
            }}
          />
        ))}
      </Sheet>
    </View>
  );
}

/// The pull request it opened, as a green button that opens it.
export function PullRequestButton({ link, name }: { link: string; name?: string }) {
  const p = usePalette();
  const green = accentColor("green", p.dark);
  return (
    <Pressable
      onPress={() => void Linking.openURL(link)}
      accessibilityRole="link"
      accessibilityLabel={t("Open the pull request")}
      style={({ pressed }) => [styles.pr, { backgroundColor: alpha(green, 0.11), borderColor: alpha(green, 0.28), opacity: pressed ? 0.7 : 1 }]}
    >
      <Symbol name="arrow.triangle.pull" size={14} color={green} />
      <Text style={{ color: p.label, fontSize: 14, fontWeight: "600" }} numberOfLines={1}>
        {name || t("Pull request")}
      </Text>
      <Symbol name="arrow.up.right" size={11} color={p.tertiaryLabel} />
    </Pressable>
  );
}

/// How full its context is, as a hairline along a surface's foot.
export function ContextHairline({ agent }: { agent: AgentRun }) {
  const p = usePalette();
  const context = agentContext(agent);
  if (!context) return null;
  return (
    <View style={{ height: 2, backgroundColor: p.separator }}>
      <View style={{ height: 2, width: `${Math.max(context.share * 100, 2)}%`, backgroundColor: context.share > 0.9 ? accentColor("red", p.dark) : accentColor("blue", p.dark) }} />
    </View>
  );
}

const styles = StyleSheet.create({
  tile: { alignItems: "center", justifyContent: "center", borderWidth: StyleSheet.hairlineWidth, flexShrink: 0 },
  pill: { flexDirection: "row", alignItems: "center", gap: 5, height: 22, paddingLeft: 8, paddingRight: 9, borderRadius: 11, flexShrink: 0 },
  dot: { width: 6, height: 6, borderRadius: 3 },
  pillText: { fontSize: 12, fontWeight: "600", fontVariant: ["tabular-nums"] },
  header: { flexDirection: "row", alignItems: "center", gap: 10, minWidth: 0 },
  stop: { alignItems: "center", justifyContent: "center", flexShrink: 0 },
  console: { borderRadius: 10, borderWidth: StyleSheet.hairlineWidth, paddingVertical: 8, paddingHorizontal: 10, overflow: "hidden" },
  consoleLine: { flexDirection: "row", alignItems: "center", gap: 6, minHeight: 16 },
  consoleSpinner: { transform: [{ scale: 0.55 }], width: 12, height: 12 },
  consoleText: { fontFamily: mono, fontSize: 11.5, lineHeight: 16, flexShrink: 1 },
  callout: { borderRadius: 14, borderWidth: StyleSheet.hairlineWidth, paddingTop: 11, paddingBottom: 12, paddingHorizontal: 12, gap: 8 },
  calloutTitle: { flex: 1, fontSize: 14, fontWeight: "600", lineHeight: 19 },
  strip: { borderRadius: 14, overflow: "hidden" },
  cellInner: { paddingTop: 9, paddingBottom: 10, paddingHorizontal: 12, gap: 2 },
  cellLabel: { fontSize: 11.5, fontWeight: "500" },
  cellValue: { fontSize: 14, fontWeight: "500", flexShrink: 1 },
  use: { flexDirection: "row", alignItems: "center", gap: 10, paddingVertical: 9, paddingHorizontal: 12 },
  meter: { flex: 1, minWidth: 24, height: 4, borderRadius: 2, overflow: "hidden" },
  useText: { fontSize: 12, fontVariant: ["tabular-nums"], flexShrink: 0 },
  pr: { flexDirection: "row", alignItems: "center", gap: 7, alignSelf: "flex-start", height: 34, paddingLeft: 10, paddingRight: 12, borderRadius: 10, borderWidth: StyleSheet.hairlineWidth },
});
