import { expect, test } from "bun:test";
import { preview } from "./format";

test("the list previews the roster's last message before the chat's page loads", () => {
  const chat = {
    id: "c",
    kind: "dm",
    bot_ids: ["b"],
    messages: [],
    unread_count: 0,
    created_at: 0,
    last_message: { id: "m", chat_id: "c", author: { kind: "you" }, body: { kind: "text", text: "hello" }, state: { kind: "complete" }, created_at: 0 },
  } as never;
  expect(preview(chat, new Map())).toBe("hello");
});
