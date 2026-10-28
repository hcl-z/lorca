import { describe, expect, test } from "bun:test";
import { chatsSendParams } from "./send";

describe("chats.send", () => {
  test("sendMessage passes the agent ids", () => {
    expect(chatsSendParams("chat", "status", [], ["b1"], ["a1"], "m1")).toEqual({
      chat_id: "chat",
      text: "status",
      attachments: [],
      mentions: ["b1"],
      agents: ["a1"],
      reply_to: "m1",
    });
  });

  test("a message without agents leaves the field out", () => {
    expect(chatsSendParams("chat", "hello", [], ["b1"])).toEqual({
      chat_id: "chat",
      text: "hello",
      attachments: [],
      mentions: ["b1"],
    });
  });
});
