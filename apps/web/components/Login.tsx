"use client";

import { useEffect, useState } from "react";
import { api, type AgentList, publicHealth } from "@/lib/api";
import { Button, ErrorBox, inputClass } from "./ui";

export function Login({ onSignIn }: { onSignIn: (token: string) => void }) {
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [health, setHealth] = useState<string>("comprobando API…");

  useEffect(() => {
    let alive = true;
    publicHealth().then((h) => {
      if (!alive) return;
      setHealth(h ? `API ${h.version} · online` : "API no disponible");
    });
    return () => {
      alive = false;
    };
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const value = token.trim();
    if (!value) {
      setError("Introduce el token de administración.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api<AgentList>("/api/v1/admin/agents", value);
      onSignIn(value);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo validar el token.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="relative z-10 grid min-h-screen lg:grid-cols-[1.15fr_0.85fr]">
      <section className="relative hidden overflow-hidden border-r border-ink-700 lg:flex lg:flex-col lg:justify-between lg:p-12">
        <div className="grid-fade absolute inset-0 opacity-60" />
        <div className="absolute -right-24 top-1/3 h-[420px] w-[420px] rounded-full border border-signal/10" />
        <div className="absolute -right-10 top-1/2 h-[220px] w-[220px] rounded-full border border-signal/20" />

        <div className="relative">
          <div className="flex items-center gap-3 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
            <span className="h-2 w-2 bg-signal" />
            Consola de continuidad
          </div>
          <h1 className="mt-10 max-w-xl font-display text-7xl font-semibold uppercase leading-[0.86] tracking-wide text-slate-100">
            Proteger.
            <br />
            Verificar.
            <br />
            <span className="text-signal">Recuperar.</span>
          </h1>
          <p className="mt-8 max-w-md text-sm leading-relaxed text-slate-400">
            Un plano de control para copias de seguridad, repositorios inmutables y
            restauración de máquinas en Proxmox. Sin agentes huérfanos, sin estados
            ambiguos.
          </p>
        </div>

        <div className="relative flex items-end justify-between gap-6 border-t border-ink-700 pt-6">
          <div className="font-mono text-[10px] uppercase leading-relaxed tracking-[0.18em] text-slate-600">
            BDR // CONTROL PLANE
            <br />
            {health}
          </div>
          <div className="rail-text font-mono text-[9px] uppercase tracking-[0.35em] text-slate-700">
            PROTECT · RECOVER · VERIFY
          </div>
        </div>
      </section>

      <section className="flex items-center justify-center px-5 py-12 sm:px-10">
        <div className="w-full max-w-md">
          <div className="mb-10 lg:hidden">
            <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal">
              BDR // Control Plane
            </p>
            <h1 className="mt-2 font-display text-5xl uppercase leading-none text-slate-100">
              Consola
            </h1>
          </div>

          <div className="panel cut p-7">
            <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
              01 / Autenticación
            </p>
            <h2 className="mt-2 font-display text-3xl uppercase tracking-wide text-slate-100">
              Acceso administrativo
            </h2>
            <p className="mt-3 text-sm leading-relaxed text-slate-400">
              Introduce el valor de <code className="font-mono text-xs text-ice">BDR_ADMIN_TOKEN</code>{" "}
              del servidor. Se guarda solo en esta pestaña.
            </p>

            <form onSubmit={submit} className="mt-7 space-y-4">
              <label className="block">
                <span className="mb-1.5 block font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                  Token de administración
                </span>
                <input
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="••••••••••••••••"
                  autoComplete="off"
                  spellCheck={false}
                  className={inputClass}
                />
              </label>
              {error ? <ErrorBox message={error} /> : null}
              <Button type="submit" loading={busy} className="w-full">
                Entrar en la consola
              </Button>
            </form>

            <div className="mt-7 border-t border-ink-700 pt-5 font-mono text-[10px] leading-relaxed tracking-wide text-slate-500">
              <p className="text-slate-400">Sesión local, sin cookies.</p>
              <p className="mt-1">
                Para desarrollo: <span className="text-ice">BDR_ADMIN_TOKEN=dev-admin-token</span>
              </p>
            </div>
          </div>

          <p className="mt-6 text-center font-mono text-[9px] uppercase tracking-[0.24em] text-slate-600">
            Acceso restringido · toda acción queda auditada
          </p>
        </div>
      </section>
    </div>
  );
}
