"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorBox,
  PageHeader,
  cx,
  severityLabel,
  severityTone,
} from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { api, type AgentList, type Alert, type AlertList } from "@/lib/api";
import { formatDateTime, shortId, timeAgo } from "@/lib/format";

type SeverityFilter = "all" | "critical" | "high" | "medium" | "low";
type StateFilter = "open" | "acknowledged" | "resolved";

const SEVERITY_FILTERS: Array<{ id: SeverityFilter; label: string }> = [
  { id: "all", label: "Todas" },
  { id: "critical", label: "Críticas" },
  { id: "high", label: "Altas" },
  { id: "medium", label: "Medias" },
  { id: "low", label: "Bajas" },
];

export default function AlertsPage() {
  const { token } = useAuth();
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [agents, setAgents] = useState<AgentList["items"]>([]);
  const [severity, setSeverity] = useState<SeverityFilter>("all");
  const [state, setState] = useState<StateFilter>("open");
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [a, ag] = await Promise.all([
        api<AlertList>("/api/v1/admin/alerts", token),
        api<AgentList>("/api/v1/admin/agents", token),
      ]);
      setAlerts(a.items);
      setAgents(ag.items);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudieron cargar las alertas.");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    load();
    const id = window.setInterval(load, 15000);
    return () => window.clearInterval(id);
  }, [load]);

  const visible = useMemo(() => {
    return alerts.filter(
      (a) => (severity === "all" || a.severity === severity) && a.state === state,
    );
  }, [alerts, severity, state]);

  const openBySeverity = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const a of alerts) {
      if (a.state !== "resolved") counts[a.severity] = (counts[a.severity] ?? 0) + 1;
    }
    return counts;
  }, [alerts]);

  const hostnames = useMemo(
    () => new Map(agents.map((a) => [a.id, a.hostname])),
    [agents],
  );

  async function act(alert: Alert, action: "ack" | "resolve") {
    setBusyId(alert.id);
    setError(null);
    setNotice(null);
    try {
      await api(`/api/v1/admin/alerts/${alert.id}/${action}`, token, { method: "POST" });
      setNotice(action === "ack" ? "Alerta marcada con acuse." : "Alerta resuelta.");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo actualizar la alerta.");
    } finally {
      setBusyId(null);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        kicker="06 / Operaciones"
        title="Alertas"
        description="Incidencias deduplicadas por equipo y plan: fallos de backup, equipos sin contacto, verificaciones fallidas y leases perdidos. El acuse indica que alguien lo está mirando; la resolución lo cierra."
        actions={
          <Button variant="secondary" onClick={load} loading={loading}>
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

      <div className="grid gap-3 sm:grid-cols-4">
        {(["critical", "high", "medium", "low"] as const).map((sev) => (
          <Card key={sev} cut className="p-4">
            <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-500">
              {severityLabel(sev)}
            </p>
            <p
              className={cx(
                "mt-2 font-display text-3xl",
                openBySeverity[sev] ? severityText(sev) : "text-slate-600",
              )}
            >
              {openBySeverity[sev] ?? 0}
            </p>
          </Card>
        ))}
      </div>

      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-ink-700 px-4 py-3">
          <div className="flex flex-wrap items-center gap-1">
            {SEVERITY_FILTERS.map((item) => (
              <button
                key={item.id}
                onClick={() => setSeverity(item.id)}
                className={cx(
                  "border px-3 py-1.5 font-mono text-[10px] uppercase tracking-[0.14em] transition",
                  severity === item.id
                    ? "border-signal/40 bg-signal/10 text-signal"
                    : "border-transparent text-slate-500 hover:text-slate-200",
                )}
              >
                {item.label}
              </button>
            ))}
            <span className="mx-2 h-5 w-px bg-ink-700" />
            {(["open", "acknowledged", "resolved"] as const).map((st) => (
              <button
                key={st}
                onClick={() => setState(st)}
                className={cx(
                  "border px-3 py-1.5 font-mono text-[10px] uppercase tracking-[0.14em] transition",
                  state === st
                    ? "border-ice/40 bg-ice/10 text-ice"
                    : "border-transparent text-slate-500 hover:text-slate-200",
                )}
              >
                {st === "open" ? "Abiertas" : st === "acknowledged" ? "Con acuse" : "Resueltas"}
              </button>
            ))}
          </div>
          <span className="font-mono text-[9px] uppercase tracking-widest2 text-slate-600">
            {visible.length} registros · refresco 15 s
          </span>
        </div>

        {visible.length === 0 ? (
          <div className="p-5">
            <EmptyState
              index="00 / ALERTS"
              title={alerts.length === 0 ? "Sin alertas" : "Sin coincidencias"}
              description={
                alerts.length === 0
                  ? "Cuando un backup falle, un equipo deje de reportar o una verificación no pase, aparecerá aquí con su severidad."
                  : "Cambia los filtros de severidad o estado."
              }
            />
          </div>
        ) : (
          <ul className="divide-y divide-ink-800/80">
            {visible.map((a) => {
              const actionable = a.state !== "resolved";
              return (
                <li key={a.id} className="px-4 py-4 transition hover:bg-white/[0.025]">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <Badge tone={severityTone(a.severity)}>
                          <span className={cx("h-1.5 w-1.5", severityDot(a.severity))} />
                          {severityLabel(a.severity)}
                        </Badge>
                        <span className="font-display text-lg uppercase leading-none tracking-[0.08em] text-slate-100">
                          {a.title}
                        </span>
                        {a.count > 1 ? (
                          <Badge tone="neutral">×{a.count}</Badge>
                        ) : null}
                        {a.state === "acknowledged" ? (
                          <Badge tone="ice">acuse: {a.ack_by ?? "—"}</Badge>
                        ) : null}
                      </div>
                      {a.detail ? (
                        <p className="mt-1.5 break-words text-sm text-slate-400">{a.detail}</p>
                      ) : null}
                      <p className="mt-1.5 font-mono text-[10px] uppercase tracking-[0.14em] text-slate-600">
                        {a.kind} · primera {formatDateTime(a.first_at)} · última{" "}
                        {timeAgo(a.last_at)}
                        {a.agent_id
                          ? ` · ${hostnames.get(a.agent_id) ?? shortId(a.agent_id)}`
                          : ""}
                        {a.job_id ? ` · job ${shortId(a.job_id)}` : ""}
                      </p>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      {actionable && a.state === "open" ? (
                        <Button
                          size="sm"
                          variant="secondary"
                          loading={busyId === a.id}
                          onClick={() => act(a, "ack")}
                        >
                          Acuse
                        </Button>
                      ) : null}
                      {actionable ? (
                        <Button
                          size="sm"
                          loading={busyId === a.id}
                          onClick={() => act(a, "resolve")}
                        >
                          Resolver
                        </Button>
                      ) : (
                        <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-slate-600">
                          resuelta {a.resolved_by ? `por ${a.resolved_by}` : ""}
                        </span>
                      )}
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </Card>

      <p className="border-t border-ink-800 pt-4 font-mono text-[9px] uppercase tracking-[0.18em] text-slate-600">
        Las alertas se deduplican por clave (equipo+plan o identificador) y se re-notifican
        según el periodo configurado en ajustes · severidad mínima configurable
      </p>
    </div>
  );
}

function severityText(sev: string): string {
  switch (sev) {
    case "critical":
      return "text-danger";
    case "high":
      return "text-alert";
    case "medium":
      return "text-ice";
    default:
      return "text-slate-300";
  }
}

function severityDot(sev: string): string {
  switch (sev) {
    case "critical":
      return "bg-danger";
    case "high":
      return "bg-alert";
    case "medium":
      return "bg-ice";
    default:
      return "bg-slate-400";
  }
}
