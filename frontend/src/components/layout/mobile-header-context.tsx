"use client";

import { createContext, useContext, type Dispatch, type SetStateAction } from "react";

export interface MobileHeaderAction {
  label: string;
  onClick: () => void;
}

interface MobileHeaderContextValue {
  setAction: Dispatch<SetStateAction<MobileHeaderAction | null>>;
  setTitle: Dispatch<SetStateAction<string>>;
}

export const MobileHeaderContext = createContext<MobileHeaderContextValue>({
  setAction: () => undefined,
  setTitle: () => undefined,
});

export function useMobileHeader() {
  return useContext(MobileHeaderContext);
}
