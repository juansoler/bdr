"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Job, type JobEvent, type JobState } from "@/lib/api";
import { formatBytes, formatDateTime, shortId } from "@/lib/format";
import { Badge, Button, EmptyState, ErrorBox, Progress, StatusBadge, triggerLabel, cx } from "./ui";
import { Modal } from "./Modal";

const FINAL_STATES: JobState[] = ["succeeded", "failed", "canceled", "expired"];

// Detalle de un job con registro de eventos (estado / progreso / log).
// Mientras el job no esté terminado, refresca cada 2 segundos.
export function JobDetailModal({
  token,
  jobId,
  onClose,
}: {
  token: string;
  jobId: string;
  onClose: () => void;
}) {
  const [job, setJob] = useState<Job | null>(null);
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const lastSeq = useRef(0);

  const load = useCallback(
    async (quiet = false) => {
      if (!quiet) setError(null);
      try {
        const j = await api<Job>(`/api/v1/admin/jobs/${jobId}`, token);
        setJob(j);
        const after = lastSeq.current;
        const out = await api<{ items: JobEvent[] }>(
          `/api/v1/admin/jobs/${jobId}/events${after > 0 ? `?after=${after}` : ""}`,
          token,
        );
        const incoming = out?.items ?? [];
        if (incoming.length > 0) {
          lastSeq.current = incoming[incoming.length - 1].seq;
          setEvents((prev) => [...prev, ...incoming]);
        }
      } catch (err) {
        if (!quiet) setError(err instanceof Error ? err.message : "No se pudo cargar el job.");
      }
    },
    [jobId, token],
  );

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (job && FINAL_STATES.includes(job.state)) return;
    const id = window.setInterval(() => load(true), 2000);
    return () => window.clearInterval(id);
  }, [job, load]);

  const finished = job ? FINAL_STATES.includes(job.state) : false;

  return (
    <Modal kicker={`Job ${shortId(jobId)}`} title={job ? job.type.replace("_", " ") : "Detalle"} onClose={onClose} wide>
      <div className="mt-6 space-y-6">
        {error ? <ErrorBox message={error} onRetry={() => load()} /> : null}

        {!job && !error ? (
          <div className="flex items-center justify-center py-10">
            <span className="h-6 w-6 animate-spin border border-signal border-t-transparent" />
          </div>
        ) : null}

        {job ? (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <StatusBadge status={job.state} />
              <Badge tone="neutral">disparo: {triggerLabel(job.trigger)}</Badge>
              <Badge tone="neutral">
                intento {job.attempts}/{job.max_attempts || "—"}
              </Badge>
              {job.retry_of ? <Badge tone="alert">reintento de {shortId(job.retry_of)}</Badge> : null}
            </div>

            <div className="grid gap-x-6 gap-y-3 border border-ink-700 p-4 font-mono text-[11px] sm:grid-cols-2">
              <InfoRow label="Job" value={job.id} />
              <InfoRow label="Agente" value={shortId(job.agent_id)} />
              {job.plan_id ? <InfoRow label="Plan" value={shortId(job.plan_id)} /> : null}
              {job.restore_point_id ? (
                <InfoRow label="Punto de recuperación" value={shortId(job.restore_point_id)} />
              ) : null}
              <InfoRow label="Creado" value={formatDateTime(job.created_at)} />
              <InfoRow label="Iniciado" value={formatDateTime(job.started_at)} />
              <InfoRow label="Terminado" value={formatDateTime(job.finished_at)} />
              <InfoRow label="Actualizado" value={formatDateTime(job.updated_at)} />
            </div>

            <div>
              <div className="mb-2 flex items-center justify-between font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                <span>Progreso</span>
                <span className="text-slate-300">
                  {job.bytes_total ? (
                    <>
                      {formatBytes(job.bytes_done)} / {formatBytes(job.bytes_total)} · {job.progress}%
                    </>
                  ) : (
                    `${job.progress}%`
                  )}
                </span>
              </div>
              <Progress
                value={job.progress}
                tone={job.state === "failed" ? "danger" : job.state === "succeeded" ? "signal" : "ice"}
                animated={job.state === "running" || job.state === "leased"}
              />
              {job.message ? (
                <p className="mt-2 font-mono text-[11px] text-slate-400">{job.message}</p>
              ) : null}
              {job.error_code ? (
                <p className="mt-2 font-mono text-[11px] text-danger">
                  {job.error_code}: {job.error_message ?? "sin detalle"}
                </p>
              ) : null}
              {job.result?.snapshots && job.result.snapshots.length > 0 ? (
                <div className="mt-3 space-y-1 border border-ink-700 bg-ink-950/60 p-3">
                  {job.result.snapshots.map((s) => (
                    <p key={s.id} className="font-mono text-[10px] text-slate-400">
                      <span className="text-signal">{s.path}</span> · {formatBytes(s.size_bytes)} ·{" "}
                      {s.files} archivos · {s.dirs} dirs
                      {s.error_count > 0 ? (
                        <span className="text-alert"> · {s.error_count} errores</span>
                      ) : null}
                    </p>
                  ))}
                </div>
              ) : null}
              {job.result?.restored && job.result.restored.length > 0 ? (
                <div className="mt-3 space-y-1 border border-ink-700 bg-ink-950/60 p-3">
                  {job.result.restored.map((r) => (
                    <p key={`${r.item}-${r.destination}`} className="font-mono text-[10px] text-slate-400">
                      <span className="text-ice">{r.item}</span> → {r.destination} ·{" "}
                      {formatBytes(r.bytes)} · {r.files} archivos
                    </p>
                  ))}
                </div>
              ) : null}
            </div>

            <div>
              <p className="mb-2 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                Registro de eventos {finished ? "" : "· refresco 2 s"}
              </p>
              {events.length === 0 ? (
                <EmptyState
                  index="00 / LOG"
                  title="Sin eventos"
                  description="El agente todavía no ha publicado eventos para este job."
                />
              ) : (
                <div className="scroll-thin max-h-72 space-y-0.5 overflow-y-auto border border-ink-700 bg-ink-950/70 p-3">
                  {events.map((ev) => (
                    <p
                      key={ev.seq}
                      className={cx(
                        "font-mono text-[10px] leading-relaxed",
                        ev.level === "error" ? "text-danger" : ev.level === "warn" ? "text-alert" : "text-slate-400",
                      )}
                    >
                      <span className="text-slate-600">{formatDateTime(ev.at)}</span>{" "}
                      <span className="text-slate-600">[{ev.kind}]</span>{" "}
                      {ev.kind === "state" && ev.state ? <span className="text-ice">{ev.state} </span> : null}
                      {ev.kind === "progress" && typeof ev.percent === "number" ? (
                        <span className="text-ice">{ev.percent}% </span>
                      ) : null}
                      {ev.message ?? ""}
                    </p>
                  ))}
                </div>
              )}
            </div>

            <div className="flex justify-end border-t border-ink-700 pt-5">
              <Button variant="secondary" size="sm" onClick={() => load()}>
                Refrescar ahora
              </Button>
            </div>
          </>
        ) : null}
      </div>
    </Modal>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span className="text-slate-600">{label}</span>
      <span className="truncate text-slate-300" title={value}>
        {value}
      </span>
    </div>
  );
}
