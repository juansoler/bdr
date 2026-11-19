"use client";

import { useState } from "react";
import { api, type Agent } from "@/lib/api";
import { Button, ErrorBox, Field, Textarea } from "./ui";
import { Modal } from "./Modal";

// Diálogo de revocación: exige teclear el hostname para habilitar la acción.
export function RevokeModal({
  token,
  agent,
  onClose,
  onDone,
}: {
  token: string;
  agent: Agent;
  onClose: () => void;
  onDone: () => void | Promise<void>;
}) {
  const [reason, setReason] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const confirmed = confirm.trim().toLowerCase() === agent.hostname.toLowerCase();

  async function revoke() {
    setBusy(true);
    setError(null);
    try {
      await api(`/api/v1/admin/agents/${agent.id}/revoke`, token, {
        method: "POST",
        body: JSON.stringify({
          reason: reason.trim() || "Revocado desde la consola de administración",
        }),
      });
      await onDone();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "No se pudo revocar el equipo.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal kicker="Acción destructiva" title="Revocar equipo" onClose={onClose}>
      <div className="mt-6 space-y-5">
        <div className="border border-danger/30 bg-danger/5 p-4 text-sm leading-relaxed text-slate-300">
          El agente <span className="font-mono text-danger">{agent.hostname}</span> perderá el
          acceso al plano de control y al repositorio. No se eliminarán sus copias. La operación
          queda registrada en la auditoría.
        </div>

        <Field label="Motivo" hint="Se guarda en el registro de auditoría.">
          <Textarea
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="Equipo retirado / comprometido / sustituido…"
            rows={2}
          />
        </Field>

        <Field
          label={`Confirma tecleando «${agent.hostname}»`}
          hint="Escribe el hostname exacto para habilitar el botón."
        >
          <input
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            placeholder={agent.hostname}
            autoComplete="off"
            spellCheck={false}
            className="w-full rounded-sm border border-danger/40 bg-ink-950/70 px-3 py-2.5 font-mono text-sm text-slate-100 placeholder:text-slate-600 transition focus:border-danger/70 focus:outline-none focus:ring-1 focus:ring-danger/30"
          />
        </Field>

        {error ? <ErrorBox message={error} /> : null}

        <div className="flex items-center justify-end gap-2 border-t border-ink-700 pt-5">
          <Button variant="quiet" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="danger" onClick={revoke} loading={busy} disabled={!confirmed}>
            Revocar acceso
          </Button>
        </div>
      </div>
    </Modal>
  );
}
