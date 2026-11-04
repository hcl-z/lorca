// A chat in the list, after Cindy's home list: an avatar (a breathing dot while a bot works), the
// title with its stamp at the end of the line, a one-line preview without a "You:" prefix (three
// pulsing dots and "Working…" while the chat has a turn in flight), and an ink count pill while it
// has unread messages, over a hairline that starts at the text. A chat with nothing to preview is
// one line.

import { memo, useEffect, useMemo } from "react";
import { StyleSheet, View, type ColorValue } from "react-native";
import { Text } from "./AppText";
import { Pressable } from "./Pressable";
import Animated, { Easing, useAnimatedStyle, useSharedValue, withRepeat, withTiming, type SharedValue } from "react-native-reanimated";
import { isMuted, type Bot, type Chat } from "../core/model";
import { t, useLanguage } from "../i18n";
import { AvatarCluster } from "./Avatar";
import { lastActivity, preview, stamp } from "./format";
import { Symbol } from "./Symbol";
import { LIST_ROW_AVATAR, listRowStyles } from "./listRowStyles";
import { useTheme, useThemedStyles } from "./ThemeProvider";
import { iconSize } from "./tokens";

/// `selected` is set in a sidebar, where the open chat's row stays lit and no row points onward.
/// `responding` is a turn in flight in this chat; `working` also lights the avatar's dot for a
/// member at work in another chat.
export const ChatRow = memo(function ChatRow({ chat, bots, title, working, responding, selected, onPress, onLongPress }: { chat: Chat; bots: Map<string, Bot>; title: string; working: boolean; responding: boolean; selected?: boolean; onPress: () => void; onLongPress?: () => void }) {
  useLanguage();
  const styles = useThemedStyles(listRowStyles);
  const { colors } = useTheme();
  const members = useMemo(() => chat.bot_ids.map((id) => bots.get(id)).filter((b): b is Bot => !!b), [chat.bot_ids, bots]);
  const unread = chat.unread_count;
  const text = responding ? "" : preview(chat, bots);
  const singleLine = !responding && !text && unread === 0;
  const sidebar = selected !== undefined;
  return (
    <Pressable onPress={onPress} onLongPress={onLongPress} style={({ pressed }) => [styles.row, singleLine && styles.singleLine, sidebar && styles.sidebar, (pressed || selected) && styles.pressed]}>
      <AvatarCluster bots={members} size={LIST_ROW_AVATAR} working={working} service={chat.channel?.service} />
      <View style={[styles.content, sidebar && styles.contentPlain]}>
        <View style={styles.titleLine}>
          <View style={styles.titleGroup}>
            <Text style={styles.title} numberOfLines={1}>
              {title}
            </Text>
            {isMuted(chat) && <Symbol name="bell.slash.fill" size={iconSize.xs} color={colors.textTertiary} />}
            {chat.is_pinned && <Symbol name="pin.fill" size={iconSize.xs} color={colors.textTertiary} />}
          </View>
          <Text style={[styles.stamp, unread > 0 && styles.stampUnread]}>{stamp(new Date(lastActivity(chat) * 1000))}</Text>
        </View>
        {singleLine ? null : (
          <View style={styles.previewLine}>
            {responding ? (
              <View style={rowStyles.working}>
                <PulsingDots color={colors.textSecondary} />
                <Text style={styles.preview} numberOfLines={1}>
                  {t("Working…")}
                </Text>
              </View>
            ) : (
              <Text style={styles.preview} numberOfLines={1}>
                {text}
              </Text>
            )}
            {unread > 0 && (
              <View style={[styles.badge, isMuted(chat) && styles.badgeMuted]} accessibilityLabel={t("{count} unread", { count: unread })}>
                <Text style={styles.badgeText}>{unread > 999 ? "999+" : unread}</Text>
              </View>
            )}
          </View>
        )}
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
    <View style={rowStyles.dots}>
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
  return <Animated.View style={[rowStyles.dot, { backgroundColor: color }, animated]} />;
}

const rowStyles = StyleSheet.create({
  working: { flex: 1, minWidth: 0, flexDirection: "row", alignItems: "center", gap: 7 },
  dots: { height: 20, flexDirection: "row", alignItems: "center", gap: 3 },
  dot: { width: 5, height: 5, borderRadius: 2.5 },
});
