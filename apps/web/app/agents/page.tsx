"use client";

import Link from "next/link";
import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { AgentDetailView } from "./AgentDetailView";
import { EnrollmentModal } from "@/components/EnrollmentModal";
import { JobDetailModal } from "@/components/JobDetailModal";
import { RevokeModal } from "@/components/RevokeModal";
import { DeleteAgentModal } from "@/components/DeleteAgentModal";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorBox,
  Input,
  PageHeader,
  StatusBadge,
  cx,
} from "@/components/ui";
import { useAuth } from "@/lib/auth";
import {
  ApiError,
  api,
  type Agent,
  type AgentList,
  type Job,
  type JobList,
  type PlanList,
} from "@/lib/api";
import { formatDateTime, shortId, timeAgo } from "@/lib/format";

function AgentsList() {
  const { token } = useAuth();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [plans, setPlans] = useState<PlanList["items"]>([]);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [showEnroll, setShowEnroll] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<Agent | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Agent | null>(null);
  const [detailJob, setDetailJob] = useState<Job | null>(null);

  const load = useCallback(async () => {
    try {
      const [a, j, p] = await Promise.all([
        api<AgentList>("/api/v1/admin/agents", token),
        api<JobList>("/api/v1/admin/jobs", token),
        api<PlanList>("/api/v1/admin/backup-plans", token),
      ]);
      setAgents(a.items ?? []);
      setJobs(j.items ?? []);
      setPlans(p.items ?? []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo cargar el inventario.");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    load();
    const id = window.setInterval(load, 5000);
    return () => window.clearInterval(id);
  }, [load]);

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return agents;
    return agents.filter(
      (a) =>
        a.hostname.toLowerCase().includes(q) ||
        a.os_version.toLowerCase().includes(q) ||
        a.id.toLowerCase().includes(q) ||
        (a.groups ?? []).some((g) => g.name.toLowerCase().includes(q)),
    );
  }, [agents, query]);

  async function backup(agent: Agent) {
    setBusyId(agent.id);
    setError(null);
    setNotice(null);
    try {
      const out = await api<{ job_id: string; state: string }>(
        `/api/v1/admin/agents/${agent.id}/backup`,
        token,
        { method: "POST", body: "{}" },
      );
      setNotice(`Trabajo ${shortId(out.job_id)} en cola para ${agent.hostname}.`);
      await load();
    } catch (err) {
      if (err instanceof ApiError && err.code === "E_PLAN_REQUIRED") {
        setError(`${agent.hostname} no tiene un plan asignado. Crea o asigna un plan primero.`);
      } else {
        setError(err instanceof Error ? err.message : "No se pudo lanzar el backup.");
      }
    } finally {
      setBusyId(null);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        kicker="02 / Inventario"
        title="Equipos"
        description="Máquinas enroladas, su estado de contacto y la última ejecución conocida. Lanza un backup, abre la ficha o revoca el acceso."
        actions={
          <>
            <Button variant="secondary" onClick={load} loading={loading}>
              Actualizar
            </Button>
            <Button onClick={() => setShowEnroll(true)}>Instalar agente</Button>
          </>
        }
      />

      {error ? <ErrorBox message={error} onRetry={load} /> : null}
      {notice ? (
        <div className="border border-signal/30 bg-signal/5 px-4 py-3 font-mono text-xs text-signal">
          {notice}
        </div>
      ) : null}

      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-ink-700 px-4 py-3">
          <div className="flex items-center gap-3">
            <span className="font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
              {visible.length} de {agents.length}
            </span>
            <span className="hidden font-mono text-[9px] uppercase tracking-widest2 text-slate-600 sm:inline">
              refresco 5 s
            </span>
          </div>
          <div className="w-full sm:w-72">
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Buscar por hostname, grupo, SO o ID…"
              className="py-2 text-xs"
            />
          </div>
        </div>

        {visible.length === 0 ? (
          <div className="p-5">
            <EmptyState
              index="00 / FLEET"
              title={agents.length === 0 ? "Sin equipos" : "Sin coincidencias"}
              description={
                agents.length === 0
                  ? "Genera un token de un solo uso y ejecuta el enrolamiento en el equipo. Al recibir su primer heartbeat aparecerá aquí."
                  : "Prueba con otro hostname, grupo, sistema operativo o identificador."
              }
              action={
                agents.length === 0 ? (
                  <Button onClick={() => setShowEnroll(true)}>Generar token</Button>
                ) : null
              }
            />
          </div>
        ) : (
          <div className="scroll-thin overflow-x-auto">
            <table className="w-full min-w-[1080px] border-collapse text-left">
              <thead>
                <tr className="border-b border-ink-700 font-mono text-[9px] uppercase tracking-[0.16em] text-slate-500">
                  <th className="px-4 py-3 font-normal">Equipo</th>
                  <th className="px-4 py-3 font-normal">Estado</th>
                  <th className="px-4 py-3 font-normal">Sistema</th>
                  <th className="px-4 py-3 font-normal">Hardware</th>
                  <th className="px-4 py-3 font-normal">Grupos</th>
                  <th className="px-4 py-3 font-normal">Copias</th>
                  <th className="px-4 py-3 font-normal">Último job</th>
                  <th className="px-4 py-3 text-right font-normal">Acciones</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((agent) => {
                  const lastJob = jobs.find((j) => j.agent_id === agent.id);
                  const inv = agent.inventory;
                  const online = agent.status === "online";
                  const groups = agent.groups ?? [];
                  return (
                    <tr
                      key={agent.id}
                      className="border-b border-ink-800/80 align-top text-sm transition hover:bg-white/[0.025]"
                    >
                      <td className="px-4 py-4">
                        <div className="flex items-start gap-3">
                          <span
                            className={cx(
                              "mt-1 h-2 w-2 shrink-0",
                              online
                                ? "bg-signal dot-live"
                                : agent.status === "offline"
                                  ? "bg-danger"
                                  : "bg-alert",
                            )}
                          />
                          <div>
                            <Link
                              href={`/agents?id=${agent.id}`}
                              className="font-display text-lg uppercase leading-none tracking-[0.1em] text-slate-100 transition hover:text-signal"
                            >
                              {agent.hostname}
                            </Link>
                            <p className="mt-1 font-mono text-[10px] text-slate-600" title={agent.id}>
                              {shortId(agent.id)} · visto {timeAgo(agent.last_seen_at)}
                            </p>
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-4">
                        <StatusBadge status={agent.status} />
                      </td>
                      <td className="px-4 py-4">
                        <p className="text-xs text-slate-300">{agent.os_version || agent.os_family}</p>
                        <p className="mt-0.5 font-mono text-[10px] text-slate-600">{agent.arch}</p>
                      </td>
                      <td className="px-4 py-4 font-mono text-[11px] text-slate-400">
                        <p>{inv ? `${inv.cpu_cores} vCPU` : "—"}</p>
                        <p className="mt-0.5 text-slate-600">
                          {inv?.ram_mb ? `${Math.round(inv.ram_mb / 1024)} GB RAM` : "sin inventario"}
                        </p>
                      </td>
                      <td className="max-w-[160px] px-4 py-4">
                        {groups.length > 0 ? (
                          <div className="flex flex-wrap gap-1">
                            {groups.slice(0, 3).map((g) => (
                              <Badge key={g.id} tone="ice">
                                {g.name}
                              </Badge>
                            ))}
                            {groups.length > 3 ? (
                              <Badge tone="neutral">+{groups.length - 3}</Badge>
                            ) : null}
                          </div>
                        ) : (
                          <span className="font-mono text-[10px] text-slate-600">sin grupo</span>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-4 py-4 font-mono text-[10px]">
                        <p className="text-slate-400">
                          <span className="text-slate-600">próx.</span>{" "}
                          {formatDateTime(agent.next_backup_at)}
                        </p>
                        <p className="mt-1 text-slate-400">
                          <span className="text-slate-600">últ.</span>{" "}
                          {formatDateTime(agent.last_backup_at)}
                          {agent.last_backup_state === "failed" ? (
                            <span className="text-danger"> · fallido</span>
                          ) : null}
                        </p>
                      </td>
                      <td className="px-4 py-4">
                        {lastJob ? (
                          <button
                            onClick={() => setDetailJob(lastJob)}
                            className="flex flex-col items-start gap-1 text-left"
                            title="Ver detalle del job"
                          >
                            <StatusBadge status={lastJob.state} />
                            <span className="font-mono text-[9px] text-slate-600 underline decoration-dotted underline-offset-2">
                              {shortId(lastJob.id)}
                            </span>
                          </button>
                        ) : (
                          <span className="font-mono text-[10px] text-slate-600">sin trabajos</span>
                        )}
                      </td>
                      <td className="px-4 py-4 text-right">
                        <div className="flex flex-col items-end gap-1.5">
                          <Button
                            size="sm"
                            variant={online ? "primary" : "secondary"}
                            loading={busyId === agent.id}
                            onClick={() => backup(agent)}
                          >
                            Backup ahora
                          </Button>
                          <div className="flex gap-1.5">
                            <Link href={`/agents?id=${agent.id}`}>
                              <Button size="sm" variant="secondary">
                                Ficha
                              </Button>
                            </Link>
                            {agent.status !== "revoked" ? (
                              <Button size="sm" variant="danger" onClick={() => setRevokeTarget(agent)}>
                                Revocar
                              </Button>
                            ) : (
                              <Button size="sm" variant="danger" onClick={() => setDeleteTarget(agent)}>
                                Eliminar
                              </Button>
                            )}
                          </div>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <div className="flex flex-wrap items-center justify-between gap-2 font-mono text-[9px] uppercase tracking-[0.18em] text-slate-600">
        <span>{plans.length} plan(es) disponibles</span>
        <span>Los agentes sin plan no pueden ejecutar backups</span>
      </div>

      {showEnroll ? (
        <EnrollmentModal token={token} onClose={() => setShowEnroll(false)} />
      ) : null}
      {revokeTarget ? (
        <RevokeModal
          token={token}
          agent={revokeTarget}
          onClose={() => setRevokeTarget(null)}
          onDone={() => {
            setNotice(`Acceso revocado para ${revokeTarget.hostname}.`);
            return load();
          }}
        />
      ) : null}
      {deleteTarget ? (
        <DeleteAgentModal
          token={token}
          agent={deleteTarget}
          onClose={() => setDeleteTarget(null)}
          onDone={() => {
            setNotice(`Equipo ${deleteTarget.hostname} eliminado del inventario.`);
            return load();
          }}
        />
      ) : null}
      {detailJob ? (
        <JobDetailModal token={token} jobId={detailJob.id} onClose={() => setDetailJob(null)} />
      ) : null}
    </div>
  );
}

// Página con export estático: el detalle se resuelve por query param
// (useSearchParams exige límite de Suspense en prerender).
export default function AgentsPage() {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-[40vh] items-center justify-center">
          <span className="h-8 w-8 animate-spin border border-signal border-t-transparent" />
        </div>
      }
    >
      <AgentsRouter />
    </Suspense>
  );
}

function AgentsRouter() {
  const sp = useSearchParams();
  const id = sp.get("id");
  if (id) return <AgentDetailView id={id} />;
  return <AgentsList />;
}
