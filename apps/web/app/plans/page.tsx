"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/Modal";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorBox,
  Field,
  Input,
  PageHeader,
  Select,
  Textarea,
  cx,
} from "@/components/ui";
import { useAuth } from "@/lib/auth";
import {
  api,
  type AgentList,
  type GroupList,
  type Plan,
  type PlanList,
  type PlanValidation,
  type Retention,
  type StorageTargetList,
  type StorageTargetView,
  type TargetType,
} from "@/lib/api";
import { formatDateTime, retentionSummary, shortId } from "@/lib/format";

interface FormState {
  name: string;
  description: string;
  kind: "file" | "image";
  storageTargetId: string;
  paths: string;
  excludes: string;
  disks: string;
  schedule: string;
  timezone: string;
  windowStart: string;
  windowEnd: string;
  keepLatest: number;
  keepHourly: number;
  keepDaily: number;
  keepWeekly: number;
  keepMonthly: number;
  keepAnnual: number;
}

const INITIAL_FORM: FormState = {
  name: "",
  description: "",
  kind: "file",
  storageTargetId: "",
  paths: "",
  excludes: "",
  disks: "",
  schedule: "",
  timezone: "UTC",
  windowStart: "",
  windowEnd: "",
  keepLatest: 7,
  keepHourly: 0,
  keepDaily: 7,
  keepWeekly: 4,
  keepMonthly: 6,
  keepAnnual: 0,
};

