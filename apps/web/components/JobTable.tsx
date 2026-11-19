"use client";

import type { Agent, Job } from "@/lib/api";
import { formatBytes, formatDateTime, shortId } from "@/lib/format";
import { EmptyState, Progress, StatusBadge, triggerLabel } from "./ui";
import { Button } from "./ui";

const ACTIVE_STATES = new Set(["pending", "leased", "running"]);
const RETRYABLE_STATES = new Set(["failed", "expired", "canceled"]);

export function JobTable({
  jobs,
  agents,
  compact = false,
  onView,
  onCancel,
  onRetry,
  busyId,
}: {
  jobs: Job[];
  agents: Agent[];
  compact?: boolean;
  onView?: (job: Job) => void;
  onCancel?: (job: Job) => void;
  onRetry?: (job: Job) => void;
  busyId?: string | null;
}) {
  if (jobs.length === 0) {
    return (
      <EmptyState
        index="00 / JOBS"
        title="Sin trabajos todavía"
        description="Cuando lances un backup, el job aparecerá aquí con su progreso y resultado."
      />
    );
  }

  const names = new Map(agents.map((a) => [a.id, a.hostname]));
  const hasActions = Boolean(onView || onCancel || onRetry);

  return (
    <div className="scroll-thin overflow-x-auto">
      <table className="w-full min-w-[760px] border-collapse text-left">
        <thead>
          <tr className="border-b border-ink-700 font-mono text-[9px] uppercase tracking-[0.16em] text-slate-500">
            <th className="px-3 py-3 font-normal">Job</th>
            <th className="px-3 py-3 font-normal">Equipo</th>
            {!compact ? <th className="px-3 py-3 font-normal">Tipo</th> : null}
            {!compact ? <th className="px-3 py-3 font-normal">Disparo</th> : null}
            <th className="px-3 py-3 font-normal">Estado</th>
            <th className="px-3 py-3 font-normal">Progreso</th>
            <th className="px-3 py-3 font-normal">Mensaje</th>
            <th className="px-3 py-3 font-normal">Actualizado</th>
            {hasActions ? (
              <th className="px-3 py-3 text-right font-normal">Acciones</th>
            ) : null}
          </tr>
        </thead>
        <tbody>
          {jobs.map((job) => {
            const active = ACTIVE_STATES.has(job.state);
            const cancelRequested = Boolean(job.cancel_requested_at);
            const message = job.message
              ? job.message
              : job.error_code
                ? `${job.error_code}: ${job.error_message ?? ""}`
                : job.result?.snapshots && job.result.snapshots.length > 0
                  ? job.result.snapshots.map((s) => s.path).join(", ")
                  : job.result?.restored && job.result.restored.length > 0
                    ? `${job.result.restored.length} elemento(s) restaurados`
                    : "—";
            return (
              <tr
                key={job.id}
                className="border-b border-ink-800/80 text-sm transition hover:bg-white/[0.025]"
              >
                <td className="px-3 py-3 font-mono text-[11px] text-slate-400" title={job.id}>
                  {shortId(job.id)}
                </td>
                <td className="px-3 py-3 text-slate-200">
                  {names.get(job.agent_id) ?? shortId(job.agent_id)}
                </td>
                {!compact ? (
                  <td className="px-3 py-3 font-mono text-[10px] uppercase tracking-wide text-slate-500">
                    {job.type.replace("_", " ")}
                  </td>
                ) : null}
                {!compact ? (
                  <td className="px-3 py-3 font-mono text-[10px] text-slate-500">
                    {triggerLabel(job.trigger)}
                  </td>
                ) : null}
                <td className="px-3 py-3">
                  <StatusBadge status={job.state} />
                </td>
                <td className="w-40 px-3 py-3">
                  <div className="flex items-center gap-2">
                    <Progress
                      value={job.progress}
                      tone={job.state === "failed" ? "danger" : job.state === "succeeded" ? "signal" : "ice"}
                      animated={active}
                    />
                    <span className="w-8 text-right font-mono text-[10px] text-slate-500">
                      {job.progress}%
                    </span>
                  </div>
                  {job.bytes_total ? (
                    <p className="mt-1 font-mono text-[9px] text-slate-600">
                      {formatBytes(job.bytes_done)} / {formatBytes(job.bytes_total)}
                    </p>
                  ) : null}
                </td>
                <td
                  className="max-w-[220px] truncate px-3 py-3 font-mono text-[10px] text-slate-500"
                  title={message}
                >
                  {message}
                </td>
                <td className="whitespace-nowrap px-3 py-3 font-mono text-[10px] text-slate-500">
                  {formatDateTime(job.updated_at)}
                </td>
                {hasActions ? (
                  <td className="px-3 py-3">
                    <div className="flex justify-end gap-1.5">
                      {onView ? (
                        <Button variant="ghost" size="sm" onClick={() => onView(job)}>
                          Detalle
                        </Button>
                      ) : null}
                      {onCancel && active && !cancelRequested ? (
                        <Button
                          variant="secondary"
                          size="sm"
                          loading={busyId === job.id}
                          onClick={() => onCancel(job)}
                        >
                          Cancelar
                        </Button>
                      ) : null}
                      {onCancel && active && cancelRequested ? (
                        <Button variant="quiet" size="sm" disabled>
                          Cancelando…
                        </Button>
                      ) : null}
                      {onRetry && RETRYABLE_STATES.has(job.state) ? (
                        <Button
                          variant="secondary"
                          size="sm"
                          loading={busyId === job.id}
                          onClick={() => onRetry(job)}
                        >
                          Reintentar
                        </Button>
                      ) : null}
                    </div>
                  </td>
                ) : null}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
