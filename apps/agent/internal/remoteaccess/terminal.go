// Package remoteaccess define la interfaz del terminal en el agente.
package remoteaccess

import "context"

// TerminalSessionInterface es la interfaz que un agente debe implementar para
// manejar sesiones de terminal remota.
type TerminalSessionInterface interface {
	// Open abre la pseudoterminal y envía los parámetros iniciales al servidor.
	Open(ctx context.Context, sessionID string, shell, cols, rows, privilege string) error
	// Opened recibe la confirmación del servidor (session_id).
	Opened(ctx context.Context, sessionID string) error
	// Error recibe un mensaje de error al abrir la sesión.
	Error(ctx context.Context, sessionID string, code, message string) error
	// Exit finaliza el shell enviando el código de salida.
	Exit(ctx context.Context, sessionID int, code int) error
	// AckClose confirma el cierre por parte del agente.
	AckClose(ctx context.Context, sessionID string) error
	// Close cierra la sesión.
	Close(ctx context.Context, sessionID string, reason string) error
}
