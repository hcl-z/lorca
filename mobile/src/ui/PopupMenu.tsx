// A menu that drops out of a button or a pressed row. iOS keeps UIKit's (`MenuView` over SwiftUI's
// Menu), which draws its own material and follows the system. Android's stock dropdown is a
// square-cornered list that always opens at the top-left of whatever wraps it, so there the app
// draws the menu: a rounded card in the palette that opens beside the button, or under the finger
// that pressed a row, grows out of that point, and closes on a tap outside, Back, or a choice.
// A submenu replaces the card's items under a header that goes back, and a group between dividers
// is an inline action. It takes `MenuView`'s props; it opens through `ref.show()`, which is what
// the buttons and rows that use it already call.

import { MenuView, type MenuAction, type MenuComponentProps, type MenuComponentRef } from "@expo/ui/community/menu";
import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import { Modal, Platform, ScrollView, StyleSheet, Text, useWindowDimensions, View, type ImageSourcePropType } from "react-native";
import Animated, { Easing, runOnJS, useAnimatedStyle, useSharedValue, withTiming } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { AndroidIcons } from "./navigation";
import { Pressable } from "./Pressable";
import { Symbol } from "./Symbol";
import { Font, menuShadow, usePalette } from "./theme";

const WIDTH = 240;
const ROW = 48;
const EDGE = 12;
const GAP = 6;
const CARD_PADDING = 8;

/// The app's icons for its Android action images, which arrive as XML drawables that only the
/// native menus draw.
const ICONS = new Map<ImageSourcePropType, string>([
  [AndroidIcons.photos, "photo.on.rectangle"],
  [AndroidIcons.camera, "camera.fill"],
  [AndroidIcons.folder, "folder.fill"],
  [AndroidIcons.newFolder, "folder.badge.plus"],
  [AndroidIcons.group, "person.2.fill"],
  [AndroidIcons.personAdd, "person.badge.plus"],
  [AndroidIcons.workflow, "point.3.connected.trianglepath.dotted"],
  [AndroidIcons.download, "square.and.arrow.down"],
  [AndroidIcons.pin, "pin"],
  [AndroidIcons.read, "checkmark.circle"],
  [AndroidIcons.delete, "trash"],
  [AndroidIcons.mute, "bell.slash"],
  [AndroidIcons.unmute, "bell"],
  [AndroidIcons.hide, "eye.slash"],
  [AndroidIcons.show, "eye"],
  [AndroidIcons.copy, "doc.on.doc"],
  [AndroidIcons.link, "link"],
  [AndroidIcons.send, "paperplane"],
]);

export const PopupMenu = forwardRef<MenuComponentRef, MenuComponentProps>(function PopupMenu(props, ref) {
  if (Platform.OS !== "android") return <MenuView ref={ref} {...props} />;
  return <AndroidPopup ref={ref} {...props} />;
});

/// Where the menu opens from, in window coordinates: the trigger's frame and, for a wide trigger
/// such as a list row, the point that was pressed.
interface Anchor {
  x: number;
  y: number;
  width: number;
  height: number;
  point: { x: number; y: number } | null;
}

const AndroidPopup = forwardRef<MenuComponentRef, MenuComponentProps>(function AndroidPopup({ actions, onPressAction, onOpenMenu, onCloseMenu, style, testID, children }, ref) {
  const frame = useRef<View>(null);
  const touch = useRef<{ x: number; y: number; at: number } | null>(null);
  const [anchor, setAnchor] = useState<Anchor | null>(null);

  useImperativeHandle(
    ref,
    () => ({
      show() {
        frame.current?.measureInWindow((x, y, width, height) => {
          const t = touch.current;
          const pressed = !!t && Date.now() - t.at < 2500 && t.x >= x && t.x <= x + width && t.y >= y && t.y <= y + height;
          // A button opens its menu at the button; a row, where the finger is.
          setAnchor({ x, y, width, height, point: pressed && width > 200 ? { x: t.x, y: t.y } : null });
          onOpenMenu?.();
        });
      },
    }),
    [onOpenMenu],
  );

  const closed = useCallback(
    (id: string | null) => {
      setAnchor(null);
      onCloseMenu?.();
      if (id !== null) onPressAction?.({ nativeEvent: { event: id } });
    },
    [onCloseMenu, onPressAction],
  );

  return (
    <View
      ref={frame}
      collapsable={false}
      style={style}
      testID={testID}
      onTouchStart={(e) => {
        touch.current = { x: e.nativeEvent.pageX, y: e.nativeEvent.pageY, at: Date.now() };
      }}
    >
      {children}
      {anchor ? <PopupCard anchor={anchor} actions={actions} onClose={closed} /> : null}
    </View>
  );
});

