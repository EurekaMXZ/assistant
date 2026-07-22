import * as React from "react";

import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

interface FormFieldProps extends React.ComponentProps<"div"> {
  label: React.ReactNode;
  htmlFor?: string;
  labelClassName?: string;
  labelAction?: React.ReactNode;
  error?: React.ReactNode;
  errorId?: string;
}

function FormField({
  label,
  htmlFor,
  labelClassName,
  labelAction,
  error,
  errorId,
  className,
  children,
  ...props
}: FormFieldProps) {
  return (
    <div data-slot="form-field" className={cn("grid gap-2", className)} {...props}>
      {labelAction ? (
        <div className="flex items-center justify-between">
          <Label htmlFor={htmlFor} className={labelClassName}>
            {label}
          </Label>
          {labelAction}
        </div>
      ) : (
        <Label htmlFor={htmlFor} className={labelClassName}>
          {label}
        </Label>
      )}
      {children}
      {error ? (
        <p
          id={errorId}
          data-slot="form-field-error"
          role="alert"
          className="text-sm text-destructive"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}

export { FormField };
