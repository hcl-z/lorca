import { expect, test } from "bun:test";
import type { Chat, Message } from "./model";
import { canEdit, canRegenerate, editedText, rerunnable } from "./rerun";

function asked(overrides: Partial<Message> = {}): Message {
  return { id: "msg-asked", chat_id: "chat", author: { kind: "you" }, body: { kind: "text", text: "Summarize the pull request." }, state: { kind: "complete" }, created_at: 1, ...overrides };
}

function reply(id = "msg-reply", overrides: Partial<Message> = {}): Message {
  return { id, chat_id: "chat", author: { kind: "bot", bot_id: "bot" }, body: { kind: "text", text: "It fixes the broken link in the guide." }, state: { kind: "complete" }, created_at: 2, ...overrides };
}

function chat(messages: Message[], overrides: Partial<Chat> = {}): Chat {
  return { id: "chat", kind: "dm", bot_ids: ["bot"], is_pinned: false, created_at: 0, unread_count: 0, messages, ...overrides };
}

test("the user's latest message and the bot's last reply to it run again", () => {
  const first = reply("msg-first");
  const last = reply("msg-last");
  const c = chat([reply("msg-older"), asked(), first, last]);
  expect(rerunnable(c, false)?.asked.id).toBe("msg-asked");
  expect(canEdit(c, false, c.messages[1])).toBe(true);
  expect(canEdit(c, false, c.messages[0])).toBe(false);
  expect(canRegenerate(c, false, last)).toBe(true);
  expect(canRegenerate(c, false, first)).toBe(false);
  expect(canRegenerate(c, false, c.messages[1])).toBe(false);
});

test("nothing runs again while a turn works, in a group or a channel, or before the message is sent", () => {
  const messages = [asked(), reply()];
  expect(rerunnable(chat(messages), true)).toBeNull();
  expect(rerunnable(chat(messages, { kind: "group" }), false)).toBeNull();
  expect(rerunnable(chat(messages, { channel: { service: "telegram" } as any }), false)).toBeNull();
  expect(rerunnable(chat([asked({ queued: true }), reply()]), false)).toBeNull();
  expect(rerunnable(chat([asked({ state: { kind: "streaming" } })]), false)).toBeNull();
  expect(rerunnable(chat([reply()]), false)).toBeNull();
});

test("a reply that left a draft, an output, or work with another bot stays", () => {
  const draft: Message = { ...reply("msg-draft"), body: { kind: "draft", review_id: "r", version: 1, state: "pending", plugin_id: "gmail", account: "Work", draft: { kind: "email", body: "Hi" } } as any };
  const output: Message = { ...reply("msg-output"), output: { id: "o", name: "a.txt", mime: "text/plain", bot_id: "bot", version: 1 } };
  const handed: Message = { ...reply("msg-handed"), body: { kind: "tool", name: "message_bot", summary: "Messaged Paddock", detail: "", is_running: false, target_bot_id: "other" } };
  for (const later of [draft, output, handed]) expect(rerunnable(chat([asked(), later, reply()]), false)).toBeNull();
});

test("an edit sends new words only when they changed", () => {
  const message = asked();
  expect(editedText(message, "  Summarize the pull request in one line. ")).toBe("Summarize the pull request in one line.");
  expect(editedText(message, "Summarize the pull request.")).toBeUndefined();
  expect(editedText(message, "   ")).toBeUndefined();
  expect(editedText(message, undefined)).toBeUndefined();
});
