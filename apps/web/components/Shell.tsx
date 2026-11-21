"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";
import { api, publicHealth, type Health, type Summary, type UpdateStatus, type UpdateInstallResponse } from "@/lib/api";
import { Badge, Button, Dot, cx } from "./ui";

const NAV = [
  { href: "/", index: "01", label: "Resumen", hint: "Estado global" },
  { href: "/agents", index: "02", label: "Equipos", hint: "Inventario y acciones" },
  { href: "/plans", index: "03", label: "Planes", hint: "Selección y destinos" },
  { href: "/jobs", index: "04", label: "Trabajos", hint: "Ejecución y errores" },
  { href: "/restore-points", index: "05", label: "Puntos de recuperación", hint: "Snapshots y exploración" },
  { href: "/alerts", index: "06", label: "Alertas", hint: "Incidencias abiertas", alertBadge: true },
  { href: "/proxmox", index: "07", label: "Proxmox", hint: "Instancias e inventario" },
  { href: "/storage", index: "08", label: "Almacenamiento", hint: "S3 y rutas físicas" },
] as const;

function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <Link href="/" className="group flex items-center gap-3">
      <span className="relative flex h-9 w-9 items-center justify-center border border-signal/60 bg-signal/10">
        <span className="absolute h-px w-5 rotate-45 bg-signal" />
        <span className="absolute h-2 w-2 bg-signal" />
      </span>
      <span>
        <span className="block font-display text-xl font-semibold uppercase leading-none tracking-[0.18em] text-slate-100">
          BDR
        </span>
        {!compact ? (
          <span className="mt-0.5 block font-mono text-[8px] uppercase tracking-[0.28em] text-slate-500">
            Control plane
          </span>
        ) : null}
      </span>
    </Link>
  );
}

function useHealth() {
  const [health, setHealth] = useState<Health | null>(null);
  useEffect(() => {
    let alive = true;
    const load = () => {
      publicHealth().then((h) => {
        if (alive) setHealth(h);
      });
    };
    load();
    const id = window.setInterval(load, 15000);
    return () => {
      alive = false;
      window.clearInterval(id);
    };
  }, []);
  return health;
}

// Contador de alertas abiertas para la insignia del menú (refresco 30 s).
function useOpenAlerts() {
  const { token } = useAuth();
  const [count, setCount] = useState<number | null>(null);
  const load = useCallback(async () => {
    try {
      const s = await api<Summary>("/api/v1/admin/summary", token);
      const total = Object.values(s.alerts_open ?? {}).reduce((acc, n) => acc + n, 0);
      setCount(total);
    } catch {
      // sin token válido o sin conexión: se mantiene el último valor
    }
  }, [token]);
  useEffect(() => {
    load();
    const id = window.setInterval(load, 30000);
    return () => window.clearInterval(id);
  }, [load]);
  return count;
}

function AlertCount({ count }: { count: number | null }) {
  if (count === null || count <= 0) return null;
  return (
    <Badge tone={count > 0 ? "danger" : "neutral"} className="shrink-0">
      {count}
    </Badge>
  );
}

// useUpdateBanner sondea el canal de actualizaciones. Al abrir el panel
// fuerza un chequeo inmediato (POST /check) para no depender de la cadencia
// del servidor (1 h); después sondea el estado cada 10 min. Solo notifica:
// la instalación exige confirmación del administrador.
function useUpdateBanner(): {
  status: UpdateStatus | null;
  refresh: () => Promise<void>;
  checking: boolean;
} {
  const { token } = useAuth();
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const load = useCallback(async () => {
    try {
      const s = await api<UpdateStatus>("/api/v1/admin/system/update", token);
      setStatus(s);
    } catch {
      // sin sesión: el banner no aplica
    }
  }, [token]);
  const refresh = useCallback(async () => {
    setChecking(true);
    try {
      const s = await api<UpdateStatus>("/api/v1/admin/system/update/check", token, {
        method: "POST",
        body: "{}",
      });
      setStatus(s);
    } catch {
      // canal no disponible: se mantiene el último estado conocido
    }
    setChecking(false);
  }, [token]);
  useEffect(() => {
    refresh();
    const id = window.setInterval(load, 600000);
    return () => window.clearInterval(id);
  }, [refresh, load]);
  return { status, refresh, checking };
}

