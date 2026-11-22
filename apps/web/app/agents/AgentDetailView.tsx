"use client";

import Link from "next/link";
import { Fragment, useCallback, useEffect, useState } from "react";
import { JobDetailModal } from "@/components/JobDetailModal";
import { JobTable } from "@/components/JobTable";
import { RevokeModal } from "@/components/RevokeModal";
import { TerminalConsole } from "@/components/TerminalConsole";
import { ScreenViewer } from "@/components/ScreenViewer";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorBox,
  PageHeader,
  Progress,
  StatusBadge,
  VerifyBadge,
  severityLabel,
  severityTone,
} from "@/components/ui";
import { useAuth } from "@/lib/auth";
import {
  ApiError,
  api,
  type AgentDetail,
  type BlockDevice,
} from "@/lib/api";
import { formatBytes, formatDateTime, retentionSummary, shortId, timeAgo } from "@/lib/format";

export function AgentDetailView({ id }: { id: string }) {
  const { token } = useAuth();
  const [detail, setDetail] = useState<AgentDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [showRevoke, setShowRevoke] = useState(false);
  const [detailJob, setDetailJob] = useState<string | null>(null);
  const [ctlVersion, setCtlVersion] = useState<string | null>(null);
  const [upgrading, setUpgrading] = useState(false);
  const [showTerminal, setShowTerminal] = useState(false);
  const [showScreen, setShowScreen] = useState(false);

  // Versión del control (para mostrar «Forzar actualización» solo si difiere).
  useEffect(() => {
    let alive = true;
    api<{ current: string }>("/api/v1/admin/system/update", token)
      .then((s) => {
        if (alive) setCtlVersion(s.current);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [token]);

  const load = useCallback(async () => {
    try {
      const d = await api<AgentDetail>(`/api/v1/admin/agents/${id}`, token);
      setDetail(d);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo cargar la ficha del equipo.");
    } finally {
      setLoading(false);
    }
  }, [id, token]);

  useEffect(() => {
    load();
    const interval = window.setInterval(load, 5000);
    return () => window.clearInterval(interval);
  }, [load]);

  async function backupNow() {
    if (!detail) return;
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const out = await api<{ job_id: string }>(
        `/api/v1/admin/agents/${detail.agent.id}/backup`,
        token,
        { method: "POST", body: "{}" },
      );
      setNotice(`Trabajo ${shortId(out.job_id)} en cola.`);
      await load();
    } catch (err) {
      if (err instanceof ApiError && err.code === "E_PLAN_REQUIRED") {
        setError("Este equipo no tiene un plan asignado. Crea o asigna un plan primero.");
      } else {
        setError(err instanceof Error ? err.message : "No se pudo lanzar el backup.");
      }
    } finally {
      setBusy(false);
    }
  }

  async function forceUpgrade() {
    if (!detail) return;
    setUpgrading(true);
    setError(null);
    setNotice(null);
    try {
      const out = await api<{ message: string }>(
        `/api/v1/admin/agents/${detail.agent.id}/upgrade`,
        token,
        { method: "POST", body: "{}" },
      );
      setNotice(out.message || "Actualización solicitada.");
      await load();
    } catch (err) {
      if (err instanceof ApiError && err.code === "E_UPGRADE_NONE") {
        setNotice(err.message);
      } else {
        setError(err instanceof Error ? err.message : "No se pudo solicitar la actualización.");
      }
    } finally {
      setUpgrading(false);
    }
  }

  if (loading && !detail) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <span className="h-8 w-8 animate-spin border border-signal border-t-transparent" />
      </div>
    );
  }

  if (error && !detail) {
    return (
      <div className="space-y-6">
        <PageHeader
          kicker="02 / Ficha de equipo"
          title="Equipo"
          actions={
            <Link href="/agents">
              <Button variant="secondary">← Volver a equipos</Button>
            </Link>
          }
        />
        <ErrorBox message={error} onRetry={load} />
      </div>
    );
  }

  if (!detail) return null;

  const agent = detail.agent;
  const inv = agent.inventory;
  const revoked = agent.status === "revoked";
  const plans = detail.effective_plans ?? [];
  const jobs = detail.recent_jobs ?? [];
  const points = (detail.restore_points ?? []).slice(0, 10);
  const alerts = detail.open_alerts ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        kicker="02 / Ficha de equipo"
        title={agent.hostname}
        description={`Enrolado ${timeAgo(agent.enrolled_at)} · agente v${agent.agent_version || "—"} · ${agent.os_version || agent.os_family} ${agent.arch}`}
        actions={
          <>
            <Link href="/agents">
              <Button variant="quiet">← Equipos</Button>
            </Link>
            <Button variant="secondary" onClick={load} loading={loading}>
              Actualizar
            </Button>
            {!revoked ? (
              <>
                <Button variant="secondary" onClick={backupNow} loading={busy}>
                  Backup ahora
                </Button>
                <Button variant="secondary" onClick={() => setShowTerminal(true)}>
                  Abrir consola
                </Button>
                <Button variant="secondary" onClick={() => setShowScreen(true)}>
                  Ver pantalla
                </Button>
                {ctlVersion && agent.agent_version && agent.agent_version !== ctlVersion ? (
                  <Button variant="secondary" onClick={forceUpgrade} loading={upgrading}>
                    Forzar actualización{ctlVersion ? ` a v${ctlVersion}` : ""}
                  </Button>
                ) : null}
                <Button variant="danger" onClick={() => setShowRevoke(true)}>
                  Revocar
                </Button>
              </>
            ) : null}
          </>
        }
      />

      {error ? <ErrorBox message={error} onRetry={load} /> : null}
      {notice ? (
        <div className="border border-signal/30 bg-signal/5 px-4 py-3 font-mono text-xs text-signal">
          {notice}
        </div>
      ) : null}
      {revoked ? (
        <div className="border border-neutral-500/30 bg-white/5 px-4 py-3 font-mono text-xs text-slate-400">
          Equipo revocado{agent.revoked_at ? ` el ${formatDateTime(agent.revoked_at)}` : ""}
          {agent.revoked_reason ? ` — ${agent.revoked_reason}` : ""}.
        </div>
      ) : null}

      <div className="grid gap-6 xl:grid-cols-[1.35fr_0.65fr]">
        <div className="space-y-6">
          <Card className="overflow-hidden">
            <div className="flex items-center justify-between border-b border-ink-700 px-5 py-4">
              <div>
                <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
                  Protección
                </p>
                <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
                  Planes en vigor
                </h2>
              </div>
              <Link
                href="/plans"
                className="font-mono text-[10px] uppercase tracking-widest2 text-slate-500 transition hover:text-signal"
              >
                Planes →
              </Link>
            </div>
            <div className="p-2 sm:p-3">
              {plans.length === 0 ? (
                <div className="p-3">
                  <EmptyState
                    index="00 / PLANS"
                    title="Sin plan asignado"
                    description="Este equipo no ejecutará backups hasta que asignes un plan (directo o vía grupo)."
                    action={
                      <Link href="/plans">
                        <Button size="sm">Ir a planes</Button>
                      </Link>
                    }
                  />
                </div>
              ) : (
                <div className="scroll-thin overflow-x-auto">
                  <table className="w-full min-w-[640px] border-collapse text-left">
                    <thead>
                      <tr className="border-b border-ink-700 font-mono text-[9px] uppercase tracking-[0.16em] text-slate-500">
                        <th className="px-3 py-3 font-normal">Plan</th>
                        <th className="px-3 py-3 font-normal">Vía</th>
                        <th className="px-3 py-3 font-normal">Programación</th>
                        <th className="px-3 py-3 font-normal">Retención</th>
                        <th className="px-3 py-3 font-normal">Próxima ejecución</th>
                      </tr>
                    </thead>
                    <tbody>
                      {plans.map((ep) => (
                        <tr
                          key={ep.assignment_id}
                          className="border-b border-ink-800/80 text-sm transition hover:bg-white/[0.025]"
                        >
                          <td className="px-3 py-3">
                            <p className="text-slate-200">{ep.plan.name}</p>
                            <p className="mt-0.5 font-mono text-[10px] text-slate-600">
                              {ep.plan.kind === "image"
                                ? "imagen completa del equipo"
                                : (ep.plan.paths ?? []).join(" · ")}
                            </p>
                          </td>
                          <td className="px-3 py-3">
                            <Badge tone={ep.via === "group" ? "ice" : "signal"}>
                              {ep.via === "group" ? `grupo: ${ep.group_name ?? shortId(ep.group_id)}` : "directo"}
                            </Badge>
                          </td>
                          <td className="px-3 py-3 font-mono text-[10px] text-slate-500">
                            {ep.plan.schedule || "solo manual"}
                            {ep.plan.window ? (
                              <span className="text-slate-600">
                                {" "}
                                · {ep.plan.window.start}–{ep.plan.window.end}
                              </span>
                            ) : null}
                          </td>
                          <td className="px-3 py-3 font-mono text-[10px] text-slate-500">
                            {retentionSummary(ep.plan.retention)}
                          </td>
                          <td className="whitespace-nowrap px-3 py-3 font-mono text-[10px] text-ice">
                            {formatDateTime(ep.next_run_at)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </Card>

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
              <JobTable
                jobs={jobs.slice(0, 8)}
                agents={[agent]}
                compact
                onView={(job) => setDetailJob(job.id)}
              />
            </div>
          </Card>

          <Card className="overflow-hidden">
            <div className="flex items-center justify-between border-b border-ink-700 px-5 py-4">
              <div>
                <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
                  Recuperación
                </p>
                <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
                  Puntos de recuperación
                </h2>
              </div>
              <Link
                href="/restore-points"
                className="font-mono text-[10px] uppercase tracking-widest2 text-slate-500 transition hover:text-signal"
              >
                Ver todos →
              </Link>
            </div>
            <div className="p-2 sm:p-3">
              {points.length === 0 ? (
                <div className="p-3">
                  <EmptyState
                    index="00 / RPS"
                    title="Sin copias todavía"
                    description="Cuando el primer backup termine correctamente, su punto de recuperación aparecerá aquí."
                  />
                </div>
              ) : (
                <div className="scroll-thin overflow-x-auto">
                  <table className="w-full min-w-[640px] border-collapse text-left">
                    <thead>
                      <tr className="border-b border-ink-700 font-mono text-[9px] uppercase tracking-[0.16em] text-slate-500">
                        <th className="px-3 py-3 font-normal">Fecha</th>
                        <th className="px-3 py-3 font-normal">Origen</th>
                        <th className="px-3 py-3 font-normal">Tamaño</th>
                        <th className="px-3 py-3 font-normal">Verificación</th>
                      </tr>
                    </thead>
                    <tbody>
                      {points.map((rp) => (
                        <tr
                          key={rp.id}
                          className="border-b border-ink-800/80 text-sm transition hover:bg-white/[0.025]"
                        >
                          <td className="whitespace-nowrap px-3 py-3 font-mono text-[10px] text-slate-400">
                            {formatDateTime(rp.created_at)}
                          </td>
                          <td className="px-3 py-3 font-mono text-[11px] text-slate-300" title={rp.source_path}>
                            <span className="block max-w-[280px] truncate">{rp.source_path}</span>
                          </td>
                          <td className="whitespace-nowrap px-3 py-3 font-mono text-[10px] text-slate-400">
                            {formatBytes(rp.size_bytes)} · {rp.files} arch.
                          </td>
                          <td className="px-3 py-3">
                            <VerifyBadge state={rp.verify_state} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </Card>
        </div>

        <div className="space-y-6">
          <Card className="p-5">
            <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
              Estado
            </p>
            <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
              Contacto
            </h2>
            <div className="mt-4 space-y-3 font-mono text-[11px]">
              <Row label="Estado">
                <StatusBadge status={agent.status} />
              </Row>
              <Row label="Último contacto">{timeAgo(agent.last_seen_at)}</Row>
              <Row label="Próxima copia">{formatDateTime(agent.next_backup_at)}</Row>
              <Row label="Última copia">
                {formatDateTime(agent.last_backup_at)}
                {agent.last_backup_state === "failed" ? (
                  <span className="text-danger"> · fallida</span>
                ) : null}
              </Row>
              <Row label="Certificado">{formatDateTime(agent.cert_expires_at)}</Row>
              <Row label="Usuario repo">{agent.repository_username || "—"}</Row>
              <Row label="Job actual">{shortId(agent.current_job_id)}</Row>
              {agent.upgrade_requested_at ? (
                <Row label="Actualización forzada">
                  solicitada {timeAgo(agent.upgrade_requested_at)}
                </Row>
              ) : null}
              <Row label="Grupos">
                {(agent.groups ?? []).length > 0 ? (
                  <span className="flex flex-wrap justify-end gap-1">
                    {(agent.groups ?? []).map((g) => (
                      <Badge key={g.id} tone="ice">
                        {g.name}
                      </Badge>
                    ))}
                  </span>
                ) : (
                  "—"
                )}
              </Row>
            </div>
          </Card>

          <Card className="p-5">
            <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
              Inventario
            </p>
            <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
              Hardware
            </h2>
            {!inv ? (
              <p className="mt-4 border border-ink-700 px-3 py-4 text-center font-mono text-[10px] uppercase tracking-widest2 text-slate-600">
                Sin inventario recibido
              </p>
            ) : (
              <div className="mt-4 space-y-4">
                <div className="grid grid-cols-2 gap-2">
                  <Tile label="CPU" value={`${inv.cpu_cores} vCPU`} />
                  <Tile label="RAM" value={`${Math.round(inv.ram_mb / 1024)} GB`} />
                  <Tile label="Sistema" value={inv.os_version || inv.os_family} />
                  <Tile label="Arquitectura" value={inv.arch} />
                </div>
                <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-600">
                  Recogido {timeAgo(inv.collected_at)}
                </p>

                {inv.disks && inv.disks.length > 0 ? (
                  <div>
                    <p className="mb-2 font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
                      Volúmenes montados
                    </p>
                    <div className="space-y-2">
                      {inv.disks.map((d) => {
                        const used =
                          d.total_bytes > 0
                            ? Math.max(0, Math.min(100, ((d.total_bytes - d.free_bytes) / d.total_bytes) * 100))
                            : 0;
                        return (
                          <div key={`${d.name}-${d.mountpoint}`} className="border border-ink-700 px-3 py-2">
                            <div className="flex items-center justify-between gap-2 font-mono text-[10px]">
                              <span className="text-slate-200">{d.mountpoint || d.name}</span>
                              <span className="text-slate-500">
                                {formatBytes(d.total_bytes - d.free_bytes)} / {formatBytes(d.total_bytes)}
                              </span>
                            </div>
                            <div className="mt-1.5">
                              <Progress
                                value={used}
                                tone={used > 90 ? "danger" : used > 75 ? "alert" : "signal"}
                              />
                            </div>
                            <p className="mt-1 font-mono text-[9px] text-slate-600">
                              libres {formatBytes(d.free_bytes)}
                              {d.fs_type ? ` · ${d.fs_type}` : ""}
                            </p>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                ) : null}

                {inv.block_devices && inv.block_devices.length > 0 ? (
                  <div>
                    <p className="mb-2 font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
                      Dispositivos de bloque
                    </p>
                    <div className="scroll-thin overflow-x-auto">
                      <table className="w-full border-collapse text-left">
                        <thead>
                          <tr className="border-b border-ink-700 font-mono text-[9px] uppercase tracking-[0.16em] text-slate-500">
                            <th className="py-2 pr-3 font-normal">Nombre</th>
                            <th className="py-2 pr-3 font-normal">Tipo</th>
                            <th className="py-2 pr-3 font-normal">Tamaño</th>
                            <th className="py-2 font-normal">Montaje</th>
                          </tr>
                        </thead>
                        <tbody>
                          <BlockRows devices={inv.block_devices} depth={0} />
                        </tbody>
                      </table>
                    </div>
                  </div>
                ) : null}

                {inv.firmware ? (
                  <p className="font-mono text-[10px] text-slate-500">
                    Firmware: {inv.firmware.mode || "—"}
                    {inv.firmware.secure_boot === true ? " · secure boot" : ""}
                    {inv.firmware.tpm ? ` · TPM ${inv.firmware.tpm_version ?? ""}` : ""}
                  </p>
                ) : null}
                {inv.encrypted_volumes && inv.encrypted_volumes.length > 0 ? (
                  <div className="flex flex-wrap gap-1">
                    {inv.encrypted_volumes.map((v) => (
                      <Badge key={v.volume} tone={v.locked ? "alert" : "signal"}>
                        {v.volume}: {v.kind}
                        {v.locked ? " (bloqueado)" : ""}
                      </Badge>
                    ))}
                  </div>
                ) : null}
              </div>
            )}
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
                Ver todas →
              </Link>
            </div>
            <div className="mt-4 space-y-2">
              {alerts.length === 0 ? (
                <p className="border border-signal/20 bg-signal/5 px-3 py-4 text-center font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
                  Sin alertas para este equipo
                </p>
              ) : (
                alerts.map((a) => (
                  <div key={a.id} className="border border-ink-700 px-3 py-2.5">
                    <div className="flex items-center justify-between gap-2">
                      <Badge tone={severityTone(a.severity)}>{severityLabel(a.severity)}</Badge>
                      <span className="font-mono text-[10px] text-slate-500">×{a.count}</span>
                    </div>
                    <p className="mt-1.5 text-sm text-slate-200">{a.title}</p>
                    {a.detail ? (
                      <p className="mt-0.5 line-clamp-2 text-[11px] text-slate-500">{a.detail}</p>
                    ) : null}
                    <p className="mt-1 font-mono text-[9px] text-slate-600">
                      última {timeAgo(a.last_at)}
                    </p>
                  </div>
                ))
              )}
            </div>
          </Card>
        </div>
      </div>

      {showRevoke ? (
        <RevokeModal
          token={token}
          agent={agent}
          onClose={() => setShowRevoke(false)}
          onDone={() => {
            setNotice("Acceso revocado.");
            return load();
          }}
        />
      ) : null}
      {detailJob ? (
        <JobDetailModal token={token} jobId={detailJob} onClose={() => setDetailJob(null)} />
      ) : null}
      {showTerminal ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60">
          <div className="w-full max-w-4xl h-[85vh] bg-ink-950 border border-ink-700 rounded overflow-hidden">
            <div className="flex items-center justify-between border-b border-ink-700 px-4 py-3">
              <div className="flex items-center gap-2">
                <span className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">Terminal</span>
                <span className="text-slate-300">Consola remota</span>
              </div>
              <Button variant="quiet" size="sm" onClick={() => setShowTerminal(false)}>
                Cerrar
              </Button>
            </div>
            <TerminalConsole agentId={id} />
          </div>
        </div>
      ) : null}
      {showScreen ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60">
          <div className="w-full max-w-5xl h-[85vh] bg-ink-950 border border-ink-700 rounded overflow-hidden flex flex-col">
            <div className="flex items-center justify-between border-b border-ink-700 px-4 py-3">
              <div className="flex items-center gap-2">
                <span className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">Pantalla</span>
                <span className="text-slate-300">Escritorio remoto</span>
              </div>
              <Button variant="quiet" size="sm" onClick={() => setShowScreen(false)}>
                Cerrar
              </Button>
            </div>
            <div className="min-h-0 flex-1">
              <ScreenViewer agentId={id} />
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-slate-600">{label}</span>
      <span className="text-right text-slate-300">{children}</span>
    </div>
  );
}

function Tile({ label, value }: { label: string; value: string }) {
  return (
    <div className="border border-ink-700 px-3 py-2">
      <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-600">{label}</p>
      <p className="mt-0.5 truncate font-display text-lg uppercase text-slate-200" title={value}>
        {value}
      </p>
    </div>
  );
}

function BlockRows({
  devices,
  depth,
}: {
  devices: BlockDevice[];
  depth: number;
}) {
  return (
    <>
      {devices.map((d) => (
        <Fragment key={d.name}>
          <tr className="border-b border-ink-800/80 font-mono text-[10px] text-slate-400">
            <td className="py-2 pr-3" style={{ paddingLeft: `${depth * 14}px` }}>
              {d.name}
            </td>
            <td className="py-2 pr-3 text-slate-500">{d.type}</td>
            <td className="whitespace-nowrap py-2 pr-3">{formatBytes(d.size_bytes)}</td>
            <td className="py-2 text-slate-500">{d.mountpoint || "—"}</td>
          </tr>
          {d.children && d.children.length > 0 ? (
            <BlockRows devices={d.children} depth={depth + 1} />
          ) : null}
        </Fragment>
      ))}
    </>
  );
}
