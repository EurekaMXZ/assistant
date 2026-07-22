"use client";

import type { FormEvent, ReactNode } from "react";

import { Spinner } from "@/components/shared/spinner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

interface FormDialogProps {
  bodyClassName?: string;
  cancelText?: string;
  children: ReactNode;
  contentClassName?: string;
  description?: ReactNode;
  onOpenChange: (open: boolean) => void;
  onSubmit: () => void | Promise<void>;
  open: boolean;
  saving?: boolean;
  submitDisabled?: boolean;
  submitText?: string;
  title: ReactNode;
}

function FormDialog({
  bodyClassName,
  cancelText = "取消",
  children,
  contentClassName,
  description,
  onOpenChange,
  onSubmit,
  open,
  saving,
  submitDisabled,
  submitText = "保存",
  title,
}: FormDialogProps) {
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void onSubmit();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={contentClassName}>
        <form className="contents" onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            {description ? <DialogDescription>{description}</DialogDescription> : null}
          </DialogHeader>
          <div className={cn("space-y-4", bodyClassName)}>{children}</div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={saving}
              onClick={() => onOpenChange(false)}
            >
              {cancelText}
            </Button>
            <Button type="submit" disabled={saving || submitDisabled}>
              {saving ? <Spinner /> : null}
              {submitText}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export { FormDialog };
