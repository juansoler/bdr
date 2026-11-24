"use client";

import { useEffect, useState } from "react";
import { api, type Agent, type ReconnectToken } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { Button, ErrorBox } from "./ui";

// ReconnectModal genera un token de reconexión para un agente existente y
// muestra el one-liner que lo reenlaza SIN crear un equipo nuevo: conserva
// su identidad, grupos, planes e historial. El token dura 15 minutos y es
// de un solo uso.
export function ReconnectModal({
  token,
  agent,
  onClose,
  onReconnected,
}: {
  token: string;
  agent: Agent;
  onClose: () => void;
  onReconnected: () => void;
}) {
  const [created, setCreated] = useState<ReconnectToken | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    let alive = true;
    api<ReconnectToken>(`/api/v1/admin/agents/${agent.id}/reconnect-token`, token, {
      method: "POST",
      body: "{}",
    })
      .then((out) => {
        if (alive) setCreated(out);
      })
      .catch((err) => {
        if (alive) setError(err instanceof Error ? err.message : "No se pudo generar el token de reconexión.");
      });
    return () => {
      alive = false;
    };
  }, [agent.id, token]);

  async function copy(label: string, value: string) {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(value);
      } else {
        const ta = document.createElement("textarea");
        ta.value = value;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        const ok = document.execCommand("copy");
        document.body.removeChild(ta);
        if (!ok) throw new Error("execCommand falló");
      }
      setCopied(label);
      window.setTimeout(() => setCopied(null), 1600);
    } catch {
      setError("El navegador no permitió copiar. Selecciona el texto manualmente.");
    }
  }

  // Comando manual de respaldo si el SO/arquitectura no tienen instalador.
  const manualCommand = created
    ? agent.os_family === "windows"
      ? `bdr-agent.exe enroll --server ${window.location.protocol}//${window.location.hostname}:8080 --token ${created.token} --data-dir C:\\ProgramData\\BDR`
      : `sudo bdr-agent enroll --server ${window.location.protocol}//${window.location.hostname}:8080 --token ${created.token} --data-dir /var/lib/bdr-agent`
    : "";

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/80 px-4 py-8 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="reconnect-title"
        className="panel cut scroll-thin max-h-[90vh] w-full max-w-2xl overflow-y-auto p-6 sm:p-8"
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
              Equipo offline
            </p>
            <h2
              id="reconnect-title"
              className="mt-1 font-display text-3xl uppercase tracking-wide text-slate-100"
            >
              Reconectar agente
            </h2>
          </div>
          <Button variant="ghost" size="sm" onClick={onClose} aria-label="Cerrar">
            Cerrar
          </Button>
        </div>

        <div className="mt-5 space-y-5">
          <p className="text-sm leading-relaxed text-slate-400">
            Este equipo conserva su identidad: grupos, planes e historial no se tocan; solo se
            rotan sus credenciales. Ejecuta el comando en el equipo{agent.os_family === "windows" ? " (PowerShell como Administrador)" : " (con sudo)"} y volverá a aparecer conectado con el mismo nombre.
          </p>

          {error ? <ErrorBox message={error} /> : null}

          {!created && !error ? (
            <p className="border border-ink-700 px-3 py-4 text-center font-mono text-[10px] uppercase tracking-widest2 text-slate-600">
              Generando token…
            </p>
          ) : null}

          {created ? (
            <div className="border border-signal/30 bg-signal/5 p-4">
              <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal">
                Token de reconexión — {agent.hostname}
              </p>
              <p className="mt-2 text-sm text-slate-300">
                De un solo uso; caduca el {formatDateTime(created.expires_at)}.
              </p>
              {created.one_liner ? (
                <div className="mt-3 flex items-start gap-2">
                  <code className="scroll-thin flex-1 overflow-x-auto border border-ink-600 bg-ink-950 px-3 py-2 font-mono text-xs text-signal">
                    {created.one_liner}
                  </code>
                  <Button size="sm" variant="ghost" onClick={() => copy("oneliner", created.one_liner ?? "")}>
                    {copied === "oneliner" ? "Copiado" : "Copiar"}
                  </Button>
                </div>
              ) : (
                <div className="mt-3">
                  <p className="mb-2 font-mono text-[10px] uppercase tracking-widest2 text-alert">
                    Sin instalador para {created.os}/{created.arch} — usa el token a mano:
                  </p>
                  <pre className="scroll-thin overflow-x-auto border border-ink-600 bg-ink-950 p-3 font-mono text-[11px] text-slate-300">
                    {manualCommand}
                  </pre>
                  <div className="mt-2 flex items-start gap-2">
                    <code className="scroll-thin flex-1 overflow-x-auto border border-ink-600 bg-ink-950 px-3 py-2 font-mono text-xs text-signal">
                      {created.token}
                    </code>
                    <Button size="sm" variant="ghost" onClick={() => copy("token", created.token)}>
                      {copied === "token" ? "Copiado" : "Copiar"}
                    </Button>
                  </div>
                </div>
              )}
              <p className="mt-3 font-mono text-[10px] text-slate-500">
                Si el agente sigue instalado pero desconectado (p. ej. DNS), este comando lo
                recupera. Si el equipo se reinstaló desde cero, también sirve: no duplica el equipo.
              </p>
            </div>
          ) : null}

          <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
            <Button variant="quiet" onClick={onClose}>
              Cerrar
            </Button>
            {created ? (
              <Button
                variant="secondary"
                onClick={() => {
                  onReconnected();
                  onClose();
                }}
              >
                Hecho, volver a la ficha
              </Button>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}
