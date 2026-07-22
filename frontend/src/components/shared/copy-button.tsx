"use client";

import { forwardRef } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useCopyToClipboard } from "@/hooks/use-copy-to-clipboard";

interface CopyButtonProps {
  className?: string;
  text: string;
}

export const CopyButton = forwardRef<HTMLButtonElement, CopyButtonProps>(function CopyButton(
  { text, className },
  ref,
) {
  const { copied, copyToClipboard } = useCopyToClipboard();

  return (
    <Button
      ref={ref}
      variant="ghost"
      size="icon-md"
      className={className}
      onClick={() => void copyToClipboard(text)}
    >
      {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
      <span className="sr-only">复制</span>
    </Button>
  );
});
