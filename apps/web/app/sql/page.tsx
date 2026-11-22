"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Button, ErrorBox, Field, Input, cx } from "@/components/ui";
import { Modal } from "@/components/Modal";
import { ApiError, api, type AgentList, type RestorePointViewList, type StorageTargetList, type StorageTargetView } from "@/lib/api";
import { useAuth } from "@/lib/auth";

// Tipos alineados con contracts.SQLPlanView, contracts.RestorePointSQLView
// y los payloads de los endpoints /api/v1/admin/sql/*.
interface AgentSummary {
  id: string;
  hostname: string;
}

interface SQLPlanView {
  id: string;
  agent_id: string;
  server: string;
  instance?: string;
  database: string;
  type: string;
  schedule: string;
  timezone?: string;
  storage_target_id: string;
  retention_days?: number;
  verify: boolean;
  verify_cron?: string;
  enabled: boolean;
  last_job_id?: string;
  next_run_at?: string | null;
  next_verify_at?: string | null;
}

interface SQLRPView {
  id: string;
  job_id: string;
  kind: string;
  snapshot_id: string; // URI del objeto en el destino
  created_at: string;
  size_bytes?: number;
  verify_state?: string;
  sql?: {
    instance: string;
    database: string;
    backup_type: string;
    database_lsn?: string;
    first_lsn?: string;
    last_lsn?: string;
    verified: boolean;
  };
}

// Base de datos del inventario (contracts.SQLDatabaseView).
interface SQLDBView {
  name: string;
  recovery_model: string;
  state: string;
  read_only?: boolean;
  is_system: boolean;
  protectable: boolean;
}

interface SQLDatabasesResp {
  items: SQLDBView[];
  updated_at?: string | null;
}

// Eslabón de la cadena calculada (contracts.SQLRestoreChainStep).
interface SQLChainStep {
  restore_point_id: string;
  backup_type: string;
  backup_finish: string;
  first_lsn?: string;
  last_lsn?: string;
  repo_uri: string;
  size_bytes?: number;
}

interface SQLChainView {
  database: string;
  valid: boolean;
  error?: string;
  stop_at?: string | null;
  files?: string[];
  steps?: SQLChainStep[];
}

const JOB_TERMINAL = ["succeeded", "failed", "canceled", "expired"];

// Tamaño legible: MB por debajo de 1 GiB (los backups de lab son pequeños
// y "0 GiB" confundía, hallazgo del lab).
const fmtGiB = (b?: number) => {
  if (!b) return "—";
  if (b < 1 << 30) return `${(b / (1 << 20)).toFixed(1)} MB`;
  return `${Math.round(b / (1 << 30))} GiB`;
};

