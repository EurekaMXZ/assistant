"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import {
  commitInitialTurn,
  isSessionUnauthorizedError,
  uploadConversationAttachment,
  prepareInitialTurn,
} from "@/lib/api";
import { openAuthDialog } from "@/lib/auth-dialog-events";
import { emitConversationUpdated } from "@/lib/conversation-events";
import { useAuth } from "@/hooks/use-auth";
import { useComposerPreferences } from "@/hooks/use-composer-preferences";
import { stashPendingHomeTurn } from "@/lib/pending-home-turn";
import {
  createInitialTurnOperation,
  loadInitialTurnOperation,
  operationMatches,
  runInitialTurnOperation,
  saveInitialTurnOperation,
} from "@/lib/initial-turn-operation";
import { Button } from "@/components/ui/button";
import { WelcomePanel } from "@/components/welcome/welcome-panel";
import { toast } from "sonner";

export default function HomePage() {
  const [draft, setDraft] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const { user, isLoading } = useAuth();
  const composerPreferences = useComposerPreferences(Boolean(user) && !isLoading);
  const router = useRouter();

  const handleSubmit = async () => {
    const content = draft.trim();
    if ((!content && files.length === 0) || submitting || isLoading) {
      return;
    }

    if (!user) {
      openAuthDialog("login");
      return;
    }

    setSubmitting(true);
    try {
      const input = {
        content,
        attachment_ids: [],
        ...(composerPreferences.modelId ? { model_id: composerPreferences.modelId } : {}),
        ...(composerPreferences.reasoningEffort
          ? { reasoning_effort: composerPreferences.reasoningEffort }
          : {}),
        metadata: { source: "home" },
      };
      const stored = loadInitialTurnOperation();
      const operation =
        stored && operationMatches(stored, input, files, user.id)
          ? stored
          : createInitialTurnOperation(input, files, user.id);
      saveInitialTurnOperation(operation);
      const { operation: completedOperation, result: messageResult } =
        await runInitialTurnOperation(operation, files, {
          prepare: prepareInitialTurn,
          uploadAttachment: (conversationId, file, key) =>
            uploadConversationAttachment(conversationId, file, key),
          commit: (conversationId, descriptor, key) =>
            commitInitialTurn(key, conversationId, descriptor),
        });
      stashPendingHomeTurn({
        conversation_id: messageResult.conversation_id,
        message: messageResult.message,
        turn: messageResult.turn,
        stream_path: messageResult.stream_path,
      });
      if (messageResult.conversation) {
        emitConversationUpdated({
          conversation: messageResult.conversation,
          id: messageResult.conversation.id,
        });
      }
      setDraft("");
      setFiles([]);
      router.push(`/c/${completedOperation.conversation_id}`);
    } catch (err) {
      if (isSessionUnauthorizedError(err)) {
        setSubmitting(false);
        return;
      }
      toast.error(err instanceof Error ? err.message : "创建会话失败");
      setSubmitting(false);
    }
  };

  return (
    <WelcomePanel
      actions={
        user || isLoading ? null : (
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => openAuthDialog("login")}>
              登录
            </Button>
            <Button size="sm" onClick={() => openAuthDialog("register")}>
              注册
            </Button>
          </div>
        )
      }
      disabled={isLoading}
      files={files}
      submitting={submitting}
      value={draft}
      models={composerPreferences.models}
      modelsLoading={composerPreferences.modelsLoading}
      modelId={composerPreferences.modelId}
      reasoningEfforts={composerPreferences.reasoningEfforts}
      onChange={setDraft}
      onFilesChange={setFiles}
      onModelChange={composerPreferences.setModelId}
      onModelReasoningEffortChange={composerPreferences.setModelReasoningEffort}
      onSubmit={() => void handleSubmit()}
    />
  );
}
