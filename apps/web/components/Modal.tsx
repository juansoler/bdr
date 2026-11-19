"use client";

import { useEffect, type ReactNode } from "react";
import { Button, cx } from "./ui";

// Contenedor genérico de diálogo: overlay, panel recortado y cabecera.
export function Modal({
  kicker,
  title,
  onClose,
  children,
  wide = false,
}: {
  kicker: string;
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/80 px-4 py-8 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={cx(
          "panel cut scroll-thin max-h-[90vh] w-full overflow-y-auto p-6 sm:p-8",
          wide ? "max-w-3xl" : "max-w-2xl",
        )}
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
              {kicker}
            </p>
            <h2 className="mt-1 font-display text-3xl uppercase tracking-wide text-slate-100">
              {title}
            </h2>
          </div>
          <Button variant="ghost" size="sm" onClick={onClose} aria-label="Cerrar">
            Cerrar
          </Button>
        </div>
        {children}
      </div>
    </div>
  );
}

// Casilla de verificación con el estilo de la consola.
export function Check({
  label,
  hint,
  checked,
  onChange,
  disabled,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label className={cx("flex items-start gap-2.5", disabled && "opacity-60")}>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-0.5 h-4 w-4 shrink-0 accent-signal"
      />
      <span>
        <span className="block text-sm text-slate-200">{label}</span>
        {hint ? <span className="mt-0.5 block text-[11px] text-slate-500">{hint}</span> : null}
      </span>
    </label>
  );
}
