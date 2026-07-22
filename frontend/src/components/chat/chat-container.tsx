"use client";

import { useChatController } from "@/hooks/use-chat-controller";
import { ChatView } from "./chat-view";

export function ChatContainer({ conversationId }: { conversationId: string }) {
  return <ChatView controller={useChatController(conversationId)} />;
}
