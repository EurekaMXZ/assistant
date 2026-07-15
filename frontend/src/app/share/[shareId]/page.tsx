import { SharedConversation } from "@/components/chat/shared-conversation";

interface PageProps {
  params: Promise<{ shareId: string }>;
}

export default async function ConversationSharePage({ params }: PageProps) {
  const { shareId } = await params;
  return <SharedConversation shareId={shareId} />;
}
