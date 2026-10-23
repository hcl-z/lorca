// A bot's MEMORY.md in the editor, from its Memory screen.

import { useLocalSearchParams, useRouter } from "expo-router";
import { botMemory, writeBotMemory } from "../../../src/core/memory";
import { useBotMap } from "../../../src/core/store";
import { t, useLanguage } from "../../../src/i18n";
import { MemoryEditor } from "../../../src/ui/MemoryEditor";

export default function EditBotMemoryScreen() {
  useLanguage();
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const bot = useBotMap().get(id);
  if (!bot) return null;
  return (
    <MemoryEditor
      title={t("{name}'s memory", { name: bot.name })}
      footer={t("What {name} keeps between chats. A turn loads the start of it; the rest is left out.", { name: bot.name })}
      owner={bot.name}
      load={async () => (await botMemory(id)).index}
      write={(text, expectedHash) => writeBotMemory(id, text, expectedHash)}
      onSaved={() => router.back()}
    />
  );
}
