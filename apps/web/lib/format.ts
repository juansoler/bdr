import type { Inventory, NetInterface, Retention } from "./api";

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

// Dirección sin el prefijo de red («192.168.1.10/24» → «192.168.1.10»).
function addrHost(address: string): string {
  return address.split("/")[0] ?? address;
}

function isGlobalV4(ip: string): boolean {
  return (
    /^\d{1,3}(\.\d{1,3}){3}$/.test(ip) &&
    ip !== "127.0.0.1" &&
    !ip.startsWith("169.254.")
  );
}

function isLoopback(ip: string): boolean {
  return ip === "127.0.0.1" || ip === "::1" || ip.startsWith("fe80");
}

// IP primaria del inventario: la primera IPv4 global (no loopback ni
// link-local) de una interfaz activa; si no la hay, la primera IPv4 global
// de cualquier interfaz; y como último recurso, cualquier dirección no local.
export function primaryIP(inv?: Inventory): string {
  if (!inv?.network?.length) return "—";
  const passes: Array<(ip: string, up: boolean) => boolean> = [
    (ip, up) => up && isGlobalV4(ip),
    (ip) => isGlobalV4(ip),
    (ip) => !isLoopback(ip),
  ];
  for (const accept of passes) {
    for (const iface of inv.network) {
      for (const raw of iface.addresses ?? []) {
        const ip = addrHost(raw);
        if (accept(ip, iface.up)) return ip;
      }
    }
  }
  return "—";
}

// Resumen de interfaces: cuántas están activas sobre el total («2/4»).
export function interfaceSummary(inv?: Inventory): string {
  const list = inv?.network ?? [];
  if (list.length === 0) return "—";
  const up = list.filter((n) => n.up).length;
  return `${up}/${list.length}`;
}

// Texto plano de las interfaces para tooltips y búsqueda: «eth0 192.168.1.10 · lo 127.0.0.1».
export function networkSummary(inv?: Inventory): string {
  const list = inv?.network ?? [];
  if (list.length === 0) return "";
  return list
    .map((n: NetInterface) => `${n.name} ${(n.addresses ?? []).join(" ")}`.trim())
    .join(" · ");
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
