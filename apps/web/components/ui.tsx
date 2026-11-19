"use client";

import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";

export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

export const inputClass =
  "w-full rounded-sm border border-ink-600 bg-ink-950/70 px-3 py-2.5 text-sm text-slate-100 placeholder:text-slate-600 transition focus:border-signal/60 focus:outline-none focus:ring-1 focus:ring-signal/30";

export const labelClass =
  "mb-1.5 block font-mono text-[10px] uppercase tracking-widest2 text-slate-500";

export function Card({
  children,
  className,
  cut = false,
}: {
  children: ReactNode;
  className?: string;
  cut?: boolean;
}) {
  return <div className={cx("panel", cut && "cut", className)}>{children}</div>;
}

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "quiet";
type ButtonSize = "sm" | "md";

const variants: Record<ButtonVariant, string> = {
  primary:
    "border border-signal bg-signal text-ink-950 hover:bg-[#d8ff7c] hover:shadow-signal disabled:bg-signal/40",
  secondary:
    "border border-ink-600 bg-ink-800 text-slate-200 hover:border-signal/50 hover:text-signal",
  ghost: "border border-transparent text-slate-300 hover:bg-white/5 hover:text-signal",
  danger:
    "border border-danger/40 bg-danger/5 text-danger hover:bg-danger/10",
  quiet:
    "border border-ink-700 bg-transparent text-slate-400 hover:border-ink-600 hover:text-slate-200",
};

const sizes: Record<ButtonSize, string> = {
  sm: "px-2.5 py-1.5 text-[10px]",
  md: "px-4 py-2.5 text-[11px]",
};

export function Button({
  children,
  variant = "primary",
  size = "md",
  loading = false,
  className,
  disabled,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
}) {
  return (
    <button
      {...props}
      type={props.type ?? "button"}
      disabled={disabled || loading}
      className={cx(
        "inline-flex items-center justify-center gap-2 font-display font-semibold uppercase tracking-[0.16em] transition disabled:cursor-not-allowed disabled:opacity-60",
        variants[variant],
        sizes[size],
        className,
      )}
    >
      {loading ? (
        <span className="h-3 w-3 animate-spin rounded-full border border-current border-t-transparent" />
      ) : null}
      {children}
    </button>
  );
}

type Tone = "signal" | "alert" | "danger" | "neutral" | "ice";

const toneClasses: Record<Tone, string> = {
  signal: "border-signal/30 bg-signal/10 text-signal",
  alert: "border-alert/30 bg-alert/10 text-alert",
  danger: "border-danger/30 bg-danger/10 text-danger",
  neutral: "border-ink-600 bg-white/5 text-slate-300",
  ice: "border-ice/30 bg-ice/10 text-ice",
};

const dotClasses: Record<Tone, string> = {
  signal: "bg-signal",
  alert: "bg-alert",
  danger: "bg-danger",
  neutral: "bg-slate-500",
  ice: "bg-ice",
};

