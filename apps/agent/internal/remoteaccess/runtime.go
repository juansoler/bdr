// Package remoteaccess implementa el runtime de terminal remota del agente:
// mantiene el canal WebSocket con el servidor, procesa mensajes de control
// (texto) y datos (binarios), y bombea la salida de la pseudoterminal de
// vuelta al servidor.
//
// El agente SIEMPRE inicia la conexión (outbound); el servidor nunca abre
// puertos entrantes en el equipo gestionado.
package remoteaccess

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/coder/websocket"
)

// maxFramePayload acota el payload de datos por frame (1 MiB, alineado con
// contracts.MaxTerminalOutputBytes).
const maxFramePayload = 1 << 20

// writeTimeoutSeconds acota cada escritura al servidor.
const writeTimeoutSeconds = 10

// Runtime mantiene la conexión persistente con el servidor y coordina el
// ciclo de vida de las sesiones de terminal locales.
type Runtime struct {
	mu      sync.Mutex // serializa escrituras al WebSocket
	conn    *websocket.Conn
	manager *SessionManager
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewRuntime crea un runtime ligado al contexto del agente.
func NewRuntime(ctx context.Context) *Runtime {
	ctx, cancel := context.WithCancel(ctx)
	return &Runtime{
		ctx:     ctx,
		cancel:  cancel,
		manager: NewSessionManager(),
	}
}

// Run gestiona una conexión ya establecida: lee mensajes del servidor hasta
// que el canal se cierre o se cancele el contexto. Siempre cierra las
// sesiones locales al terminar (política: sin canal = sin shell).
func (r *Runtime) Run(conn *websocket.Conn) error {
	r.mu.Lock()
	r.conn = conn
	r.mu.Unlock()

	err := r.readLoop()

	// Conexión perdida: termina todas las sesiones locales.
	r.manager.CloseAll("se perdió la conexión al servidor")
	return err
}

// Close detiene el runtime y cierra todas las sesiones.
func (r *Runtime) Close() {
	r.cancel()
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "agente detenido")
	}
	r.manager.CloseAll("agente detenido")
}

// readLoop procesa los mensajes entrantes del servidor: control (texto) y
// datos de terminal (binarios multiplexados por ID de sesión).
func (r *Runtime) readLoop() error {
	for {
		r.mu.Lock()
		conn := r.conn
		r.mu.Unlock()
		if conn == nil {
			return errors.New("sin conexión")
		}

		typ, data, err := conn.Read(r.ctx)
		if err != nil {
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

// handleControl procesa un mensaje de control del servidor.
func (r *Runtime) handleControl(msg TerminalRequest) {
	switch msg.Type {
	case MsgOpen:
		var p OpenPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil || p.SessionID == "" {
			return
		}
		s, err := r.manager.NewSession(p.SessionID, p.Shell, p.Cols, p.Rows, p.Privilege)
		if err != nil {
			_ = r.sendJSON(TerminalResponse{
				Type:    MsgError,
				Session: p.SessionID,
				Payload: mustJSON(ErrorPayload{Code: "E_AGENT_PTY", Message: err.Error()}),
			})
			return
		}
		// Bomba de salida: PTY → servidor; al terminar el shell, notifica exit.
		go r.pumpOutput(s)
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
		if s, ok := r.manager.GetSession(msg.Session); ok && s.Cmd != nil && s.Cmd.Process != nil {
			if sig := signalFromName(p.Signal); sig != nil {
				_ = s.Cmd.Process.Signal(sig) //nolint:errcheck // best-effort
			}
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

// handleBinary escribe la entrada del operador en la PTY de la sesión.
func (r *Runtime) handleBinary(sessionID string, payload []byte) {
	if s, ok := r.manager.GetSession(sessionID); ok && s.PTY != nil {
		_, _ = s.PTY.Write(payload)
	}
}

// pumpOutput lee la salida de la PTY y la envía al servidor como frames
// binarios multiplexados. Cuando el shell termina, notifica `exit` y limpia
// la sesión local.
func (r *Runtime) pumpOutput(s *PTYSession) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.PTY.Read(buf)
		if n > 0 {
			if frame, encErr := encodeAgentData(s.ID, buf[:n]); encErr == nil {
				_ = r.sendBinary(frame)
			}
		}
		if err != nil {
			break
		}
	}

	// El maestro de la PTY se cerró (shell terminó o sesión cerrada):
	// recoger el estado del proceso y notificar al servidor.
	code := 0
	if s.Cmd != nil {
		if waitErr := s.Cmd.Wait(); waitErr != nil {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				code = exitErr.ExitCode()
				if code < 0 { // terminado por señal (p. ej. SIGKILL del close)
					code = 130
				}
			} else {
				code = 1
			}
		}
	}
	// Solo notifica exit si la sesión sigue registrada (si el servidor pidió
	// close, ya conoce el final y el ack_close basta).
	if _, ok := r.manager.GetSession(s.ID); ok {
		_ = r.sendJSON(TerminalResponse{
			Type:    MsgExit,
			Session: s.ID,
			Payload: mustJSON(ExitPayload{Code: code}),
		})
		r.manager.CloseSession(s.ID, "shell finalizado")
	}
}

// sendJSON envía un mensaje de control (texto) al servidor, serializado.
func (r *Runtime) sendJSON(msg TerminalResponse) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return errors.New("sin conexión")
	}
	ctx, cancel := context.WithTimeout(r.ctx, writeTimeoutSeconds*1e9)
	defer cancel()
	return r.conn.Write(ctx, websocket.MessageText, data)
}

// sendBinary envía un frame binario al servidor, serializado.
func (r *Runtime) sendBinary(frame []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return errors.New("sin conexión")
	}
	ctx, cancel := context.WithTimeout(r.ctx, writeTimeoutSeconds*1e9)
	defer cancel()
	return r.conn.Write(ctx, websocket.MessageBinary, frame)
}

// encodeAgentData construye un frame binario del canal agente-servidor:
//
//	[2 bytes BE longitud del session id][session id UTF-8][payload]
func encodeAgentData(sessionID string, payload []byte) ([]byte, error) {
	if len(sessionID) == 0 || len(sessionID) > 64 {
		return nil, errMalformedFrame
	}
	if len(payload) > maxFramePayload {
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