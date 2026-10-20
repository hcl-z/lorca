// One output, slid in inside the chat's details, after the macOS app's output sheet: a preview of
// the file or the link and its versions, any of which the screen can show. Open shows the file in Quick Look (iOS) or the link in the browser; Share hands either to
// the share sheet.

import { Stack, useLocalSearchParams } from "expo-router";
import { useState } from "react";
import { Platform, ScrollView } from "react-native";
import { engine } from "../../../src/core/engine";
import { documentUrl, fileSize } from "../../../src/core/model";
import { useBotMap, useOutputs, useStore } from "../../../src/core/store";
import { t, useLanguage } from "../../../src/i18n";
import { CheckRow, Row, Section } from "../../../src/ui/forms";
import { daySeparator } from "../../../src/ui/format";
import { openFile, openLink, OutputPreview, outputAttachment, shareFile, shareLink } from "../../../src/ui/outputs";
import { canPreviewFiles } from "../../../modules/lorca-core";

export default function OutputScreen() {
  useLanguage();
  // `version` opens on that version's message, as a task's evidence names it.
  const { id, chat, version } = useLocalSearchParams<{ id: string; chat: string; version?: string }>();
  const bots = useBotMap();
  const series = useOutputs(chat).find((each) => each.id === id);
  const [picked, setPicked] = useState<string | undefined>(version || undefined);
  const message = series?.versions.find((version) => version.id === picked) ?? series?.versions[0];
  const attachment = message ? outputAttachment(message) : undefined;
  const error = useStore((s) => (attachment ? s.fileErrors[attachment.id] : undefined));
  const fetched = useStore((s) => (attachment ? !!s.files[attachment.id] : false));
  if (!series || !message?.output) return null;

  const output = message.output;
  const link = documentUrl(output);
  const published = `${bots.get(output.bot_id)?.name ?? t("A bot")} · ${daySeparator(new Date(message.created_at * 1000))}`;

  return (
    <>
      <Stack.Screen options={{ title: output.name }} />
      <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={{ paddingBottom: 40 }}>
        {attachment ? (
          <Section footer={published}>
            <OutputPreview key={attachment.id} attachment={attachment} />
          </Section>
        ) : link ? (
          <Section title={t("Link")} footer={published}>
            <Row title={new URL(link).host} subtitle={link} icon="link" chevron onPress={() => openLink(link)} />
          </Section>
        ) : null}

        {series.versions.length > 1 && (
          <Section title={t("Versions")}>
            {series.versions.map((version) => (
              <CheckRow
                key={version.id}
                title={t("Version {number}", { number: version.output!.version })}
                subtitle={[daySeparator(new Date(version.created_at * 1000)), outputAttachment(version) ? fileSize(outputAttachment(version)!.size) : null].filter(Boolean).join(" · ")}
                checked={version.id === message.id}
                onPress={() => setPicked(version.id === series.versions[0].id ? undefined : version.id)}
              />
            ))}
          </Section>
        )}

        <Section>
          {attachment && error ? (
            <Row title={t("Try Again")} icon="arrow.clockwise" onPress={() => engine.retryFile(attachment)} />
          ) : attachment && canPreviewFiles ? (
            <Row title={t("Open")} icon="eye" onPress={fetched ? () => void openFile(attachment) : undefined} />
          ) : link ? (
            <Row title={t("Open")} icon="safari" onPress={() => openLink(link)} />
          ) : null}
          {attachment ? (
            <Row title={Platform.OS === "ios" ? t("Share…") : t("Share")} icon="square.and.arrow.up" onPress={fetched ? () => void shareFile(attachment) : undefined} />
          ) : link ? (
            <Row title={Platform.OS === "ios" ? t("Share…") : t("Share")} icon="square.and.arrow.up" onPress={() => shareLink(link)} />
          ) : null}
        </Section>
      </ScrollView>
    </>
  );
}
