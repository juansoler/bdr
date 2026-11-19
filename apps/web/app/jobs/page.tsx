"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { JobDetailModal } from "@/components/JobDetailModal";
import { JobTable } from "@/components/JobTable";
import { Badge, Button, Card, EmptyState, ErrorBox, PageHeader, cx } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { ApiError, api, type AgentList, type Job, type JobList } from "@/lib/api";
import { shortId } from "@/lib/format";

type Filter = "all" | "active" | "succeeded" | "failed";

const FILTERS: Array<{ id: Filter; label: string }> = [
  { id: "all", label: "Todos" },
  { id: "active", label: "En curso" },
  { id: "succeeded", label: "Correctos" },
  { id: "failed", label: "Fallidos" },
];

const PAGE_SIZE = 25;

export default function JobsPage() {
  const { token } = useAuth();
  const [jobs, setJobs] = useState<Job[]>([]);
  const [agents, setAgents] = useState<AgentList["items"]>([]);
  const [filter, setFilter] = useState<Filter>("all");
  const [limit, setLimit] = useState(PAGE_SIZE);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [detailJob, setDetailJob] = useState<string | null>(null);

  const load = useCallback(
    async (currentLimit: number) => {
      try {
        const [j, a] = await Promise.all([
          api<JobList>(`/api/v1/admin/jobs?limit=${currentLimit}`, token),
          api<AgentList>("/api/v1/admin/agents", token),
        ]);
        setJobs(j.items ?? []);
        setAgents(a.items ?? []);
        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : "No se pudieron cargar los trabajos.");
      } finally {
        setLoading(false);
      }
    },
    [token],
  );

  useEffect(() => {
    load(limit);
    const id = window.setInterval(() => load(limit), 5000);
    return () => window.clearInterval(id);
  }, [load, limit]);

  function loadMore() {
    setLimit((prev) => prev + PAGE_SIZE);
  }

  async function cancel(job: Job) {
    setBusyId(job.id);
    setError(null);
    setNotice(null);
    try {
      await api(`/api/v1/admin/jobs/${job.id}/cancel`, token, { method: "POST" });
      setNotice(`Cancelación solicitada para ${shortId(job.id)}.`);
      await load(limit);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError(`No se pudo cancelar ${shortId(job.id)}: el trabajo ya terminó.`);
      } else {
        setError(err instanceof Error ? err.message : "No se pudo cancelar el trabajo.");
      }
    } finally {
      setBusyId(null);
    }
  }

  async function retry(job: Job) {
    setBusyId(job.id);
    setError(null);
    setNotice(null);
    try {
      await api(`/api/v1/admin/jobs/${job.id}/retry`, token, { method: "POST" });
      setNotice(`Reintento en cola (nuevo job a partir de ${shortId(job.id)}).`);
      await load(limit);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError(
          `No se puede reintentar ${shortId(job.id)}: ${
            err.message || "el trabajo no es reintentable ahora mismo."
          }`,
        );
      } else {
        setError(err instanceof Error ? err.message : "No se pudo reintentar el trabajo.");
      }
    } finally {
      setBusyId(null);
    }
  }

  const visible = useMemo(() => {
    if (filter === "all") return jobs;
    if (filter === "active")
      return jobs.filter((j) => j.state === "running" || j.state === "leased");
    return jobs.filter((j) => j.state === filter);
  }, [jobs, filter]);

  const counts = {
    active: jobs.filter((j) => j.state === "running" || j.state === "leased").length,
    succeeded: jobs.filter((j) => j.state === "succeeded").length,
    failed: jobs.filter((j) => j.state === "failed").length,
  };

  return (
    <div className="space-y-6">
      <PageHeader
        kicker="04 / Operaciones"
        title="Trabajos"
        description="Cola de ejecución, progreso y errores. Los jobs se asignan por lease al agente; si el lease caduca, vuelven a la cola."
        actions={
          <Button variant="secondary" onClick={() => load(limit)} loading={loading}>
            Actualizar
          </Button>
        }
      />

      {error ? <ErrorBox message={error} onRetry={() => load(limit)} /> : null}
      {notice ? (
        <div className="border border-signal/30 bg-signal/5 px-4 py-3 font-mono text-xs text-signal">
          {notice}
        </div>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-3">
        <Card cut className="p-4">
          <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-500">En curso</p>
          <p className="mt-2 font-display text-3xl text-ice">{counts.active}</p>
        </Card>
        <Card cut className="p-4">
          <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-500">Correctos</p>
          <p className="mt-2 font-display text-3xl text-signal">{counts.succeeded}</p>
        </Card>
        <Card cut className="p-4">
          <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-500">Fallidos</p>
          <p className="mt-2 font-display text-3xl text-danger">{counts.failed}</p>
        </Card>
      </div>

      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-ink-700 px-4 py-3">
          <div className="flex flex-wrap gap-1">
            {FILTERS.map((item) => (
              <button
                key={item.id}
                onClick={() => setFilter(item.id)}
                className={cx(
                  "border px-3 py-1.5 font-mono text-[10px] uppercase tracking-[0.14em] transition",
                  filter === item.id
                    ? "border-signal/40 bg-signal/10 text-signal"
                    : "border-transparent text-slate-500 hover:text-slate-200",
                )}
              >
                {item.label}
              </button>
            ))}
          </div>
          <span className="font-mono text-[9px] uppercase tracking-widest2 text-slate-600">
            {visible.length} registros · refresco 5 s
          </span>
        </div>
        <div className="p-2 sm:p-3">
          {visible.length === 0 ? (
            <EmptyState
              index="00 / QUEUE"
              title={jobs.length === 0 ? "Cola vacía" : "Sin coincidencias"}
              description={
                jobs.length === 0
                  ? "Lanza un backup desde Equipos o espera a la programación. Los jobs aparecerán aquí con su progreso."
                  : "Cambia el filtro para ver otros estados."
              }
            />
          ) : (
            <JobTable
              jobs={visible}
              agents={agents}
              onView={(job) => setDetailJob(job.id)}
              onCancel={cancel}
              onRetry={retry}
              busyId={busyId}
            />
          )}
        </div>
        {jobs.length >= limit ? (
          <div className="flex items-center justify-center border-t border-ink-700 px-4 py-3">
            <Button variant="quiet" size="sm" onClick={loadMore}>
              Cargar más
            </Button>
          </div>
        ) : null}
      </Card>

      <div className="flex flex-wrap items-center gap-2 border-t border-ink-800 pt-4">
        <Badge tone="ice">lease 10 min</Badge>
        <Badge tone="signal">renovación por progreso</Badge>
        <Badge tone="neutral">un job por agente</Badge>
      </div>

      {detailJob ? (
        <JobDetailModal token={token} jobId={detailJob} onClose={() => setDetailJob(null)} />
      ) : null}
    </div>
  );
}
