// Package contracts define los tipos JSON compartidos para el subsistema de
// acceso por pantalla (VNC en el navegador vía noVNC). Complementa el canal
// de terminal remota: mismo modelo de relay (el agente inicia la conexión
// saliente y el servidor solo autoriza y retransmite), mismo framing binario
// multiplexado por ID de sesión.
//
// Sigue las reglas de compatibilidad de /api/v1 (docs/06-api.md §9): los
// cambios son aditivos, los campos nuevos son opcionales y los clientes
// ignoran los desconocidos.
package contracts

import (
	"encoding/json"
	"time"
)

// --- Capacidades de acceso por pantalla --------------------------------------

const (
	// CapScreenRead consulta disponibilidad y lista sesiones de pantalla.
	CapScreenRead = "screen:read"
	// CapScreenConnect abre una sesión de pantalla y usa su WebSocket de datos.
	CapScreenConnect = "screen:connect"
	// CapScreenTerminate cierra cualquier sesión de pantalla.
	CapScreenTerminate = "screen:terminate"
	// CapScreenAudit consulta la auditoría de sesiones de pantalla.
	CapScreenAudit = "screen:audit"
)

// --- Estados de una sesión de pantalla ---------------------------------------

const (
	// ScreenSessionPending: creada en el servidor, esperando a que el agente
	// atienda el VNC local.
	ScreenSessionPending = "pending"
	// ScreenSessionActive: VNC local conectado y relay establecido.
	ScreenSessionActive = "active"
	// ScreenSessionClosed: cerrada ordenadamente por operador, agente o
	// servidor (incluido tiempo de inactividad).
	ScreenSessionClosed = "closed"
	// ScreenSessionInterrupted: interrumpida por pérdida del canal de control,
	// reinicio del servidor o cierre forzado.
	ScreenSessionInterrupted = "interrupted"
)

// --- Calidad de la sesión ----------------------------------------------------

const (
	// ScreenQualityLow prioriza latencia sobre fidelidad (redes lentas).
	ScreenQualityLow = "low"
	// ScreenQualityMedium equilibrio por defecto.
	ScreenQualityMedium = "medium"
	// ScreenQualityHigh prioriza fidelidad (LAN).
	ScreenQualityHigh = "high"
)

// ValidScreenQuality indica si la calidad solicitada es válida.
func ValidScreenQuality(q string) bool {
	switch q {
	case "", ScreenQualityLow, ScreenQualityMedium, ScreenQualityHigh:
		return true
	}
	return false
}

// --- Create / vista / lista --------------------------------------------------

// CreateScreenSessionRequest es el cuerpo de
// POST /api/v1/admin/agents/{id}/screen/sessions.
type CreateScreenSessionRequest struct {
	// Quality solicitada: low | medium | high ("" = medium).
	Quality string `json:"quality,omitempty"`
	// AllowClipboard habilita la sincronización del portapapeles entre el
	// navegador y el equipo gestionado (por defecto desactivada).
	AllowClipboard bool `json:"allow_clipboard,omitempty"`
}

