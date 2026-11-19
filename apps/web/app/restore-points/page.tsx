"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Check, Modal } from "@/components/Modal";
import {
  Button,
  Card,
  EmptyState,
  ErrorBox,
  Field,
  Input,
  PageHeader,
  Select,
  Textarea,
  VerifyBadge,
  cx,
} from "@/components/ui";
import { useAuth } from "@/lib/auth";
import {
  ApiError,
  api,
  apiDownload,
  type AgentList,
  type FileEntry,
  type FileListing,
  type FileSearchResult,
  type PlanList,
  type RestorePoint,
  type RestorePointList,
  type RestoreResponse,
} from "@/lib/api";
import { formatBytes, formatDateTime, shortId, timeAgo } from "@/lib/format";

const PAGE_SIZE = 25;
const BROWSE_NOTICE =
  "Navegación no disponible: el plano de control no tiene acceso directo al repositorio";

export default function RestorePointsPage() {
  const { token } = useAuth();
  const [points, setPoints] = useState<RestorePoint[]>([]);
  const [agents, setAgents] = useState<AgentList["items"]>([]);
  const [plans, setPlans] = useState<PlanList["items"]>([]);
  const [agentFilter, setAgentFilter] = useState("");
  const [planFilter, setPlanFilter] = useState("");
  const [limit, setLimit] = useState(PAGE_SIZE);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [explore, setExplore] = useState<RestorePoint | null>(null);
  const [verify, setVerify] = useState<RestorePoint | null>(null);
  const [restore, setRestore] = useState<RestorePoint | null>(null);
  const [deleting, setDeleting] = useState<RestorePoint | null>(null);

  const load = useCallback(
    async (currentLimit: number) => {
      const params = new URLSearchParams();
      if (agentFilter) params.set("agent_id", agentFilter);
      if (planFilter) params.set("plan_id", planFilter);
      params.set("limit", String(currentLimit));
      const qs = params.toString();
      try {
        const [rp, a, p] = await Promise.all([
          api<RestorePointList>(`/api/v1/admin/restore-points?${qs}`, token),
          api<AgentList>("/api/v1/admin/agents", token),
          api<PlanList>("/api/v1/admin/backup-plans", token),
        ]);
        setPoints(rp.items ?? []);
        setAgents(a.items ?? []);
        setPlans(p.items ?? []);
        setError(null);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "No se pudieron cargar los puntos de recuperación.",
        );
      } finally {
        setLoading(false);
      }
    },
    [agentFilter, planFilter, token],
  );

  useEffect(() => {
    setLoading(true);
    load(limit);
    const id = window.setInterval(() => load(limit), 15000);
    return () => window.clearInterval(id);
  }, [load, limit]);

  const hostnames = useMemo(
    () => new Map(agents.map((a) => [a.id, a.hostname])),
    [agents],
  );

  const anyNotBrowsable = points.some((rp) => !rp.browsable);

  return (
    <div className="space-y-6">
      <PageHeader
        kicker="05 / Recuperación"
        title="Puntos de recuperación"
        description="Snapshots completados por equipo y ruta. Verifica su contenido, explora los ficheros o restaura lo que necesites sobre el original o en otra máquina."
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

      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-end justify-between gap-3 border-b border-ink-700 px-4 py-3">
          <div className="flex flex-wrap items-end gap-3">
            <div className="w-48">
              <Field label="Equipo">
                <Select
                  value={agentFilter}
                  onChange={(e) => {
                    setAgentFilter(e.target.value);
                    setLimit(PAGE_SIZE);
                  }}
                  className="py-2 text-xs"
                >
                  <option value="">Todos los equipos</option>
                  {agents.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.hostname}
                    </option>
                  ))}
                </Select>
              </Field>
            </div>
            <div className="w-56">
              <Field label="Plan">
                <Select
                  value={planFilter}
                  onChange={(e) => {
                    setPlanFilter(e.target.value);
                    setLimit(PAGE_SIZE);
                  }}
                  className="py-2 text-xs"
                >
                  <option value="">Todos los planes</option>
                  {plans.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </Select>
              </Field>
            </div>
            <span className="pb-2.5 font-mono text-[9px] uppercase tracking-widest2 text-slate-600">
              {points.length} puntos · refresco 15 s
            </span>
          </div>
        </div>

        <div className="p-2 sm:p-3">
          {points.length === 0 ? (
            <EmptyState
              index="00 / RPS"
              title={loading ? "Cargando…" : "Sin puntos de recuperación"}
              description={
                loading
                  ? "Consultando el catálogo de snapshots."
                  : "Cuando un backup termine correctamente, su punto de recuperación aparecerá aquí. Ajusta los filtros si buscas algo concreto."
              }
            />
          ) : (
            <div className="scroll-thin overflow-x-auto">
              <table className="w-full min-w-[1020px] border-collapse text-left">
                <thead>
                  <tr className="border-b border-ink-700 font-mono text-[9px] uppercase tracking-[0.16em] text-slate-500">
                    <th className="px-3 py-3 font-normal">Equipo</th>
                    <th className="px-3 py-3 font-normal">Ruta origen</th>
                    <th className="px-3 py-3 font-normal">Fecha</th>
                    <th className="px-3 py-3 font-normal">Tamaño</th>
                    <th className="px-3 py-3 font-normal">Contenido</th>
                    <th className="px-3 py-3 font-normal">Verificación</th>
                    <th className="px-3 py-3 text-right font-normal">Acciones</th>
                  </tr>
                </thead>
                <tbody>
                  {points.map((rp) => (
                    <tr
                      key={rp.id}
                      className="border-b border-ink-800/80 text-sm transition hover:bg-white/[0.025]"
                    >
                      <td className="px-3 py-3">
                        <p className="text-slate-200">
                          {rp.hostname || hostnames.get(rp.agent_id) || shortId(rp.agent_id)}
                        </p>
                        <p className="mt-0.5 font-mono text-[9px] text-slate-600">
                          {rp.plan_name || shortId(rp.plan_id)}
                        </p>
                      </td>
                      <td className="px-3 py-3 font-mono text-[11px] text-slate-300">
                        <span className="block max-w-[240px] truncate" title={rp.source_path}>
                          {rp.source_path}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-3 py-3 font-mono text-[10px] text-slate-500">
                        {formatDateTime(rp.created_at)}
                      </td>
                      <td className="whitespace-nowrap px-3 py-3 font-mono text-[10px] text-slate-400">
                        {formatBytes(rp.size_bytes)}
                      </td>
                      <td className="whitespace-nowrap px-3 py-3 font-mono text-[10px] text-slate-500">
                        {rp.files} arch. · {rp.dirs} dirs
                        {rp.error_count > 0 ? (
                          <span className="text-alert"> · {rp.error_count} err.</span>
                        ) : null}
                      </td>
                      <td className="px-3 py-3">
                        <div className="flex flex-col items-start gap-1">
                          <VerifyBadge state={rp.verify_state} />
                          {rp.verified_at ? (
                            <span className="font-mono text-[9px] text-slate-600">
                              {timeAgo(rp.verified_at)}
                            </span>
                          ) : null}
                        </div>
                      </td>
                      <td className="px-3 py-3">
                        <div className="flex flex-col items-end gap-1.5">
                          <div className="flex justify-end gap-1.5">
                            {rp.browsable ? (
                              <>
                                <Button
                                  size="sm"
                                  variant="secondary"
                                  onClick={() => setExplore(rp)}
                                >
                                  Explorar
                                </Button>
                                <Button size="sm" variant="secondary" onClick={() => setVerify(rp)}>
                                  Verificar
                                </Button>
                              </>
                            ) : (
                              <span
                                className="max-w-[210px] font-mono text-[9px] leading-relaxed text-slate-600"
                                title={BROWSE_NOTICE}
                              >
                                navegación no disponible
                              </span>
                            )}
                            <Button size="sm" variant="primary" onClick={() => setRestore(rp)}>
                              Restaurar
                            </Button>
                            <Button size="sm" variant="danger" onClick={() => setDeleting(rp)}>
                              Eliminar
                            </Button>
                          </div>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {points.length >= limit ? (
          <div className="flex items-center justify-center border-t border-ink-700 px-4 py-3">
            <Button variant="quiet" size="sm" onClick={() => setLimit((prev) => prev + PAGE_SIZE)}>
              Cargar más
            </Button>
          </div>
        ) : null}
      </Card>

      {anyNotBrowsable ? (
        <p className="border-t border-ink-800 pt-4 font-mono text-[9px] uppercase tracking-[0.18em] text-slate-600">
          {BROWSE_NOTICE}
        </p>
      ) : null}

      {explore ? (
        <ExplorerDrawer token={token} rp={explore} onClose={() => setExplore(null)} />
      ) : null}
      {verify ? (
        <VerifyModal
          token={token}
          rp={verify}
          onClose={() => setVerify(null)}
          onDone={() => load(limit)}
        />
      ) : null}
      {restore ? (
        <RestoreModal
          token={token}
          rp={restore}
          agents={agents}
          onClose={() => setRestore(null)}
        />
      ) : null}
      {deleting ? (
        <DeleteModal
          token={token}
          rp={deleting}
          onClose={() => setDeleting(null)}
          onDone={() => load(limit)}
          onNotice={setNotice}
        />
      ) : null}
    </div>
  );
}

// Confirmación de borrado del punto de restauración: borra el snapshot en el
// repositorio (file) o los objetos del destino S3/ruta física (image/vm).
function DeleteModal({
  token,
  rp,
  onClose,
  onDone,
  onNotice,
}: {
  token: string;
  rp: RestorePoint;
  onClose: () => void;
  onDone: () => void | Promise<void>;
  onNotice: (msg: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function remove() {
    setBusy(true);
    setError(null);
    try {
      const out = await api<{ detail?: string }>(
        `/api/v1/admin/restore-points/${rp.id}`, token, { method: "DELETE" });
      await onDone();
      onClose();
      onNotice(
        out?.detail
          ? `Backup eliminado: ${out.detail}.`
          : "Backup eliminado del almacenamiento.",
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo eliminar el punto de recuperación.");
      setBusy(false);
    }
  }

  return (
    <Modal kicker="Acción destructiva" title="Eliminar punto de recuperación" onClose={onClose}>
      <div className="mt-6 space-y-5">
        <div className="border border-danger/30 bg-danger/5 p-4 text-sm leading-relaxed text-slate-300">
          Se eliminará el backup de <span className="font-mono text-danger">{formatDateTime(rp.created_at)}</span>{" "}
          del equipo <span className="font-mono text-danger">{rp.hostname || rp.agent_id}</span>
          {rp.kind === "file"
            ? " del repositorio (los bloques compartidos con otros snapshots se conservan hasta el mantenimiento del repositorio)."
            : " del destino de almacenamiento (manifiesto y segmentos)."}
          {" "}Si el destino aplica Object Lock, los objetos retenidos no se podrán borrar hasta que
          expire la retención. La operación queda registrada en la auditoría.
        </div>

        {error ? <ErrorBox message={error} /> : null}

        <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
          <Button variant="quiet" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="danger" onClick={remove} loading={busy}>
            Eliminar backup
          </Button>
        </div>
      </div>
    </Modal>
  );
}

// Verificación de contenido: lanza la comprobación y sondea hasta que termina.
function VerifyModal({
  token,
  rp,
  onClose,
  onDone,
}: {
  token: string;
  rp: RestorePoint;
  onClose: () => void;
  onDone: () => void | Promise<void>;
}) {
  const [percent, setPercent] = useState(0);
  const [current, setCurrent] = useState<RestorePoint>(rp);
  const [phase, setPhase] = useState<"form" | "polling" | "done">("form");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const poll = useCallback(async () => {
    try {
      const detail = await api<RestorePoint>(`/api/v1/admin/restore-points/${rp.id}`, token);
      setCurrent(detail);
      if (detail.verify_state !== "running") {
        setPhase("done");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo consultar el estado.");
      setPhase("done");
    }
  }, [rp.id, token]);

  useEffect(() => {
    if (phase !== "polling") return;
    const id = window.setInterval(poll, 3000);
    return () => window.clearInterval(id);
  }, [phase, poll]);

  useEffect(() => {
    if (phase === "done") {
      void onDone();
    }
  }, [phase]);

  async function start() {
    setBusy(true);
    setError(null);
    try {
      await api(`/api/v1/admin/restore-points/${rp.id}/verify`, token, {
        method: "POST",
        body: JSON.stringify({ files_percent: percent }),
      });
      setCurrent({ ...current, verify_state: "running" });
      setPhase("polling");
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError("Ya hay una verificación en curso para este punto.");
      } else {
        setError(err instanceof Error ? err.message : "No se pudo iniciar la verificación.");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal kicker="Verificación de contenido" title={`Snapshot ${shortId(rp.snapshot_id)}`} onClose={onClose}>
      <div className="mt-6 space-y-5">
        <div className="border border-ink-700 p-4 font-mono text-[11px]">
          <p className="text-slate-300">{rp.hostname || shortId(rp.agent_id)} · {rp.source_path}</p>
          <p className="mt-1 text-slate-500">
            {formatBytes(rp.size_bytes)} · {rp.files} archivos · {rp.dirs} directorios
          </p>
        </div>

        {phase === "form" ? (
          <>
            <Field
              label="Profundidad de la verificación"
              hint="0 % = solo estructura del snapshot. 100 % lee el contenido de todos los archivos (más lento)."
            >
              <Select value={String(percent)} onChange={(e) => setPercent(Number(e.target.value))}>
                <option value="0">0 % · solo estructura</option>
                <option value="5">5 % · muestra rápida</option>
                <option value="25">25 % · muestreo medio</option>
                <option value="100">100 % · contenido completo</option>
              </Select>
            </Field>
            {error ? <ErrorBox message={error} /> : null}
            <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
              <Button variant="quiet" onClick={onClose}>
                Cancelar
              </Button>
              <Button onClick={start} loading={busy}>
                Iniciar verificación
              </Button>
            </div>
          </>
        ) : (
          <>
            <div className="flex flex-col items-center gap-3 border border-ink-700 px-4 py-8">
              {phase === "polling" ? (
                <>
                  <span className="h-6 w-6 animate-spin border border-ice border-t-transparent" />
                  <p className="font-mono text-[10px] uppercase tracking-widest2 text-ice">
                    Verificando… sondeo cada 3 s
                  </p>
                </>
              ) : (
                <p className="font-mono text-[10px] uppercase tracking-widest2 text-slate-400">
                  Verificación terminada
                </p>
              )}
              <VerifyBadge state={current.verify_state} />
              {current.verify_detail ? (
                <p className="max-w-md text-center font-mono text-[10px] leading-relaxed text-slate-500">
                  {current.verify_detail}
                </p>
              ) : null}
              {current.verified_at ? (
                <p className="font-mono text-[9px] text-slate-600">
                  {formatDateTime(current.verified_at)}
                </p>
              ) : null}
            </div>
            {error ? <ErrorBox message={error} /> : null}
            <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
              <Button variant="secondary" onClick={onClose}>
                Cerrar
              </Button>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}

// Restauración de ficheros: sobre el original, en otra máquina o en una ruta nueva.
function RestoreModal({
  token,
  rp,
  agents,
  onClose,
}: {
  token: string;
  rp: RestorePoint;
  agents: AgentList["items"];
  onClose: () => void;
}) {
  const [targetAgent, setTargetAgent] = useState(rp.agent_id);
  const [destination, setDestination] = useState("");
  const [items, setItems] = useState("");
  const [inPlace, setInPlace] = useState(false);
  const [overwrite, setOverwrite] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<RestoreResponse | null>(null);

  const itemList = items
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  async function submit() {
    if (!inPlace && !destination.trim()) {
      setError("Indica una ruta de destino absoluta en el equipo destino.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const out = await api<RestoreResponse>("/api/v1/admin/restores/files", token, {
        method: "POST",
        body: JSON.stringify({
          restore_point_id: rp.id,
          target_agent_id: targetAgent || undefined,
          items: itemList.length > 0 ? itemList : undefined,
          destination: inPlace ? undefined : destination.trim(),
          in_place: inPlace,
          overwrite,
        }),
      });
      setResult(out);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo lanzar la restauración.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal kicker="Restauración de ficheros" title={`Restaurar ${rp.source_path}`} onClose={onClose} wide>
      <div className="mt-6 space-y-5">
        {result ? (
          <div className="space-y-4">
            <div className="border border-signal/30 bg-signal/5 p-4">
              <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal">
                Restauración en cola
              </p>
              <p className="mt-2 text-sm text-slate-300">
                El agente ejecutará la restauración como job{" "}
                <span className="font-mono text-ice">{shortId(result.job_id)}</span> con estado{" "}
                <span className="font-mono">{result.state}</span>.
              </p>
            </div>
            <div className="flex items-center justify-between border-t border-ink-700 pt-5">
              <a
                href="/jobs"
                className="font-mono text-[10px] uppercase tracking-widest2 text-slate-500 transition hover:text-signal"
              >
                Ver en trabajos →
              </a>
              <Button variant="secondary" onClick={onClose}>
                Cerrar
              </Button>
            </div>
          </div>
        ) : (
          <>
            <div className="border border-ink-700 p-4 font-mono text-[11px]">
              <p className="text-slate-300">{rp.hostname || shortId(rp.agent_id)} · {rp.source_path}</p>
              <p className="mt-1 text-slate-500">
                snapshot {shortId(rp.snapshot_id)} · {formatBytes(rp.size_bytes)} · {formatDateTime(rp.created_at)}
              </p>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Equipo destino" hint="Por defecto, el equipo de origen.">
                <Select value={targetAgent} onChange={(e) => setTargetAgent(e.target.value)}>
                  {agents.some((a) => a.id === rp.agent_id) ? (
                    <option value={rp.agent_id}>
                      {rp.hostname || shortId(rp.agent_id)} (origen)
                    </option>
                  ) : null}
                  {agents
                    .filter((a) => a.id !== rp.agent_id)
                    .map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.hostname} · {a.status}
                      </option>
                    ))}
                </Select>
              </Field>
              <Field label="Ruta destino" hint="Ruta absoluta en el equipo destino.">
                <Input
                  value={destination}
                  onChange={(e) => setDestination(e.target.value)}
                  placeholder="/srv/restore"
                  disabled={inPlace}
                  spellCheck={false}
                  className="font-mono"
                />
              </Field>
            </div>

            <Field
              label="Elementos (opcional)"
              hint="Rutas relativas dentro del snapshot, una por línea. Vacío = restaurar todo."
            >
              <Textarea
                value={items}
                onChange={(e) => setItems(e.target.value)}
                placeholder={"docs/contrato.pdf\nfotos/2024"}
                rows={3}
                className="font-mono"
              />
            </Field>

            <div className="space-y-3 border border-ink-700 p-4">
              <Check
                label="Restaurar en el lugar original"
                hint="Sobrescribe los archivos originales; se ignora la ruta destino."
                checked={inPlace}
                onChange={setInPlace}
              />
              {inPlace ? (
                <p className="border border-alert/30 bg-alert/5 px-3 py-2 font-mono text-[10px] uppercase tracking-widest2 text-alert">
                  Atención: sobrescribe los archivos originales
                </p>
              ) : null}
              <Check
                label="Sobrescribir existentes"
                hint="Sin esto, el agente no reemplaza ficheros que ya existen en el destino."
                checked={overwrite}
                onChange={setOverwrite}
              />
            </div>

            {error ? <ErrorBox message={error} /> : null}

            <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
              <Button variant="quiet" onClick={onClose}>
                Cancelar
              </Button>
              <Button onClick={submit} loading={busy}>
                Lanzar restauración
              </Button>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}

const TYPE_GLYPH: Record<string, { glyph: string; className: string }> = {
  dir: { glyph: "▸", className: "text-ice" },
  file: { glyph: "▪", className: "text-slate-400" },
  symlink: { glyph: "↪", className: "text-alert" },
  other: { glyph: "○", className: "text-slate-600" },
};

// Explorador del snapshot: navegación por breadcrumb y descarga autenticada.
function ExplorerDrawer({
  token,
  rp,
  onClose,
}: {
  token: string;
  rp: RestorePoint;
  onClose: () => void;
}) {
  const [path, setPath] = useState("");
  const [listing, setListing] = useState<FileListing | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [downloading, setDownloading] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [results, setResults] = useState<FileSearchResult | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    setResults(null);
    try {
      const qs = path ? `?path=${encodeURIComponent(path)}` : "";
      const out = await api<FileListing>(`/api/v1/admin/restore-points/${rp.id}/files${qs}`, token);
      setListing(out);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo listar el directorio.");
    } finally {
      setLoading(false);
    }
  }, [path, rp.id, token]);

  useEffect(() => {
    load();
  }, [load]);

  async function search(e?: React.FormEvent) {
    e?.preventDefault();
    const q = query.trim();
    if (!q) {
      setResults(null);
      return;
    }
    setSearching(true);
    setError(null);
    try {
      const base = path ? `&path=${encodeURIComponent(path)}` : "";
      const out = await api<FileSearchResult>(
        `/api/v1/admin/restore-points/${rp.id}/files/search?q=${encodeURIComponent(q)}${base}`,
        token,
      );
      setResults(out);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo buscar.");
    } finally {
      setSearching(false);
    }
  }

  async function downloadZip() {
    const dir = listing?.path ?? path;
    const name = dir ? `${dir.split("/").pop()}.zip` : "snapshot.zip";
    setDownloading(dir || "/");
    setError(null);
    try {
      const blob = await apiDownload(
        `/api/v1/admin/restore-points/${rp.id}/files/download?path=${encodeURIComponent(dir)}`,
        token,
      );
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 4000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo descargar el directorio.");
    } finally {
      setDownloading(null);
    }
  }

  async function download(entry: FileEntry) {
    setDownloading(entry.path);
    setError(null);
    try {
      const blob = await apiDownload(
        `/api/v1/admin/restore-points/${rp.id}/files/content?path=${encodeURIComponent(entry.path)}`,
        token,
      );
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = entry.name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 4000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo descargar el fichero.");
    } finally {
      setDownloading(null);
    }
  }

  const entries = useMemo(() => {
    const list = listing?.entries ? [...listing.entries] : [];
    list.sort((a, b) => {
      const dirA = a.type === "dir" ? 0 : 1;
      const dirB = b.type === "dir" ? 0 : 1;
      if (dirA !== dirB) return dirA - dirB;
      return a.name.localeCompare(b.name);
    });
    return list;
  }, [listing]);

  const segments = (listing?.path ?? path).split("/").filter(Boolean);

  return (
    <div
      className="fixed inset-0 z-50 flex justify-end bg-ink-950/80 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Explorador del snapshot"
        className="panel flex h-full w-full max-w-3xl flex-col border-l border-ink-700"
      >
        <div className="flex items-start justify-between gap-4 border-b border-ink-700 px-5 py-4">
          <div className="min-w-0">
            <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
              Explorador de copia
            </p>
            <h2 className="mt-1 truncate font-display text-2xl uppercase tracking-wide text-slate-100">
              {rp.hostname || shortId(rp.agent_id)} · {rp.source_path}
            </h2>
            <p className="mt-1 font-mono text-[10px] text-slate-600">
              snapshot {shortId(rp.snapshot_id)} · {formatDateTime(rp.created_at)} ·{" "}
              {formatBytes(rp.size_bytes)}
            </p>
          </div>
          <Button variant="ghost" size="sm" onClick={onClose} aria-label="Cerrar explorador">
            Cerrar
          </Button>
        </div>

        <div className="flex flex-wrap items-center gap-2 border-b border-ink-700 px-5 py-3 font-mono text-[11px]">
          <div className="flex flex-wrap items-center gap-1">
            <button
              onClick={() => {
                setQuery("");
                setResults(null);
                setPath("");
              }}
              className={cx(
                "uppercase tracking-widest2 transition hover:text-signal",
                segments.length === 0 && !results ? "text-signal" : "text-slate-400",
              )}
            >
              raíz
            </button>
            {segments.map((seg, i) => {
              const target = segments.slice(0, i + 1).join("/");
              const isLast = i === segments.length - 1;
              return (
                <span key={target} className="flex items-center gap-1">
                  <span className="text-slate-600">/</span>
                  <button
                    onClick={() => {
                      setQuery("");
                      setResults(null);
                      setPath(target);
                    }}
                    className={cx(
                      "transition hover:text-signal",
                      isLast && !results ? "text-signal" : "text-slate-400",
                    )}
                  >
                    {seg}
                  </button>
                </span>
              );
            })}
          </div>
          <form onSubmit={search} className="ml-auto flex items-center gap-2">
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="buscar en el snapshot…"
              aria-label="Buscar ficheros por nombre"
              className="w-44 border border-ink-600 bg-ink-900 px-2 py-1 text-[11px] text-slate-200 placeholder:text-slate-600 focus:border-signal focus:outline-none"
            />
            <Button size="sm" variant="ghost" type="submit" loading={searching}>
              Buscar
            </Button>
            <Button
              size="sm"
              variant="ghost"
              type="button"
              loading={downloading === (listing?.path ?? path)}
              onClick={downloadZip}
              title="Descargar este directorio en ZIP"
            >
              ZIP
            </Button>
          </form>
        </div>

        {results ? (
          <div className="border-b border-ink-700 bg-white/[0.02] px-5 py-3 font-mono text-[11px]">
            <div className="flex items-center justify-between gap-3">
              <span className="text-slate-300">
                {results.items.length} resultado(s) para «{results.query}»
                {results.truncated ? " · recorrido truncado" : ""}
              </span>
              <button
                onClick={() => setResults(null)}
                className="uppercase tracking-widest2 text-slate-400 transition hover:text-signal"
              >
                volver al directorio
              </button>
            </div>
            {results.items.length > 0 ? (
              <ul className="mt-2 space-y-1">
                {results.items.map((item) => (
                  <li key={item.path} className="flex items-center justify-between gap-3">
                    <span className="truncate text-slate-300" title={item.path}>
                      {item.type === "dir" ? "▾ " : ""}
                      {item.path}
                    </span>
                    {item.type !== "dir" && item.type !== "symlink" ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        loading={downloading === item.path}
                        onClick={() => download(item)}
                      >
                        Descargar
                      </Button>
                    ) : item.type === "dir" ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          setQuery("");
                          setResults(null);
                          setPath(item.path);
                        }}
                      >
                        Abrir
                      </Button>
                    ) : null}
                  </li>
                ))}
              </ul>
            ) : null}
          </div>
        ) : null}

        <div className="scroll-thin flex-1 overflow-y-auto px-2 py-2">
          {error ? (
            <div className="p-3">
              <ErrorBox message={error} onRetry={load} />
            </div>
          ) : loading ? (
            <div className="flex items-center justify-center py-16">
              <span className="h-6 w-6 animate-spin border border-signal border-t-transparent" />
            </div>
          ) : entries.length === 0 ? (
            <EmptyState
              index="00 / DIR"
              title="Directorio vacío"
              description="Este directorio no contiene elementos en el snapshot."
            />
          ) : (
            <table className="w-full border-collapse text-left">
              <tbody>
                {entries.map((entry) => {
                  const icon = TYPE_GLYPH[entry.type] ?? TYPE_GLYPH.other;
                  const isDir = entry.type === "dir";
                  return (
                    <tr
                      key={entry.path}
                      className="border-b border-ink-800/80 text-sm transition hover:bg-white/[0.025]"
                    >
                      <td className="w-8 px-3 py-2.5 text-center">
                        <span className={cx("font-mono", icon.className)} aria-hidden>
                          {icon.glyph}
                        </span>
                      </td>
                      <td className="px-2 py-2.5">
                        {isDir ? (
                          <button
                            onClick={() => setPath(entry.path)}
                            className="font-mono text-[12px] text-slate-200 transition hover:text-signal"
                            title={entry.path}
                          >
                            {entry.name}
                          </button>
                        ) : (
                          <span
                            className="font-mono text-[12px] text-slate-300"
                            title={entry.path}
                          >
                            {entry.name}
                          </span>
                        )}
                        {entry.mode ? (
                          <span className="ml-2 font-mono text-[9px] text-slate-600">{entry.mode}</span>
                        ) : null}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[10px] text-slate-500">
                        {isDir
                          ? entry.files
                            ? `${entry.files} elementos`
                            : "—"
                          : formatBytes(entry.size)}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[10px] text-slate-600">
                        {formatDateTime(entry.mtime)}
                      </td>
                      <td className="px-3 py-2.5 text-right">
                        {!isDir && entry.type !== "symlink" ? (
                          <Button
                            size="sm"
                            variant="ghost"
                            loading={downloading === entry.path}
                            onClick={() => download(entry)}
                          >
                            Descargar
                          </Button>
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        <div className="flex items-center justify-between border-t border-ink-700 px-5 py-3 font-mono text-[9px] uppercase tracking-[0.18em] text-slate-600">
          <span>{entries.length} elemento(s)</span>
          <span>Descarga vía plano de control · autenticada</span>
        </div>
      </div>
    </div>
  );
}
