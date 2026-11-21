// Package contracts define los tipos JSON compartidos para el subsistema de
// Remote Access (terminales remotas). Sigue las reglas de compatibilidad de
// /api/v1 (docs/06-api.md §9): los cambios son aditivos, los campos nuevos
// son opcionales y los clientes ignoran los desconocidos.
package contracts

import (
	"encoding/json"
	"time"
)

// --- Estados de una sesión de terminal -------------------------------------

const (
	// TerminalSessionPending: creada en el servidor, esperando a que el agente
	// acepte y abra la pseudoterminal local.
	TerminalSessionPending = "pending"
	// TerminalSessionActive: PTY abierta y conexión de datos establecida.
	TerminalSessionActive = "active"
	// TerminalSessionClosed: cerrada ordenadamente por el operador, el agente
	// o el servidor (incluido tiempo de inactividad).
	TerminalSessionClosed = "closed"
	// TerminalSessionInterrupted: interrumpida por pérdida del canal de
	// control, reinicio del servidor o cierre forzado.
	TerminalSessionInterrupted = "interrupted"
)

// --- Niveles de privilegio --------------------------------------------------

const (
	// TerminalPrivilegeStandard ejecuta el shell bajo una identidad
	// restringida (sin elevación automática de los privilegios del agente).
	TerminalPrivilegeStandard = "standard"
	// TerminalPrivilegeElevated requiere step-up (TOTP) y es un permiso
	// independiente del servicio BDR. Fase 1: reservado, no implementado.
	TerminalPrivilegeElevated = "elevated"
)

// --- Shells soportados -------------------------------------------------------

const (
	TerminalShellDefault    = "default"
	TerminalShellBash       = "bash"
	TerminalShellZsh        = "zsh"
	TerminalShellPowerShell = "powershell"
)

// --- Capacidades de acceso a terminal ---------------------------------------

const (
	CapTerminalRead      = "terminal:read"      // consultar disponibilidad y listar sesiones
	CapTerminalConnect   = "terminal:connect"   // abrir una sesión y usar el WebSocket de datos
	CapTerminalElevated  = "terminal:elevated"  // abrir terminal con identidad elevada (step-up)
	CapTerminalTerminate = "terminal:terminate" // cerrar cualquier sesión
	CapTerminalAudit     = "terminal:audit"     // consultar auditoría de terminal
)

// DefaultTerminalCols y Rows son los valores por defecto de una sesión nueva.
const (
	DefaultTerminalCols = 120
	DefaultTerminalRows = 36
)

// MaxTerminalOutputBytes es el tamaño máximo de un mensaje binario de datos
// de terminal (protege contra saturación de buffers del relay).
const MaxTerminalOutputBytes = 1 << 20 // 1 MiB por frame de datos

// --- CreateTerminalSessionRequest -------------------------------------------

// CreateTerminalSessionRequest es el cuerpo de
// POST /api/v1/admin/agents/{id}/terminal/sessions.
type CreateTerminalSessionRequest struct {
	// Shell indica el shell a arrancar en el agente (default | bash | zsh | powershell).
	Shell string `json:"shell,omitempty"`
	// Cols y Rows son las dimensiones iniciales de la pseudoterminal.
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	// Privilege es el nivel de privilegios solicitado (standard | elevated).
	// En Fase 1 solo se admite "standard".
	Privilege string `json:"privilege,omitempty"`
}

