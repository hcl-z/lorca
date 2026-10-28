// The composer's `@` picker, as pure text helpers: where a mention starts, how a pick replaces
// what was typed, and which picks are still in the text when the user sends.

export interface MentionTrigger {
  /// The index of `@`.
  start: number;
  /// The end of the text the pick replaces, which is the caret at the end.
  end: number;
  /// What follows `@`, matched against the targets' names.
  query: string;
}

/// The `@` the user is typing at the end of the text: after a word boundary, on one line. A
/// name with spaces keeps matching until a pick or another character makes it stop.
export function mentionTrigger(text: string, caret = text.length): MentionTrigger | null {
  const before = text.slice(0, caret);
  const at = before.lastIndexOf("@");
  if (at < 0) return null;
  if (at > 0 && !/\s/.test(before[at - 1])) return null;
  const query = before.slice(at + 1);
  if (query.includes("\n")) return null;
  return { start: at, end: caret, query };
}

/// A pick replaces the `@` and everything typed after it, and leaves one space for the message.
/// The name is not repeated: a typed `@Claude Code` becomes `@Claude Code `.
export function insertMention(text: string, trigger: MentionTrigger, target: { name: string }): { text: string; caret: number } {
  const before = text.slice(0, trigger.start);
  const after = text.slice(trigger.end);
  const inserted = `@${target.name} `;
  return { text: before + inserted + after, caret: before.length + inserted.length };
}

export interface MentionPick {
  kind: "bot" | "agent";
  id: string;
  name: string;
}

/// The picks still addressed by `text`, split into bot ids and agent ids. A pick whose `@Name`
/// the user deleted or edited away is dropped; ids are kept once.
export function pickMentions(text: string, picks: MentionPick[]): { mentions: string[]; agents: string[] } {
  const lower = text.toLocaleLowerCase();
  const mentions: string[] = [];
  const agents: string[] = [];
  const seen = { bot: new Set<string>(), agent: new Set<string>() };
  for (const pick of picks) {
    if (!lower.includes(`@${pick.name.toLocaleLowerCase()}`)) continue;
    const ids = pick.kind === "bot" ? mentions : agents;
    if (seen[pick.kind].has(pick.id)) continue;
    seen[pick.kind].add(pick.id);
    ids.push(pick.id);
  }
  return { mentions, agents };
}