export default function PlansPage() {
  const { token } = useAuth();
  const [plans, setPlans] = useState<Plan[]>([]);
  const [agents, setAgents] = useState<AgentList["items"]>([]);
  const [groups, setGroups] = useState<GroupList["items"]>([]);
  const [form, setForm] = useState<FormState>(INITIAL_FORM);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // Acciones por fila
  const [validation, setValidation] = useState<Record<string, PlanValidation>>({});
  const [validating, setValidating] = useState<string | null>(null);
  const [assignPlan, setAssignPlan] = useState<Plan | null>(null);
  const [deletePlan, setDeletePlan] = useState<Plan | null>(null);
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null);
  const [targets, setTargets] = useState<StorageTargetView[]>([]);

  const load = useCallback(async () => {
    try {
      const [p, a, g, s] = await Promise.all([
        api<PlanList>("/api/v1/admin/backup-plans", token),
        api<AgentList>("/api/v1/admin/agents", token),
        api<GroupList>("/api/v1/admin/groups", token),
        api<StorageTargetList>("/api/v1/admin/storage/targets", token),
      ]);
      setPlans(p.items ?? []);
      setAgents(a.items ?? []);
      setGroups(g.items ?? []);
      setTargets(s.items ?? []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudieron cargar los planes.");
    }
  }, [token]);

  useEffect(() => {
    load();
  }, [load]);

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  const splitLines = (value: string) =>
    value
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean);

  // Discos de un plan de imagen: "0, 1" → [0, 1]; vacío = todos.
  const parseDisks = (value: string): number[] | null => {
    const parts = value
      .split(/[,\s]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    if (parts.length === 0) return [];
    const out: number[] = [];
    for (const p of parts) {
      if (!/^\d+$/.test(p)) return null;
      out.push(Number(p));
    }
    return out;
  };

  async function create(e: React.FormEvent) {
    e.preventDefault();
    const isImage = form.kind === "image";
    const pathList = splitLines(form.paths);
    const excludeList = splitLines(form.excludes);
    if (!form.name.trim() || (!isImage && pathList.length === 0)) {
      setError("El plan necesita un nombre" + (isImage ? "." : " y al menos una ruta."));
      return;
    }
    if (isImage && !form.storageTargetId) {
      setError("Los planes de imagen requieren un destino de almacenamiento (S3 o ruta física; créalo en la sección Almacenamiento).");
      return;
    }
    const diskList = isImage ? parseDisks(form.disks) : [];
    if (diskList === null) {
      setError("Discos a capturar: introduce índices numéricos separados por comas (p. ej. 0, 1). Vacío = todos los discos fijos.");
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const schedule = form.schedule.trim();
      const body = JSON.stringify({
        name: form.name.trim(),
        description: form.description.trim() || undefined,
        kind: form.kind,
        storage_target_id: form.storageTargetId || undefined,
        paths: isImage ? undefined : pathList,
        excludes: isImage || excludeList.length === 0 ? undefined : excludeList,
        disks: isImage ? diskList : undefined, // [] = limpiar selección (todos los discos)
        enabled: true,
        schedule: schedule || undefined,
        timezone: form.timezone.trim() || "UTC",
        window:
          form.windowStart.trim() && form.windowEnd.trim()
            ? { start: form.windowStart.trim(), end: form.windowEnd.trim() }
            : undefined,
        retention: {
          keep_latest: Math.max(0, form.keepLatest),
          keep_hourly: Math.max(0, form.keepHourly),
          keep_daily: Math.max(0, form.keepDaily),
          keep_weekly: Math.max(0, form.keepWeekly),
          keep_monthly: Math.max(0, form.keepMonthly),
          keep_annual: Math.max(0, form.keepAnnual),
        },
      });
      const plan = await api<Plan>(
        editingPlan
          ? `/api/v1/admin/backup-plans/${editingPlan.id}`
          : "/api/v1/admin/backup-plans",
        token,
        { method: editingPlan ? "PATCH" : "POST", body },
      );
      setNotice(`Plan «${plan.name}» ${editingPlan ? "actualizado" : "creado"}.`);
      setEditingPlan(null);
      setForm(INITIAL_FORM);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo guardar el plan.");
    } finally {
      setBusy(false);
    }
  }

  function startEdit(p: Plan) {
    setEditingPlan(p);
    setError(null);
    setNotice(null);
    setForm({
      name: p.name,
      description: p.description ?? "",
      kind: p.kind === "image" ? "image" : "file",
      storageTargetId: p.storage_target_id ?? "",
      paths: (p.paths ?? []).join("\n"),
      excludes: (p.excludes ?? []).join("\n"),
      disks: (p.disks ?? []).join(", "),
      schedule: p.schedule ?? "",
      timezone: p.timezone || "UTC",
      windowStart: p.window?.start ?? "",
      windowEnd: p.window?.end ?? "",
      keepLatest: p.retention?.keep_latest ?? 7,
      keepHourly: p.retention?.keep_hourly ?? 0,
      keepDaily: p.retention?.keep_daily ?? 7,
      keepWeekly: p.retention?.keep_weekly ?? 4,
      keepMonthly: p.retention?.keep_monthly ?? 6,
      keepAnnual: p.retention?.keep_annual ?? 0,
    });
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  async function validate(plan: Plan) {
    setValidating(plan.id);
    setError(null);
    try {
      const out = await api<PlanValidation>(
        `/api/v1/admin/backup-plans/${plan.id}/validate`,
        token,
        { method: "POST", body: "{}" },
      );
      setValidation((prev) => ({ ...prev, [plan.id]: out }));
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo validar el plan.");
    } finally {
      setValidating(null);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        kicker="03 / Políticas"
        title="Planes de protección"
        description="Define qué se copia (rutas o imagen completa del equipo), con qué programación y retención, y asígnalas a equipos o grupos."
        actions={
          <Button variant="secondary" onClick={load}>
            Actualizar
          </Button>
        }
      />

      {error ? <ErrorBox message={error} onRetry={load} /> : null}
      {notice ? (
        <div className="border border-signal/30 bg-signal/5 px-4 py-3 font-mono text-xs text-signal">
          {notice}
        </div>
      ) : null}

      <div className="grid gap-6 xl:grid-cols-[0.8fr_1.2fr]">
        <Card cut className="h-fit p-5 sm:p-6">
          <p className="font-mono text-[9px] uppercase tracking-widest2 text-signal/70">
            {editingPlan ? "Editar plan" : "Nuevo plan"}
          </p>
          <h2 className="mt-1 font-display text-2xl uppercase tracking-wide text-slate-100">
            {editingPlan ? `«${editingPlan.name}»` : "Definir selección"}
          </h2>
          <form onSubmit={create} className="mt-5 space-y-4">
            <Field label="Nombre">
              <Input
                value={form.name}
                onChange={(e) => set("name", e.target.value)}
                placeholder="Archivos servidores"
              />
            </Field>
            <Field label="Descripción (opcional)">
              <Input
                value={form.description}
                onChange={(e) => set("description", e.target.value)}
                placeholder="Copia nocturna de datos críticos"
              />
            </Field>
            <Field label="Tipo de plan">
              <div className="grid grid-cols-2 gap-2">
                <button
                  type="button"
                  disabled={!!editingPlan}
                  onClick={() => set("kind", "file")}
                  className={cx(
                    "border px-3 py-2 text-left text-sm transition",
                    form.kind === "file"
                      ? "border-signal/60 bg-signal/10 text-slate-100"
                      : "border-ink-600 text-slate-400 hover:border-ink-500",
                  )}
                >
                  <span className="block font-mono text-[10px] uppercase tracking-widest2">Archivos</span>
                  <span className="text-xs text-slate-500">Rutas concretas con Kopia (deduplicado)</span>
                </button>
                <button
                  type="button"
                  disabled={!!editingPlan}
                  onClick={() => set("kind", "image")}
                  className={cx(
                    "border px-3 py-2 text-left text-sm transition",
                    form.kind === "image"
                      ? "border-signal/60 bg-signal/10 text-slate-100"
                      : "border-ink-600 text-slate-400 hover:border-ink-500",
                  )}
                >
                  <span className="block font-mono text-[10px] uppercase tracking-widest2">Imagen completa</span>
                  <span className="text-xs text-slate-500">Equipo íntegro a un destino de almacenamiento (S3 o ruta física), para desplegar en otro equipo</span>
                </button>
              </div>
            </Field>
            {form.kind === "image" ? (
              <>
              <div className="border border-ink-600 bg-ink-900/40 p-3 text-xs text-slate-400">
                La captura de imagen lee <strong className="text-slate-200">todos los discos fijos</strong> del
                equipo y sube cada segmento <strong className="text-slate-200">directamente al destino
                de almacenamiento</strong> (no ocupa espacio en el equipo; los segmentos que no cambian no se
                vuelven a subir). Incluye tabla de particiones, ESP, sistema y datos; coherente ante
                caídas (crash-consistent). Requiere agente ≥ 0.6.2 y elegir destino abajo. El
                despliegue en otro equipo se hace con <code className="text-signal">bdr-agent
                image-fetch</code> + <code className="text-signal">image-write</code> desde un entorno
                live (ver docs/recuperacion-bare-metal.md).
              </div>
              <Field
                label="Discos a capturar (opcional)"
                hint="Índices separados por comas. Vacío = todos los discos fijos. Los discos fuera de la selección quedan excluidos de la imagen (no se restaurarán en un despliegue bare-metal)."
              >
                <Input
                  value={form.disks}
                  onChange={(e) => set("disks", e.target.value)}
                  placeholder="0, 1   (vacío = todos)"
                  spellCheck={false}
                  className="font-mono"
                />
              </Field>
              </>
            ) : (
              <>
            <Field label="Rutas" hint="Una ruta por línea. Ejemplo: C:\\Datos o /srv/datos">
              <Textarea
                value={form.paths}
                onChange={(e) => set("paths", e.target.value)}
                placeholder={"/srv/datos\n/home"}
                rows={4}
              />
            </Field>
            <Field label="Exclusiones (opcional)" hint="Una ruta por línea.">
              <Textarea
                value={form.excludes}
                onChange={(e) => set("excludes", e.target.value)}
                placeholder={"/srv/datos/cache\n/var/tmp"}
                rows={2}
              />
            </Field>
              </>
            )}

            <Field
              label="Destino de almacenamiento"
              hint={
                targets.length === 0
                  ? "Sin destinos: se usa el repositorio central."
                  : "Los destinos se definen en la sección Almacenamiento."
              }
            >
              <Select
                value={form.storageTargetId}
                onChange={(e) => set("storageTargetId", e.target.value)}
              >
                <option value="">Repositorio central (con réplicas)</option>
                {targets.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name} · {t.kind === "filesystem" ? t.path : `${t.endpoint}/${t.bucket}`}
                  </option>
                ))}
              </Select>
            </Field>

            <div className="border-t border-ink-700 pt-4">
              <p className="mb-3 font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
                Programación
              </p>
              <div className="space-y-4">
                <Field
                  label="Expresión cron"
                  hint="@daily, @hourly o cron de 5 campos (0 3 * * *). Vacío = solo manual."
                >
                  <Input
                    value={form.schedule}
                    onChange={(e) => set("schedule", e.target.value)}
                    placeholder="0 3 * * *"
                    spellCheck={false}
                    className="font-mono"
                  />
                </Field>
                <Field label="Zona horaria" hint="IANA, p. ej. Europe/Madrid.">
                  <Input
                    value={form.timezone}
                    onChange={(e) => set("timezone", e.target.value)}
                    placeholder="UTC"
                    spellCheck={false}
                    className="font-mono"
                  />
                </Field>
                <div className="grid grid-cols-2 gap-3">
                  <Field label="Ventana inicio" hint="HH:MM (opcional).">
                    <Input
                      value={form.windowStart}
                      onChange={(e) => set("windowStart", e.target.value)}
                      placeholder="22:00"
                      className="font-mono"
                    />
                  </Field>
                  <Field label="Ventana fin" hint="HH:MM; si es menor, cruza medianoche.">
                    <Input
                      value={form.windowEnd}
                      onChange={(e) => set("windowEnd", e.target.value)}
                      placeholder="06:00"
                      className="font-mono"
                    />
                  </Field>
                </div>
              </div>
            </div>

            <div className="border-t border-ink-700 pt-4">
              <p className="mb-3 font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
                Retención GFS
              </p>
              <div className="grid grid-cols-3 gap-3">
                <Field label="Últimas">
                  <Input
                    type="number"
                    min={0}
                    value={form.keepLatest}
                    onChange={(e) => set("keepLatest", Number(e.target.value) || 0)}
                  />
                </Field>
                <Field label="Horarias">
                  <Input
                    type="number"
                    min={0}
                    value={form.keepHourly}
                    onChange={(e) => set("keepHourly", Number(e.target.value) || 0)}
                  />
                </Field>
                <Field label="Diarias">
                  <Input
                    type="number"
                    min={0}
                    value={form.keepDaily}
                    onChange={(e) => set("keepDaily", Number(e.target.value) || 0)}
                  />
                </Field>
                <Field label="Semanales">
                  <Input
                    type="number"
                    min={0}
                    value={form.keepWeekly}
                    onChange={(e) => set("keepWeekly", Number(e.target.value) || 0)}
                  />
                </Field>
                <Field label="Mensuales">
                  <Input
                    type="number"
                    min={0}
                    value={form.keepMonthly}
                    onChange={(e) => set("keepMonthly", Number(e.target.value) || 0)}
                  />
                </Field>
                <Field label="Anuales">
                  <Input
                    type="number"
                    min={0}
                    value={form.keepAnnual}
                    onChange={(e) => set("keepAnnual", Number(e.target.value) || 0)}
                  />
                </Field>
              </div>
            </div>

            <div className="flex items-center justify-between border-t border-ink-700 pt-4">
              <Badge tone="ice">kind: {form.kind === "image" ? "image (equipo completo)" : "file"}</Badge>
              <div className="flex gap-2">
                {editingPlan ? (
                  <Button type="button" variant="ghost" onClick={() => { setEditingPlan(null); setForm(INITIAL_FORM); }}>
                    Cancelar
                  </Button>
                ) : null}
                <Button type="submit" loading={busy}>
                  {editingPlan ? "Guardar cambios" : "Crear plan"}
                </Button>
              </div>
            </div>
          </form>
        </Card>

        <div className="space-y-4">
          {plans.length === 0 ? (
            <EmptyState
              index="00 / PLANS"
              title="Sin planes"
              description="Crea un plan con al menos una ruta y asígnalo a un equipo o grupo para que pueda ejecutar backups."
            />
          ) : (
            plans.map((plan) => {
              const v = validation[plan.id];
              return (
                <Card key={plan.id} className="p-5">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div>
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="font-display text-2xl uppercase tracking-wide text-slate-100">
                          {plan.name}
                        </h3>
                        <Badge tone={plan.enabled ? "signal" : "neutral"}>
                          {plan.enabled ? "Activo" : "Inactivo"}
                        </Badge>
                        <Badge tone="ice">{plan.kind}</Badge>
                        <Badge tone="neutral">{retentionSummary(plan.retention)}</Badge>
                      </div>
                      <p className="mt-1 font-mono text-[10px] text-slate-600" title={plan.id}>
                        {shortId(plan.id)} · creado {formatDateTime(plan.created_at)}
                        {plan.timezone ? ` · ${plan.timezone}` : ""}
                      </p>
                      {plan.description ? (
                        <p className="mt-1 text-xs text-slate-400">{plan.description}</p>
                      ) : null}
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => startEdit(plan)}
                      >
                        Editar
                      </Button>
                      <Button
                        size="sm"
                        variant="secondary"
                        loading={validating === plan.id}
                        onClick={() => validate(plan)}
                      >
                        Validar
                      </Button>
                      <Button size="sm" variant="secondary" onClick={() => setAssignPlan(plan)}>
                        Asignar
                      </Button>
                      <Button size="sm" variant="danger" onClick={() => setDeletePlan(plan)}>
                        Eliminar
                      </Button>
                    </div>
                  </div>

                  <div className="mt-4 grid gap-3 sm:grid-cols-2">
                    <div>
                      <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
                        Rutas
                      </p>
                      <div className="mt-2 flex flex-wrap gap-1.5">
                        {plan.kind === "image" ? (
                          plan.disks && plan.disks.length > 0 ? (
                            plan.disks.map((d) => (
                              <code
                                key={d}
                                className="border border-signal/40 bg-ink-950 px-2 py-1 font-mono text-[10px] text-signal"
                              >
                                disco {d}
                              </code>
                            ))
                          ) : (
                            <code className="border border-signal/40 bg-ink-950 px-2 py-1 font-mono text-[10px] text-signal">
                              equipo completo (imagen)
                            </code>
                          )
                        ) : (
                          (plan.paths ?? []).map((p) => (
                            <code
                              key={p}
                              className="border border-ink-600 bg-ink-950 px-2 py-1 font-mono text-[10px] text-slate-300"
                            >
                              {p}
                            </code>
                          ))
                        )}
                      </div>
                    </div>
                    <div>
                      <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
                        Exclusiones
                      </p>
                      <div className="mt-2 flex flex-wrap gap-1.5">
                        {plan.excludes && plan.excludes.length > 0 ? (
                          plan.excludes.map((p) => (
                            <code
                              key={p}
                              className="border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-[10px] text-slate-500"
                            >
                              {p}
                            </code>
                          ))
                        ) : (
                          <span className="font-mono text-[10px] text-slate-600">ninguna</span>
                        )}
                      </div>
                    </div>
                  </div>

                  <div className="mt-4 flex flex-wrap items-center gap-x-6 gap-y-2 border-t border-ink-700 pt-3 font-mono text-[10px]">
                    <span className="text-slate-500">
                      <span className="text-slate-600">programación:</span>{" "}
                      <span className="text-slate-300">{plan.schedule || "solo manual"}</span>
                      {plan.window ? (
                        <span className="text-slate-600">
                          {" "}
                          · ventana {plan.window.start}–{plan.window.end}
                        </span>
                      ) : null}
                    </span>
                    {plan.next_runs && plan.next_runs.length > 0 ? (
                      <span className="text-slate-500">
                        <span className="text-slate-600">próxima:</span>{" "}
                        <span className="text-ice">{formatDateTime(plan.next_runs[0])}</span>
                        {plan.next_runs.length > 1 ? (
                          <span className="text-slate-600"> (+{plan.next_runs.length - 1})</span>
                        ) : null}
                      </span>
                    ) : null}
                    {plan.rpo_hours ? (
                      <span className="text-slate-500">
                        <span className="text-slate-600">RPO:</span>{" "}
                        <span className="text-alert">{plan.rpo_hours} h</span>
                      </span>
                    ) : null}
                  </div>

                  {v ? (
                    <div
                      className={`mt-4 border p-3 ${
                        v.valid ? "border-signal/30 bg-signal/5" : "border-danger/30 bg-danger/5"
                      }`}
                    >
                      <div className="flex items-center gap-2">
                        <Badge tone={v.valid ? "signal" : "danger"}>
                          {v.valid ? "Configuración válida" : "Configuración inválida"}
                        </Badge>
                      </div>
                      {v.errors && v.errors.length > 0 ? (
                        <ul className="mt-2 space-y-1 font-mono text-[10px] text-danger">
                          {v.errors.map((e) => (
                            <li key={e}>· {e}</li>
                          ))}
                        </ul>
                      ) : null}
                      {v.warnings && v.warnings.length > 0 ? (
                        <ul className="mt-2 space-y-1 font-mono text-[10px] text-alert">
                          {v.warnings.map((w) => (
                            <li key={w}>· {w}</li>
                          ))}
                        </ul>
                      ) : null}
                      {v.next_runs && v.next_runs.length > 0 ? (
                        <p className="mt-2 font-mono text-[10px] text-slate-500">
                          próximas ejecuciones:{" "}
                          {v.next_runs.map((t) => formatDateTime(t)).join(" · ")}
                        </p>
                      ) : null}
                    </div>
                  ) : null}
                </Card>
              );
            })
          )}
        </div>
      </div>

      <p className="border-t border-ink-800 pt-4 font-mono text-[9px] uppercase tracking-[0.18em] text-slate-600">
        Un equipo sin plan asignado devuelve E_PLAN_REQUIRED al lanzar un backup
      </p>

      {assignPlan ? (
        <AssignModal
          token={token}
          plan={assignPlan}
          agents={agents}
          groups={groups}
          onClose={() => setAssignPlan(null)}
          onDone={async (label) => {
            setNotice(`Plan «${assignPlan.name}» asignado a ${label}.`);
            setAssignPlan(null);
            await load();
          }}
        />
      ) : null}

      {deletePlan ? (
        <DeleteModal
          token={token}
          plan={deletePlan}
          onClose={() => setDeletePlan(null)}
          onDone={async () => {
            setNotice(`Plan «${deletePlan.name}» eliminado.`);
            setDeletePlan(null);
            await load();
          }}
        />
      ) : null}
    </div>
  );
}