type Entry = { kind: "divider"; key: string } | { kind: "action"; key: string; action: MenuAction };

/// An action list as rows: an inline group stands between dividers, except at either end.
function entriesOf(actions: MenuAction[]): Entry[] {
  const entries: Entry[] = [];
  const divide = () => {
    if (entries.length && entries[entries.length - 1].kind !== "divider") entries.push({ kind: "divider", key: `divider-${entries.length}` });
  };
  actions
    .filter((action) => !action.attributes?.hidden)
    .forEach((action, index) => {
      if (action.displayInline && action.subactions) {
        divide();
        action.subactions.filter((sub) => !sub.attributes?.hidden).forEach((sub, at) => entries.push({ kind: "action", key: `${index}-${at}-${sub.id ?? sub.title}`, action: sub }));
        divide();
      } else {
        entries.push({ kind: "action", key: `${index}-${action.id ?? action.title}`, action });
      }
    });
  if (entries.length && entries[entries.length - 1].kind === "divider") entries.pop();
  return entries;
}

function PopupCard({ anchor, actions, onClose }: { anchor: Anchor; actions: MenuAction[]; onClose: (id: string | null) => void }) {
  const p = usePalette();
  const { width: screenWidth, height: screenHeight } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  // The submenus entered, outermost first.
  const [trail, setTrail] = useState<MenuAction[]>([]);
  const level = trail[trail.length - 1];
  const entries = useMemo(() => entriesOf(level?.subactions ?? actions), [actions, level]);
  const progress = useSharedValue(0);
  const closing = useRef(false);

  useEffect(() => {
    progress.value = withTiming(1, { duration: 170, easing: Easing.out(Easing.cubic) });
  }, [progress]);

  const dismiss = useCallback(
    (id: string | null) => {
      if (closing.current) return;
      closing.current = true;
      progress.value = withTiming(0, { duration: 110, easing: Easing.in(Easing.quad) }, (finished) => {
        if (finished) runOnJS(onClose)(id);
      });
    },
    [onClose, progress],
  );

  const cardStyle = useAnimatedStyle(() => ({ opacity: progress.value, transform: [{ scale: 0.88 + 0.12 * progress.value }] }));
  const scrimStyle = useAnimatedStyle(() => ({ opacity: progress.value }));

  const choose = (action: MenuAction) => {
    if (action.attributes?.disabled) return;
    if (action.subactions?.length) return setTrail((current) => [...current, action]);
    dismiss(action.id ?? action.title);
  };

  // Which side of the trigger has room for the card, and where along it the card hangs.
  const near = anchor.point ?? { x: anchor.x + anchor.width / 2, y: 0 };
  const top = anchor.point ? anchor.point.y : anchor.y + anchor.height;
  const bottom = anchor.point ? anchor.point.y : anchor.y;
  const estimate = entries.length * ROW + CARD_PADDING * 2 + (level ? ROW : 0);
  const spaceBelow = screenHeight - insets.bottom - top - EDGE - GAP;
  const spaceAbove = bottom - insets.top - EDGE - GAP;
  const below = estimate <= spaceBelow || spaceBelow >= spaceAbove;
  const left = anchor.point
    ? Math.min(Math.max(anchor.point.x - WIDTH / 2, EDGE), screenWidth - WIDTH - EDGE)
    : Math.min(Math.max(near.x < screenWidth / 2 ? anchor.x : anchor.x + anchor.width - WIDTH, EDGE), screenWidth - WIDTH - EDGE);
  const originX = Math.min(100, Math.max(0, ((near.x - left) / WIDTH) * 100));
  const place = below ? { top: top + GAP, maxHeight: Math.max(ROW * 2, spaceBelow) } : { bottom: screenHeight - bottom + GAP, maxHeight: Math.max(ROW * 2, spaceAbove) };
  const hasIcon = entries.some((entry) => entry.kind === "action" && iconOf(entry.action));

  return (
    <Modal transparent visible animationType="none" statusBarTranslucent navigationBarTranslucent onRequestClose={() => (trail.length ? setTrail((current) => current.slice(0, -1)) : dismiss(null))}>
      <Pressable ripple="none" onPress={() => dismiss(null)} style={StyleSheet.absoluteFill} accessible={false}>
        <Animated.View pointerEvents="none" style={[StyleSheet.absoluteFill, { backgroundColor: p.dark ? "rgba(0,0,0,0.34)" : "rgba(0,0,0,0.12)" }, scrimStyle]} />
      </Pressable>
      <Animated.View
        style={[
          styles.card,
          menuShadow(p),
          { left, backgroundColor: p.cell, borderColor: p.chromeBorder, transformOrigin: `${originX}% ${below ? 0 : 100}%` },
          place,
          cardStyle,
        ]}
      >
        <View style={styles.clip}>
          <ScrollView bounces={false} showsVerticalScrollIndicator={false} contentContainerStyle={styles.list}>
            {level ? (
              <Pressable onPress={() => setTrail((current) => current.slice(0, -1))} style={styles.row} accessibilityRole="button" accessibilityLabel={level.title}>
                <Symbol name="chevron.left" size={20} color={p.secondaryLabel} />
                <Text style={[styles.label, styles.header, { color: p.label }]} numberOfLines={1}>
                  {level.title}
                </Text>
              </Pressable>
            ) : null}
            {entries.map((entry) => {
              if (entry.kind === "divider") return <View key={entry.key} style={[styles.divider, { backgroundColor: p.separator }]} />;
              const action = entry.action;
              const disabled = !!action.attributes?.disabled;
              const color = action.attributes?.destructive ? p.red : p.label;
              const icon = iconOf(action);
              const submenu = !!action.subactions?.length;
              return (
                <Pressable
                  key={entry.key}
                  onPress={() => choose(action)}
                  disabled={disabled}
                  style={[styles.row, disabled && styles.disabled]}
                  accessibilityRole="menuitem"
                  accessibilityLabel={action.title}
                  accessibilityState={{ disabled, checked: action.state === "on" ? true : undefined }}
                >
                  {icon ? (
                    <Symbol name={icon} size={22} color={action.attributes?.destructive ? p.red : p.secondaryLabel} />
                  ) : hasIcon ? (
                    <View style={styles.icon} />
                  ) : null}
                  <Text style={[styles.label, { color }]} numberOfLines={1}>
                    {action.title}
                  </Text>
                  {submenu ? <Symbol name="chevron.right" size={20} color={p.tertiaryLabel} /> : action.state === "on" ? <Symbol name="checkmark" size={20} color={p.label} weight="semibold" /> : null}
                </Pressable>
              );
            })}
          </ScrollView>
        </View>
      </Animated.View>
    </Modal>
  );
}

function iconOf(action: MenuAction): string | undefined {
  return typeof action.image === "string" ? undefined : ICONS.get(action.image as ImageSourcePropType);
}

const styles = StyleSheet.create({
  // The shadow sits on the card and the clip on the view inside it: an Android view that clips
  // its children draws no shadow of its own on some versions.
  card: { position: "absolute", width: WIDTH, borderRadius: 20, borderWidth: StyleSheet.hairlineWidth },
  clip: { flexShrink: 1, borderRadius: 20, overflow: "hidden" },
  list: { paddingVertical: CARD_PADDING },
  row: { height: ROW, flexDirection: "row", alignItems: "center", gap: 14, paddingHorizontal: 18 },
  icon: { width: 22, height: 22 },
  label: { flex: 1, fontSize: Font.body, letterSpacing: 0.1 },
  header: { fontWeight: "600" },
  disabled: { opacity: 0.38 },
  divider: { height: StyleSheet.hairlineWidth, marginVertical: 6, marginHorizontal: 18 },
});
