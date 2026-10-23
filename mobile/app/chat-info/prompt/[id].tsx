// What a bot's next turn sends in a chat: each section of its system prompt with its size, then the
// notes after the transcript, with Copy, as the desktop's Prompt sheet shows them (`bots.prompt`).

import * as Clipboard from "expo-clipboard";
import { Stack, useLocalSearchParams } from "expo-router";
import { useEffect, useState } from "react";
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from "react-native";
import { fileSize } from "../../../src/core/model";
import { promptPreview, type PromptPreview } from "../../../src/core/memory";
import { t, useLanguage } from "../../../src/i18n";
import { mono } from "../../../src/ui/agentKit";
import { Row, Section } from "../../../src/ui/forms";
import { Font, usePalette } from "../../../src/ui/theme";

export default function PromptScreen() {
  useLanguage();
  const { id, chat } = useLocalSearchParams<{ id: string; chat: string }>();
  const p = usePalette();
  const [preview, setPreview] = useState<PromptPreview>();
  const [error, setError] = useState<string>();
  const [open, setOpen] = useState<Record<string, boolean>>({});

  const load = () => {
    setError(undefined);
    promptPreview(id, chat)
      .then(setPreview)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(load, [id, chat]);

  const whole = preview ? [...preview.sections.map((section) => `## ${section.title}\n\n${section.text}`), ...preview.notes].join("\n\n") : "";
  return (
    <>
      <Stack.Screen options={{ title: t("Prompt") }} />
      <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
        {!preview ? (
          <View style={styles.center}>
            {error ? (
              <>
                <Text style={[styles.note, { color: p.secondaryLabel }]}>{error}</Text>
                <Section style={{ alignSelf: "stretch" }}>
                  <Row title={t("Retry")} action onPress={load} />
                </Section>
              </>
            ) : (
              <ActivityIndicator />
            )}
          </View>
        ) : (
          <>
            <Section footer={t("About {tokens} tokens. A turn's brief belongs to what started it, so it is not shown here.", { tokens: preview.approx_tokens.toLocaleString() })}>
              <Row title={t("Copy")} icon="doc.on.doc" action onPress={() => void Clipboard.setStringAsync(whole)} />
            </Section>
            <Section title={t("System prompt")}>
              {preview.sections.map((section) => (
                <View key={section.id}>
                  <Row title={section.title} detail={fileSize(section.bytes)} chevron onPress={() => setOpen((was) => ({ ...was, [section.id]: !was[section.id] }))} />
                  {open[section.id] ? (
                    <Text selectable style={[styles.text, { color: p.label, backgroundColor: p.code }]}>
                      {section.text}
                    </Text>
                  ) : null}
                </View>
              ))}
            </Section>
            {preview.notes.length ? (
              <Section title={t("After the transcript")} footer={fileSize(preview.notes_bytes)}>
                {preview.notes.map((note, at) => (
                  <Text key={at} selectable style={[styles.text, { color: p.label }]}>
                    {note}
                  </Text>
                ))}
              </Section>
            ) : null}
          </>
        )}
      </ScrollView>
    </>
  );
}

const styles = StyleSheet.create({
  content: { paddingBottom: 40 },
  text: { fontFamily: mono, fontSize: 12, lineHeight: 17, padding: 12 },
  center: { alignItems: "center", justifyContent: "center", gap: 12, padding: 24, paddingTop: 80 },
  note: { fontSize: Font.body, textAlign: "center" },
});
