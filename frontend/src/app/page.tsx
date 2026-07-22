"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Share } from "lucide-react";
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
import { useCopyToClipboard } from "@/hooks/use-copy-to-clipboard";
import { useMobileHeader } from "@/components/layout/mobile-header-context";
import { stashPendingHomeTurn } from "@/lib/pending-home-turn";
import {
  commitPreparedInitialTurn,
  loadInitialTurnOperation,
  saveInitialTurnOperation,
  syncInitialTurnOperation,
  uploadInitialTurnAttachment,
} from "@/lib/initial-turn-operation";
import { Button } from "@/components/ui/button";
import type { ComposerShellAttachment } from "@/components/chat/composer-shell";
import { WelcomePanel } from "@/components/welcome/welcome-panel";
import { toast } from "sonner";

type HomeAttachment = ComposerShellAttachment & { file: File };

export default function HomePage() {
  const [draft, setDraft] = useState("");
  const [attachments, setAttachments] = useState<HomeAttachment[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const { user, isLoading } = useAuth();
  const composerPreferences = useComposerPreferences(Boolean(user) && !isLoading);
  const { copyToClipboard } = useCopyToClipboard({
    successMessage: "网站 URL 已复制",
    errorMessage: "复制网站 URL 失败",
  });
  const { setAction: setMobileAction } = useMobileHeader();
  const router = useRouter();
  const attachmentsRef = useRef<HomeAttachment[]>([]);
  const uploadQueueRef = useRef(Promise.resolve());

  const replaceAttachments = (next: HomeAttachment[]) => {
    attachmentsRef.current = next;
    setAttachments(next);
  };

  const updateAttachment = (key: string, update: Partial<HomeAttachment>) => {
    const current = attachmentsRef.current;
    const index = current.findIndex((attachment) => attachment.key === key);
    if (index === -1) return;
    replaceAttachments(
      current.map((attachment, attachmentIndex) =>
        attachmentIndex === index ? { ...attachment, ...update } : attachment,
      ),
    );
  };

  const initialTurnInput = () => ({
    content: draft.trim(),
    ...(composerPreferences.modelId ? { model_id: composerPreferences.modelId } : {}),
    ...(composerPreferences.reasoningEffort
      ? { reasoning_effort: composerPreferences.reasoningEffort }
      : {}),
    metadata: { source: "home" },
  });

  const enqueueAttachmentUpload = (attachment: HomeAttachment, operationKey: string) => {
    uploadQueueRef.current = uploadQueueRef.current
      .catch(() => undefined)
      .then(async () => {
        try {
          const result = await uploadInitialTurnAttachment(
            operationKey,
            attachment.key,
            attachment.file,
            {
              prepare: prepareInitialTurn,
              uploadAttachment: (conversationId, file, key) =>
                uploadConversationAttachment(conversationId, file, key),
            },
          );
          if (!result) return;
          updateAttachment(attachment.key, {
            attachmentId: result.attachmentId,
            contentType: result.attachment?.content_type || attachment.contentType,
            conversationId: result.conversationId,
            status: "ready",
          });
        } catch (error) {
          updateAttachment(attachment.key, {
            error: error instanceof Error ? error.message : "文件上传失败",
            status: "failed",
          });
        }
      });
  };

  const copyWebsiteUrl = useCallback(async () => {
    await copyToClipboard(new URL("/", window.location.origin).toString());
  }, [copyToClipboard]);

  useEffect(() => {
    setMobileAction({
      icon: <Share className="size-4" />,
      label: "复制网站 URL",
      onClick: () => void copyWebsiteUrl(),
    });
    return () => setMobileAction(null);
  }, [copyWebsiteUrl, setMobileAction]);

  const handleSubmit = async () => {
    const content = draft.trim();
    const readyAttachmentIds = attachments.flatMap((attachment) =>
      attachment.status === "ready" && attachment.attachmentId ? [attachment.attachmentId] : [],
    );
    if ((!content && readyAttachmentIds.length === 0) || submitting || isLoading) {
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
      const operation = syncInitialTurnOperation(
        loadInitialTurnOperation(),
        input,
        attachments.map((attachment) => attachment.file),
        user.id,
      );
      saveInitialTurnOperation(operation);
      let prepared = operation;
      if (!prepared.conversation_id) {
        const { conversation } = await prepareInitialTurn(prepared.key);
        const latest = loadInitialTurnOperation();
        prepared = {
          ...(latest?.key === prepared.key ? latest : prepared),
          conversation_id: conversation.id,
        };
        saveInitialTurnOperation(prepared);
      }
      const readyAttachmentIdSet = new Set(readyAttachmentIds);
      const commitOperation = {
        ...prepared,
        files: prepared.files.filter(
          (file) => file.attachment_id && readyAttachmentIdSet.has(file.attachment_id),
        ),
      };
      const { operation: completedOperation, result: messageResult } =
        await commitPreparedInitialTurn(commitOperation, {
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
      replaceAttachments([]);
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

  const handleFilesSelected = (selectedFiles: File[]) => {
    if (submitting || isLoading) return;
    if (!user) {
      openAuthDialog("login");
      return;
    }
    if (selectedFiles.length === 0) return;

    const existing = attachmentsRef.current;
    const allFiles = [...existing.map((attachment) => attachment.file), ...selectedFiles];
    const operation = syncInitialTurnOperation(
      loadInitialTurnOperation(),
      initialTurnInput(),
      allFiles,
      user.id,
    );
    saveInitialTurnOperation(operation);
    const pending = selectedFiles.map<HomeAttachment>((file, index) => ({
      contentType: file.type,
      file,
      key: operation.files[existing.length + index].upload_key as string,
      name: file.name,
      size: file.size,
      status: "uploading",
    }));
    replaceAttachments([...existing, ...pending]);
    pending.forEach((attachment) => enqueueAttachmentUpload(attachment, operation.key));
  };

  const handleRemoveAttachment = (key: string) => {
    const next = attachmentsRef.current.filter((attachment) => attachment.key !== key);
    if (next.length === attachmentsRef.current.length || !user) return;
    const operation = syncInitialTurnOperation(
      loadInitialTurnOperation(),
      initialTurnInput(),
      next.map((attachment) => attachment.file),
      user.id,
    );
    saveInitialTurnOperation(operation);
    replaceAttachments(next);
  };

  return (
    <WelcomePanel
      actions={
        <div className="flex items-center gap-2">
          {!user && !isLoading ? (
            <>
              <Button variant="ghost" size="sm" onClick={() => openAuthDialog("login")}>
                登录
              </Button>
              <Button size="sm" onClick={() => openAuthDialog("register")}>
                注册
              </Button>
            </>
          ) : null}
          <Button
            type="button"
            variant="ghost"
            size="icon-md"
            className="shrink-0"
            aria-label="复制网站 URL"
            title="复制网站 URL"
            onClick={() => void copyWebsiteUrl()}
          >
            <Share className="size-3.5" />
            <span className="sr-only">复制网站 URL</span>
          </Button>
        </div>
      }
      attachments={attachments}
      disabled={isLoading}
      submitting={submitting}
      value={draft}
      models={composerPreferences.models}
      modelsLoading={composerPreferences.modelsLoading}
      modelId={composerPreferences.modelId}
      reasoningEfforts={composerPreferences.reasoningEfforts}
      onChange={setDraft}
      onFilesSelected={handleFilesSelected}
      onModelChange={composerPreferences.setModelId}
      onModelReasoningEffortChange={composerPreferences.setModelReasoningEffort}
      onRemoveAttachment={handleRemoveAttachment}
      onSubmit={() => void handleSubmit()}
    />
  );
}