// CreateScreenSessionResponse se devuelve al crear una sesión de pantalla.
type CreateScreenSessionResponse struct {
	SessionID string    `json:"session_id"`
	AgentID   string    `json:"agent_id"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
	StreamURL string    `json:"stream_url,omitempty"` // WebSocket de datos para el operador
	// StreamTicket solo se devuelve en la creación: los WebSockets del
	// navegador no pueden enviar cabeceras de autorización, así que el
	// operador lo presenta como ?ticket=… en la URL del stream.
	StreamTicket string `json:"stream_ticket,omitempty"`
}

// ScreenSessionView es la vista de una sesión de pantalla.
type ScreenSessionView struct {
	SessionID      string     `json:"session_id"`
	AgentID        string     `json:"agent_id"`
	Hostname       string     `json:"hostname,omitempty"`
	OSFamily       string     `json:"os_family,omitempty"`
	Quality        string     `json:"quality"`
	AllowClipboard bool       `json:"allow_clipboard"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	OpenedAt       *time.Time `json:"opened_at,omitempty"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	ExpiresAt      time.Time  `json:"expires_at"`
	Reason         string     `json:"reason,omitempty"`
	OperatorID     string     `json:"operator_id"`
	OperatorName   string     `json:"operator_name,omitempty"`
	StreamURL      string     `json:"stream_url,omitempty"`
}

// ScreenSessionList es la respuesta de GET /api/v1/admin/screen/sessions.
type ScreenSessionList struct {
	Items []ScreenSessionView `json:"items"`
}

// ScreenCapabilities devuelve la disponibilidad de pantalla de un agente.
type ScreenCapabilities struct {
	AgentID  string `json:"agent_id"`
	Online   bool   `json:"online"`
	OSFamily string `json:"os_family"`
	// Provider describe cómo obtiene el agente el escritorio local:
	//   - darwin: "vnc-built-in" (Compartir pantalla de macOS)
	//   - linux:  "vnc-helper" (x11vnc/x0vncserver lanzado bajo demanda)
	//   - windows: "vnc-external" (servidor VNC ya instalado en localhost)
	Provider string `json:"provider,omitempty"`
	// DisplayRequired indica si el equipo necesita una sesión gráfica activa.
	DisplayRequired bool `json:"display_required,omitempty"`
}

// --- Protocolo de datos (WebSocket) ------------------------------------------

// ScreenMessage es el sobre para mensajes de control (frames de texto) del
// canal de datos de pantalla. Los frames binarios llevan bytes crudos del
// protocolo RFB (VNC) en ambas direcciones; en el canal agente-servidor están
// prefixados por el ID de sesión para multiplexar varias sesiones en una
// sola conexión.
type ScreenMessage struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// Mensajes de control del canal navegador ↔ servidor.
//
// El stream de pantalla NO lleva mensajes de control texto del servidor al
// navegador: noVNC interpreta todo el canal como protocolo RFB y un frame de
// texto lo corrompería. Los estados de «sesión terminada» viajan en el frame
// de cierre de WebSocket (reason), y la única orden del visor es close.
const (
	ScreenMsgBrowserClose = "close" // navegador → servidor: cerrar sesión (texto)
)

// Mensajes de control del canal agente ↔ servidor.
const (
	ScreenMsgAgentOpen     = "screen_open"      // servidor → agente: abrir sesión (texto)
	ScreenMsgAgentOpened   = "screen_opened"    // agente → servidor: sesión abierta (texto)
	ScreenMsgAgentError    = "screen_error"     // agente → servidor: error al abrir (texto)
	ScreenMsgAgentExit     = "screen_exit"      // agente → servidor: VNC local finalizó (texto)
	ScreenMsgAgentAckClose = "screen_ack_close" // agente → servidor: acuse de cierre (texto)
	ScreenMsgServerClose   = "screen_close"     // servidor → agente: cerrar sesión (texto)
	ScreenMsgPing          = "screen_ping"
	ScreenMsgPong          = "screen_pong"
)

// ScreenOpenPayload es el payload de ScreenMsgAgentOpen (servidor → agente).
type ScreenOpenPayload struct {
	SessionID string `json:"session_id"`
	// Quality solicitada: low | medium | high.
	Quality string `json:"quality,omitempty"`
	// AllowClipboard habilita la sincronización del portapapeles.
	AllowClipboard bool `json:"allow_clipboard,omitempty"`
}

// ScreenOpenedPayload es el payload de ScreenMsgAgentOpened (agente → servidor).
type ScreenOpenedPayload struct {
	SessionID string `json:"session_id"`
	// Width y Height del framebuffer inicial, si el VNC local los informa.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

// ScreenErrorPayload es el payload JSON de los mensajes de error de sesión.
type ScreenErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ScreenExitPayload es el payload JSON de los mensajes exit.
type ScreenExitPayload struct {
	// Code: 0 si el VNC local terminó ordenadamente.
	Code int `json:"code"`
}

// ScreenClosePayload es el payload de screen_close en ambos canales.
type ScreenClosePayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// ScreenAckClosePayload es el payload de ScreenMsgAgentAckClose.
type ScreenAckClosePayload struct {
	SessionID string `json:"session_id"`
}
