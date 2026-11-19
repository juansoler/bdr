"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { JobDetailModal } from "@/components/JobDetailModal";
import { JobTable } from "@/components/JobTable";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorBox,
  PageHeader,
  Stat,
  repoStatusLabel,
  repoStatusTone,
  severityLabel,
  severityTone,
} from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { api, type AgentList, type Job, type JobList, type Summary } from "@/lib/api";
import { formatBytes, formatDateTime, timeAgo } from "@/lib/format";

const SEVERITIES = ["critical", "high", "medium", "low"] as const;

export default function DashboardPage() {
  const { token } = useAuth();
  const [summary, setSummary] = useState<Summary | null>(null);
  const [jobs, setJobs] = useState<JobList["items"]>([]);
  const [agents, setAgents] = useState<AgentList["items"]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [lastLoaded, setLastLoaded] = useState<string | null>(null);
  const [detailJob, setDetailJob] = useState<Job | null>(null);

  const load = useCallback(async () => {
    try {
      const [s, j, a] = await Promise.all([
        api<Summary>("/api/v1/admin/summary", token),
        api<JobList>("/api/v1/admin/jobs?limit=8", token),
        api<AgentList>("/api/v1/admin/agents", token),
      ]);
      setSummary(s);
      setJobs(j.items ?? []);
      setAgents(a.items ?? []);
      setLastLoaded(new Date().toISOString());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo cargar el resumen.");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    load();
    const id = window.setInterval(load, 5000);
    return () => window.clearInterval(id);
  }, [load]);

  const protectedCount = summary?.agents_protected ?? 0;
  const total = summary?.agents_total ?? 0;
  const ratio = summary?.success_24h;
  const successPct = ratio && ratio.percent >= 0 ? Math.round(ratio.percent) : -1;
  const upcoming = summary?.upcoming ?? [];
  const alertsOpen = summary?.alerts_open ?? {};
  const alertsTotal = Object.values(alertsOpen).reduce((acc, n) => acc + n, 0);

  return (
    <div className="space-y-8">
      <PageHeader
        kicker="01 / Panorama"
        title="Resumen de protección"
        description="Equipos protegidos, eficacia de las copias, actividad y estado del repositorio. Los datos se refrescan cada cinco segundos."
        actions={
          <Button variant="secondary" onClick={load} loading={loading}>
            Actualizar
          </Button>
        }
      />

      {error ? <ErrorBox message={error} onRetry={load} /> : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <div className="rise" style={{ animationDelay: "0ms" }}>
          <Stat
            index="A1"
            label="Equipos protegidos"
            value={`${protectedCount} de ${total}`}
            hint={`${summary?.agents_online ?? 0} en línea · ${summary?.agents_revoked ?? 0} revocados`}
            tone={total > 0 && protectedCount >= total ? "signal" : total > 0 ? "alert" : "neutral"}
          />
        </div>
        <div className="rise" style={{ animationDelay: "60ms" }}>
          <Stat
            index="A2"
            label="Éxito 24 h"
            value={successPct >= 0 ? `${successPct}%` : "—"}
            hint={`${ratio?.succeeded ?? 0} correctos · ${ratio?.failed ?? 0} fallidos`}
            tone={
              successPct < 0
                ? "neutral"
                : successPct >= 90
                  ? "signal"
                  : successPct >= 70
                    ? "alert"
                    : "danger"
            }
          />
        </div>
        <div className="rise" style={{ animationDelay: "120ms" }}>
          <Stat
            index="A3"
            label="Trabajos activos"
            value={summary?.jobs_active ?? 0}
            hint="en cola o en ejecución"
            tone="ice"
          />
        </div>
        <div className="rise" style={{ animationDelay: "180ms" }}>
          <Stat
            index="A4"
            label="Puntos de recuperación"
            value={summary?.restore_points ?? 0}
            hint={`${formatBytes(summary?.logical_bytes)} lógicos en el repositorio`}
            tone="signal"
          />
        </div>
      </div>

      {total === 0 && !loading ? (
        <EmptyState
          index="00 / ONBOARDING"
          title="Todavía no hay equipos"
          description="Genera un token de un solo uso e instala el agente en el primer equipo. Cuando envíe su primer heartbeat aparecerá aquí."
          action={
            <Link href="/agents">
              <Button>Instalar primer agente</Button>
            </Link>
          }
        />
      ) : null}

      <div className="grid gap-6 xl:grid-cols-[1.45fr_0.55fr]">
        <Card className="overflow-hidden">
          <div className="flex items-center justify-between border-b border-ink-700 px-5 py-4">
            <div>
              <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
                Actividad
              </p>
              <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
                Últimos trabajos
              </h2>
            </div>
            <Link
              href="/jobs"
              className="font-mono text-[10px] uppercase tracking-widest2 text-slate-500 transition hover:text-signal"
            >
              Ver todos →
            </Link>
          </div>
          <div className="p-2 sm:p-3">
            <JobTable jobs={jobs.slice(0, 6)} agents={agents} compact onView={setDetailJob} />
          </div>
        </Card>

        <div className="space-y-6">
          <Card className="p-5">
            <div className="flex items-center justify-between gap-2">
              <div>
                <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
                  Programación
                </p>
                <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
                  Próximos backups
                </h2>
              </div>
              <Link
                href="/plans"
                className="font-mono text-[10px] uppercase tracking-widest2 text-slate-500 transition hover:text-signal"
              >
                Planes →
              </Link>
            </div>
            <div className="mt-4 space-y-2">
              {upcoming.slice(0, 6).map((run) => (
                <div
                  key={`${run.at}-${run.plan_id}-${run.agent_id}`}
                  className="flex items-center justify-between gap-3 border border-ink-700 px-3 py-2"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm text-slate-200" title={run.plan_name}>
                      {run.plan_name}
                    </p>
                    <p className="font-mono text-[10px] text-slate-500">{run.hostname}</p>
                  </div>
                  <span className="whitespace-nowrap font-mono text-[10px] text-ice">
                    {formatDateTime(run.at)}
                  </span>
                </div>
              ))}
              {upcoming.length === 0 ? (
                <p className="border border-ink-700 px-3 py-4 text-center font-mono text-[10px] uppercase tracking-widest2 text-slate-600">
                  Sin ejecuciones programadas
                </p>
              ) : null}
            </div>
          </Card>

          <Card className="p-5">
            <div className="flex items-center justify-between gap-2">
              <div>
                <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
                  Incidencias
                </p>
                <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
                  Alertas abiertas
                </h2>
              </div>
              <Link
                href="/alerts"
                className="font-mono text-[10px] uppercase tracking-widest2 text-slate-500 transition hover:text-signal"
              >
                Ver alertas →
              </Link>
            </div>
            {alertsTotal === 0 ? (
              <p className="mt-4 border border-signal/20 bg-signal/5 px-3 py-4 text-center font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
                Sin alertas abiertas
              </p>
            ) : (
              <div className="mt-4 space-y-2">
                {SEVERITIES.map((sev) => {
                  const n = alertsOpen[sev] ?? 0;
                  if (n === 0) return null;
                  return (
                    <Link
                      key={sev}
                      href={`/alerts?severidad=${sev}`}
                      className="flex items-center justify-between gap-3 border border-ink-700 px-3 py-2 transition hover:border-signal/40"
                    >
                      <Badge tone={severityTone(sev)}>{severityLabel(sev)}</Badge>
                      <span className="font-mono text-sm text-slate-200">{n}</span>
                    </Link>
                  );
                })}
              </div>
            )}
          </Card>

          <Card className="p-5">
            <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
              Repositorio
            </p>
            <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
              Estado del destino
            </h2>
            <div className="mt-4 flex items-center justify-between gap-3 border border-ink-700 px-3 py-3">
              <span className="font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                Kopia
              </span>
              <Badge tone={repoStatusTone(summary?.repository_status ?? "unknown")}>
                {repoStatusLabel(summary?.repository_status ?? "unknown")}
              </Badge>
            </div>
            <p className="mt-3 font-mono text-[10px] text-slate-600">
              Scheduler: {summary?.last_scheduler_tick ? timeAgo(summary.last_scheduler_tick) : "sin señal"}
            </p>
          </Card>

          <Card className="p-5">
            <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
              Accesos
            </p>
            <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
              Operaciones
            </h2>
            <div className="mt-4 grid gap-2">
              {[
                { href: "/agents", label: "Instalar agente" },
                { href: "/plans", label: "Crear plan" },
                { href: "/restore-points", label: "Explorar copias" },
                { href: "/jobs", label: "Revisar fallos" },
              ].map((item) => (
                <Link
                  key={item.href}
                  href={item.href}
                  className="flex items-center justify-between border border-ink-700 px-3 py-3 transition hover:border-signal/40 hover:bg-signal/5"
                >
                  <span className="font-display text-base uppercase tracking-[0.14em] text-slate-200">
                    {item.label}
                  </span>
                  <span className="font-mono text-[10px] text-slate-500">→</span>
                </Link>
              ))}
            </div>
          </Card>
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-ink-800 pt-4 font-mono text-[9px] uppercase tracking-[0.18em] text-slate-600">
        <span>Última lectura: {formatDateTime(lastLoaded ?? undefined)}</span>
        <span>Refresco automático · 5 s</span>
      </div>

      {detailJob ? (
        <JobDetailModal token={token} jobId={detailJob.id} onClose={() => setDetailJob(null)} />
      ) : null}
    </div>
  );
}
