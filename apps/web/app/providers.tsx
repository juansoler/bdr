"use client";

import { useEffect, useState } from "react";
import { Login } from "@/components/Login";
import { Shell } from "@/components/Shell";
import { AuthContext } from "@/lib/auth";

const STORAGE_KEY = "bdr_admin_token";

export function Providers({ children }: { children: React.ReactNode }) {
  const [token, setToken] = useState<string | null>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    setToken(window.sessionStorage.getItem(STORAGE_KEY));
    setReady(true);
  }, []);

  if (!ready) {
    return (
      <div className="relative z-10 flex min-h-screen items-center justify-center">
        <div className="text-center">
          <div className="mx-auto h-8 w-8 animate-spin border border-signal border-t-transparent" />
          <p className="mt-4 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
            Iniciando consola
          </p>
        </div>
      </div>
    );
  }

  if (!token) {
    return (
      <Login
        onSignIn={(value) => {
          window.sessionStorage.setItem(STORAGE_KEY, value);
          setToken(value);
        }}
      />
    );
  }

  const signOut = () => {
    window.sessionStorage.removeItem(STORAGE_KEY);
    setToken(null);
  };

  return (
    <AuthContext.Provider value={{ token, signOut }}>
      <Shell>{children}</Shell>
    </AuthContext.Provider>
  );
}
