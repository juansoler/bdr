"use client";

import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/lib/auth";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";

type ConsoleStatus = "connecting" | "open" | "error";

// Terminal remota: crea una sesión en el servidor, conecta el WebSocket del
// stream y renderiza xterm.js. El contenedor se monta SIEMPRE (los estados
// de espera y error son overlays): si el div no estuviera en el DOM, el
// efecto no podría inicializar xterm y la sesión nunca se abriría.
export function TerminalConsole({ agentId }: { agentId: string }) {
  const { token } = useAuth();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [status, setStatus] = useState<ConsoleStatus>("connecting");
  const [errorMsg, setErrorMsg] = useState("");

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    let disposed = false;
    let ws: WebSocket | null = null;

    const term = new Terminal({
      fontFamily: "'SF Mono', 'Menlo', 'Consolas', 'monospace'",
      fontSize: 12,
      lineHeight: 1.3,
      cursorBlink: true,
      convertEol: false,
      scrollback: 2000,
      theme: {
        background: "#282a36",
        foreground: "#c5c8c6",
        cursor: "#c5c8c6",
        selectionBackground: "#44475a",
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(el);
    fit.fit();

    const dataSub = term.onData((data) => {
      // Entrada del operador: bytes crudos en frame binario.
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data));
      }
    });

    const sendResize = () => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(
          JSON.stringify({ type: "resize", payload: { cols: term.cols, rows: term.rows } }),
        );
      }
    };
    const onWinResize = () => {
      fit.fit();
      sendResize();
    };
    window.addEventListener("resize", onWinResize);

    async function openSession() {
      try {
        const resp = await fetch(`/api/v1/admin/agents/${agentId}/terminal/sessions`, {
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
        };
        if (disposed) return;

        // El servidor puede devolver la URL absoluta (ws://…) o relativa.
        const raw =
          data.stream_url ||
          `/api/v1/admin/terminal/sessions/${data.session_id}/stream`;
        const wsURL = raw.startsWith("ws")
          ? raw
          : `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}${raw}`;

        ws = new WebSocket(wsURL);
        ws.binaryType = "arraybuffer";
        wsRef.current = ws;

        ws.onopen = () => {
          // Ajusta el tamaño inicial de la PTY al del panel.
          sendResize();
        };

        ws.onmessage = (ev) => {
          if (ev.data instanceof ArrayBuffer) {
            term.write(new Uint8Array(ev.data));
            if (status !== "open") setStatus("open");
            return;
          }
          if (typeof ev.data !== "string") return;
          try {
            const msg = JSON.parse(ev.data) as {
              type: string;
              payload?: { code?: number; reason?: string; message?: string };
            };
            switch (msg.type) {
              case "open": // sesión lista (PTY abierta en el agente)
                setStatus("open");
                term.focus();
                break;
              case "exit":
                setStatus("error");
                setErrorMsg(`El shell terminó (código ${msg.payload?.code ?? "?"}).`);
                break;
              case "closed":
                setStatus("error");
                setErrorMsg(
                  `Sesión cerrada${msg.payload?.reason ? `: ${msg.payload.reason}` : "."}`,
                );
                break;
              case "error":
                setStatus("error");
                setErrorMsg(`Error: ${msg.payload?.message ?? "desconocido"}`);
                break;
            }
          } catch {
            // Mensaje de control no JSON: ignorar.
          }
        };

        ws.onerror = () => {
          setStatus((prev) => {
            if (prev === "error") return prev;
            setErrorMsg("Error de conexión WebSocket con el servidor.");
            return "error";
          });
        };

        ws.onclose = () => {
          setStatus((prev) => (prev === "error" ? prev : "open"));
          term.write("\r\n\u001b[38;5;245m— conexión finalizada —\u001b[0m\r\n");
        };
      } catch (e) {
        if (disposed) return;
        setStatus("error");
        setErrorMsg(e instanceof Error ? e.message : "Error desconocido");
      }
    }

    openSession();

    return () => {
      disposed = true;
      window.removeEventListener("resize", onWinResize);
      dataSub.dispose();
      ws?.close();
      term.dispose();
      wsRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, token]);

  return (
    <div className="relative h-[600px] w-full border border-ink-700 bg-[#282a36]">
      {/* Contenedor SIEMPRE montado: xterm necesita el elemento en el DOM. */}
      <div ref={containerRef} className="h-full w-full p-1" />

      {status === "connecting" ? (
        <div className="absolute inset-0 flex items-center justify-center bg-[#282a36]">
          <span className="h-8 w-8 animate-spin border border-signal border-t-transparent" />
          <span className="ml-4 font-mono text-sm text-slate-300">
            Conectando consola remota…
          </span>
        </div>
      ) : null}

      {status === "error" ? (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-[#282a36]/95 px-6 text-center">
          <p className="font-mono text-xs uppercase tracking-widest2 text-signal">
            La consola no está disponible
          </p>
          <p className="max-w-md break-all text-sm text-slate-300">{errorMsg}</p>
          <p className="max-w-md text-xs text-slate-500">
            Comprueba que el equipo esté online y que el agente tenga la versión
            con canal de terminal (≥ 0.12.2).
          </p>
        </div>
      ) : null}
    </div>
  );
}