// CreateTerminalSessionResponse se devuelve al crear una sesión.
type CreateTerminalSessionResponse struct {
	SessionID string    `json:"session_id"`
	AgentID   string    `json:"agent_id"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
	StreamURL string    `json:"stream_url,omitempty"` // WebSocket de datos para el operador
}

// TerminalSessionView es la vista de una sesión de terminal.
type TerminalSessionView struct {
	SessionID    string     `json:"session_id"`
	AgentID      string     `json:"agent_id"`
	Hostname     string     `json:"hostname,omitempty"`
	OSFamily     string     `json:"os_family,omitempty"`
	Shell        string     `json:"shell"`
	Privilege    string     `json:"privilege"`
	State        string     `json:"state"`
	Cols         uint16     `json:"cols"`
	Rows         uint16     `json:"rows"`
	CreatedAt    time.Time  `json:"created_at"`
	OpenedAt     *time.Time `json:"opened_at,omitempty"`
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
	ExpiresAt    time.Time  `json:"expires_at"`
	Reason       string     `json:"reason,omitempty"`
	OperatorID   string     `json:"operator_id"`
	OperatorName string     `json:"operator_name,omitempty"`
	StreamURL    string     `json:"stream_url,omitempty"` // WebSocket de datos para el operador
	// StreamTicket solo se devuelve en la creación de la sesión: los
	// WebSockets del navegador no pueden enviar cabeceras de autorización,
	// así que el operador lo presenta como ?ticket=… en la URL del stream.
	StreamTicket string `json:"stream_ticket,omitempty"`
}

// TerminalSessionList es la respuesta de GET /api/v1/admin/terminal/sessions.
type TerminalSessionList struct {
	Items []TerminalSessionView `json:"items"`
}

// TerminalCapabilities devuelve qué puede hacer el autenticado con este agente.
type TerminalCapabilities struct {
	AgentID    string   `json:"agent_id"`
	Online     bool     `json:"online"`
	OSFamily   string   `json:"os_family"`
	Shells     []string `json:"shells"`
	Privileges []string `json:"privileges"`
}

// --- Protocolo de datos (WebSocket) -----------------------------------------

// TerminalMessage es el sobre para mensajes de control (frames de texto) del
// canal de datos de terminal. Los frames binarios llevan bytes crudos del
// terminal (entrada o salida); en el canal agente-servidor están prefixados
// por el ID de sesión para multiplexar varias sesiones en una sola conexión.
type TerminalMessage struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// Mensajes de control del canal navegador ↔ servidor.
const (
	TerminalMsgBrowserInput  = "input"  // navegador → servidor: tecla pulsada (datos binarios)
	TerminalMsgBrowserResize = "resize" // navegador → servidor: cambio de dimensiones (texto)
	TerminalMsgBrowserSignal = "signal" // navegador → servidor: señal al shell (texto)
	TerminalMsgBrowserClose  = "close"  // navegador → servidor: cerrar sesión (texto)
	TerminalMsgServerOutput  = "output" // servidor → navegador: salida de la terminal (binario)
	TerminalMsgServerOpen    = "open"   // servidor → navegador: sesión lista para datos (texto)
	TerminalMsgServerExit    = "exit"   // servidor → navegador: shell finalizó (texto)
	TerminalMsgServerClosed  = "closed" // servidor → navegador: sesión cerrada (texto)
	TerminalMsgServerError   = "error"  // servidor → navegador: error de transporte/ejecución (texto)
)

// Mensajes de control del canal agente ↔ servidor.
const (
	TerminalMsgAgentOpen     = "open"      // servidor → agente: abrir sesión (texto)
	TerminalMsgAgentOpened   = "opened"    // agente → servidor: sesión abierta (texto)
	TerminalMsgAgentError    = "error"     // agente → servidor: error al abrir (texto)
	TerminalMsgAgentExit     = "exit"      // agente → servidor: shell finalizó (texto)
	TerminalMsgAgentAckClose = "ack_close" // agente → servidor: acuse de cierre (texto)
	TerminalMsgServerClose   = "close"     // servidor → agente: cerrar sesión (texto)
	TerminalMsgServerResize  = "resize"    // servidor → agente: cambio de dimensiones (texto)
	TerminalMsgServerSignal  = "signal"    // servidor → agente: enviar señal (texto)
	TerminalMsgPing          = "ping"
	TerminalMsgPong          = "pong"
)

// TerminalResizePayload es el payload JSON de los mensajes resize.
type TerminalResizePayload struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// TerminalSignalPayload es el payload JSON de los mensajes signal.
type TerminalSignalPayload struct {
	Signal string `json:"signal"`
}

// TerminalErrorPayload es el payload JSON de los mensajes de error de sesión.
type TerminalErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TerminalExitPayload es el payload JSON de los mensajes exit.
type TerminalExitPayload struct {
	Code int `json:"code"`
}

// TerminalOpenPayload es el payload de TerminalMsgAgentOpen (servidor → agente).
type TerminalOpenPayload struct {
	SessionID string `json:"session_id"`
	Shell     string `json:"shell"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
	Privilege string `json:"privilege"`
}

// TerminalOpenedPayload es el payload de TerminalMsgAgentOpened (agente → servidor).
type TerminalOpenedPayload struct {
	SessionID string `json:"session_id"`
}

// TerminalClosePayload es el payload de close en ambos canales.
type TerminalClosePayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// TerminalAckClosePayload es el payload de TerminalMsgAgentAckClose.
type TerminalAckClosePayload struct {
	SessionID string `json:"session_id"`
}

// TerminalPingPayload mantiene vivo el canal del agente.
type TerminalPingPayload struct {
	Timestamp int64 `json:"ts"`
}
