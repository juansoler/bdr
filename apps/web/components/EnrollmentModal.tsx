"use client";

import { useEffect, useState } from "react";
import { api, type EnrollmentToken } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { Button, ErrorBox, Field, Input, cx } from "./ui";

type Tab = "windows" | "linux" | "macos";
type Mode = "installer" | "manual";
type Arch = "amd64" | "arm64";

const tabLabel: Record<Tab, string> = {
  windows: "Windows",
  linux: "Linux",
  macos: "macOS",
};
const archLabel: Record<Arch, string> = {
  amd64: "amd64 (Intel)",
  arm64: "arm64 (Apple Silicon)",
};

interface InstallerOut {
  os: string;
  arch: string;
  token: string;
  expires_at: string;
  installer_path: string;
  one_liner: string;
}

export function EnrollmentModal({
  token,
  onClose,
}: {
  token: string;
  onClose: () => void;
}) {
  const [mode, setMode] = useState<Mode>("installer");
  const [expectedHostname, setExpectedHostname] = useState("");
  const [ttl, setTtl] = useState(60);
  const [osSel, setOsSel] = useState<Tab>("windows");
  const [arch, setArch] = useState<Arch>("amd64");
  const [serverURL, setServerURL] = useState(() => {
    if (typeof window === "undefined") return "http://127.0.0.1:8080";
    return `${window.location.protocol}//${window.location.hostname}:8080`;
  });
  const [created, setCreated] = useState<EnrollmentToken | null>(null);
  const [installer, setInstaller] = useState<InstallerOut | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("windows");
  const [copied, setCopied] = useState<string | null>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  async function createInstaller() {
    setBusy(true);
    setError(null);
    try {
      const out = await api<InstallerOut>("/api/v1/admin/installers", token, {
        method: "POST",
        body: JSON.stringify({
          os: osSel === "macos" ? "darwin" : osSel,
          arch: osSel === "windows" ? "amd64" : arch,
          expires_in_seconds: Math.max(1, ttl) * 60,
          expected_hostname: expectedHostname.trim() || undefined,
          note: "installer",
        }),
      });
      setInstaller(out);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo generar el instalador.");
    } finally {
      setBusy(false);
    }
  }

  async function create() {
    setBusy(true);
    setError(null);
    try {
      const out = await api<EnrollmentToken>("/api/v1/admin/enrollment-tokens", token, {
        method: "POST",
        body: JSON.stringify({
          expires_in_seconds: Math.max(1, ttl) * 60,
          expected_hostname: expectedHostname.trim() || undefined,
        }),
      });
      setCreated(out);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo crear el token.");
    } finally {
      setBusy(false);
    }
  }

  async function copy(label: string, value: string) {
    try {
      // navigator.clipboard solo existe en contextos seguros (https/localhost);
      // el panel suele servirse por http en LAN, así que hay fallback.
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

  const winCommand = created
    ? `bdr-agent.exe enroll --server ${serverURL} --token ${created.token} --data-dir C:\\ProgramData\\BDR\\agent`
    : "";
  const linuxCommand = created
    ? `sudo bdr-agent enroll --server ${serverURL} --token ${created.token} --data-dir /var/lib/bdr`
    : "";
  const macosCommand = created
    ? `sudo bdr-agent enroll --server ${serverURL} --token ${created.token} --data-dir "/Library/Application Support/BDR"`
    : "";
  const command =
    tab === "windows" ? winCommand : tab === "macos" ? macosCommand : linuxCommand;

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
        aria-labelledby="enroll-title"
        className="panel cut scroll-thin max-h-[90vh] w-full max-w-2xl overflow-y-auto p-6 sm:p-8"
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
              Alta de equipo
            </p>
            <h2
              id="enroll-title"
              className="mt-1 font-display text-3xl uppercase tracking-wide text-slate-100"
            >
              Instalar agente
            </h2>
          </div>
          <Button variant="ghost" size="sm" onClick={onClose} aria-label="Cerrar">
            Cerrar
          </Button>
        </div>

        <div className="mt-5 flex gap-2">
          <button
            onClick={() => setMode("installer")}
            className={cx(
              "border px-3 py-1.5 font-mono text-[10px] uppercase tracking-widest2 transition",
              mode === "installer"
                ? "border-signal text-signal"
                : "border-ink-600 text-slate-400 hover:text-slate-200",
            )}
          >
            Instalador automático
          </button>
          <button
            onClick={() => setMode("manual")}
            className={cx(
              "border px-3 py-1.5 font-mono text-[10px] uppercase tracking-widest2 transition",
              mode === "manual"
                ? "border-signal text-signal"
                : "border-ink-600 text-slate-400 hover:text-slate-200",
            )}
          >
            Token manual
          </button>
        </div>

        {!created && !installer ? (
          <div className="mt-7 space-y-5">
            <p className="text-sm leading-relaxed text-slate-400">
              {mode === "installer"
                ? "Genera un instalador con todo embebido: descarga el binario, auto-enrola el equipo y deja el agente conectado. Sin token que copiar ni pasos manuales."
                : "Genera un token de un solo uso. Se mostrará una única vez y caducará aunque no se use."}
            </p>

            {mode === "installer" ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Sistema operativo">
                  <select
                    value={osSel}
                    onChange={(e) => setOsSel(e.target.value as Tab)}
                    className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
                  >
                    <option value="windows">Windows</option>
                    <option value="linux">Linux</option>
                    <option value="macos">macOS</option>
                  </select>
                </Field>
                <Field
                  label="Arquitectura"
                  hint={
                    osSel === "windows"
                      ? "Windows: amd64"
                      : osSel === "macos"
                        ? "macOS: Intel (amd64) o Apple Silicon (arm64)"
                        : "Linux: amd64 o arm64"
                  }
                >
                  <select
                    value={osSel === "windows" ? "amd64" : arch}
                    onChange={(e) => setArch(e.target.value as Arch)}
                    disabled={osSel === "windows"}
                    className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
                  >
                    <option value="amd64">{archLabel.amd64}</option>
                    {osSel === "linux" || osSel === "macos" ? (
                      <option value="arm64">{archLabel.arm64}</option>
                    ) : null}
                  </select>
                </Field>
              </div>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Hostname esperado" hint="Opcional; vincula el token a un equipo.">
                <Input
                  value={expectedHostname}
                  onChange={(e) => setExpectedHostname(e.target.value)}
                  placeholder="srv-datos-01"
                />
              </Field>
              <Field label="Validez (minutos)" hint="Entre 1 minuto y 24 horas.">
                <Input
                  type="number"
                  min={1}
                  max={1440}
                  value={ttl}
                  onChange={(e) => setTtl(Number(e.target.value) || 60)}
                />
              </Field>
            </div>

            {mode === "manual" ? (
              <Field
                label="URL del servidor para el agente"
                hint="El agente debe poder alcanzar este puerto."
              >
                <Input value={serverURL} onChange={(e) => setServerURL(e.target.value)} />
              </Field>
            ) : null}

            {error ? <ErrorBox message={error} /> : null}

            <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
              <Button variant="quiet" onClick={onClose}>
                Cancelar
              </Button>
              <Button
                onClick={mode === "installer" ? createInstaller : create}
                loading={busy}
              >
                {mode === "installer" ? "Generar instalador" : "Generar token"}
              </Button>
            </div>
          </div>
        ) : installer ? (
          <div className="mt-7 space-y-6">
            <div className="border border-signal/30 bg-signal/5 p-4">
              <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal">
                Instalador generado — {installer.os} · {installer.arch}
              </p>
              <p className="mt-2 text-sm text-slate-300">
                Ejecuta este comando en el equipo destino{tab === "windows" ? " (PowerShell como Administrador)" : " (con sudo)"}.
                El agente se descarga, se enrola y queda conectado automáticamente.
                Caduca el {formatDateTime(installer.expires_at)}.
              </p>
              <div className="mt-3 flex items-start gap-2">
                <code className="scroll-thin flex-1 overflow-x-auto border border-ink-600 bg-ink-950 px-3 py-2 font-mono text-xs text-signal">
                  {installer.one_liner}
                </code>
                <Button size="sm" variant="ghost" onClick={() => copy("oneliner", installer.one_liner)}>
                  {copied === "oneliner" ? "Copiado" : "Copiar"}
                </Button>
              </div>
              <div className="mt-3 flex flex-wrap items-center gap-2">
                <a href={installer.installer_path} download target="_blank" rel="noreferrer">
                  <Button size="sm" variant="ghost">
                    Descargar instalador
                  </Button>
                </a>
                <span className="font-mono text-[10px] text-slate-500">
                  El instalador es de un solo uso: cada «Añadir equipo» genera uno nuevo.
                </span>
              </div>
            </div>
            <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
              <Button variant="quiet" onClick={onClose}>
                Cerrar
              </Button>
            </div>
          </div>
        ) : created ? (
          <div className="mt-7 space-y-6">
            <div className="border border-alert/30 bg-alert/5 p-4">
              <p className="font-mono text-[10px] uppercase tracking-widest2 text-alert">
                Copia el token ahora
              </p>
              <p className="mt-2 text-sm text-slate-300">
                No se volverá a mostrar. Caduca el {formatDateTime(created.expires_at)} y
                solo sirve para un equipo.
              </p>
              <div className="mt-3 flex items-start gap-2">
                <code className="scroll-thin flex-1 overflow-x-auto border border-ink-600 bg-ink-950 px-3 py-2 font-mono text-xs text-signal">
                  {created.token}
                </code>
                <Button variant="secondary" size="sm" onClick={() => copy("token", created.token)}>
                  {copied === "token" ? "Copiado" : "Copiar"}
                </Button>
              </div>
            </div>

            <div>
              <div className="flex gap-1 border-b border-ink-700">
                {(["windows", "linux", "macos"] as const).map((item) => (
                  <button
                    key={item}
                    onClick={() => setTab(item)}
                    className={cx(
                      "border-b-2 px-4 py-2 font-display text-sm uppercase tracking-[0.16em] transition",
                      tab === item
                        ? "border-signal text-signal"
                        : "border-transparent text-slate-500 hover:text-slate-300",
                    )}
                  >
                    {tabLabel[item]}
                  </button>
                ))}
              </div>
              <div className="mt-4">
                <p className="mb-2 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                  Comando de enrolamiento
                </p>
                <pre className="scroll-thin overflow-x-auto border border-ink-600 bg-ink-950 p-4 font-mono text-[11px] leading-relaxed text-slate-300">
                  {command}
                </pre>
                <div className="mt-2 flex justify-end">
                  <Button variant="secondary" size="sm" onClick={() => copy("command", command)}>
                    {copied === "command" ? "Copiado" : "Copiar comando"}
                  </Button>
                </div>
              </div>
            </div>

            <div className="border-t border-ink-700 pt-5 font-mono text-[10px] leading-relaxed tracking-wide text-slate-500">
              <p className="text-slate-400">Recomendación de seguridad</p>
              <p className="mt-1">
                Usa el paquete firmado y verifica su hash. Evita `curl | bash` en equipos
                de producción. El token es una credencial: no lo pegues en tickets ni
                chats.
              </p>
            </div>

            <div className="flex justify-end">
              <Button
                variant="secondary"
                onClick={() => {
                  setCreated(null);
                  setExpectedHostname("");
                }}
              >
                Generar otro
              </Button>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
