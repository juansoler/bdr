import type { Retention } from "./api";

const dateFmt = new Intl.DateTimeFormat("es-ES", {
  day: "2-digit",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
});

export function formatDateTime(value?: string): string {
  if (!value) return "—";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "—";
  return dateFmt.format(d);
}

export function timeAgo(value?: string): string {
  if (!value) return "nunca";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "nunca";
  const seconds = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
  if (seconds < 60) return `hace ${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `hace ${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `hace ${hours}h`;
  const days = Math.floor(hours / 24);
  return `hace ${days}d`;
}

export function shortId(value?: string): string {
  if (!value) return "—";
  return value.slice(0, 8);
}

export function formatBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null || Number.isNaN(bytes)) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatPercent(value?: number): string {
  if (value === undefined || value === null) return "—";
  return `${Math.max(0, Math.min(100, value))}%`;
}

// Resumen de retención tipo "7L/7D/4S/6M" (L=latest, H=horas, D=días, S=semanas, M=meses, A=años).
export function retentionSummary(retention?: Retention): string {
  if (!retention) return "—";
  const parts: string[] = [`${retention.keep_latest ?? 0}L`];
  if (retention.keep_hourly) parts.push(`${retention.keep_hourly}H`);
  if (retention.keep_daily) parts.push(`${retention.keep_daily}D`);
  if (retention.keep_weekly) parts.push(`${retention.keep_weekly}S`);
  if (retention.keep_monthly) parts.push(`${retention.keep_monthly}M`);
  if (retention.keep_annual) parts.push(`${retention.keep_annual}A`);
  return parts.join("/");
}