export function Badge({
  children,
  tone = "neutral",
  className,
}: {
  children: ReactNode;
  tone?: Tone;
  className?: string;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.5 border px-2 py-0.5 font-mono text-[9px] uppercase tracking-[0.16em]",
        toneClasses[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

export function Dot({ tone = "signal", live = false }: { tone?: Tone; live?: boolean }) {
  return (
    <span
      className={cx(
        "inline-block h-1.5 w-1.5 rounded-full",
        dotClasses[tone],
        live && "dot-live",
      )}
    />
  );
}

export function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { tone: Tone; label: string; live?: boolean }> = {
    online: { tone: "signal", label: "En línea", live: true },
    pending: { tone: "alert", label: "Pendiente" },
    offline: { tone: "danger", label: "Sin conexión" },
    revoked: { tone: "neutral", label: "Revocado" },
    running: { tone: "ice", label: "Ejecutando", live: true },
    leased: { tone: "ice", label: "Asignado" },
    succeeded: { tone: "signal", label: "Correcto" },
    failed: { tone: "danger", label: "Fallido" },
    canceled: { tone: "neutral", label: "Cancelado" },
    expired: { tone: "alert", label: "Caducado" },
  };
  const item = map[status] ?? { tone: "neutral" as Tone, label: status };
  return (
    <Badge tone={item.tone}>
      <Dot tone={item.tone} live={item.live} />
      {item.label}
    </Badge>
  );
}

// Estado de verificación de un punto de recuperación.
export function VerifyBadge({ state }: { state: string }) {
  const map: Record<string, { tone: Tone; label: string; live?: boolean }> = {
    unverified: { tone: "neutral", label: "Sin verificar" },
    running: { tone: "ice", label: "Verificando", live: true },
    structural_ok: { tone: "signal", label: "Estructura OK" },
    content_ok: { tone: "signal", label: "Contenido OK" },
    failed: { tone: "danger", label: "Verificación fallida" },
  };
  const item = map[state] ?? { tone: "neutral" as Tone, label: state };
  return (
    <Badge tone={item.tone}>
      <Dot tone={item.tone} live={item.live} />
      {item.label}
    </Badge>
  );
}

// Tono y etiqueta de severidad de alerta.
export function severityTone(severity: string): Tone {
  switch (severity) {
    case "critical":
      return "danger";
    case "high":
      return "alert";
    case "medium":
      return "ice";
    default:
      return "neutral";
  }
}

export function severityLabel(severity: string): string {
  switch (severity) {
    case "critical":
      return "Crítica";
    case "high":
      return "Alta";
    case "medium":
      return "Media";
    case "low":
      return "Baja";
    default:
      return severity;
  }
}

// Tono del estado del repositorio (resumen y repos).
export function repoStatusTone(status: string): Tone {
  switch (status) {
    case "ok":
      return "signal";
    case "limited":
    case "degraded":
      return "alert";
    case "error":
      return "danger";
    default:
      return "neutral";
  }
}

export function repoStatusLabel(status: string): string {
  switch (status) {
    case "ok":
      return "Operativo";
    case "limited":
      return "Limitado";
    case "error":
      return "Error";
    case "unconfigured":
      return "Sin configurar";
    case "unknown":
      return "Desconocido";
    default:
      return status;
  }
}

// Etiquetas de disparo de un job.
export function triggerLabel(trigger?: string): string {
  switch (trigger) {
    case "manual":
      return "Manual";
    case "schedule":
      return "Programado";
    case "retry":
      return "Reintento";
    case "api":
      return "API";
    default:
      return "—";
  }
}

export function Progress({
  value,
  tone = "signal",
  animated = false,
}: {
  value: number;
  tone?: Tone;
  animated?: boolean;
}) {
  const clamped = Math.max(0, Math.min(100, value));
  const bar = {
    signal: "bg-signal",
    alert: "bg-alert",
    danger: "bg-danger",
    neutral: "bg-slate-500",
    ice: "bg-ice",
  }[tone];
  return (
    <div className="h-1.5 w-full overflow-hidden bg-ink-800">
      <div
        className={cx("relative h-full transition-all duration-500", bar, animated && "bar-live")}
        style={{ width: `${clamped}%` }}
      />
    </div>
  );
}

export function PageHeader({
  kicker,
  title,
  description,
  actions,
}: {
  kicker: string;
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4 border-b border-ink-700 pb-5">
      <div>
        <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
          {kicker}
        </p>
        <h1 className="mt-1 font-display text-4xl font-semibold uppercase leading-none tracking-wide text-slate-100 sm:text-5xl">
          {title}
        </h1>
        {description ? (
          <p className="mt-3 max-w-2xl text-sm leading-relaxed text-slate-400">
            {description}
          </p>
        ) : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}

export function Stat({
  index,
  label,
  value,
  hint,
  tone = "signal",
}: {
  index: string;
  label: string;
  value: string | number;
  hint?: string;
  tone?: Tone;
}) {
  const accent = {
    signal: "text-signal",
    alert: "text-alert",
    danger: "text-danger",
    neutral: "text-slate-200",
    ice: "text-ice",
  }[tone];
  return (
    <Card cut className="relative overflow-hidden p-5">
      <div className="absolute right-3 top-2 font-mono text-[9px] tracking-widest2 text-slate-600">
        {index}
      </div>
      <p className="font-mono text-[9px] uppercase tracking-widest2 text-slate-500">{label}</p>
      <p className={cx("mt-3 font-display text-4xl font-semibold leading-none", accent)}>
        {value}
      </p>
      {hint ? <p className="mt-2 text-[11px] text-slate-500">{hint}</p> : null}
    </Card>
  );
}

export function EmptyState({
  index,
  title,
  description,
  action,
}: {
  index: string;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="panel-soft flex flex-col items-start gap-3 p-8">
      <span className="font-mono text-[10px] tracking-widest2 text-slate-600">{index}</span>
      <h3 className="font-display text-2xl uppercase tracking-wide text-slate-200">{title}</h3>
      <p className="max-w-xl text-sm text-slate-400">{description}</p>
      {action}
    </div>
  );
}

export function ErrorBox({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border border-danger/30 bg-danger/5 px-4 py-3">
      <p className="font-mono text-xs text-danger">{message}</p>
      {onRetry ? (
        <Button variant="quiet" size="sm" onClick={onRetry}>
          Reintentar
        </Button>
      ) : null}
    </div>
  );
}

export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="block">
      <span className={labelClass}>{label}</span>
      {children}
      {hint ? <span className="mt-1 block text-[11px] text-slate-500">{hint}</span> : null}
    </label>
  );
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cx(inputClass, props.className)} />;
}

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} className={cx(inputClass, "min-h-24", props.className)} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} className={cx(inputClass, "pr-8", props.className)} />;
}
