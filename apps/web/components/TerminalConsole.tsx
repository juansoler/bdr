"use client";

import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/lib/auth";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";

export function TerminalConsole({ agentId }: { agentId: string }) {
  const { token } = useAuth();
  const terminalRef = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [isConnecting, setIsConnecting] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);

  useEffect(() => {
    if (!terminalRef.current) return;

    const termInstance = new Terminal({
      fontFamily: "'SF Mono', 'Consolas', 'Monaco', 'monospace'",
      fontSize: 12,
      lineHeight: 1.4,
      theme: {
        background: "#282a36",
        foreground: "#c5c8c6",
        cursor: "#c5c8c6",
        cursorAccent: "#282a36",
        selectionBackground: "#44475a",
        black: "#1b1c1f",
        red: "#ff5555",
        green: "#50fa7b",
        yellow: "#f1fa8c",
        blue: "#bd93f9",
        magenta: "#ff79c6",
        cyan: "#8be9fd",
        white: "#c5c8c6",
        brightBlack: "#6272a4",
        brightRed: "#ff6e6e",
        brightGreen: "#69ff94",
        brightYellow: "#ffffa5",
        brightBlue: "#d6acff",
        brightMagenta: "#ff92df",
        brightCyan: "#a4ffff",
        brightWhite: "#ffffff",
      },
      cursorBlink: true,
      convertEol: true,
      scrollback: 1000,
    });

    term.current = termInstance;
    termInstance.open(terminalRef.current);

    const fit = new FitAddon();
    fitAddon.current = fit;
    termInstance.loadAddon(fit);
    fit.fit();

    termInstance.loadAddon(new WebLinksAddon());

    // Enviar entrada del terminal al WebSocket
    const disposable = termInstance.onData((data) => {
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        // Enviar como mensaje binario (bytes crudos)
        const encoder = new TextEncoder();
        wsRef.current.send(encoder.encode(data));
      }
    });

    // Crear sesión en el servidor
    async function openSession() {
      try {
        const resp = await fetch(
          `/api/v1/admin/agents/${agentId}/terminal/sessions`,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              Authorization: `Bearer ${token}`,
            },
            credentials: "include",
          }
        );

        if (!resp.ok) {
          const txt = await resp.text();
          throw new Error(`HTTP ${resp.status}: ${txt}`);
        }

        const data = (await resp.json()) as {
          session_id: string;
          state: string;
          cols: number;
          rows: number;
          stream_url: string;
          privilege: string;
        };
        setSessionId(data.session_id);

        // El servidor puede devolver la URL absoluta (ws://host/...) o
        // relativa (/api/v1/...): normalízala contra el origen actual.
        const wsURL = data.stream_url?.startsWith("ws")
          ? data.stream_url
          : `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}${data.stream_url}`;

        // Conectar WebSocket (mismo origen: la cookie de sesión viaja sola)
        const ws = new WebSocket(wsURL);
        ws.binaryType = "arraybuffer";
        wsRef.current = ws;

        ws.onopen = () => {
          console.log("Terminal WebSocket conectado");
          // Notificar tamaño inicial al servidor (resize)
          ws.send(
            JSON.stringify({
              type: "resize",
              payload: { cols: termInstance.cols, rows: termInstance.rows },
            })
          );
        };

        ws.onmessage = (event) => {
          if (event.data instanceof ArrayBuffer) {
            // Datos binarios: salida del terminal
            termInstance.write(new Uint8Array(event.data));
          } else if (typeof event.data === "string") {
            try {
              const msg = JSON.parse(event.data);
              switch (msg.type) {
                case "open":
                  console.log("Sesión de terminal lista");
                  break;
                case "exit":
                  console.log("Shell terminó:", msg.payload);
                  setError(`Shell terminó (código ${msg.payload?.code ?? "?"})`);
                  break;
                case "closed":
                  console.log("Sesión cerrada:", msg.payload);
                  setError("Sesión de terminal cerrada");
                  break;
                case "error":
                  console.error("Error de terminal:", msg.payload);
                  setError(`Error: ${msg.payload?.message ?? "desconocido"}`);
                  break;
              }
            } catch {
              // No es JSON, ignorar
            }
          }
        };

        ws.onerror = (err) => {
          console.error("WebSocket error:", err);
          setError("Error de conexión WebSocket");
        };

        ws.onclose = () => {
          console.log("WebSocket cerrado");
          if (!error) {
            setError("Conexión finalizada");
          }
        };
      } catch (e: any) {
        console.error("Error abriendo terminal:", e);
        setError(e.message || "Error desconocido");
      } finally {
        setIsConnecting(false);
      }
    }

    openSession();

    // Ajustar tamaño al cambiar ventana
    const handleResize = () => {
      fitAddon.current?.fit();
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        wsRef.current.send(
          JSON.stringify({
            type: "resize",
            payload: { cols: termInstance.cols, rows: termInstance.rows },
          })
        );
      }
    };

    window.addEventListener("resize", handleResize);

    return () => {
      window.removeEventListener("resize", handleResize);
      disposable.dispose();
      wsRef.current?.close();
      termInstance.dispose();
      term.current = null;
    };
  }, [agentId, token]);

  if (isConnecting) {
    return (
      <div className="h-[600px] w-full flex items-center justify-center">
        <span className="h-8 w-8 animate-spin border border-signal border-t-transparent" />
        <span className="ml-4 text-slate-300">Conectando consola remota…</span>
      </div>
    );
  }

  if (error && !sessionId) {
    return (
      <div className="h-[600px] w-full border border-ink-700 bg-signal/5 p-4 text-signal">
        <p className="font-mono text-sm uppercase tracking-widest2">No se pudo abrir la consola</p>
        <p className="mt-2">{error}</p>
        <p className="mt-2 text-sm text-slate-500">
          Asegúrate de que el agente esté online y tenga conexión de terminal activa.
        </p>
      </div>
    );
  }

  return (
    <div className="h-[600px] w-full border border-ink-700">
      <div ref={terminalRef} className="h-full w-full" />
    </div>
  );
}