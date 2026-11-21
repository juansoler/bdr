// Package remoteaccess implementa el tiempo de ejecución del agente: conexión WebSocket,
// manejo de mensajes y coordinación con la pseudoterminal local.
package remoteaccess

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"syscall"

	"github.com/coder/websocket"
)

// Runtime mantiene una conexión persistente al servidor y coordina el ciclo de
// vida de las sesiones de terminal.
type Runtime struct {
	conn    *websocket.Conn
	manager *SessionManager
	ctx     context.Context
	cancel  context.CancelFunc
	once    func()
}

// NewRuntime crea un nuevo Runtime para el agente.
func NewRuntime(ctx context.Context) *Runtime {
	ctx, cancel := context.WithCancel(ctx)
	return &Runtime{
		ctx:     ctx,
		cancel:  cancel,
		manager: NewSessionManager(),
		once:    func() {},
	}
}

// Connect conecta al WebSocket del agente. Bloquea mientras la conexión esté
// activa; lee mensajes internamente.
func (r *Runtime) Connect(srvURL string) error {
	ws, _, err := websocket.Dial(r.ctx, srvURL, nil)
	if err != nil {
		return err
	}
	r.conn = ws
	return r.readLoop()
}

// Close cierra el runtime y todas sus sesiones.
func (r *Runtime) Close() {
	r.once()
	r.cancel()
}

// readLoop lee mensajes entrantes desde el agente y los distribuye.
func (r *Runtime) readLoop() error {
	for {
		typ, data, err := r.conn.Read(r.ctx)
		if err != nil {
			// Conexión cerrada: termina todas las sesiones.
			r.manager.mu.Lock()
			for id := range r.manager.sessions {
				r.manager.CloseSession(id, "se perdió la conexión al servidor")
			}
			r.manager.mu.Unlock()
			return err
		}

		switch typ {
		case websocket.MessageText:
			var msg TerminalRequest
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			r.handleControl(msg)

		case websocket.MessageBinary:
			sessionID, payload, err := decodeAgentData(data)
			if err != nil {
				continue
			}
			r.handleBinary(sessionID, payload)
		}
	}
}

// handleControl procesa mensajes de control del servidor.
func (r *Runtime) handleControl(msg TerminalRequest) {
	switch msg.Type {
	case MsgOpen:
		var p OpenPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil || p.SessionID == "" {
			return
		}
		_, err := r.manager.NewSession(p.SessionID, p.Shell, p.Cols, p.Rows, "")
		if err != nil {
			_ = r.sendJSON(TerminalResponse{
				Type: MsgError,
				Payload: mustJSON(ErrorPayload{
					Code:    "E_AGENT_PTY",
					Message: err.Error(),
				}),
			})
			return
		}
		_ = r.sendJSON(TerminalResponse{
			Type:    MsgOpened,
			Session: p.SessionID,
		})

	case MsgResize:
		var p ResizePayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			return
		}
		if s, ok := r.manager.GetSession(msg.Session); ok {
			_ = ResizeTerminal(s.PTY, p.Cols, p.Rows)
		}

	case MsgSignal:
		var p SignalPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			return
		}
		if s, ok := r.manager.GetSession(msg.Session); ok && s.Cmd != nil {
			s.Cmd.Signal(signalFromName(p.Signal))
		}

	case MsgClose:
		var p ClosePayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			return
		}
		r.manager.CloseSession(p.SessionID, p.Reason)
		_ = r.sendJSON(TerminalResponse{
			Type:    MsgAckClose,
			Session: p.SessionID,
		})

	case MsgPing:
		_ = r.sendJSON(TerminalResponse{Type: MsgPong})
	}
}

// handleBinary maneja datos binarios con prefijo de sesión.
func (r *Runtime) handleBinary(sessionID string, payload []byte) {
	if s, ok := r.manager.GetSession(sessionID); ok && s.PTY != nil {
		_, _ = s.PTY.Write(payload)
	}
}

// sendJSON envía un mensaje de texto JSON.
func (r *Runtime) sendJSON(msg TerminalResponse) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return r.conn.Write(r.ctx, websocket.MessageText, data)
}

// encodeAgentData construye un frame binario del canal agente-servidor:
//
//	[2 bytes BE longitud del session id][session id UTF-8][payload]
func encodeAgentData(sessionID string, payload []byte) ([]byte, error) {
	if len(sessionID) == 0 || len(sessionID) > 64 {
		return nil, errMalformedFrame
	}
	if len(payload) > (1 << 20) {
		return nil, errMalformedFrame
	}
	frame := make([]byte, 2+len(sessionID)+len(payload))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(sessionID)))
	copy(frame[2:], sessionID)
	copy(frame[2+len(sessionID):], payload)
	return frame, nil
}

// decodeAgentData separa el ID de sesión y el payload de un frame binario.
func decodeAgentData(frame []byte) (sessionID string, payload []byte, err error) {
	if len(frame) < 2 {
		return "", nil, errMalformedFrame
	}
	n := int(binary.BigEndian.Uint16(frame[:2]))
	if n <= 0 || n > 64 || len(frame) < 2+n {
		return "", nil, errMalformedFrame
	}
	return string(frame[2 : 2+n]), frame[2+n:], nil
}

// signalFromName traduce un nombre de señal a su equivalente unix.
func signalFromName(name string) os.Signal {
	switch name {
	case "INT":
		return syscall.SIGINT
	case "TERM":
		return syscall.SIGTERM
	case "KILL":
		return syscall.SIGKILL
	case "HUP":
		return syscall.SIGHUP
	case "USR1":
		return syscall.SIGUSR1
	case "USR2":
		return syscall.SIGUSR2
	default:
		return nil
	}
}

// errMalformedFrame indica un frame binario con formato inválido.
var errMalformedFrame = errors.New("frame binario malformado")

// mustJSON serializa un valor a JSON.
func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
