"use client";

import { createContext, useContext } from "react";

export interface AuthContextValue {
  token: string;
  signOut: () => void;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("useAuth debe usarse dentro de Providers");
  }
  return value;
}
