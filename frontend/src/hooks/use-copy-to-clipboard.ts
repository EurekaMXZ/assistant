"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

interface CopyToClipboardOptions {
  successMessage?: string;
  errorMessage?: string;
  resetAfter?: number | false;
}

async function writeText(text: string) {
  try {
    if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const input = document.createElement("textarea");
    try {
      input.value = text;
      input.readOnly = true;
      input.style.position = "fixed";
      input.style.opacity = "0";
      document.body.appendChild(input);
      input.select();
      return document.execCommand("copy");
    } catch {
      return false;
    } finally {
      input.remove();
    }
  }
}

function useCopyToClipboard({
  successMessage,
  errorMessage,
  resetAfter = 1500,
}: CopyToClipboardOptions = {}) {
  const [copied, setCopied] = useState(false);
  const resetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const resetCopied = useCallback(() => {
    if (resetTimerRef.current) clearTimeout(resetTimerRef.current);
    resetTimerRef.current = null;
    setCopied(false);
  }, []);

  useEffect(
    () => () => {
      if (resetTimerRef.current) clearTimeout(resetTimerRef.current);
    },
    [],
  );

  const copyToClipboard = useCallback(
    async (text: string) => {
      const succeeded = await writeText(text);
      if (!succeeded) {
        if (errorMessage) toast.error(errorMessage);
        return false;
      }

      setCopied(true);
      if (successMessage) toast.success(successMessage);
      if (resetAfter !== false) {
        if (resetTimerRef.current) clearTimeout(resetTimerRef.current);
        resetTimerRef.current = setTimeout(resetCopied, resetAfter);
      }
      return true;
    },
    [errorMessage, resetAfter, resetCopied, successMessage],
  );

  return { copied, copyToClipboard, resetCopied };
}

export { useCopyToClipboard };
