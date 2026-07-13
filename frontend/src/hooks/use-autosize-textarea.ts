"use client";

import { useLayoutEffect, useState } from "react";

interface AutosizeTextareaState {
  currentHeight: number;
  isMultiline: boolean;
  maxHeight: number;
  singleLineHeight: number;
}

interface AutosizeTextareaInsets {
  multilineLeft: number;
  multilineRight: number;
  singleLineLeft: number;
  singleLineRight: number;
}

export function useAutosizeTextarea(
  ref: React.RefObject<HTMLTextAreaElement | null>,
  value: string,
  maxRows = 3,
  insets?: AutosizeTextareaInsets,
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

    const previousLeft = textarea.style.left;
    const previousRight = textarea.style.right;
    const measureAtInsets = (left: number | undefined, right: number | undefined) => {
      if (left !== undefined) textarea.style.left = `${left}px`;
      if (right !== undefined) textarea.style.right = `${right}px`;
      textarea.style.height = "0px";
      return textarea.scrollHeight;
    };

    // Always decide the mode at the compact width. Measuring at the expanded
    // width would make a wrapped line fit again and oscillate between modes.
    const singleLineScrollHeight = measureAtInsets(insets?.singleLineLeft, insets?.singleLineRight);
    const multiline = singleLineScrollHeight > singleLineHeight + 1;
    const scrollHeight = multiline
      ? measureAtInsets(insets?.multilineLeft, insets?.multilineRight)
      : singleLineScrollHeight;
    const nextHeight = multiline ? Math.min(scrollHeight, maxHeight) : singleLineHeight;

    textarea.style.left = previousLeft;
    textarea.style.right = previousRight;
    textarea.style.height = `${nextHeight}px`;
    textarea.style.overflowY = scrollHeight > maxHeight ? "auto" : "hidden";
    setState({
      currentHeight: nextHeight,
      isMultiline: multiline,
      maxHeight,
      singleLineHeight,
    });
  }, [
    insets?.multilineLeft,
    insets?.multilineRight,
    insets?.singleLineLeft,
    insets?.singleLineRight,
    maxRows,
    ref,
    value,
  ]);

  return state;
}
