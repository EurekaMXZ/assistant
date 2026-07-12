"use client";

import { createContext, useContext, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  clearToken,
  getToken,
  isSessionUnauthorizedError,
  login as apiLogin,
  me,
  register as apiRegister,
} from "@/lib/api";
import { emitAuthStateChange, subscribeAuthStateChange } from "@/lib/auth-state-events";
import type { RegistrationResult, Session, User } from "@/lib/types";

interface AuthContextValue {
  user: User | null;
  isLoading: boolean;
  error: string | null;
  status: "loading" | "authenticated" | "unauthenticated" | "error";
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, username: string, password: string) => Promise<RegistrationResult>;
  logout: () => void;
  refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();

  const loadUser = async () => {
    setError(null);
    const token = getToken();
    if (!token) {
      setUser(null);
      setIsLoading(false);
      return;
    }
    try {
      const u = await me();
      setUser(u);
    } catch (loadError) {
      if (isSessionUnauthorizedError(loadError)) {
        setUser(null);
      } else {
        setError(loadError instanceof Error ? loadError.message : "账户加载失败");
      }
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    return subscribeAuthStateChange(() => {
      setUser(null);
      setError(null);
      setIsLoading(false);
    });
  }, []);

  useEffect(() => {
    loadUser();
  }, []);

  const login = async (email: string, password: string) => {
    const session: Session = await apiLogin(email, password);
    setUser(session.user);
    setError(null);
    router.push("/");
  };

  const register = async (email: string, username: string, password: string) => {
    return apiRegister(email, username, password);
  };

  const logout = () => {
    clearToken();
    emitAuthStateChange({ reason: "logout" });
    router.push("/");
  };

  const refresh = async () => {
    setIsLoading(true);
    await loadUser();
  };

  const status = isLoading
    ? "loading"
    : error
      ? "error"
      : user
        ? "authenticated"
        : "unauthenticated";

  return (
    <AuthContext.Provider
      value={{ user, isLoading, error, status, login, register, logout, refresh }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
