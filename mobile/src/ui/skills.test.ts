import { describe, expect, test } from "bun:test";
import { skillInputOf, skillRecordOf, type Chat, type Message, type SkillView } from "../core/model";
import { addSkillFile, captureKind, captureSources, fillSkillForm, safeFileName, skillFormContent, skillNameProblem, skillScopeLine, skillSlug, useSkillForm } from "./skills";

const message = (id: string, author: Message["author"], text = id, at = 1): Message => ({
  id,
  chat_id: "room",
  author,
  body: { kind: "text", text },
  state: { kind: "complete" },
  created_at: at,
});
const you = { kind: "you" } as const;
const chef = { kind: "bot", bot_id: "chef" } as const;
const writer = { kind: "bot", bot_id: "writer" } as const;
const room: Chat = { id: "room", kind: "group", bot_ids: ["chef", "writer"], owner_bot_id: "writer", is_pinned: false, created_at: 0, unread_count: 0, messages: [] };

describe("skills", () => {
  test("a name typed the way people write it goes as a slug; one the core refuses says so", () => {
    expect(skillSlug(" Weekly Report ")).toBe("weekly-report");
    expect(skillNameProblem("Weekly Report")).toBeUndefined();
    expect(skillNameProblem("weekly/report")).toBe("Use lowercase letters, numbers, and hyphens.");
    expect(skillNameProblem("")).toBeUndefined();
    expect(safeFileName("checklist.md")).toBe(true);
    expect(safeFileName("sub/notes.md")).toBe(true);
    for (const bad of ["../outside.md", ".env", "a//b", ""]) expect(safeFileName(bad)).toBe(false);
  });

  test("a reply makes a workflow for its bot; two of the user's messages a standing instruction for the owner", () => {
    expect(captureKind(room, [message("ask", you), message("reply", chef)])).toEqual({ kind: "workflow", botId: "chef" });
    expect(captureKind(room, [message("fix", you)])).toBeUndefined();
    expect(captureKind(room, [message("fix", you), message("again", you)])).toEqual({ kind: "corrections", botId: "writer" });
    const chat = { ...room, messages: [message("ask", you), { ...message("live", writer), state: { kind: "streaming" } as const }, message("reply", chef)] };
    expect(captureSources(chat).map((m) => m.id)).toEqual(["ask", "reply"]);
  });

  test("the form keeps files in their folders", () => {
    fillSkillForm({ name: "report", description: "d", instructions: "i", examples: "", references: [{ path: "references/style.md", text: "s" }], scripts: [] });
    addSkillFile("references");
    addSkillFile("scripts");
    expect(skillFormContent(useSkillForm.getState()).references.map((f) => f.path)).toEqual(["references/style.md", "references/notes.md"]);
    expect(skillFormContent(useSkillForm.getState()).scripts.map((f) => f.path)).toEqual(["scripts/script.sh"]);
  });

  test("a skill from its Runner keeps the files the screens do not show, and a save sends them back", () => {
    const view: SkillView = {
      scope: { kind: "bot", id: "chef" }, name: "pdf-tools", description: "Fill PDFs", path: "/skills/pdf-tools", builtin: false, updated_at: 1,
      instructions: "Use the scripts.", hash: "h1",
      files: [
        { path: "references/forms.md", size: 5, text: "forms" },
        { path: "scripts/fill.py", size: 3, text: "run" },
        { path: "LICENSE.txt", size: 3, text: "MIT" },
        { path: "assets/logo.png", size: 900, text: null },
      ],
    };
    const record = skillRecordOf(view);
    expect(record.id).toBe("pdf-tools");
    expect(record.content?.references.map((f) => f.path)).toEqual(["references/forms.md"]);
    expect(record.content?.other?.map((f) => f.path)).toEqual(["LICENSE.txt"]);
    const input = skillInputOf({ ...record.content!, examples: "A filled form" });
    expect(input.instructions).toBe("Use the scripts.\n\n## Examples\n\nA filled form");
    expect(input.files.map((f) => f.path)).toEqual(["references/forms.md", "scripts/fill.py", "LICENSE.txt"]);
    expect(skillScopeLine({ kind: "shared", id: "" }, false, {})).toBe("Every bot on this Runner can use it.");
  });
});
