"use client";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";

export function RenameDialog({
  open,
  title,
  onOpenChange,
  onSave,
  onTitleChange,
}: {
  open: boolean;
  title: string;
  onOpenChange: (open: boolean) => void;
  onSave: () => void;
  onTitleChange: (title: string) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>重命名会话</DialogTitle>
        </DialogHeader>
        <FormField label="标题" htmlFor="title" className="py-4">
          <Input
            id="title"
            value={title}
            onChange={(event) => onTitleChange(event.target.value)}
            onKeyDown={(event) => event.key === "Enter" && onSave()}
          />
        </FormField>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={onSave}>保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
