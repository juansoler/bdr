// screen_runtime.go implementa el runtime de acceso por pantalla (protocolo
// de tiles) del agente: mantiene el canal WebSocket con el servidor, procesa
// mensajes de control (texto) y bytes del protocolo de tiles (binarios
// multiplexados), y coordina los backends de escritorio por plataforma.
//
// El agente SIEMPRE inicia la conexión (outbound); el servidor nunca abre
// puertos entrantes en el equipo gestionado. El backend produce los tiles él
// mismo (captura nativa o bridge VNC de loopback): no hay servicio VNC de
// terceros que instalar.
package remoteaccess

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/coder/websocket"
	"github.com/juansoler/bdr/packages/contracts"
)

// ScreenRuntime mantiene la conexión persistente con el servidor y coordina
// el ciclo de vida de las sesiones de pantalla locales.
type ScreenRuntime struct {
	mu      sync.Mutex // serializa escrituras al WebSocket
	conn    *websocket.Conn
	manager *ScreenSessionManager
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewScreenRuntime crea un runtime ligado al contexto del agente.
func NewScreenRuntime(ctx context.Context) *ScreenRuntime {
	ctx, cancel := context.WithCancel(ctx)
	return &ScreenRuntime{
		ctx:     ctx,
		cancel:  cancel,
		manager: NewScreenSessionManager(),
	}
}

// Run gestiona una conexión ya establecida: lee mensajes del servidor hasta
// que el canal se cierre o se cancele el contexto. Siempre cierra las
// sesiones locales al terminar (política: sin canal = sin escritorio).
func (r *ScreenRuntime) Run(conn *websocket.Conn) error {
	r.mu.Lock()
	r.conn = conn
	r.mu.Unlock()

	err := r.screenReadLoop()

	// Conexión perdida: termina todas las sesiones locales.
	r.manager.CloseAll()
	return err
}

// Close detiene el runtime y cierra todas las sesiones.
func (r *ScreenRuntime) Close() {
	r.cancel()
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "agente detenido")
	}
	r.manager.CloseAll()
}

// screenReadLoop procesa los mensajes entrantes del servidor: control (texto)
// y datos del protocolo de tiles (binarios multiplexados por ID de sesión).
func (r *ScreenRuntime) screenReadLoop() error {
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
			var msg contracts.ScreenMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			r.handleScreenControl(msg)
		case websocket.MessageBinary:
			sessionID, payload, err := decodeAgentData(data)
			if err != nil {
				continue
			}
			r.handleScreenBinary(sessionID, payload)
		}
	}
}

// handleScreenControl procesa un mensaje de control del servidor.
func (r *ScreenRuntime) handleScreenControl(msg contracts.ScreenMessage) {
	switch msg.Type {
	case contracts.ScreenMsgAgentOpen:
		var p contracts.ScreenOpenPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil || p.SessionID == "" {
			return
		}
		s, err := r.manager.NewSession(p.SessionID, p.AllowClipboard)
		if err != nil {
			_ = r.sendScreenJSON(contracts.ScreenMessage{
				Type:      contracts.ScreenMsgAgentError,
				SessionID: p.SessionID,
				Payload:   mustJSON(contracts.ScreenErrorPayload{Code: "E_AGENT_VNC", Message: err.Error()}),
			})
			return
		}
		// Bomba de salida: backend → servidor; al terminar, notifica exit.
		go r.pumpSession(s)
		_ = r.sendScreenJSON(contracts.ScreenMessage{
			Type:      contracts.ScreenMsgAgentOpened,
			SessionID: p.SessionID,
			Payload:   mustJSON(contracts.ScreenOpenedPayload{SessionID: p.SessionID}),
		})

	case contracts.ScreenMsgServerClose:
		var p contracts.ScreenClosePayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			return
		}
		r.manager.CloseSession(p.SessionID)
		_ = r.sendScreenJSON(contracts.ScreenMessage{
			Type:      contracts.ScreenMsgAgentAckClose,
			SessionID: p.SessionID,
		})

	case contracts.ScreenMsgPing:
		_ = r.sendScreenJSON(contracts.ScreenMessage{Type: contracts.ScreenMsgPong})
	}
}

// handleScreenBinary pasa la entrada del visor al backend de la sesión.
func (r *ScreenRuntime) handleScreenBinary(sessionID string, payload []byte) {
	if s, ok := r.manager.GetSession(sessionID); ok {
		s.backend.Handle(payload)
	}
}

// pumpSession bombea los eventos del backend hacia el servidor como frames
// binarios multiplexados. Al terminar el backend, notifica exit y limpia.
func (r *ScreenRuntime) pumpSession(s *ScreenSession) {
	for {
		select {
		case <-s.backend.Done():
			r.notifyExit(s.ID)
			r.manager.CloseSession(s.ID)
			return
		case msg, ok := <-s.backend.Events():
			if !ok {
				r.notifyExit(s.ID)
				r.manager.CloseSession(s.ID)
				return
			}
			if frame, err := encodeAgentData(s.ID, msg); err == nil {
				_ = r.sendScreenBinary(frame)
			}
		}
	}
}

// notifyExit informa al servidor de que el backend terminó. Solo si la
// sesión sigue registrada (si el servidor pidió close, basta el ack).
func (r *ScreenRuntime) notifyExit(sessionID string) {
	if _, ok := r.manager.GetSession(sessionID); !ok {
		return
	}
	_ = r.sendScreenJSON(contracts.ScreenMessage{
		Type:      contracts.ScreenMsgAgentExit,
		SessionID: sessionID,
		Payload:   mustJSON(contracts.ScreenExitPayload{Code: 0}),
	})
}

// sendScreenJSON envía un mensaje de control (texto) al servidor.
func (r *ScreenRuntime) sendScreenJSON(msg contracts.ScreenMessage) error {
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

// sendScreenBinary envía un frame binario al servidor, serializado.
func (r *ScreenRuntime) sendScreenBinary(frame []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return errors.New("sin conexión")
	}
	ctx, cancel := context.WithTimeout(r.ctx, writeTimeoutSeconds*1e9)
	defer cancel()
	return r.conn.Write(ctx, websocket.MessageBinary, frame)
}

// --- Gestor de sesiones locales ----------------------------------------------

// ScreenSessionManager coordina las sesiones de pantalla activas del agente.
type ScreenSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*ScreenSession
}

// NewScreenSessionManager crea el gestor de sesiones de pantalla.
func NewScreenSessionManager() *ScreenSessionManager {
	return &ScreenSessionManager{
		sessions: make(map[string]*ScreenSession),
	}
}

// ScreenSession representa una sesión de pantalla activa: un backend de
// escritorio (captura nativa o bridge) y su bomba de eventos.
type ScreenSession struct {
	ID      string
	backend screenBackend
}

// NewSession arranca el backend de pantalla de la plataforma y registra la
// sesión. allowClipboard queda reservado (v1 sin sincronización de clipboard).
func (m *ScreenSessionManager) NewSession(id string, allowClipboard bool) (*ScreenSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	backend, err := newScreenBackend()
	if err != nil {
		return nil, err
	}
	s := &ScreenSession{ID: id, backend: backend}
	m.sessions[id] = s
	return s, nil
}

// GetSession devuelve una sesión por ID.
func (m *ScreenSessionManager) GetSession(id string) (*ScreenSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// CloseSession cierra la sesión y su backend.
func (m *ScreenSessionManager) CloseSession(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return
	}
	s.backend.Close()
}

// CloseAll cierra todas las sesiones (pérdida del canal con el servidor).
func (m *ScreenSessionManager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.CloseSession(id)
	}
}
