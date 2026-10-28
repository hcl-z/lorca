// The request body for a sent message, kept pure so the phone's send path is plain to pin:
// files as attachments, the picked bots and coding agents, and the quoted message.

export interface SendAttachment {
  path: string;
  name: string;
  mime: string;
  width?: number;
  height?: number;
}

export function chatsSendParams(
  chatId: string,
  text: string,
  attachments: SendAttachment[] = [],
  mentions: string[] = [],
  agents: string[] = [],
  replyTo?: string,
) {
  return {
    chat_id: chatId,
    text,
    attachments,
    mentions,
    ...(agents.length ? { agents } : {}),
    ...(replyTo ? { reply_to: replyTo } : {}),
  };
}
