"use client";

import {
  createContext,
  useContext,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from "react";

export interface MobileHeaderAction {
  busy?: boolean;
  conversationId?: string;
  disabled?: boolean;
  icon: ReactNode;
  label: string;
  onClick: () => void;
}

export interface MobileHeaderTitleAction {
  conversationId?: string;
  label: string;
  onLongPress: () => void;
}

interface MobileHeaderContextValue {
  setAction: Dispatch<SetStateAction<MobileHeaderAction | null>>;
  setTitle: Dispatch<SetStateAction<string>>;
  setTitleAction: Dispatch<SetStateAction<MobileHeaderTitleAction | null>>;
}

export const MobileHeaderContext = createContext<MobileHeaderContextValue>({
  setAction: () => undefined,
  setTitle: () => undefined,
  setTitleAction: () => undefined,
});

export function useMobileHeader() {
  return useContext(MobileHeaderContext);
}
