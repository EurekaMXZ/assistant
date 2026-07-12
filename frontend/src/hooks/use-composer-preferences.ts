"use client";

import { useEffect, useState } from "react";
import { isSessionUnauthorizedError, listModels } from "@/lib/api";
import type { Model, ReasoningEffort } from "@/lib/types";
import { supportedReasoningEfforts } from "@/lib/model-capabilities";

const MODEL_STORAGE_KEY = "assistant_composer_model_id";
const REASONING_STORAGE_KEY = "assistant_composer_reasoning_efforts";

function storedReasoningEfforts(value: string | null): Record<string, ReasoningEffort> {
  if (!value) return {};
  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    return Object.fromEntries(
      Object.entries(parsed).filter((entry): entry is [string, ReasoningEffort] =>
        ["low", "medium", "high", "xhigh"].includes(String(entry[1])),
      ),
    );
  } catch {
    return {};
  }
}

export function useComposerPreferences(enabled: boolean) {
  const [models, setModels] = useState<Model[]>([]);
  const [modelId, setModelIdState] = useState("");
  const [reasoningEfforts, setReasoningEfforts] = useState<Record<string, ReasoningEffort>>({});
  const [modelsLoading, setModelsLoading] = useState(false);

  useEffect(() => {
    setModelIdState(localStorage.getItem(MODEL_STORAGE_KEY) || "");
    setReasoningEfforts(storedReasoningEfforts(localStorage.getItem(REASONING_STORAGE_KEY)));
  }, []);

  useEffect(() => {
    if (!enabled) {
      setModels([]);
      return;
    }
    let cancelled = false;
    setModelsLoading(true);
    void listModels()
      .then((items) => {
        if (cancelled) return;
        setModels(items);
        setModelIdState((current) => {
          if (!current || items.some((item) => item.id === current)) return current;
          localStorage.removeItem(MODEL_STORAGE_KEY);
          return "";
        });
      })
      .catch((error) => {
        if (!cancelled && !isSessionUnauthorizedError(error)) setModels([]);
      })
      .finally(() => {
        if (!cancelled) setModelsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);

  const setModelId = (value: string) => {
    setModelIdState(value);
    if (value) localStorage.setItem(MODEL_STORAGE_KEY, value);
    else localStorage.removeItem(MODEL_STORAGE_KEY);
  };

  const setModelReasoningEffort = (targetModelId: string, value: ReasoningEffort | "") => {
    setReasoningEfforts((current) => {
      const next = { ...current };
      if (value) next[targetModelId] = value;
      else delete next[targetModelId];
      if (Object.keys(next).length > 0)
        localStorage.setItem(REASONING_STORAGE_KEY, JSON.stringify(next));
      else localStorage.removeItem(REASONING_STORAGE_KEY);
      return next;
    });
  };

  const defaultModel = models.find((item) => item.is_default) || null;
  const selectedModel =
    (modelId ? models.find((item) => item.id === modelId) : defaultModel) || null;
  const supportedEfforts = supportedReasoningEfforts(selectedModel);
  const storedEffort = selectedModel ? reasoningEfforts[selectedModel.id] : undefined;
  const reasoningEffort: ReasoningEffort | "" =
    storedEffort && supportedEfforts.includes(storedEffort) ? storedEffort : "";

  return {
    models,
    modelsLoading,
    modelId,
    reasoningEffort,
    reasoningEfforts,
    setModelId,
    setModelReasoningEffort,
  };
}
