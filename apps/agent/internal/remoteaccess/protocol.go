// Package remoteaccess implementa el cliente WebSocket del agente para terminales
// remotas. El agente se conecta al servidor y actúa como puente hacia una
// pseudoterminal local (PTY en Unix, ConPTY en Windows).
package remoteaccess

import (
	"encoding/json"
)

// TerminalRequest es un mensaje de control recibido desde el servidor.
type TerminalRequest struct {
	Type    string          `json:"type"`
	Session string          `json:"session,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// TerminalResponse es un mensaje de respuesta del agente al servidor.
type TerminalResponse struct {
	Type    string          `json:"type"`
	Session string          `json:"session,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Payload de apertura de sesión.
type OpenPayload struct {
	SessionID string `json:"session_id"`
	Shell     string `json:"shell,omitempty"`
	Cols      uint16 `json:"cols,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
}

// Payload de confirmación de sesión abierta.
type OpenedPayload struct {
	SessionID string `json:"session_id"`
}

// Payload de error al abrir sesión.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Payload de tamaño de terminal.
type ResizePayload struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Payload de señal al shell.
type SignalPayload struct {
	Signal string `json:"signal"`
}

// Payload de cierre.
type ClosePayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// Payload de confirmación de cierre.
type AckClosePayload struct {
	SessionID string `json:"session_id"`
}

// Payload de salida (exit).
type ExitPayload struct {
	Code int `json:"code"`
}

// constantes de tipo de mensaje
const (
	MsgOpen      = "open"
	MsgOpened    = "opened"
	MsgError     = "error"
	MsgExit      = "exit"
	MsgAckClose  = "ack_close"
	MsgClose     = "close"
	MsgResize    = "resize"
	MsgSignal    = "signal"
	MsgPing      = "ping"
	MsgPong      = "pong"
)