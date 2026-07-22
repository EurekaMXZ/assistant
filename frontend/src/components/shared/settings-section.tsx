import * as React from "react";

import { cn } from "@/lib/utils";

interface SettingsSectionProps extends Omit<React.ComponentProps<"div">, "title"> {
  title: React.ReactNode;
}

function SettingsSection({ title, className, children, ...props }: SettingsSectionProps) {
  return (
    <div className={cn("space-y-9", className)} {...props}>
      <header>
        <h2 className="text-xl font-semibold">{title}</h2>
      </header>
      {children}
    </div>
  );
}

export { SettingsSection };
