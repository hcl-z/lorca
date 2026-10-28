// The composer's "+" menu as a bottom sheet: the same three attachment sources on both
// platforms, with the sheet's native drag and scrim instead of an anchored popup.

import { t, useLanguage } from "../i18n";
import { ActionSheet, type SheetAction } from "./Sheet";
import { SHEET_PRESETS } from "./sheetPresets";

export type AttachSource = "photos" | "camera" | "file";

export function AttachSheet({ visible, onClose, onPick }: { visible: boolean; onClose: () => void; onPick: (source: AttachSource) => void }) {
  useLanguage();
  const actions: SheetAction[] = [
    { label: t("Photo Library"), icon: "photo.on.rectangle", onPress: () => onPick("photos") },
    { label: t("Take Photo"), icon: "camera", onPress: () => onPick("camera") },
    { label: t("Choose File"), icon: "folder", onPress: () => onPick("file") },
  ];
  return <ActionSheet visible={visible} title={t("Attach")} actions={actions} onClose={onClose} snapPoints={SHEET_PRESETS.attach} />;
}
