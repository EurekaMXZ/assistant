"use client";

import { useLayoutEffect, useState } from "react";

interface AutosizeTextareaState {
  currentHeight: number;
  isMultiline: boolean;
  maxHeight: number;
  singleLineHeight: number;
}

export function useAutosizeTextarea(
  ref: React.RefObject<HTMLTextAreaElement | null>,
  value: string,
  maxRows = 3,
) {
  const [state, setState] = useState<AutosizeTextareaState>({
    currentHeight: 24,
    isMultiline: false,
    maxHeight: 120,
    singleLineHeight: 24,
  });

  useLayoutEffect(() => {
    const textarea = ref.current;
    if (!textarea) {
      return;
    }

    const styles = window.getComputedStyle(textarea);
    const lineHeight = Number.parseFloat(styles.lineHeight) || 24;
    const paddingTop = Number.parseFloat(styles.paddingTop) || 0;
    const paddingBottom = Number.parseFloat(styles.paddingBottom) || 0;
    const borderTop = Number.parseFloat(styles.borderTopWidth) || 0;
    const borderBottom = Number.parseFloat(styles.borderBottomWidth) || 0;
    const singleLineHeight = lineHeight + paddingTop + paddingBottom + borderTop + borderBottom;
    const maxHeight = lineHeight * maxRows + paddingTop + paddingBottom + borderTop + borderBottom;

    textarea.style.height = "0px";

    const scrollHeight = textarea.scrollHeight;
    const multiline = scrollHeight > singleLineHeight + 1;
    const nextHeight = multiline ? Math.min(scrollHeight, maxHeight) : singleLineHeight;

    textarea.style.height = `${nextHeight}px`;
    textarea.style.overflowY = scrollHeight > maxHeight ? "auto" : "hidden";
    setState({
      currentHeight: nextHeight,
      isMultiline: multiline,
      maxHeight,
      singleLineHeight,
    });
  }, [maxRows, ref, value]);

  return state;
}
