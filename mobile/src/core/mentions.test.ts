import { describe, expect, test } from "bun:test";
import { insertMention, mentionTrigger, pickMentions } from "./mentions";

describe("@ mentions", () => {
  test("a mention trigger reads the name with spaces", () => {
    expect(mentionTrigger("ask @Claude Co")).toEqual({ start: 4, end: 14, query: "Claude Co" });
    expect(mentionTrigger("mail me@example.com")).toBeNull();
    expect(mentionTrigger("plain text")).toBeNull();
  });

  test("a mention pick replaces the typed prefix", () => {
    const text = "please @Cla";
    const trigger = mentionTrigger(text)!;
    expect(insertMention(text, trigger, { name: "Claude Code" })).toEqual({ text: "please @Claude Code ", caret: 20 });
  });

  test("a pick after a typed name leaves one mention", () => {
    const text = "@Claude Code";
    const trigger = mentionTrigger(text)!;
    const picked = insertMention(text, trigger, { name: "Claude Code" });
    expect(picked.text).toBe("@Claude Code ");
    expect(picked.text.match(/@Claude Code/g)).toHaveLength(1);
  });

  test("picks keep their targets and drop names no longer in the text", () => {
    const picks = [
      { kind: "bot" as const, id: "b1", name: "Chef" },
      { kind: "agent" as const, id: "a1", name: "Claude Code" },
      { kind: "agent" as const, id: "a2", name: "Codex" },
    ];
    expect(pickMentions("@Chef and @claude code, status?", picks)).toEqual({ mentions: ["b1"], agents: ["a1"] });
    expect(pickMentions("nothing here", picks)).toEqual({ mentions: [], agents: [] });
  });

  test("picking an agent never repeats its name", () => {
    const picks = [
      { kind: "agent" as const, id: "a1", name: "Claude Code" },
      { kind: "agent" as const, id: "a1", name: "Claude Code" },
    ];
    expect(pickMentions("@Claude Code", picks)).toEqual({ mentions: [], agents: ["a1"] });
  });
});
