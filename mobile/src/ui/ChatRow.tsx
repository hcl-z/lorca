// A chat in the list, after the reference's: a large avatar (a breathing dot while a bot works), the
// title with its stamp at the end of the line, a one-line preview without a "You:" prefix (three
// pulsing dots and "Working…" while the chat has a turn in flight), and an ink count pill while it
// has unread messages. No chevron and no separators; the rows breathe instead.

import { memo, useEffect, useMemo } from "react";
import { StyleSheet, Text, View, type ColorValue } from "react-native";
import { Pressable } from "./Pressable";
import Animated, { Easing, useAnimatedStyle, useSharedValue, withRepeat, withTiming, type SharedValue } from "react-native-reanimated";
import { isMuted, type Bot, type Chat } from "../core/model";
import { t, useLanguage } from "../i18n";
import { AvatarCluster } from "./Avatar";
import { AgentTile } from "./agentKit";
import { lastActivity, preview, previewAgent, stamp } from "./format";
import { Symbol } from "./Symbol";
import { Font, usePalette } from "./theme";

/// `selected` is set in a sidebar, where the open chat's row stays lit and no row points onward.
/// `responding` is a turn in flight in this chat; `working` also lights the avatar's dot for a
/// member at work in another chat.
export const ChatRow = memo(function ChatRow({ chat, bots, title, working, responding, selected, onPress, onLongPress }: { chat: Chat; bots: Map<string, Bot>; title: string; working: boolean; responding: boolean; selected?: boolean; onPress: () => void; onLongPress?: () => void }) {
  useLanguage();
  const p = usePalette();
  const members = useMemo(() => chat.bot_ids.map((id) => bots.get(id)).filter((b): b is Bot => !!b), [chat.bot_ids, bots]);
  const unread = chat.unread_count;
  const agent = previewAgent(chat);
  return (
    <Pressable onPress={onPress} onLongPress={onLongPress} style={({ pressed }) => [styles.row, { backgroundColor: pressed || selected ? p.fill : p.background }, selected !== undefined && styles.sidebarRow]}>
      <AvatarCluster bots={members} size={52} working={working} service={chat.channel?.service} />
      <View style={styles.text}>
        <View style={styles.titleLine}>
          <View style={styles.titleGroup}>
            <Text style={[styles.title, { color: p.label }]} numberOfLines={1}>
              {title}
            </Text>
            {isMuted(chat) && <Symbol name="bell.slash.fill" size={12} color={p.tertiaryLabel} />}
            {chat.is_pinned && <Symbol name="pin.fill" size={12} color={p.tertiaryLabel} />}
          </View>
          <Text style={[styles.stamp, { color: unread > 0 ? p.label : p.secondaryLabel }]}>{stamp(new Date(lastActivity(chat) * 1000))}</Text>
        </View>
        <View style={styles.previewLine}>
          {responding ? (
            <View style={styles.working}>
              <PulsingDots color={p.secondaryLabel} />
              <Text style={[styles.workingText, { color: p.secondaryLabel }]} numberOfLines={1}>
                {t("Working…")}
              </Text>
            </View>
          ) : (
            <>
              {agent ? <AgentTile kind={agent.kind} size={15} round /> : null}
              <Text style={[styles.preview, { color: p.secondaryLabel }]} numberOfLines={1}>
                {preview(chat, bots)}
              </Text>
            </>
          )}
          {unread > 0 && (
            <View style={[styles.badge, { backgroundColor: isMuted(chat) ? p.tertiaryLabel : p.ink }]} accessibilityLabel={t("{count} unread", { count: unread })}>
              <Text style={[styles.badgeText, { color: p.inkText }]}>{unread > 999 ? "999+" : unread}</Text>
            </View>
          )}
        </View>
      </View>
    </Pressable>
  );
});

/// Three dots that light up in turn: a wave runs across them, then they rest. Memoized: the row
/// renders again with every streamed piece of the reply, and an animated style re-rendered reads
/// its shared values back from the UI thread.
const PulsingDots = memo(function PulsingDots({ color }: { color: ColorValue }) {
  const beat = useSharedValue(0);
  useEffect(() => {
    beat.value = withRepeat(withTiming(1, { duration: 1200, easing: Easing.linear }), -1);
  }, [beat]);
  return (
    <View style={styles.dots}>
      {[0, 1, 2].map((index) => (
        <PulsingDot key={index} beat={beat} index={index} color={color} />
      ))}
    </View>
  );
});

function PulsingDot({ beat, index, color }: { beat: SharedValue<number>; index: number; color: ColorValue }) {
  const animated = useAnimatedStyle(() => {
    // Each dot swells over 40% of the beat, a fifth of a beat after the one before it.
    const phase = (beat.value - index * 0.2 + 1) % 1;
    const lift = phase < 0.4 ? Math.sin((phase / 0.4) * Math.PI) : 0;
    return { opacity: 0.35 + 0.65 * lift, transform: [{ scale: 1 + 0.25 * lift }] };
  });
  return <Animated.View style={[styles.dot, { backgroundColor: color }, animated]} />;
}

const styles = StyleSheet.create({
  // The avatar's left edge lines up with the bar's round buttons (16pt).
  row: { flexDirection: "row", alignItems: "center", paddingLeft: 16, paddingRight: 16, paddingVertical: 11, gap: 14 },
  // In a sidebar the open chat's row is a rounded fill inside the column.
  sidebarRow: { marginHorizontal: 8, paddingLeft: 8, paddingRight: 8, borderRadius: 14 },
  text: { flex: 1, gap: 3 },
  titleLine: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 },
  titleGroup: { flexDirection: "row", alignItems: "center", gap: 5, flexShrink: 1 },
  title: { fontSize: Font.body, fontWeight: "600", flexShrink: 1, letterSpacing: -0.2 },
  stamp: { fontSize: 15 },
  previewLine: { flexDirection: "row", alignItems: "center", gap: 8 },
  preview: { flex: 1, fontSize: 15, lineHeight: 20 },
  working: { flex: 1, minHeight: 20, flexDirection: "row", alignItems: "center", gap: 7 },
  workingText: { flexShrink: 1, fontSize: 15, lineHeight: 20 },
  dots: { height: 20, flexDirection: "row", alignItems: "center", gap: 3 },
  dot: { width: 5, height: 5, borderRadius: 2.5 },
  badge: { minWidth: 20, height: 20, borderRadius: 10, paddingHorizontal: 6, alignItems: "center", justifyContent: "center" },
  badgeText: { fontSize: 12, fontWeight: "700", fontVariant: ["tabular-nums"] },
});
