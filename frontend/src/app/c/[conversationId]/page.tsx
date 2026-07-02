import { ChatContainer } from "@/components/chat/chat-container";

interface PageProps {
  params: Promise<{ conversationId: string }>;
}

export default async function ConversationPage({ params }: PageProps) {
  const { conversationId } = await params;
  return <ChatContainer conversationId={conversationId} />;
}
