import { describe, expect, test } from "bun:test";
import { chatsSendParams } from "./send";

describe("chats.send", () => {
  test("sendMessage passes the bots it mentions and the message it answers", () => {
    expect(chatsSendParams("chat", "status", [], ["b1"], "m1")).toEqual({
      chat_id: "chat",
      text: "status",
      attachments: [],
      mentions: ["b1"],
      reply_to: "m1",
    });
  });

  test("a message that answers nothing leaves the field out", () => {
    expect(chatsSendParams("chat", "hello", [], ["b1"])).toEqual({
      chat_id: "chat",
      text: "hello",
      attachments: [],
      mentions: ["b1"],
    });
  });
});
