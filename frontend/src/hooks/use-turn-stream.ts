"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getStreamUrl, getToken, getTurn, isSessionUnauthorizedError } from "@/lib/api";
import { streamEvents } from "@/lib/sse";
import {
  runTurnStreamController,
  type TurnStreamConnectionState,
} from "@/lib/turn-stream-controller";
import type { TurnStreamFrame } from "@/lib/api-schemas";
import { toast } from "sonner";

interface UseTurnStreamOptions {
  conversationId: string;
  onCompleted: (turnId: string) => Promise<void>;
  onEvent: (frame: TurnStreamFrame, turnId: string) => void;
  onFinished: (turnId: string) => void;
}

export interface TurnStreamLifecycle {
  isStreaming: boolean;
  streamingTurnId: string | null;
  streamConnectionState: TurnStreamConnectionState | "idle";
  streamTurn: (turnId: string, streamPath?: string) => Promise<void>;
  stopStream: () => void;
}

export function useTurnStream({
  conversationId,
  onCompleted,
  onEvent,
  onFinished,
}: UseTurnStreamOptions): TurnStreamLifecycle {
  const [isStreaming, setIsStreaming] = useState(false);
  const [streamingTurnId, setStreamingTurnId] = useState<string | null>(null);
  const [streamConnectionState, setStreamConnectionState] = useState<
    TurnStreamConnectionState | "idle"
  >("idle");
  const abortRef = useRef<AbortController | null>(null);
  const activeConversationIdRef = useRef(conversationId);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      abortRef.current?.abort();
      abortRef.current = null;
    };
  }, []);

  useEffect(() => {
    activeConversationIdRef.current = conversationId;
    abortRef.current?.abort();
    abortRef.current = null;
    setIsStreaming(false);
    setStreamingTurnId(null);
    setStreamConnectionState("idle");
  }, [conversationId]);

  const streamTurn = useCallback(
    async (turnId: string, streamPath?: string) => {
      if (!mountedRef.current) return;
      const requestedConversationId = conversationId;
      setIsStreaming(true);
      setStreamingTurnId(turnId);

      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;
      let completed = false;

      try {
        const result = await runTurnStreamController({
          turnId,
          signal: controller.signal,
          openStream: (signal) =>
            streamEvents(getStreamUrl(streamPath || `/turns/${turnId}/stream`), getToken(), signal),
          getTurn,
          onEvent: (event) => onEvent(event, turnId),
          onStateChange: setStreamConnectionState,
          shouldReconnect: (error) => !isSessionUnauthorizedError(error),
        });
        if (result.kind === "terminal") {
          completed = result.done.status === "completed";
        } else {
          toast.error(result.error.message);
        }
      } catch (error) {
        if ((error as Error).name !== "AbortError" && !isSessionUnauthorizedError(error)) {
          toast.error(error instanceof Error ? error.message : "流式输出失败");
        }
      } finally {
        if (abortRef.current !== controller) return;
        if (completed && activeConversationIdRef.current === requestedConversationId) {
          await onCompleted(turnId);
        }
        if (
          abortRef.current !== controller ||
          !mountedRef.current ||
          activeConversationIdRef.current !== requestedConversationId
        ) {
          return;
        }
        setIsStreaming(false);
        setStreamingTurnId(null);
        setStreamConnectionState("idle");
        onFinished(turnId);
        abortRef.current = null;
      }
    },
    [conversationId, onCompleted, onEvent, onFinished],
  );

  const stopStream = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  return {
    isStreaming,
    streamingTurnId,
    streamConnectionState,
    streamTurn,
    stopStream,
  };
}