// Asignación con destino equipo o grupo.
function AssignModal({
  token,
  plan,
  agents,
  groups,
  onClose,
  onDone,
}: {
  token: string;
  plan: Plan;
  agents: AgentList["items"];
  groups: GroupList["items"];
  onClose: () => void;
  onDone: (label: string) => void | Promise<void>;
}) {
  const [targetType, setTargetType] = useState<TargetType>(agents.length > 0 ? "agent" : "group");
  const [targetId, setTargetId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const options = targetType === "agent" ? agents : groups;

  async function assign() {
    if (!targetId) {
      setError("Selecciona un destino para la asignación.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api(`/api/v1/admin/backup-plans/${plan.id}/assignments`, token, {
        method: "POST",
        body: JSON.stringify({ target_type: targetType, target_id: targetId }),
      });
      const label =
        targetType === "agent"
          ? agents.find((a) => a.id === targetId)?.hostname ?? shortId(targetId)
          : groups.find((g) => g.id === targetId)?.name ?? shortId(targetId);
      await onDone(label);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo asignar el plan.");
      setBusy(false);
    }
  }

  return (
    <Modal kicker="Asignación" title={`Asignar «${plan.name}»`} onClose={onClose}>
      <div className="mt-6 space-y-5">
        <Field label="Tipo de destino">
          <Select
            value={targetType}
            onChange={(e) => {
              setTargetType(e.target.value as TargetType);
              setTargetId("");
            }}
          >
            <option value="agent">Equipo</option>
            <option value="group">Grupo</option>
          </Select>
        </Field>
        <Field
          label={targetType === "agent" ? "Equipo destino" : "Grupo destino"}
          hint={
            targetType === "agent"
              ? "La asignación directa prevalece sobre la del grupo."
              : "Todos los miembros del grupo heredan el plan."
          }
        >
          <Select value={targetId} onChange={(e) => setTargetId(e.target.value)}>
            <option value="">
              {options.length === 0
                ? targetType === "agent"
                  ? "No hay equipos enrolados"
                  : "No hay grupos creados"
                : "Selecciona…"}
            </option>
            {targetType === "agent"
              ? agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.hostname} · {a.status}
                  </option>
                ))
              : groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name} · {g.member_ids?.length ?? 0} miembro(s)
                  </option>
                ))}
          </Select>
        </Field>

        {error ? <ErrorBox message={error} /> : null}

        <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
          <Button variant="quiet" onClick={onClose}>
            Cancelar
          </Button>
          <Button onClick={assign} loading={busy} disabled={!targetId}>
            Asignar plan
          </Button>
        </div>
      </div>
    </Modal>
  );
}

// Confirmación de borrado con feedback de error (403/404).
function DeleteModal({
  token,
  plan,
  onClose,
  onDone,
}: {
  token: string;
  plan: Plan;
  onClose: () => void;
  onDone: () => void | Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function remove() {
    setBusy(true);
    setError(null);
    try {
      await api(`/api/v1/admin/backup-plans/${plan.id}`, token, { method: "DELETE" });
      await onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo eliminar el plan.");
      setBusy(false);
    }
  }

  return (
    <Modal kicker="Acción destructiva" title="Eliminar plan" onClose={onClose}>
      <div className="mt-6 space-y-5">
        <div className="border border-danger/30 bg-danger/5 p-4 text-sm leading-relaxed text-slate-300">
          Se eliminará el plan <span className="font-mono text-danger">{plan.name}</span> y sus
          asignaciones. Los puntos de recuperación ya creados no se borran. La operación queda
          registrada en la auditoría.
        </div>

        {error ? <ErrorBox message={error} /> : null}

        <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
          <Button variant="quiet" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="danger" onClick={remove} loading={busy}>
            Eliminar plan
          </Button>
        </div>
      </div>
    </Modal>
  );
}
