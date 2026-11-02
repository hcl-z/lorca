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

  test("picks keep their bots and drop names no longer in the text", () => {
    const picks = [
      { id: "b1", name: "Chef" },
      { id: "b2", name: "Scout" },
    ];
    expect(pickMentions("@chef, status?", picks)).toEqual(["b1"]);
    expect(pickMentions("nothing here", picks)).toEqual([]);
  });

  test("a bot picked twice is mentioned once", () => {
    const picks = [
      { id: "b1", name: "Chef" },
      { id: "b1", name: "Chef" },
    ];
    expect(pickMentions("@Chef", picks)).toEqual(["b1"]);
  });
});
