// The profile a Runner keeps of the user, which every bot on it reads: About You, from a bot's
// Memory screen. `id` is the Runner.

import { useLocalSearchParams, useRouter } from "expo-router";
import { profile, writeProfile } from "../../../src/core/memory";
import { useStore } from "../../../src/core/store";
import { t, useLanguage } from "../../../src/i18n";
import { MemoryEditor } from "../../../src/ui/MemoryEditor";

export default function ProfileScreen() {
  useLanguage();
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const runner = useStore((s) => s.devices.find((device) => device.id === id)?.name) ?? t("its Runner");
  return (
    <MemoryEditor
      title={t("About You")}
      footer={t("What the bots on {runner} know about you. They add to it as they learn; edit or remove anything here.", { runner })}
      owner={t("A bot")}
      load={() => profile(id)}
      write={(text, expectedHash) => writeProfile(id, text, expectedHash)}
      onSaved={() => router.back()}
    />
  );
}