// UpdateBanner es la notificación de versión disponible. «Actualizar ahora»
// descarga el bundle del release, verifica el SHA-256, sustituye el binario
// y reinicia el servidor (Unix). En Windows el servidor devuelve la
// instrucción de ejecutar el instalador.
function UpdateBanner({ update }: { update: UpdateStatus }) {
  const { token } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function installNow() {
    setBusy(true);
    setError(null);
    try {
      const out = await api<UpdateInstallResponse>(
        "/api/v1/admin/system/update/install",
        token,
        { method: "POST", body: "{}" },
      );
      if (out.restarting) {
        window.setTimeout(() => window.location.reload(), 12000);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo iniciar la actualización.");
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-x-0 top-0 z-20 flex flex-wrap items-center gap-3 border-b border-signal/40 bg-signal/10 px-4 py-2 text-xs text-slate-200">
      <span className="font-mono uppercase tracking-widest2 text-signal">
        Actualización {update.latest} disponible
      </span>
      <span className="text-slate-400">
        versión en ejecución {update.current} · se notifica; nada se instala sin tu confirmación
      </span>
      {update.notes_url ? (
        <a href={update.notes_url} target="_blank" rel="noreferrer" className="underline decoration-dotted underline-offset-2 hover:text-signal">
          Notas de versión
        </a>
      ) : null}
      <span className="ml-auto flex items-center gap-3">
        {error ? <span className="text-danger">{error}</span> : null}
        <Button size="sm" variant="primary" loading={busy} onClick={installNow}>
          Actualizar ahora
        </Button>
      </span>
    </div>
  );
}

export function Shell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { signOut } = useAuth();
  const health = useHealth();
  const openAlerts = useOpenAlerts();
  const { status: update, refresh: checkUpdates, checking: updateChecking } = useUpdateBanner();
  const updateNote = update?.last_error
    ? `canal: ${update.last_error}`
    : update?.update_available
      ? `disponible ${update.latest}`
      : update?.checked_at
        ? `al día · ${new Date(update.checked_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
        : "canal sin comprobar";

  return (
    <div className="relative z-10 flex min-h-screen">
      {update?.update_available ? <UpdateBanner update={update} /> : null}      <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-ink-700 bg-ink-950/80 px-4 py-6 lg:flex">
        <Brand />

        <nav className="mt-12 flex flex-col gap-1">
          {NAV.map((item) => {
            const active =
              item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cx(
                  "group flex items-center gap-3 border-l-2 px-3 py-3 transition",
                  active
                    ? "border-signal bg-signal/5 text-signal"
                    : "border-transparent text-slate-400 hover:border-ink-600 hover:bg-white/[0.03] hover:text-slate-200",
                )}
              >
                <span className="font-mono text-[9px] tracking-widest2 opacity-60">
                  {item.index}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block font-display text-lg uppercase leading-none tracking-[0.14em]">
                    {item.label}
                  </span>
                  <span className="mt-1 block text-[10px] text-slate-500 group-hover:text-slate-400">
                    {item.hint}
                  </span>
                </span>
                {"alertBadge" in item && item.alertBadge ? <AlertCount count={openAlerts} /> : null}
              </Link>
            );
          })}
        </nav>

        <div className="mt-auto space-y-4 border-t border-ink-700 pt-5">
          <div className="flex items-center justify-between font-mono text-[9px] uppercase tracking-[0.18em]">
            <span className="flex items-center gap-2 text-slate-500">
              <Dot tone={health ? "signal" : "danger"} live={Boolean(health)} />
              {health ? `API ${health.version}` : "API offline"}
            </span>
            <span className="text-slate-600">v0.4</span>
          </div>
          <div className="flex items-center justify-between gap-2">
            <span className="min-w-0 truncate font-mono text-[9px] uppercase tracking-widest2 text-slate-600">
              {updateNote}
            </span>
            <Button variant="ghost" size="sm" loading={updateChecking} onClick={() => checkUpdates()}>
              Comprobar
            </Button>
          </div>
          <div className="flex items-center justify-between">
            <span className="font-mono text-[9px] uppercase tracking-widest2 text-slate-600">
              Sesión local
            </span>
            <Button variant="ghost" size="sm" onClick={signOut}>
              Salir
            </Button>
          </div>
          <p className="rail-text hidden h-24 items-end font-mono text-[8px] uppercase tracking-[0.4em] text-slate-700 xl:flex">
            PROTECT · RECOVER · VERIFY
          </p>
        </div>
      </aside>

      <div className="min-w-0 flex-1">
        <header className="sticky top-0 z-20 border-b border-ink-700 bg-ink-950/85 backdrop-blur lg:hidden">
          <div className="flex items-center justify-between px-4 py-3">
            <Brand compact />
            <div className="flex items-center gap-3">
              <span className="flex items-center gap-1.5 font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
                <Dot tone={health ? "signal" : "danger"} live={Boolean(health)} />
                {health ? "online" : "offline"}
              </span>
              <Button variant="ghost" size="sm" onClick={signOut}>
                Salir
              </Button>
            </div>
          </div>
          <nav className="scroll-thin flex gap-1 overflow-x-auto px-3 pb-2">
            {NAV.map((item) => {
              const active =
                item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cx(
                    "whitespace-nowrap border px-3 py-1.5 font-display text-sm uppercase tracking-[0.14em]",
                    active
                      ? "border-signal/40 bg-signal/10 text-signal"
                      : "border-ink-700 text-slate-400",
                  )}
                >
                  {item.label}
                  {"alertBadge" in item && item.alertBadge && openAlerts ? (
                    <span className="ml-2 font-mono text-[9px] text-danger">({openAlerts})</span>
                  ) : null}
                </Link>
              );
            })}
          </nav>
        </header>

        <main className="mx-auto w-full max-w-7xl px-4 py-7 sm:px-8 sm:py-10">{children}</main>

        <footer className="mx-auto flex w-full max-w-7xl items-center justify-between border-t border-ink-800 px-4 py-5 font-mono text-[9px] uppercase tracking-[0.2em] text-slate-600 sm:px-8">
          <span>BDR Console · uso propio</span>
          <span className="hidden sm:inline">Los datos no salen de tu infraestructura</span>
        </footer>
      </div>
    </div>
  );
}
