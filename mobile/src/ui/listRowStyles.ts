// The chat list's rows, after Cindy's home list: a 78 pt row of two lines (60 pt for one), the
// title at 18/28 medium, the preview at 15/26 in the secondary color, the stamp at 13/22 in the
// tertiary one, and a hairline under the text that starts where the text does.

import { StyleSheet } from "react-native";
import { fontWeight, lineHeight, spacing, typeScale, type ThemeColors } from "./tokens";

export const LIST_ROW_HEIGHT = 78;
export const LIST_ROW_SINGLE_LINE_HEIGHT = 60;
/// The avatar at a row's start.
export const LIST_ROW_AVATAR = 48;

export const listRowStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    row: { flexDirection: "row", alignItems: "center", gap: spacing.md, height: LIST_ROW_HEIGHT, paddingLeft: spacing.lg, backgroundColor: colors.surface },
    singleLine: { height: LIST_ROW_SINGLE_LINE_HEIGHT },
    pressed: { backgroundColor: colors.surfaceChip },
    // In a sidebar the open chat's row is a rounded fill inside the column.
    sidebar: { marginHorizontal: spacing.sm, paddingLeft: spacing.sm, borderRadius: 12 },
    content: { flex: 1, minWidth: 0, alignSelf: "stretch", justifyContent: "center", paddingRight: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
    contentPlain: { borderBottomWidth: 0 },
    titleLine: { flexDirection: "row", alignItems: "center", gap: spacing.sm, height: lineHeight.listTitle },
    titleGroup: { flex: 1, minWidth: 0, flexDirection: "row", alignItems: "center", gap: spacing.xs },
    title: { flexShrink: 1, minWidth: 0, color: colors.textPrimary, fontSize: typeScale.subtitle, lineHeight: lineHeight.listTitle, fontWeight: fontWeight.medium },
    stamp: { flexShrink: 0, color: colors.textTertiary, fontSize: typeScale.footnote, lineHeight: lineHeight.body, fontWeight: fontWeight.regular },
    stampUnread: { color: colors.textPrimary },
    previewLine: { flexDirection: "row", alignItems: "center", gap: spacing.sm, height: lineHeight.bodyLarge },
    preview: { flex: 1, minWidth: 0, color: colors.textSecondary, fontSize: typeScale.bodySmall, lineHeight: lineHeight.bodyLarge, fontWeight: fontWeight.regular },
    badge: { minWidth: 20, height: 20, borderRadius: 10, paddingHorizontal: 6, alignItems: "center", justifyContent: "center", backgroundColor: colors.cta },
    badgeMuted: { backgroundColor: colors.textTertiary },
    badgeText: { color: colors.ctaText, fontSize: typeScale.caption, lineHeight: 16, fontWeight: fontWeight.semibold, fontVariant: ["tabular-nums"] },
  });