export default function SQLPage() {
  const { token } = useAuth();
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [targets, setTargets] = useState<StorageTargetView[]>([]);
  const [plans, setPlans] = useState<SQLPlanView[]>([]);
  const [rps, setRps] = useState<SQLRPView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  // Formulario compartido por backup manual y creación de plan.
  const [agentId, setAgentId] = useState("");
  const [server, setServer] = useState("");
  const [instance, setInstance] = useState("");
  const [database, setDatabase] = useState("");
  const [type, setType] = useState("FULL");
  const [targetId, setTargetId] = useState("");
  const [retentionDays, setRetentionDays] = useState("0");
  const [verify, setVerify] = useState(true);
  const [schedule, setSchedule] = useState("");
  const [verifyCron, setVerifyCron] = useState("");
  const [timezone, setTimezone] = useState("UTC");

  // Inventario automático: al elegir agente+servidor, carga la caché y si
  // está vacía dispara un job SQL_INVENTORY y espera su resultado.
  const [dbList, setDbList] = useState<SQLDBView[]>([]);
  const [dbUpdatedAt, setDbUpdatedAt] = useState<string | null>(null);
  const [dbBusy, setDbBusy] = useState(false);
  const dbReqId = useRef(0);

  // Asistente de restauración (RF-42): cadena calculada desde el catálogo.
  // El destino puede ser OTRA instancia (p. ej. un motor limpio): agente,
  // servidor e instancia destino propios; la cadena se calcula siempre
  // sobre los puntos del origen.
  const [restoreFor, setRestoreFor] = useState<string | null>(null);
  const [destName, setDestName] = useState("");
  const [dstAgentId, setDstAgentId] = useState("");
  const [dstServer, setDstServer] = useState("");
  const [dstInstance, setDstInstance] = useState("");
  const [stopAt, setStopAt] = useState("");
  const [chain, setChain] = useState<SQLChainView | null>(null);
  const [chainBusy, setChainBusy] = useState(false);
  const [restoreConfirm, setRestoreConfirm] = useState(false);
  const chainReqId = useRef(0);

  const load = useCallback(async () => {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      const [ag, tg, pl, rp] = await Promise.all([
        api<AgentList>("/api/v1/admin/agents", token),
        api<StorageTargetList>("/api/v1/admin/storage/targets", token),
        api<{ items: SQLPlanView[] }>("/api/v1/admin/sql/plans", token),
        api<RestorePointViewList>("/api/v1/admin/sql/restore-points", token),
      ]);
      setAgents(ag.items ?? []);
      setTargets(tg.items ?? []);
      setPlans(pl.items ?? []);
      setRps((rp.items ?? []).filter((r) => r.kind === "sql") as SQLRPView[]);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo cargar la información SQL.");
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

  // Carga y auto-inventario de bases para el nodo elegido, con debounce:
  // solo se dispara 700 ms después de dejar de escribir (cada tecla
  // invalida la petición anterior y su timer). Sin debounce, escribir
  // "LOCALHOST" lanzaba un job SQL_INVENTORY por letra (hallazgo del lab).
  useEffect(() => {
    if (!token) return;
    const reqId = ++dbReqId.current;
    const serverT = server.trim();
    if (!agentId || !serverT) {
      setDbList([]);
      setDbUpdatedAt(null);
      setDbBusy(false);
      return;
    }
    setDbBusy(true);
    const timer = setTimeout(async () => {
      try {
        const qs = new URLSearchParams({ agent_id: agentId, server: serverT });
        if (instance) qs.set("instance", instance);
        let r = await api<SQLDatabasesResp>(`/api/v1/admin/sql/databases?${qs}`, token);
        if (dbReqId.current !== reqId) return;
        if ((r.items ?? []).length === 0) {
          // Sin caché: disparar inventario y esperar el job.
          const res = await api<{ job_id: string }>("/api/v1/admin/sql/inventory", token, {
            method: "POST",
            body: JSON.stringify({ agent_id: agentId, server: serverT, instance }),
          });
          const jobURL = `/api/v1/admin/jobs/${res.job_id}`;
          for (;;) {
            await new Promise((r2) => setTimeout(r2, 2500));
            if (dbReqId.current !== reqId) return;
            const job = await api<{ state: string; error_message?: string }>(jobURL, token);
            if (JOB_TERMINAL.includes(job.state)) {
              if (job.state !== "succeeded") {
                setError(`El inventario falló en el nodo: ${job.error_message ?? job.state}`);
              }
              break;
            }
          }
          if (dbReqId.current !== reqId) return;
          r = await api<SQLDatabasesResp>(`/api/v1/admin/sql/databases?${qs}`, token);
        }
        if (dbReqId.current !== reqId) return;
        setDbList(r.items ?? []);
        setDbUpdatedAt(r.updated_at ?? null);
      } catch (err) {
        if (dbReqId.current === reqId) {
          setError(err instanceof ApiError ? err.message : "No se pudo obtener el inventario de bases.");
          setDbList([]);
        }
      } finally {
        if (dbReqId.current === reqId) setDbBusy(false);
      }
    }, 700);
    return () => clearTimeout(timer);
  }, [agentId, server, instance, token]);

  // Calcula la cadena de restauración para la base abierta en el asistente
  // (RF-42). stopAt vacío = último disponible.
  const loadChain = useCallback(
    async (database: string, cutoff: string, reqId: number) => {
      setChainBusy(true);
      setChain(null);
      try {
        const body: Record<string, unknown> = { database };
        if (agentId) body.agent_id = agentId;
        if (cutoff) body.stop_at = new Date(cutoff).toISOString();
        const v = await api<SQLChainView>("/api/v1/admin/sql/restore-chain", token, {
          method: "POST",
          body: JSON.stringify(body),
        });
        if (chainReqId.current === reqId) setChain(v);
      } catch (err) {
        if (chainReqId.current === reqId) {
          setError(err instanceof ApiError ? err.message : "No se pudo calcular la cadena de restauración.");
        }
      } finally {
        if (chainReqId.current === reqId) setChainBusy(false);
      }
    },
    [agentId, token]
  );

  const openRestore = (database: string) => {
    const reqId = ++chainReqId.current;
    setRestoreFor(database);
    setDestName(database);
    // Destino por defecto: la propia instancia de origen. Cambiando el
    // agente se restaura en otra máquina (p. ej. motor limpio).
    setDstAgentId(agentId);
    setDstServer(server);
    setDstInstance(instance);
    setStopAt("");
    setRestoreConfirm(false);
    setError(null);
    loadChain(database, "", reqId);
  };

  const changeStopAt = (value: string) => {
    const reqId = ++chainReqId.current;
    setStopAt(value);
    if (restoreFor) loadChain(restoreFor, value, reqId);
  };

  const launchRestore = () =>
    act("restore", async () => {
      if (!chain?.valid || !chain.files?.length) {
        setError("La cadena no es válida o está vacía.");
        return;
      }
      if (!dstAgentId || !dstServer) {
        setError("Elige el agente y el servidor de destino.");
        return;
      }
      if (!restoreConfirm) {
        setError("Confirma la restauración: es destructiva sobre la base destino.");
        return;
      }
      const res = await api<{ job_id: string }>("/api/v1/admin/sql/restore", token, {
        method: "POST",
        body: JSON.stringify({
          agent_id: dstAgentId,
          server: dstServer,
          instance: dstInstance,
          database: destName,
          files: chain.files,
          confirm: true, // el checkbox del asistente YA ha recogido la confirmación
          ...(stopAt ? { stop_at: new Date(stopAt).toISOString() } : {}),
        }),
      });
      setNotice(`Restauración lanzada: job ${res.job_id}.`);
      setRestoreFor(null);
    });

  const requireSel = () => {    if (!agentId || !server || !database || !targetId) {
      setError("Elige agente, destino y escribe servidor y base de datos.");
      return false;
    }
    return true;
  };

  const basePayload = () => ({
    agent_id: agentId,
    server,
    instance,
    storage_target_id: targetId,
  });

  const runBackup = () =>
    act("backup", async () => {
      if (!requireSel()) return;
      const res = await api<{ job_id: string }>("/api/v1/admin/sql/backup", token, {
        method: "POST",
        body: JSON.stringify({
          ...basePayload(),
          database,
          type,
          retention_days: Number(retentionDays) || 0,
          verify,
        }),
      });
      setNotice(`Backup lanzado: job ${res.job_id}. Síguelo en Trabajos.`);
    });

  const runInventory = () =>
    act("inventory", async () => {
      if (!agentId || !server) {
        setError("Elige agente y escribe el servidor.");
        return;
      }
      const res = await api<{ job_id: string }>("/api/v1/admin/sql/inventory", token, {
        method: "POST",
        body: JSON.stringify({ agent_id: agentId, server, instance }),
      });
      setNotice(`Inventario lanzado: job ${res.job_id}. El resultado queda en el detalle del job.`);
    });

  const runVerify = (repoURI: string) =>
    act(`verify-${repoURI}`, async () => {
      if (!agentId || !server) {
        setError("Elige agente y servidor arriba para verificar.");
        return;
      }
      const res = await api<{ job_id: string }>("/api/v1/admin/sql/verify", token, {
        method: "POST",
        body: JSON.stringify({ ...basePayload(), repo_uri: repoURI }),
      });
      setNotice(`Verificación lanzada: job ${res.job_id}.`);
    });

  const runCheckDB = () =>
    act("checkdb", async () => {
      if (!agentId || !server || !database) {
        setError("Elige agente y escribe servidor y base de datos.");
        return;
      }
      const res = await api<{ job_id: string }>("/api/v1/admin/sql/checkdb", token, {
        method: "POST",
        body: JSON.stringify({ agent_id: agentId, server, instance, database }),
      });
      setNotice(`DBCC CHECKDB lanzado: job ${res.job_id}. Si hay corrupción, el job falla y salta alerta.`);
    });

  const createPlan = () =>
    act("plan", async () => {
      if (!requireSel()) return;
      if (!schedule) {
        setError("El plan requiere horario (cron).");
        return;
      }
      await api("/api/v1/admin/sql/plans", token, {
        method: "POST",
        body: JSON.stringify({
          ...basePayload(),
          database,
          type,
          schedule,
          timezone,
          verify_cron: verifyCron,
          retention_days: Number(retentionDays) || 0,
          verify,
        }),
      });
      setNotice("Plan creado.");
      load();
    });

  const togglePlan = (p: SQLPlanView) =>
    act(`toggle-${p.id}`, async () => {
      await api(`/api/v1/admin/sql/plans/${p.id}`, token, {
        method: "PUT",
        body: JSON.stringify({ enabled: !p.enabled }),
      });
      load();
    });

  const deletePlan = (p: SQLPlanView) =>
    act(`del-${p.id}`, async () => {
      await api(`/api/v1/admin/sql/plans/${p.id}`, token, { method: "DELETE" });
      load();
    });

  return (
    <div>
      <h1 className="font-display text-3xl uppercase tracking-wide text-slate-100">SQL Server</h1>
      <p className="mt-1 font-mono text-[10px] uppercase tracking-[0.28em] text-slate-500">
        Backups nativos · planes programados · PITR por LSN
      </p>

      {notice ? (
        <div className="mt-4 border border-emerald-600/50 bg-emerald-950/40 px-3 py-2 font-mono text-xs text-emerald-300">
          {notice}
        </div>
      ) : null}
      {error ? (
        <div className="mt-4">
          <ErrorBox message={error} />
        </div>
      ) : null}

      {/* --- Backup manual / inventory --- */}
      <div className="panel mt-6 p-5">
        <h2 className="font-display text-lg uppercase tracking-wide text-slate-100">Operaciones</h2>
        <div className="mt-4 grid gap-4 md:grid-cols-3">
          <Field label="Agente (host SQL)">
            <select
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
              className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
            >
              <option value="">— elige agente —</option>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>{a.hostname}</option>
              ))}
            </select>
          </Field>
          <Field label="Servidor" hint="host o host\instancia">
            <Input value={server} onChange={(e) => setServer(e.target.value)} placeholder="SQLVM01\MSSQLSERVER" />
          </Field>
          <Field label="Instancia (opcional)">
            <Input value={instance} onChange={(e) => setInstance(e.target.value)} placeholder="SQLEXPRESS" />
          </Field>
          <Field
            label="Base de datos"
            hint={
              dbBusy
                ? "obteniendo inventario del nodo…"
                : dbList.length
                  ? `${dbList.length} bases · inventario ${dbUpdatedAt?.slice(0, 16).replace("T", " ")}`
                  : "elige agente y servidor para listar las bases"
            }
          >
            {dbList.length > 0 ? (
              <select
                value={database}
                onChange={(e) => setDatabase(e.target.value)}
                className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
              >
                <option value="">— elige base —</option>
                {dbList.map((d) => (
                  <option key={d.name} value={d.name}>
                    {d.name} · {d.recovery_model} · {d.state}
                    {d.is_system ? " · sistema" : ""}
                  </option>
                ))}
              </select>
            ) : (
              <Input value={database} onChange={(e) => setDatabase(e.target.value)} placeholder="app" />
            )}
          </Field>
          <Field label="Tipo">
            <select
              value={type}
              onChange={(e) => setType(e.target.value)}
              className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
            >
              <option value="FULL">FULL</option>
              <option value="DIFF">DIFFERENTIAL</option>
              <option value="LOG">LOG</option>
              <option value="COPY_ONLY">COPY_ONLY</option>
            </select>
          </Field>
          <Field label="Destino" hint="S3 o filesystem/SMB">
            <select
              value={targetId}
              onChange={(e) => setTargetId(e.target.value)}
              className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
            >
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
          <Field label="Horario (cron)" hint="solo para el plan programado">
            <Input value={schedule} onChange={(e) => setSchedule(e.target.value)} placeholder="0 3 * * *" />
          </Field>
          <Field label="Verify cron (opcional)" hint="RF-44: VERIFYONLY periódico del último backup">
            <Input value={verifyCron} onChange={(e) => setVerifyCron(e.target.value)} placeholder="0 5 * * 6" />
          </Field>
          <Field label="Zona horaria">
            <Input value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="Europe/Madrid" />
          </Field>
        </div>
        <label className="mt-3 flex items-center gap-2 font-mono text-xs text-slate-400">
          <input type="checkbox" checked={verify} onChange={(e) => setVerify(e.target.checked)} />
          Verificar el medio tras el backup (RESTORE VERIFYONLY)
        </label>
        <div className="mt-4 flex flex-wrap gap-2">
          <Button loading={busy === "backup"} onClick={runBackup}>Backup ahora</Button>
          <Button variant="secondary" loading={busy === "plan"} onClick={createPlan}>Crear plan programado</Button>
          <Button variant="secondary" loading={busy === "checkdb"} onClick={runCheckDB}>CheckDB</Button>
          <Button variant="ghost" loading={busy === "inventory"} onClick={runInventory}>Inventario</Button>
        </div>
      </div>

      {/* --- Planes programados --- */}
      <div className="panel mt-6 p-5">
        <h2 className="font-display text-lg uppercase tracking-wide text-slate-100">Planes programados</h2>
        {loading ? (
          <p className="mt-3 text-sm text-slate-500">Cargando…</p>
        ) : plans.length === 0 ? (
          <p className="mt-3 text-sm text-slate-500">Sin planes. Crea uno arriba con un horario cron.</p>
        ) : (
          <table className="mt-4 w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-ink-700 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                <th className="py-2 pr-4">Base</th>
                <th className="py-2 pr-4">Tipo</th>
                <th className="py-2 pr-4">Horario</th>
                <th className="py-2 pr-4">Próxima</th>
                <th className="py-2 pr-4">Estado</th>
                <th className="py-2 pr-4">Acciones</th>
              </tr>
            </thead>
            <tbody>
              {plans.map((p) => (
                <tr key={p.id} className="border-b border-ink-800/80 text-sm text-slate-300">
                  <td className="py-2 pr-4">
                    <span className="font-mono text-signal">{p.database}</span>
                    <span className="ml-2 font-mono text-[10px] text-slate-500">{p.server}{p.instance ? `\\${p.instance}` : ""}</span>
                  </td>
                  <td className="py-2 pr-4 font-mono text-xs">{p.type}</td>
                  <td className="py-2 pr-4 font-mono text-xs">
                    {p.schedule}
                    {p.timezone ? <span className="ml-1 text-slate-500">{p.timezone}</span> : null}
                  </td>
                  <td className="py-2 pr-4 font-mono text-xs">
                    {p.next_run_at ? p.next_run_at.slice(0, 16).replace("T", " ") : "—"}
                  </td>
                  <td className="py-2 pr-4">
                    <span className={cx("font-mono text-xs", p.enabled ? "text-emerald-400" : "text-slate-500")}>
                      {p.enabled ? "activo" : "pausado"}
                    </span>
                  </td>
                  <td className="py-2 pr-4">
                    <div className="flex gap-1.5">
                      <Button size="sm" variant="ghost" loading={busy === `toggle-${p.id}`} onClick={() => togglePlan(p)}>
                        {p.enabled ? "Pausar" : "Activar"}
                      </Button>
                      <Button size="sm" variant="ghost" loading={busy === `del-${p.id}`} onClick={() => deletePlan(p)}>
                        Eliminar
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* --- Puntos de restauración --- */}
      <div className="panel mt-6 p-5">
        <h2 className="font-display text-lg uppercase tracking-wide text-slate-100">Puntos de restauración</h2>
        {loading ? (
          <p className="mt-3 text-sm text-slate-500">Cargando…</p>
        ) : rps.length === 0 ? (
          <p className="mt-3 text-sm text-slate-500">
            Sin backups SQL aún. Lanza uno arriba o espera al plan programado.
          </p>
        ) : (
          <table className="mt-4 w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-ink-700 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                <th className="py-2 pr-4">Fecha</th>
                <th className="py-2 pr-4">Base</th>
                <th className="py-2 pr-4">Tipo</th>
                <th className="py-2 pr-4">Tamaño</th>
                <th className="py-2 pr-4">Último LSN</th>
                <th className="py-2 pr-4">Verif.</th>
                <th className="py-2 pr-4">Acciones</th>
              </tr>
            </thead>
            <tbody>
              {rps.map((rp) => (
                <tr key={rp.id} className="border-b border-ink-800/80 text-sm text-slate-300">
                  <td className="py-2 pr-4 font-mono text-xs">{rp.created_at?.slice(0, 16).replace("T", " ")}</td>
                  <td className="py-2 pr-4 font-mono text-signal">{rp.sql?.database ?? "—"}</td>
                  <td className="py-2 pr-4 font-mono text-xs">{rp.sql?.backup_type ?? "—"}</td>
                  <td className="py-2 pr-4 font-mono text-xs">{fmtGiB(rp.size_bytes)}</td>
                  <td className="py-2 pr-4 font-mono text-xs" title={rp.snapshot_id}>
                    {rp.sql?.last_lsn ?? "—"}
                  </td>
                  <td className="py-2 pr-4">
                    <span className={cx("font-mono text-xs",
                      rp.sql?.verified ? "text-emerald-400" : "text-slate-500")}>
                      {rp.sql?.verified ? "VERIFYONLY OK" : "sin verificar"}
                    </span>
                  </td>
                  <td className="py-2 pr-4">
                    <div className="flex gap-1.5">
                      <Button size="sm" variant="secondary"
                        loading={busy === `verify-${rp.snapshot_id}`}
                        onClick={() => runVerify(rp.snapshot_id)}>
                        Verificar
                      </Button>
                      <Button size="sm" variant="ghost"
                        onClick={() => rp.sql && openRestore(rp.sql.database)}>
                        Restaurar
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <p className="mt-3 font-mono text-[10px] text-slate-600">
          El restore de una base se lanza desde el asistente (botón Restaurar): el control calcula la cadena
          FULL→DIFF→LOG con continuidad de LSN y la confirma el operador antes de ejecutar.
        </p>
      </div>

      {restoreFor ? (
        <Modal kicker="SQL Server" title={`Restaurar ${restoreFor}`} onClose={() => setRestoreFor(null)} wide>
          <div className="space-y-5">
            <div className="border border-alert/50 bg-alert/10 px-3 py-2 font-mono text-xs text-alert">
              DESTRUCTIVO: el contenido de la base destino se reemplaza. El agente lo ejecuta con la cuenta de
              servicio del host SQL.
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <Field label="Base destino" hint={destName && destName.toLowerCase() !== restoreFor.toLowerCase()
                ? "otro nombre en la misma instancia puede exigir MOVE (rutas físicas libres)"
                : "por defecto, la misma base (restauración in situ)"}>
                <Input value={destName} onChange={(e) => setDestName(e.target.value)} />
              </Field>
              <Field label="Punto en el tiempo (opcional)" hint="vacío = el más reciente del catálogo">
                <Input type="datetime-local" value={stopAt} onChange={(e) => changeStopAt(e.target.value)} />
              </Field>
              <Field label="Agente destino" hint="cámbialo para restaurar en otra máquina (motor limpio: el nombre puede coincidir)">
                <select
                  value={dstAgentId}
                  onChange={(e) => setDstAgentId(e.target.value)}
                  className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
                >
                  <option value="">— elige agente —</option>
                  {agents.map((a) => (
                    <option key={a.id} value={a.id}>{a.hostname}</option>
                  ))}
                </select>
              </Field>
              <Field label="Servidor destino" hint="host o host\\instancia del motor donde restaurar">
                <Input value={dstServer} onChange={(e) => setDstServer(e.target.value)} />
              </Field>
              <Field label="Instancia destino (opcional)">
                <Input value={dstInstance} onChange={(e) => setDstInstance(e.target.value)} />
              </Field>
            </div>
            {dstAgentId && dstAgentId !== agentId ? (
              <p className="font-mono text-[10px] text-ice">
                Restauración cruzada: la cadena proviene del catálogo del origen y se aplica en {dstServer || "el destino"}.
                En un motor limpio no hace falta MOVE; si el destino ya tiene la base, se reemplaza (WITH REPLACE).
              </p>
            ) : null}

            <div>
              <p className="font-mono text-[10px] uppercase tracking-[0.28em] text-slate-500">
                Cadena calculada (FULL → DIFF → LOG)
              </p>
              {chainBusy ? (
                <p className="mt-2 text-sm text-slate-500">Calculando cadena…</p>
              ) : chain && !chain.valid ? (
                <div className="mt-2"><ErrorBox message={chain.error ?? "La cadena no es válida."} /></div>
              ) : chain?.steps?.length ? (
                <table className="mt-2 w-full border-collapse text-left">
                  <thead>
                    <tr className="border-b border-ink-700 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                      <th className="py-1.5 pr-3">Tipo</th>
                      <th className="py-1.5 pr-3">Fin</th>
                      <th className="py-1.5 pr-3">Último LSN</th>
                      <th className="py-1.5 pr-3">Tamaño</th>
                    </tr>
                  </thead>
                  <tbody>
                    {chain.steps.map((st, i) => (
                      <tr key={st.restore_point_id} className="border-b border-ink-800/80 text-sm text-slate-300">
                        <td className="py-1.5 pr-3">
                          <span className="font-mono text-signal">{i + 1}. {st.backup_type}</span>
                        </td>
                        <td className="py-1.5 pr-3 font-mono text-xs">
                          {st.backup_finish?.slice(0, 16).replace("T", " ")}
                        </td>
                        <td className="py-1.5 pr-3 font-mono text-xs">{st.last_lsn || "—"}</td>
                        <td className="py-1.5 pr-3 font-mono text-xs">{fmtGiB(st.size_bytes)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : null}
            </div>

            <label className="flex items-center gap-2 font-mono text-xs text-slate-300">
              <input type="checkbox" checked={restoreConfirm} onChange={(e) => setRestoreConfirm(e.target.checked)} />
              Confirmo la restauración de {restoreFor} → {destName || "—"}
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
