"use client";

import { useCallback, useEffect, useState } from "react";
import { Button, ErrorBox, Field, Input, cx } from "@/components/ui";
import {
  ApiError,
  api,
  type AgentList,
  type PVEInstanceSummary,
  type PVEVMList,
  type RestorePointViewList,
  type StorageTargetList,
  type StorageTargetView,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";

interface AgentSummary {
  id: string;
  hostname: string;
  agent_version?: string;
  last_seen_at?: string;
}

export default function ProxmoxPage() {
  const { token } = useAuth();
  const [instances, setInstances] = useState<PVEInstanceSummary[]>([]);
  const [vms, setVms] = useState<Record<string, PVEVMList | null>>({});
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [targets, setTargets] = useState<StorageTargetView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // Destino de backups seleccionado por instancia.
  const [targetSel, setTargetSel] = useState<Record<string, string>>({});
  // Puntos de restauración por VM/LXC (clave instanceId/vmid).
  const [rps, setRps] = useState<Record<string, RestorePointViewList | null>>({});
  const [restoreFor, setRestoreFor] = useState<{ rp: string; vmid: number; kind: string } | null>(null);
  const [restoreMode, setRestoreMode] = useState<"in_place" | "new">("in_place");
  const [restoreTargetInstance, setRestoreTargetInstance] = useState("");
  const [restoreName, setRestoreName] = useState("");
  const [restoreVMID, setRestoreVMID] = useState("");

  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [tokenUser, setTokenUser] = useState("root@pam");
  const [tokenId, setTokenId] = useState("bdr");
  const [tokenSecret, setTokenSecret] = useState("");
  const [verifyTLS, setVerifyTLS] = useState(false);
  const [agentId, setAgentId] = useState("");

  const load = useCallback(async () => {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      const [inst, ag, tg] = await Promise.all([
        api<{ items: PVEInstanceSummary[] }>("/api/v1/admin/proxmox/instances", token),
        api<AgentList>("/api/v1/admin/agents", token),
        api<StorageTargetList>("/api/v1/admin/storage/targets", token),
      ]);
      setInstances(inst.items);
      setAgents((ag.items ?? []).map((a) => ({
        id: a.id, hostname: a.hostname, agent_version: a.agent_version, last_seen_at: a.last_seen_at,
      })));
      setTargets(tg.items ?? []);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudieron cargar las instancias.");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    if (token) load();
  }, [token, load]);

  async function loadVMs(instanceId: string) {
    setBusy(instanceId);
    setError(null);
    try {
      const out = await api<PVEVMList>(
        `/api/v1/admin/proxmox/instances/${instanceId}/vms`,
        token,
      );
      setVms((prev) => ({ ...prev, [instanceId]: out }));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo consultar Proxmox.");
    } finally {
      setBusy(null);
    }
  }

  async function saveInstance(e: React.FormEvent) {
    e.preventDefault();
    setBusy("save");
    setError(null);
    const body: Record<string, unknown> = {
      name, url,
      token_user: tokenUser, token_id: tokenId,
      verify_tls: verifyTLS,
      agent_id: agentId || undefined,
    };
    if (tokenSecret) body.token_secret = tokenSecret;
    try {
      await api(
        editingId ? `/api/v1/admin/proxmox/instances/${editingId}` : "/api/v1/admin/proxmox/instances",
        token, { method: editingId ? "PUT" : "POST", body: JSON.stringify(body) },
      );
      setShowAdd(false);
      setEditingId(null);
      setNotice("Instancia guardada.");
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo guardar la instancia.");
    } finally {
      setBusy(null);
    }
  }

  function startEdit(id: string) {
    const inst = instances.find((i) => i.instance_id === id);
    if (!inst) return;
    setEditingId(id);
    setShowAdd(true);
    setError(null);
    setName(inst.name);
    setUrl(inst.url);
    setTokenSecret("");
    setAgentId((inst as unknown as { agent_id?: string }).agent_id ?? "");
  }

  async function backupGuest(instanceId: string, vmid: number, name: string, type: string) {
    const storage = targetSel[instanceId];
    if (!storage) {
      setError("Elige un destino de almacenamiento para los backups de VMs y LXC.");
      return;
    }
    setBusy(`bk-${instanceId}-${vmid}`);
    setError(null);
    const base = type === "lxc"
      ? `/api/v1/admin/proxmox/instances/${instanceId}/lxc/${vmid}/backup`
      : `/api/v1/admin/proxmox/instances/${instanceId}/vms/${vmid}/backup`;
    try {
      const out = await api<{ job_id: string }>(base, token,
        { method: "POST", body: JSON.stringify({ storage_target_id: storage }) });
      const what = type === "lxc" ? `del LXC ${name || vmid}` : `de la VM ${name || vmid}`;
      setNotice(`Backup ${what} en marcha (job ${out.job_id.slice(0, 8)}). Consulta su progreso en Jobs.`);
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo lanzar el backup.");
    } finally {
      setBusy(null);
    }
  }

  async function loadRPs(instanceId: string, vmid: number) {
    const key = `${instanceId}/${vmid}`;
    setBusy(`rps-${key}`);
    setError(null);
    try {
      const out = await api<RestorePointViewList>(
        `/api/v1/admin/proxmox/instances/${instanceId}/vms/${vmid}/restore-points`, token);
      setRps((prev) => ({ ...prev, [key]: out }));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudieron cargar los puntos de restauración.");
    } finally {
      setBusy(null);
    }
  }

  async function restoreVM(instanceId: string, vmid: number, rpId: string, mode: "in_place" | "new") {
    setBusy(`rs-${rpId}`);
    setError(null);
    try {
      const out = await api<{ job_id: string }>("/api/v1/admin/proxmox/restore", token, {
        method: "POST",
        body: JSON.stringify({
          restore_point_id: rpId,
          target_instance_id: restoreTargetInstance || instanceId,
          mode,
          target_vmid: mode === "in_place" ? vmid : (Number(restoreVMID) || 0),
          target_name: mode === "new" ? restoreName : undefined,
        }),
      });
      setNotice(`Restauración en marcha (job ${out.job_id.slice(0, 8)}). Consulta su progreso en Jobs.`);
      setRestoreFor(null);
      setRestoreName("");
      setRestoreVMID("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo lanzar la restauración.");
    } finally {
      setBusy(null);
    }
  }

  async function remove(id: string) {
    setBusy(id);
    try {
      await api(`/api/v1/admin/proxmox/instances/${id}`, token, { method: "DELETE" });
      setVms((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo eliminar.");
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="mx-auto max-w-6xl px-6 py-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
            Virtualización
          </p>
          <h1 className="mt-1 font-display text-4xl uppercase tracking-wide text-slate-100">
            Proxmox
          </h1>
          <p className="mt-2 max-w-2xl text-sm text-slate-400">
            Instancias conectadas por token de API. Backup y restauración de
            VMs vía el agente instalado en el nodo PVE: vincula un agente a la
            instancia, elige un destino de almacenamiento (S3 o ruta física) y lanza backups o restaura en sitio
            o como instancia nueva.
          </p>
        </div>
        <Button onClick={() => { setShowAdd((v) => !v); setEditingId(null); }}>
          {showAdd ? "Cerrar" : "Añadir instancia"}
        </Button>
      </div>

      {notice ? (
        <div className="mt-4 border border-signal/30 bg-signal/5 px-4 py-3 font-mono text-xs text-signal">
          {notice}
        </div>
      ) : null}

      {showAdd ? (
        <form onSubmit={saveInstance} className="panel mt-6 space-y-4 p-5">
          <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
            {editingId ? "Editar instancia" : "Nueva instancia"}
          </p>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Nombre" hint="Etiqueta en el panel">
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="pve-casa" required />
            </Field>
            <Field label="URL de la API" hint="https://host:8006">
              <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://203.0.113.10:8006" required />
            </Field>
            <Field label="Usuario del token" hint="p. ej. root@pam">
              <Input value={tokenUser} onChange={(e) => setTokenUser(e.target.value)} required />
            </Field>
            <Field label="ID del token" hint="p. ej. bdr">
              <Input value={tokenId} onChange={(e) => setTokenId(e.target.value)} required />
            </Field>
            <Field label="Secreto del token" hint={editingId ? "Vacío = conserva el actual" : "Se guarda cifrado (AES-256-GCM)"}>
              <Input
                type="password"
                value={tokenSecret}
                onChange={(e) => setTokenSecret(e.target.value)}
                required={!editingId}
              />
            </Field>
            <Field label="Agente del nodo PVE" hint="Instala bdr-agent en el nodo Proxmox y selecciónalo aquí">
              <select
                value={agentId}
                onChange={(e) => setAgentId(e.target.value)}
                className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
              >
                <option value="">— sin agente (solo inventario) —</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.hostname} ({a.agent_version ?? "?"})
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Verificar TLS" hint="Desactivado por defecto (certificados autofirmados de PVE)">
              <select
                value={verifyTLS ? "on" : "off"}
                onChange={(e) => setVerifyTLS(e.target.value === "on")}
                className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
              >
                <option value="off">No verificar (autofirmado)</option>
                <option value="on">Verificar (CA instalada)</option>
              </select>
            </Field>
          </div>
          {error ? <ErrorBox message={error} /> : null}
          <div className="flex justify-end gap-2">
            {editingId ? (
              <Button type="button" variant="ghost" onClick={() => { setShowAdd(false); setEditingId(null); }}>
                Cancelar
              </Button>
            ) : null}
            <Button type="submit" loading={busy === "save"}>
              {editingId ? "Guardar cambios" : "Registrar instancia"}
            </Button>
          </div>
        </form>
      ) : null}

      {error && !showAdd ? <div className="mt-4"><ErrorBox message={error} /></div> : null}

      <div className="mt-8 space-y-6">
        {loading ? (
          <p className="text-sm text-slate-500">Cargando…</p>
        ) : instances.length === 0 ? (
          <p className="text-sm text-slate-500">
            Sin instancias registradas. Añade una instancia de Proxmox con un token de API.
          </p>
        ) : (
          instances.map((inst) => {
            const boundAgent = agents.find((a) => a.id === (inst as unknown as { agent_id?: string }).agent_id);
            return (
            <div key={inst.instance_id} className="panel p-5">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 className="font-display text-xl uppercase tracking-wide text-slate-100">
                    {inst.name}
                  </h2>
                  <p className="mt-1 font-mono text-[10px] text-slate-500">
                    {inst.url} · {inst.last_check ?? "sin verificar"} ·{" "}
                    {boundAgent ? (
                      <span className="text-emerald-400">agente: {boundAgent.hostname}</span>
                    ) : (
                      <span className="text-alert">sin agente en el nodo</span>
                    )}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" variant="ghost" onClick={() => startEdit(inst.instance_id)}>Editar</Button>
                  <Button size="sm" variant="ghost" loading={busy === inst.instance_id}
                    onClick={() => loadVMs(inst.instance_id)}>Ver VMs</Button>
                  <Button size="sm" variant="ghost" loading={busy === inst.instance_id}
                    onClick={() => remove(inst.instance_id)}>Eliminar</Button>
                </div>
              </div>

              <div className="mt-3">
                <Field label="Destino de backups de VM" hint="Destino S3 o ruta física donde se suben los segmentos y manifiestos">
                  <select
                    value={targetSel[inst.instance_id] ?? ""}
                    onChange={(e) => setTargetSel((prev) => ({ ...prev, [inst.instance_id]: e.target.value }))}
                    className="w-full max-w-md border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
                  >
                    <option value="">— elige destino —</option>
                    {targets.map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.name} · {t.kind === "filesystem" ? t.path : `${t.endpoint}/${t.bucket}`}{t.prefix ? "/" + t.prefix : ""}
                      </option>
                    ))}
                  </select>
                </Field>
              </div>

              {vms[inst.instance_id] ? (
                <table className="mt-4 w-full border-collapse text-left">
                  <thead>
                    <tr className="border-b border-ink-700 font-mono text-[10px] uppercase tracking-widest2 text-slate-500">
                      <th className="py-2 pr-4">VMID</th>
                      <th className="py-2 pr-4">Nombre</th>
                      <th className="py-2 pr-4">Nodo</th>
                      <th className="py-2 pr-4">Estado</th>
                      <th className="py-2 pr-4">RAM</th>
                      <th className="py-2 pr-4">Acciones</th>
                    </tr>
                  </thead>
                  <tbody>
                    {vms[inst.instance_id]!.items.map((v) => {
                      const key = `${inst.instance_id}/${v.vmid}`;
                      const list = rps[key];
                      const isLXC = v.type === "lxc";
                      return (
                        <tr key={v.vmid} className="border-b border-ink-800/80 align-top text-sm text-slate-300">
                          <td className="py-2 pr-4 font-mono text-signal">{v.vmid}</td>
                          <td className="py-2 pr-4">
                            {v.name || "—"}
                            {isLXC ? (
                              <span className="ml-2 border border-ice/40 px-1.5 py-0.5 font-mono text-[9px] uppercase text-ice">LXC</span>
                            ) : null}
                          </td>
                          <td className="py-2 pr-4 font-mono text-xs">{v.node}</td>
                          <td className="py-2 pr-4">
                            <span className={cx("font-mono text-xs",
                              v.status === "running" ? "text-emerald-400" : "text-slate-500")}>
                              {v.status}
                            </span>
                          </td>
                          <td className="py-2 pr-4 font-mono text-xs">{v.mem_mib} MB</td>
                          <td className="py-2 pr-4">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <Button size="sm" variant="secondary"
                                loading={busy === `bk-${inst.instance_id}-${v.vmid}`}
                                onClick={() => backupGuest(inst.instance_id, v.vmid, v.name, v.type)}>
                                Backup
                              </Button>
                              <Button size="sm" variant="ghost"
                                loading={busy === `rps-${key}`}
                                onClick={() => loadRPs(inst.instance_id, v.vmid)}>
                                Puntos
                              </Button>
                            </div>
                            {list ? (
                              <div className="mt-2 space-y-1.5">
                                {list.items.length === 0 ? (
                                  <p className="font-mono text-[10px] text-slate-600">sin backups aún</p>
                                ) : (
                                  list.items.map((rp) => (
                                    <div key={rp.id} className="flex flex-wrap items-center gap-2 border border-ink-700 px-2 py-1">
                                      <span className="font-mono text-[10px] text-slate-400">
                                        {rp.created_at?.slice(0, 16).replace("T", " ")} · {Math.round((rp.size_bytes ?? 0) / (1 << 30))} GiB
                                      </span>
                                      <Button size="sm" variant="ghost"
                                        onClick={() => { setRestoreFor({ rp: rp.id, vmid: v.vmid, kind: rp.kind }); setRestoreMode("in_place"); setRestoreTargetInstance(inst.instance_id); }}>
                                        Restaurar
                                      </Button>
                                    </div>
                                  ))
                                )}
                              </div>
                            ) : null}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              ) : (
                <p className="mt-3 font-mono text-[10px] text-slate-600">
                  Pulsa «Ver VMs» para consultar el inventario.
                </p>
              )}

              {restoreFor ? (
                <div className="mt-4 border border-ink-600 bg-ink-900/50 p-4">
                  <p className="font-mono text-[10px] uppercase tracking-widest2 text-slate-400">
                    Restaurar {restoreFor.kind === "lxc" ? "contenedor LXC" : "VM"} {restoreFor.vmid}
                  </p>
                  <div className="mt-3 grid gap-3 sm:grid-cols-2">
                    <Field label="Modo">
                      <select
                        value={restoreMode}
                        onChange={(e) => setRestoreMode(e.target.value as "in_place" | "new")}
                        className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
                      >
                        {restoreFor.kind === "lxc" ? (
                          <>
                            <option value="in_place">En sitio (destruye y restaura el contenedor)</option>
                            <option value="new">Contenedor nuevo (crea un CT con la misma configuración)</option>
                          </>
                        ) : (
                          <>
                            <option value="in_place">En sitio (sobrescribe los discos de la VM)</option>
                            <option value="new">Instancia nueva (crea la VM con la misma configuración)</option>
                          </>
                        )}
                      </select>
                    </Field>
                    <Field label="Instancia destino">
                      <select
                        value={restoreTargetInstance}
                        onChange={(e) => setRestoreTargetInstance(e.target.value)}
                        className="w-full border border-ink-600 bg-ink-900 px-3 py-2 text-sm text-slate-200 focus:border-signal focus:outline-none"
                      >
                        {instances.map((i) => (
                          <option key={i.instance_id} value={i.instance_id}>{i.name}</option>
                        ))}
                      </select>
                    </Field>
                    {restoreMode === "new" ? (
                      <>
                      <Field label={restoreFor.kind === "lxc" ? "Hostname del nuevo CT" : "Nombre de la nueva VM"}>
                        <Input value={restoreName} onChange={(e) => setRestoreName(e.target.value)}
                          placeholder={restoreFor.kind === "lxc" ? "web-restaurada" : "win11-restaurada"} />
                      </Field>
                      <Field label="VMID (opcional)" hint="Vacío = el nodo elige el siguiente libre">
                        <Input value={restoreVMID} onChange={(e) => setRestoreVMID(e.target.value)}
                          placeholder="110" inputMode="numeric" />
                      </Field>
                      </>
                    ) : null}
                  </div>
                  <p className="mt-2 text-xs text-slate-500">
                    {restoreFor.kind === "lxc" ? (
                      <>La restauración descarga el archivo vzdump verificando SHA-256 por segmento y lo
                      repone con <code className="text-signal">pct restore</code>. En sitio detiene, destruye y
                      restaura el contenedor con la config del backup; en modo nuevo crea un CT con hostname,
                      memoria, red y rootfs del manifiesto.</>
                    ) : (
                      <>La restauración detiene la VM (en sitio), reescribe los discos verificando
                      SHA-256 y la arranca. En modo nueva, crea la VM con cores, RAM, BIOS, red y
                      discos del manifiesto.</>
                    )}
                  </p>
                  <div className="mt-3 flex justify-end gap-2">
                    <Button variant="ghost" onClick={() => setRestoreFor(null)}>Cerrar</Button>
                    <Button loading={busy === `rs-${restoreFor.rp}`}
                      onClick={() => restoreVM(restoreTargetInstance, restoreFor.vmid, restoreFor.rp, restoreMode)}>
                      Restaurar
                    </Button>
                  </div>
                </div>
              ) : null}
            </div>
            );
          })
        )}
      </div>
    </div>
  );
}
