// A memory file's editor, for a bot's MEMORY.md and for the profile its Runner shares with every
// bot there: the text, how much of it a turn loads, and Save. A save carries the hash the file was
// opened at; when the Runner refuses it because the file changed since, the user takes the file as
// it is now or writes theirs over it, as on the desktop.

import { Stack } from "expo-router";
import { useEffect, useState } from "react";
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from "react-native";
import { fileSize } from "../core/model";
import { gauge, isConflict, type MemoryFile } from "../core/memory";
import { t } from "../i18n";
import { mono } from "./agentKit";
import { alert } from "./alert";
import { FieldRow, Row, Section } from "./forms";
import { SaveToolbar } from "./navigation";
import { accentColor, Font, usePalette } from "./theme";

export function MemoryEditor({
  title,
  footer,
  owner,
  load,
  write,
  onSaved,
}: {
  title: string;
  /// What the file is for, under the editor.
  footer: string;
  /// Who else writes the file, for the conflict's words: the bot, or the bots on a Runner.
  owner: string;
  load: () => Promise<MemoryFile>;
  /// Writes the text; `expectedHash` null writes over whatever is there.
  write: (text: string, expectedHash: string | null) => Promise<unknown>;
  onSaved: () => void;
}) {
  const p = usePalette();
  const [file, setFile] = useState<MemoryFile>();
  const [text, setText] = useState("");
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const reload = () => {
    setError(undefined);
    load()
      .then((fresh) => {
        setFile(fresh);
        setText(fresh.text);
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(reload, []);

  async function save(expectedHash: string | null) {
    if (!file || saving) return;
    if (expectedHash !== null && text === file.text) return onSaved();
    setSaving(true);
    try {
      await write(text, expectedHash);
      onSaved();
    } catch (e) {
      setSaving(false);
      if (!isConflict(e)) return alert(t("Couldn't save the memory"), e instanceof Error ? e.message : String(e));
      alert(t("{name} changed this file while you were editing", { name: owner }), t("Reload shows {name}'s version and discards your draft. Overwrite saves yours over it.", { name: owner }), [
        { text: t("Reload"), onPress: reload },
        { text: t("Overwrite with mine"), style: "destructive", onPress: () => void save(null) },
        { text: t("Cancel"), style: "cancel" },
      ]);
    }
  }

  if (!file) {
    return (
      <>
        <Stack.Screen options={{ title }} />
        <View style={styles.center}>
          {error ? (
            <>
              <Text style={[styles.note, { color: p.secondaryLabel }]}>{error}</Text>
              <Section style={{ alignSelf: "stretch" }}>
                <Row title={t("Retry")} action onPress={reload} />
              </Section>
            </>
          ) : (
            <ActivityIndicator />
          )}
        </View>
      </>
    );
  }

  const measure = gauge(text, file);
  const color = measure.tone === "red" ? accentColor("red", p.dark) : measure.tone === "orange" ? accentColor("orange", p.dark) : p.secondaryLabel;
  const lines = measure.lines === 1 ? t("{count} line of {max}", { count: measure.lines, max: file.max_lines }) : t("{count} lines of {max}", { count: measure.lines, max: file.max_lines });
  const past =
    measure.tone !== "red" ? "" : measure.hiddenLines === 1 ? t("1 line past the budget will not load") : measure.hiddenLines > 1 ? t("{count} lines past the budget will not load", { count: measure.hiddenLines }) : t("the end will not load");
  return (
    <>
      <Stack.Screen options={{ title }} />
      <SaveToolbar label={t("Save")} disabled={saving} onSave={() => void save(file.hash)} />
      <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content} keyboardDismissMode="on-drag" keyboardShouldPersistTaps="handled">
        <Section footer={footer}>
          <FieldRow value={text} onChangeText={setText} multiline autoCapitalize="sentences" autoCorrect={false} style={styles.editor} />
        </Section>
        <Text style={[styles.gauge, { color }]}>{[lines, `${fileSize(measure.bytes)} / ${fileSize(file.max_bytes)}`, past].filter(Boolean).join(" · ")}</Text>
      </ScrollView>
    </>
  );
}

const styles = StyleSheet.create({
  content: { paddingBottom: 40 },
  editor: { minHeight: 320, fontFamily: mono, fontSize: 13, lineHeight: 19 },
  gauge: { fontSize: Font.small, marginHorizontal: 32, marginTop: -16 },
  center: { flex: 1, alignItems: "center", justifyContent: "center", gap: 12, padding: 24 },
  note: { fontSize: Font.body, textAlign: "center" },
});
