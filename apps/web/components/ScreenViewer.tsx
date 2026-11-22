"use client";

import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/lib/auth";
import { Button } from "./ui";
import RFB from "@novnc/novnc";

type ScreenStatus = "connecting" | "open" | "error";

// Visor de escritorio remoto: crea una sesión de pantalla en el servidor,
// conecta el WebSocket del stream y renderiza noVNC. El stream transporta
// SOLO bytes del protocolo RFB (VNC): el servidor no envía mensajes de
// control texto (corromperían el protocolo) — los estados de «sesión
// terminada» viajan en el frame de cierre de WebSocket, capturado aquí con
// un listener propio antes de que noVNC lo procese. El contenedor se monta
// SIEMPRE (los estados de espera y error son overlays): si el div no
// estuviera en el DOM, RFB no podría inicializar el canvas.
export function ScreenViewer({ agentId }: { agentId: string }) {
  const { token } = useAuth();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const rfbRef = useRef<RFB | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [status, setStatus] = useState<ScreenStatus>("connecting");
  const [errorMsg, setErrorMsg] = useState("");

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const target: HTMLElement = el;

    let disposed = false;
    let ws: WebSocket | null = null;
    let rfb: RFB | null = null;
    // Estado local espejo (los handlers se registran una sola vez por
    // efecto): evita efectos dentro de los actualizadores de setState.
    let current: ScreenStatus = "connecting";

    const endWithReason = (reason: string) => {
      if (disposed || current === "error") return;
      const wasOpen = current === "open";
      current = "error";
      setErrorMsg(
        wasOpen
          ? `La sesión de pantalla ha terminado${reason ? `: ${reason}` : "."}`
          : reason || "No se pudo abrir la sesión de pantalla.",
      );
      setStatus("error");
    };

    async function openSession() {
      try {
        const resp = await fetch(`/api/v1/admin/agents/${agentId}/screen/sessions`, {
          method: "POST",
          headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
          credentials: "include",
          body: "{}",
        });
        if (!resp.ok) {
          const txt = await resp.text();
          throw new Error(`HTTP ${resp.status} — ${txt.slice(0, 300)}`);
        }
        const data = (await resp.json()) as {
          session_id: string;
          stream_url?: string;
          stream_ticket?: string;
        };
        if (disposed) return;

        // El servidor puede devolver la URL absoluta (ws://…) o relativa.
        // Los WebSockets no pueden enviar la cabecera Authorization: la
        // credencial viaja como ?ticket=… (secreto entregado al crear la sesión).
        const raw =
          data.stream_url ||
          `/api/v1/admin/screen/sessions/${data.session_id}/stream`;
        const wsURL = (raw.startsWith("ws")
          ? raw
          : `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}${raw}`) +
          (data.stream_ticket ? `?ticket=${encodeURIComponent(data.stream_ticket)}` : "");

        ws = new WebSocket(wsURL);
        ws.binaryType = "arraybuffer";
        wsRef.current = ws;

        // Motivo de cierre: el servidor lo pone en el frame close (el canal
        // no admite texto: solo RFB). addEventListener coexiste con los
        // handlers onclose que noVNC instala por encima.
        ws.addEventListener("close", (ev) => {
          const reason = (ev as CloseEvent).reason?.trim() ?? "";
          endWithReason(reason);
        });

        rfb = new RFB(target, ws, { shared: true });
        rfbRef.current = rfb;
        rfb.scaleViewport = true; // ajusta el escritorio al tamaño del panel
        rfb.resizeSession = false; // no cambia la resolución del equipo
        rfb.qualityLevel = 6; // medium
        rfb.compressionLevel = 5;
        rfb.background = "#282a36";
        rfb.showDotCursor = true;

        rfb.addEventListener("connect", () => {
          if (disposed) return;
          current = "open";
          setStatus("open");
        });
        rfb.addEventListener("disconnect", () => {
          // El cierre real lo reporta el listener de close del WebSocket
          // (con motivo); este evento solo evita estados colgantes.
          endWithReason("");
        });
      } catch (e) {
        if (disposed) return;
        setStatus("error");
        setErrorMsg(e instanceof Error ? e.message : "Error desconocido");
      }
    }

    openSession();

    return () => {
      disposed = true;
      try {
        rfb?.disconnect();
      } catch {
        // El socket ya no está disponible: nada que hacer.
      }
      ws?.close();
      rfbRef.current = null;
      wsRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, token]);

  const sendCtrlAltDel = () => rfbRef.current?.sendCtrlAltDel();

  const fullscreen = () => {
    const el = containerRef.current?.parentElement;
    if (!el) return;
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void el.requestFullscreen().catch(() => {
        /* el navegador puede denegarlo: sin acción */
      });
    }
  };

  return (
    <div className="flex h-full w-full flex-col">
      <div className="flex items-center justify-between border-b border-ink-700 px-3 py-2">
        <div className="flex items-center gap-2">
          <Button
            variant="quiet"
            size="sm"
            onClick={sendCtrlAltDel}
            disabled={status !== "open"}
          >
            Ctrl+Alt+Supr
          </Button>
          <Button
            variant="quiet"
            size="sm"
            onClick={fullscreen}
            disabled={status !== "open"}
          >
            Pantalla completa
          </Button>
        </div>
        <span className="font-mono text-[9px] uppercase tracking-widest2 text-slate-600">
          Solo visible durante la sesión · se cierra al cerrar este visor
        </span>
      </div>
      <div className="relative min-h-0 flex-1 w-full bg-[#282a36]">
        {/* Contenedor SIEMPRE montado: noVNC necesita el elemento en el DOM. */}
        <div ref={containerRef} className="h-full w-full" />

        {status === "connecting" ? (
          <div className="absolute inset-0 flex items-center justify-center bg-[#282a36]">
            <span className="h-8 w-8 animate-spin border border-signal border-t-transparent" />
            <span className="ml-4 font-mono text-sm text-slate-300">
              Conectando al escritorio remoto…
            </span>
          </div>
        ) : null}

        {status === "error" ? (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-[#282a36]/95 px-6 text-center">
            <p className="font-mono text-xs uppercase tracking-widest2 text-signal">
              La pantalla no está disponible
            </p>
            <p className="max-w-md break-all text-sm text-slate-300">{errorMsg}</p>
            <p className="max-w-md text-xs text-slate-500">
              El equipo necesita una sesión gráfica activa y un servidor VNC en
              localhost (macOS: Compartir pantalla · Linux: x11vnc · Windows:
              servidor VNC en 127.0.0.1:5900).
            </p>
          </div>
        ) : null}
      </div>
    </div>
  );
}
