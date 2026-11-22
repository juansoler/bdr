"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/lib/auth";
import { Button } from "./ui";
import { keysymFromEvent, modifierKeysyms } from "@/lib/keysym";

type ScreenStatus = "connecting" | "open" | "error";

// Tipos de mensaje del protocolo de tiles (primer byte del frame binario).
const MSG_SCREEN_INFO = 0x01;
const MSG_TILE = 0x02;
const MSG_CURSOR = 0x04;
const MSG_MOUSE = 0x10;
const MSG_KEY = 0x11;
const MSG_QUALITY = 0x12;
const MSG_REFRESH = 0x13;

const DEFAULT_QUALITY = 60;
const DEFAULT_INTERVAL_MS = 120;

// Visor de escritorio remoto (protocolo de tiles estilo MeshCentral): crea
// una sesión de pantalla en el servidor, conecta el WebSocket del stream y
// pinta los tiles JPEG en un canvasEl. La entrada del operador (ratón/teclado)
// viaja en mensajes binarios al agente, que la inyecta nativamente en el
// equipo — sin servicios VNC de terceros en ninguna plataforma.
export function ScreenViewer({
  agentId,
  osFamily,
}: {
  agentId: string;
  osFamily?: string;
}) {
  const { token } = useAuth();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const cursorRef = useRef<HTMLDivElement | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [status, setStatus] = useState<ScreenStatus>("connecting");
  const [errorMsg, setErrorMsg] = useState("");

  useEffect(() => {
    const canvasEl = canvasRef.current;
    const containerEl = containerRef.current;
    if (!canvasEl || !containerEl) return;
    const ctx0 = canvasEl.getContext("2d");
    if (ctx0 === null) return; // sin canvas 2D: sin visor
    // Alias tipados NO-nulo para las closures (TS no propaga el narrowing).
    const canvas: HTMLCanvasElement = canvasEl;
    const container: HTMLElement = containerEl;
    const ctx: CanvasRenderingContext2D = ctx0;

    let disposed = false;
    let ws: WebSocket | null = null;
    let current: ScreenStatus = "connecting";
    // Teclas modificadoras que dejamos pulsadas en el equipo remoto.
    const heldKeys = new Set<number>();

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

    const sendBinary = (data: Uint8Array) => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(data);
      }
    };

    const sendKey = (down: boolean, keysym: number) => {
      const buf = new Uint8Array(6);
      const dv = new DataView(buf.buffer);
      buf[0] = MSG_KEY;
      buf[1] = down ? 0 : 1;
      dv.setUint32(2, keysym);
      sendBinary(buf);
      if (down) {
        heldKeys.add(keysym);
      } else {
        heldKeys.delete(keysym);
      }
    };

    // Ratón: coordenadas escaladas del canvas al framebuffer.
    const sendMouse = (action: number, buttons: number, ev: MouseEvent) => {
      const rect = canvasEl.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) return;
      const x = Math.round(((ev.clientX - rect.left) * canvasEl.width) / rect.width);
      const y = Math.round(((ev.clientY - rect.top) * canvasEl.height) / rect.height);
      const buf = new Uint8Array(7);
      const dv = new DataView(buf.buffer);
      buf[0] = MSG_MOUSE;
      buf[1] = action;
      buf[2] = buttons;
      dv.setUint16(3, Math.max(0, Math.min(0xffff, x)));
      dv.setUint16(5, Math.max(0, Math.min(0xffff, y)));
      sendBinary(buf);
    };

    let buttons = 0;
    const onMouseMove = (ev: MouseEvent) => {
      ev.preventDefault();
      sendMouse(0, buttons, ev);
    };
    const onMouseDown = (ev: MouseEvent) => {
      ev.preventDefault();
      buttons |= bitForButton(ev.button);
      sendMouse(1, buttons, ev);
    };
    const onMouseUp = (ev: MouseEvent) => {
      ev.preventDefault();
      buttons &= ~bitForButton(ev.button);
      sendMouse(2, buttons, ev);
    };
    const onContextMenu = (ev: MouseEvent) => ev.preventDefault();

    // Teclado: capturado a nivel de documento mientras el visor está abierto.
    const onKeyDown = (ev: KeyboardEvent) => {
      if (current !== "open") return;
      const keysym = keysymFromEvent(ev);
      if (keysym === null) return;
      ev.preventDefault();
      if (!ev.repeat) sendKey(true, keysym);
    };
    const onKeyUp = (ev: KeyboardEvent) => {
      if (current !== "open") return;
      const keysym = keysymFromEvent(ev);
      if (keysym === null) return;
      ev.preventDefault();
      sendKey(false, keysym);
    };
    // Al perder el foco, libera modificadores para no dejarlos "pegados".
    const onBlur = () => {
      for (const keysym of [...heldKeys]) {
        if (modifierKeysyms.includes(keysym)) sendKey(false, keysym);
      }
    };

    // Ajusta el tamaño CSS del canvas al contenedor manteniendo el aspecto.
    const fitCanvas = () => {
      if (canvasEl.width === 0 || canvasEl.height === 0) return;
      const cw = containerEl.clientWidth;
      const ch = containerEl.clientHeight;
      if (cw === 0 || ch === 0) return;
      const scale = Math.min(cw / canvasEl.width, ch / canvasEl.height);
      canvasEl.style.width = `${Math.floor(canvasEl.width * scale)}px`;
      canvasEl.style.height = `${Math.floor(canvasEl.height * scale)}px`;
    };
    window.addEventListener("resize", fitCanvas);

    function drawTile(dv: DataView, bytes: Uint8Array) {
      const x = dv.getUint16(1);
      const y = dv.getUint16(3);
      const w = dv.getUint16(5);
      const h = dv.getUint16(7);
      const jpegData = bytes.slice(9);
      const blob = new Blob([jpegData], { type: "image/jpeg" });
      void createImageBitmap(blob).then((bmp) => {
        ctx.drawImage(bmp, x, y, w, h);
        bmp.close();
      });
    }

    async function openSession() {
      try {
        // Pre-flight: el agente debe soportar el protocolo de tiles (≥ 0.16.0).
        // Los agentes 0.15.x respondían con provider vnc-* (RFB/noVNC).
        const capResp = await fetch(
          `/api/v1/admin/agents/${agentId}/screen${osFamily ? `?os_family=${encodeURIComponent(osFamily)}` : ""}`,
          { headers: { Authorization: `Bearer ${token}` }, credentials: "include" },
        );
        if (capResp.ok) {
          const cap = (await capResp.json()) as { online?: boolean; provider?: string };
          if (cap.provider && !cap.provider.startsWith("tiles")) {
            throw new Error(
              "El agente de este equipo no soporta el escritorio remoto nativo: actualiza el agente a 0.16.0 o superior y reintenta.",
            );
          }
          if (cap.online === false) {
            throw new Error(
              "El agente no tiene el canal de pantalla conectado (¿agente parado o versión antigua?).",
            );
          }
        }
        if (disposed) return;

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

        ws.addEventListener("close", (ev) => {
          const reason = (ev as CloseEvent).reason?.trim() ?? "";
          endWithReason(reason);
        });

        ws.onopen = () => {
          // Calidad inicial: JPEG 60, frame cada 120 ms.
          const buf = new Uint8Array(4);
          const dv = new DataView(buf.buffer);
          buf[0] = MSG_QUALITY;
          buf[1] = DEFAULT_QUALITY;
          dv.setUint16(2, DEFAULT_INTERVAL_MS);
          sendBinary(buf);
        };

        ws.onmessage = (ev) => {
          if (typeof ev.data === "string" || disposed) return; // control: ignorar
          const bytes = new Uint8Array(ev.data as ArrayBuffer);
          const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
          switch (bytes[0]) {
            case MSG_SCREEN_INFO: {
              const w = dv.getUint16(1);
              const h = dv.getUint16(3);
              canvas.width = w;
              canvas.height = h;
              ctx.fillStyle = "#282a36";
              ctx.fillRect(0, 0, w, h);
              fitCanvas();
              if (current !== "open") {
                current = "open";
                setStatus("open");
              }
              break;
            }
            case MSG_TILE:
              drawTile(dv, bytes);
              break;
            case MSG_CURSOR: {
              // Cursor remoto: BitBlt no captura el cursor, así que lo
              // dibuja el visor (posición del agente, escala del canvas).
              const cx = dv.getUint16(1);
              const cy = dv.getUint16(3);
              if (cursorRef.current && canvas.width > 0) {
                const scaleX = canvas.clientWidth / canvas.width;
                const scaleY = canvas.clientHeight / canvas.height;
                cursorRef.current.style.left = `${canvas.offsetLeft + cx * scaleX}px`;
                cursorRef.current.style.top = `${canvas.offsetTop + cy * scaleY}px`;
                cursorRef.current.style.display = "block";
              }
              break;
            }
          }
        };

        // Entrada: solo activa mientras la sesión está abierta.
        canvas.addEventListener("mousemove", onMouseMove);
        canvas.addEventListener("mousedown", onMouseDown);
        canvas.addEventListener("mouseup", onMouseUp);
        canvas.addEventListener("contextmenu", onContextMenu);
        document.addEventListener("keydown", onKeyDown);
        document.addEventListener("keyup", onKeyUp);
        window.addEventListener("blur", onBlur);
      } catch (e) {
        if (disposed) return;
        current = "error";
        setStatus("error");
        setErrorMsg(e instanceof Error ? e.message : "Error desconocido");
      }
    }

    openSession();

    return () => {
      disposed = true;
      window.removeEventListener("resize", fitCanvas);
      canvasEl.removeEventListener("mousemove", onMouseMove);
      canvasEl.removeEventListener("mousedown", onMouseDown);
      canvasEl.removeEventListener("mouseup", onMouseUp);
      canvasEl.removeEventListener("contextmenu", onContextMenu);
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("keyup", onKeyUp);
      window.removeEventListener("blur", onBlur);
      try {
        ws?.close();
      } catch {
        // El socket ya no está disponible: nada que hacer.
      }
      wsRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, token]);

  // Ctrl+Alt+Supr al equipo remoto (SAS: requiere privilegio del helper).
  const sendCtrlAltDel = useCallback(() => {
    const send = (down: boolean, keysym: number) => {
      const ws = wsRef.current;
      if (!ws || ws.readyState !== WebSocket.OPEN) return;
      const buf = new Uint8Array(6);
      const dv = new DataView(buf.buffer);
      buf[0] = MSG_KEY;
      buf[1] = down ? 0 : 1;
      dv.setUint32(2, keysym);
      ws.send(buf);
    };
    send(true, 0xffe3); // Control_L
    send(true, 0xffe9); // Alt_L
    send(true, 0xffff); // Delete
    setTimeout(() => send(false, 0xffff), 50);
    setTimeout(() => send(false, 0xffe9), 100);
    setTimeout(() => send(false, 0xffe3), 150);
  }, []);

  const fullscreen = () => {
    const el = containerRef.current?.parentElement;
    if (!el) return;
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void el.requestFullscreen().then(() => fitCanvasSoon()).catch(() => {
        /* el navegador puede denegarlo: sin acción */
      });
    }
  };
  const fitCanvasSoon = () => {
    // Reajusta tras la transición de fullscreen.
    setTimeout(() => window.dispatchEvent(new Event("resize")), 300);
  };

  const refresh = useCallback(() => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(new Uint8Array([MSG_REFRESH]));
    }
  }, []);

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
            onClick={refresh}
            disabled={status !== "open"}
          >
            Refrescar
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
      <div ref={containerRef} className="relative min-h-0 flex-1 w-full bg-[#282a36] overflow-hidden">
        {/* Canvas SIEMPRE montado; se centra y escala manteniendo el aspecto. */}
        <div className="flex h-full w-full items-center justify-center">
          <div className="relative">
            <canvas
              ref={canvasRef}
              className="border border-ink-700"
              style={{ cursor: status === "open" ? "crosshair" : "default" }}
            />
            {/* Cursor remoto (BitBlt no lo captura): SVG flecha posicionado. */}
            <div
              ref={cursorRef}
              className="pointer-events-none absolute z-10 hidden"
              style={{ width: 18, height: 24 }}
            >
              <svg width="18" height="24" viewBox="0 0 18 24" xmlns="http://www.w3.org/2000/svg">
                <path
                  d="M2 1 L2 19 L6.5 14.5 L9.5 21 L12.5 19.5 L9.5 13.5 L15 13.5 Z"
                  fill="#ffffff"
                  stroke="#000000"
                  strokeWidth="1.2"
                />
              </svg>
            </div>
          </div>
        </div>

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
              El equipo necesita una sesión gráfica activa (Windows: el agente
              lanza su helper de sesión; Linux: X11 accesible; macOS: «Compartir
              pantalla» activado).
            </p>
          </div>
        ) : null}
      </div>
    </div>
  );
}

// bitForButton mapea MouseEvent.button a la máscara del protocolo
// (bit0 izq, bit1 centro, bit2 der).
function bitForButton(button: number): number {
  switch (button) {
    case 0:
      return 1;
    case 1:
      return 2;
    case 2:
      return 4;
    default:
      return 0;
  }
}
