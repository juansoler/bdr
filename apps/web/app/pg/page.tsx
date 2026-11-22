"use client";

import { useCallback, useEffect, useState } from "react";
import { Button, ErrorBox, Field, Input, cx } from "@/components/ui";
import { Modal } from "@/components/Modal";
import { ApiError, api, type AgentList, type RestorePointViewList, type StorageTargetList, type StorageTargetView } from "@/lib/api";
import { useAuth } from "@/lib/auth";

interface AgentSummary {
  id: string;
  hostname: string;
}

// Base del inventario (contracts.PGDatabaseView).
interface PGDBView {
  name: string;
  owner?: string;
  is_system: boolean;
  connect: boolean;
  dumpable: boolean;
}

interface PGPoint {
  id: string;
  kind: string;
  snapshot_id: string;
  created_at: string;
  size_bytes?: number;
  verify_state?: string;
  pg?: {
    host: string;
    port?: string;
    database: string;
    repo_uri?: string;
    verified: boolean;
    size_bytes?: number;
  };
}

const fmtSize = (b?: number) => {
  if (!b) return "—";
  if (b < 1 << 30) return `${(b / (1 << 20)).toFixed(1)} MB`;
  return `${Math.round(b / (1 << 30))} GiB`;
};

export default function PGPage() {
  const { token } = useAuth();
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [targets, setTargets] = useState<StorageTargetView[]>([]);
  const [points, setPoints] = useState<PGPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  // Conexión e inventario (el selector de bases se puebla del inventario).
  const [agentId, setAgentId] = useState("");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("");
  const [user, setUser] = useState("");
  const [database, setDatabase] = useState("");
  const [targetId, setTargetId] = useState("");
  const [retentionDays, setRetentionDays] = useState("0");
  const [dbList, setDbList] = useState<PGDBView[]>([]);
  const [dbBusy, setDbBusy] = useState(false);

  // Detección de instancias PG del agente (PG_DISCOVER): rellena el puerto.
  const [pgInstances, setPgInstances] = useState<{ port?: string; service?: string }[]>([]);

  // Asistente de restauración.
  const [restoreFor, setRestoreFor] = useState<PGPoint | null>(null);
  const [dstAgentId, setDstAgentId] = useState("");
  const [dstHost, setDstHost] = useState("");
  const [dstPort, setDstPort] = useState("");
  const [dstUser, setDstUser] = useState("");
  const [dstDB, setDstDB] = useState("");
  const [dstCreate, setDstCreate] = useState(true);
  const [dstDrop, setDstDrop] = useState(false);
  const [restoreConfirm, setRestoreConfirm] = useState(false);

  const load = useCallback(async () => {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      const [ag, tg, rp] = await Promise.all([
        api<AgentList>("/api/v1/admin/agents", token),
        api<StorageTargetList>("/api/v1/admin/storage/targets", token),
        api<RestorePointViewList>("/api/v1/admin/pg/restore-points", token),
      ]);
      setAgents(ag.items ?? []);
      setTargets(tg.items ?? []);
      setPoints((rp.items ?? []).filter((r) => r.kind === "pg") as PGPoint[]);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo cargar la información PostgreSQL.");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    load();
  }, [load]);

  const act = async (key: string, fn: () => Promise<void>) => {
    setBusy(key);
    setError(null);
    setNotice(null);
    try {
      await fn();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Operación fallida.");
    } finally {
      setBusy(null);
    }
  };

  // Al elegir agente: PG_DISCOVER con polling corto; rellena el puerto si
  // el operador no lo ha tocado.
  useEffect(() => {
    if (!token || !agentId) {
      setPgInstances([]);
      return;
    }
    (async () => {
      try {
        const res = await api<{ job_id: string }>("/api/v1/admin/pg/discover", token, {
          method: "POST",
          body: JSON.stringify({ agent_id: agentId }),
        });
        const jobURL = `/api/v1/admin/jobs/${res.job_id}`;
        for (;;) {
          await new Promise((r) => setTimeout(r, 2000));
          const job = await api<{ state: string; result?: string }>(jobURL, token);
          if (["succeeded", "failed", "canceled", "expired"].includes(job.state)) {
            if (job.state === "succeeded" && job.result) {
              const parsed = JSON.parse(job.result) as { items?: { port?: string; service?: string }[] };
              setPgInstances(parsed.items ?? []);
              if ((parsed.items ?? []).length && !port) {
                const first = (parsed.items ?? [])[0];
                if (first?.port) setPort(first.port);
              }
            }
            break;
          }
        }
      } catch {
        /* best-effort */
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, token]);

  const runDump = () =>
    act("dump", async () => {
      if (!agentId || !database || !targetId) {
        setError("Elige agente, destino y base de datos.");
        return;
      }
      const res = await api<{ job_id: string }>("/api/v1/admin/pg/dump", token, {
        method: "POST",
        body: JSON.stringify({
          agent_id: agentId, host, port, user, database,
          storage_target_id: targetId, retention_days: Number(retentionDays) || 0,
        }),
      });
      setNotice(`Dump lanzado: job ${res.job_id}.`);
      setTimeout(load, 4000);
    });

  const runInventory = () =>
    act("inventory", async () => {
      if (!agentId) {
        setError("Elige un agente.");
        return;
      }
      setDbBusy(true);
      const res = await api<{ job_id: string }>("/api/v1/admin/pg/inventory", token, {
        method: "POST",
        body: JSON.stringify({ agent_id: agentId, host, port, user }),
      });
      setNotice(`Inventario lanzado: job ${res.job_id}.`);
      // El resultado queda en el detalle del job; el operador puede abrirlo.
      setTimeout(() => setDbBusy(false), 1000);
    });

  const openRestore = (p: PGPoint) => {
    setRestoreFor(p);
    setDstAgentId(agentId);
    setDstHost(host);
    setDstPort(port);
    setDstUser(user);
    setDstDB(p.pg?.database ?? "");
    setDstCreate(true);
    setDstDrop(false);
    setRestoreConfirm(false);
    setError(null);
  };

  const launchRestore = () =>
    act("restore", async () => {
      if (!restoreFor?.pg?.repo_uri) return;
      if (!dstAgentId || !dstDB) {
        setError("Elige agente y base de destino.");
        return;
      }
      if (!restoreConfirm) {
        setError("Confirma la restauración.");
        return;
      }
      const res = await api<{ job_id: string }>("/api/v1/admin/pg/restore", token, {
        method: "POST",
        body: JSON.stringify({
          agent_id: dstAgentId, host: dstHost, port: dstPort, user: dstUser,
          database: dstDB, create_db: dstCreate, drop_existing: dstDrop,
          repo_uri: restoreFor.pg.repo_uri,
        }),
      });
      setNotice(`Restauración lanzada: job ${res.job_id}.`);
      setRestoreFor(null);
    });

  return (
    <div>
      <h1 className="font-display text-3xl uppercase tracking-wide text-slate-100">PostgreSQL</h1>
      <p className="mt-1 font-mono text-[10px] uppercase tracking-[0.28em] text-slate-500">
        pg_dump custom · verificación pg_restore --list · restauración con createdb
      </p>

      {notice ? (
        <div className="mt-4 border border-emerald-600/50 bg-emerald-950/40 px-3 py-2 font-mono text-xs text-emerald-300">
          {notice}
        </div>
      ) : null}
      {error ? (
        <div className="mt-4"><ErrorBox message={error} /></div>
      ) : null}

      <div className="panel mt-6 p-5">
        <h2 className="font-display text-lg uppercase tracking-wide text-slate-100">Operaciones</h2>
        <p className="mt-1 font-mono text-[10px] text-slate-600">
          La contraseña va en la variable de entorno BDR_PG_PASSWORD del servicio del agente (nunca viaja por la API).
        </p>
        <div className="mt-4 grid gap-4 md:grid-cols-3">
          <Field label="Agente (host PG)">
            <select value={agentId} onChange={(e) => setAgentId(e.target.value)}
              className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none">
              <option value="">— elige agente —</option>
              {agents.map((a) => <option key={a.id} value={a.id}>{a.hostname}</option>)}
            </select>
          </Field>
          <Field label="Host" hint="vacío = socket local">
            <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="localhost" />
          </Field>
          <Field label="Puerto" hint={pgInstances.length
            ? `instancias detectadas: ${pgInstances.map((i) => `${i.service ?? "?"}${i.port ? ":" + i.port : ""}`).join(", ")}`
            : "elige agente para autodetectar"}>
            <Input value={port} onChange={(e) => setPort(e.target.value)} placeholder="5432" />
          </Field>
          <Field label="Usuario">
            <Input value={user} onChange={(e) => setUser(e.target.value)} placeholder="postgres" />
          </Field>
          <Field label="Base de datos" hint={dbBusy ? "inventario en marcha…" : "escribe el nombre o usa Inventario"}>
            {dbList.length > 0 ? (
              <select value={database} onChange={(e) => setDatabase(e.target.value)}
                className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none">
                <option value="">— elige base —</option>
                {dbList.map((d) => (
                  <option key={d.name} value={d.name} disabled={!d.dumpable}>
                    {d.name}{d.is_system ? " · sistema" : ""}
                  </option>
                ))}
              </select>
            ) : (
              <Input value={database} onChange={(e) => setDatabase(e.target.value)} placeholder="app" />
            )}
          </Field>
          <Field label="Destino" hint="S3 o filesystem/SMB">
            <select value={targetId} onChange={(e) => setTargetId(e.target.value)}
              className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none">
              <option value="">— elige destino —</option>
              {targets.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name} · {t.kind === "filesystem" ? t.path : `${t.endpoint}/${t.bucket}`}{t.prefix ? "/" + t.prefix : ""}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Retención (días)" hint="Object Lock GOVERNANCE en S3; 0 = la del destino">
            <Input value={retentionDays} onChange={(e) => setRetentionDays(e.target.value)} inputMode="numeric" />
          </Field>
        </div>
        <div className="mt-4 flex flex-wrap gap-2">
          <Button loading={busy === "dump"} onClick={runDump}>Dump ahora</Button>
          <Button variant="ghost" loading={busy === "inventory"} onClick={runInventory}>Inventario</Button>
        </div>
      </div>

      <div className="panel mt-6 p-5">
        <h2 className="font-display text-lg uppercase tracking-wide text-slate-100">Puntos de restauración</h2>
        {loading ? (
          <p className="mt-3 text-sm text-slate-500">Cargando…</p>
        ) : points.length === 0 ? (
          <p className="mt-3 text-sm text-slate-500">Sin dumps aún. Lanza uno arriba.</p>
        ) : (
          <table className="mt-4 w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-ink-700 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                <th className="py-2 pr-4">Fecha</th>
                <th className="py-2 pr-4">Base</th>
                <th className="py-2 pr-4">Tamaño</th>
                <th className="py-2 pr-4">Verif.</th>
                <th className="py-2 pr-4">Acciones</th>
              </tr>
            </thead>
            <tbody>
              {points.map((rp) => (
                <tr key={rp.id} className="border-b border-ink-800/80 text-sm text-slate-300">
                  <td className="py-2 pr-4 font-mono text-xs">{rp.created_at?.slice(0, 16).replace("T", " ")}</td>
                  <td className="py-2 pr-4 font-mono text-signal">{rp.pg?.database ?? "—"}</td>
                  <td className="py-2 pr-4 font-mono text-xs">{fmtSize(rp.size_bytes)}</td>
                  <td className="py-2 pr-4">
                    <span className={cx("font-mono text-xs", rp.pg?.verified ? "text-emerald-400" : "text-slate-500")}>
                      {rp.pg?.verified ? "índice OK" : "sin verificar"}
                    </span>
                  </td>
                  <td className="py-2 pr-4">
                    <div className="flex gap-1.5">
                      <Button size="sm" variant="secondary"
                        loading={busy === `verify-${rp.snapshot_id}`}
                        onClick={() => act(`verify-${rp.snapshot_id}`, async () => {
                          const res = await api<{ job_id: string }>("/api/v1/admin/pg/verify", token, {
                            method: "POST", body: JSON.stringify({ repo_uri: rp.snapshot_id }),
                          });
                          setNotice(`Verificación lanzada: job ${res.job_id}.`);
                        })}>
                        Verificar
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => openRestore(rp)}>Restaurar</Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {restoreFor ? (
        <Modal kicker="PostgreSQL" title={`Restaurar ${restoreFor.pg?.database}`} onClose={() => setRestoreFor(null)} wide>
          <div className="space-y-5">
            <div className="border border-alert/50 bg-alert/10 px-3 py-2 font-mono text-xs text-alert">
              El restore crea o reemplaza la base destino en el motor elegido (createdb + pg_restore).
            </div>
            <div className="grid gap-4 md:grid-cols-2">
              <Field label="Agente destino">
                <select value={dstAgentId} onChange={(e) => setDstAgentId(e.target.value)}
                  className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none">
                  <option value="">— elige agente —</option>
                  {agents.map((a) => <option key={a.id} value={a.id}>{a.hostname}</option>)}
                </select>
              </Field>
              <Field label="Host destino" hint="vacío = socket local del agente">
                <Input value={dstHost} onChange={(e) => setDstHost(e.target.value)} />
              </Field>
              <Field label="Puerto destino">
                <Input value={dstPort} onChange={(e) => setDstPort(e.target.value)} />
              </Field>
              <Field label="Usuario destino">
                <Input value={dstUser} onChange={(e) => setDstUser(e.target.value)} />
              </Field>
              <Field label="Base destino">
                <Input value={dstDB} onChange={(e) => setDstDB(e.target.value)} />
              </Field>
            </div>
            <div className="flex flex-wrap gap-6 font-mono text-xs text-slate-300">
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={dstCreate} onChange={(e) => setDstCreate(e.target.checked)} />
                createdb (motor limpio)
              </label>
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={dstDrop} onChange={(e) => setDstDrop(e.target.checked)} />
                --clean --if-exists (reemplazar contenido)
              </label>
            </div>
            <label className="flex items-center gap-2 font-mono text-xs text-slate-300">
              <input type="checkbox" checked={restoreConfirm} onChange={(e) => setRestoreConfirm(e.target.checked)} />
              Confirmo la restauración en {dstDB || "—"}
            </label>
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setRestoreFor(null)}>Cancelar</Button>
              <Button variant="danger" loading={busy === "restore"} onClick={launchRestore}>Restaurar</Button>
            </div>
          </div>
        </Modal>
      ) : null}
    </div>
  );
}
