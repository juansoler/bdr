// Package remoteaccess implementa el gestor de sesiones de terminal remota
// del plano de control: registro de agentes conectados, ciclo de vida de
// sesiones, relay WebSocket bidireccional y auditoría.
//
// El agente siempre inicia la conexión (canal saliente autenticado con mTLS y
// API key); el servidor solo autoriza y retransmite. No hay puertos
// entrantes en los equipos protegidos.
package remoteaccess

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// maxSessionsPerAgent limita las sesiones de terminal simultáneas por agente.
const maxSessionsPerAgent = 4

// agentAckTimeout es el tiempo que se espera a que el agente confirme la
// apertura de una sesión antes de considerarla fallida.
const agentAckTimeout = 60 * time.Second

// closedSessionTTL es el tiempo tras el cierre que se conserva la metadata
// en el store antes de purgarla.
const closedSessionTTL = 24 * time.Hour

// writeTimeout acota cualquier escritura a un WebSocket.
const writeTimeout = 5 * time.Second

// Errores de dominio del gestor de terminal remoto.
var (
	ErrAgentOffline     = errors.New("el agente no tiene conexión de terminal activa")
	ErrAgentNotEnrolled = errors.New("el agente no está enrolado")
	ErrSessionNotFound  = errors.New("sesión de terminal no encontrada")
	ErrSessionClosed    = errors.New("la sesión ya está cerrada")
	ErrTooManySessions  = errors.New("demasiadas sesiones de terminal simultáneas para este agente")
	ErrSessionBusy      = errors.New("la sesión ya tiene un operador conectado")
)

// OperatorInfo identifica al operador que abre una sesión (para auditoría).
type OperatorInfo struct {
	ID   string
	Name string
}

// Manager coordina el registro de agentes conectados, el ciclo de vida de
// sesiones de terminal y el relay bidireccional entre operador y agente.
// Es el único punto que conoce el estado en memoria; la metadata persistente
// (estado, fechas, motivo) vive en el store.
type Manager struct {
	store store.Store
	log   *logging.Logger
	now   func() time.Time

	mu       sync.Mutex
	agents   map[string]*agentChannel
	sessions map[string]*session
	// tickets asocia cada sesión con el ticket de stream del operador: los
	// WebSockets del navegador no pueden enviar cabeceras de autorización,
	// así que presentan este secreto como query param (?ticket=…).
	tickets map[string]string
}

// agentChannel es el canal saliente mantenido por un agente (WebSocket).
type agentChannel struct {
	agentID string
	ws      *websocket.Conn
	sendMu  sync.Mutex // serializa escrituras al agente
}

// session mantiene el estado en memoria de una sesión de terminal.
type session struct {
	id      string
	agentID string

	browserMu sync.Mutex
	browser   *browserConn // nil si no hay operador conectado

	done      chan struct{} // se cierra al cerrarse la sesión
	closeOnce sync.Once
}

// browserConn envuelve la conexión WebSocket del navegador con un mutex de
// escritura (el agente y el lector del navegador escriben desde goroutines
// distintas).
type browserConn struct {
	ws     *websocket.Conn
	sendMu sync.Mutex
}

// write escribe un mensaje al navegador de forma serializada.
func (b *browserConn) write(ctx context.Context, typ websocket.MessageType, data []byte) error {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()
	return b.ws.Write(ctx, typ, data)
}

// New crea un gestor de terminal remota.
func New(st store.Store, log *logging.Logger) *Manager {
	if log == nil {
		log = logging.New("info")
	}
	return &Manager{
		store:    st,
		log:      log,
		now:      time.Now,
		agents:   map[string]*agentChannel{},
		sessions: map[string]*session{},
		tickets:  map[string]string{},
	}
}

// SetClock permite inyectar un reloj para tests.
func (m *Manager) SetClock(now func() time.Time) { m.now = now }

// AgentOnline indica si el agente tiene canal de terminal abierto.
func (m *Manager) AgentOnline(agentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.agents[agentID]
	return ok
}

// AgentCapabilities devuelve la disponibilidad de terminal de un agente.
func (m *Manager) AgentCapabilities(agentID, osFamily string) contracts.TerminalCapabilities {
	m.mu.Lock()
	online := false
	if _, ok := m.agents[agentID]; ok {
		online = true
	}
	m.mu.Unlock()
	return contracts.TerminalCapabilities{
		AgentID:    agentID,
		Online:     online,
		OSFamily:   osFamily,
		Shells:     shellsFor(osFamily),
		Privileges: []string{contracts.TerminalPrivilegeStandard},
	}
}

// shellsFor devuelve los shells soportados según el sistema operativo.
func shellsFor(osFamily string) []string {
	switch osFamily {
	case "windows":
		return []string{contracts.TerminalShellDefault, contracts.TerminalShellPowerShell, "cmd"}
	case "darwin":
		return []string{contracts.TerminalShellDefault, contracts.TerminalShellZsh, contracts.TerminalShellBash}
	default:
		return []string{contracts.TerminalShellDefault, contracts.TerminalShellBash}
	}
}

// RegisterAgent registra el canal saliente de un agente y bloquea mientras
// la conexión siga viva (el handler WebSocket no debe retornar hasta que
// termine). Al cerrarse el canal, desconecta al agente y marca sus sesiones
// como interrumpidas.
func (m *Manager) RegisterAgent(ctx context.Context, agentID string, ws *websocket.Conn) {
	// Cierra un canal duplicado del mismo agente (solo uno a la vez).
	m.mu.Lock()
	if old, ok := m.agents[agentID]; ok {
		m.mu.Unlock()
		_ = old.ws.Close(websocket.StatusPolicyViolation, "canal duplicado")
		// Espera a que el bucle antiguo termine de limpiar antes de seguir.
		time.Sleep(50 * time.Millisecond)
		m.mu.Lock()
	}
	ch := &agentChannel{agentID: agentID, ws: ws}
	m.agents[agentID] = ch
	m.mu.Unlock()

	// Si el operador creó sesiones mientras el agente estaba sin canal
	// (p. ej. reinicio del servicio), reenvía la orden de apertura ahora.
	m.resumePendingSessions(ch)

	m.agentReadLoop(ctx, ch)

	m.mu.Lock()
	if m.agents[agentID] == ch {
		delete(m.agents, agentID)
	}
	m.mu.Unlock()
	m.handleAgentDisconnect(ch)
}

// resumePendingSessions reenvía la orden de apertura de las sesiones
// pendientes de un agente que acaba de conectar su canal. Cubre la carrera
// «el operador abrió la consola mientras el agente se reiniciaba».
func (m *Manager) resumePendingSessions(ch *agentChannel) {
	m.mu.Lock()
	var ids []string
	for _, s := range m.sessions {
		if s.agentID == ch.agentID && !s.isDone() {
			ids = append(ids, s.id)
		}
	}
	m.mu.Unlock()

	for _, id := range ids {
		var rec *store.TerminalSession
		_ = m.store.View(context.Background(), func(st *store.State) error {
			rec = st.TerminalSessions[id]
			return nil
		})
		if rec == nil || rec.State != contracts.TerminalSessionPending {
			continue
		}
		msg := contracts.TerminalMessage{
			Type: contracts.TerminalMsgAgentOpen,
			Payload: mustJSON(contracts.TerminalOpenPayload{
				SessionID: id, Shell: rec.Shell, Cols: rec.Cols, Rows: rec.Rows,
				Privilege: rec.Privilege,
			}),
		}
		if err := m.sendJSON(ch, msg); err != nil {
			m.interruptSession(id, "no se pudo reenviar la orden de apertura al agente")
		}
	}
}

// agentReadLoop procesa los mensajes del agente: control (texto) y datos
// binarios multiplexados por sesión.
func (m *Manager) agentReadLoop(ctx context.Context, ch *agentChannel) {
	for {
		typ, data, err := ch.ws.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			sessionID, payload, err := decodeAgentData(data)
			if err != nil {
				continue
			}
			m.relayOutput(sessionID, payload)
		case websocket.MessageText:
			var msg contracts.TerminalMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			m.handleAgentControl(ch, msg)
		}
	}
}

// handleAgentControl procesa un mensaje de control del agente.
func (m *Manager) handleAgentControl(ch *agentChannel, msg contracts.TerminalMessage) {
	switch msg.Type {
	case contracts.TerminalMsgAgentOpened:
		var p contracts.TerminalOpenedPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			p.SessionID = ""
		}
		// El ID puede venir en el payload o en el propio sobre.
		sessionID := p.SessionID
		if sessionID == "" {
			sessionID = msg.SessionID
		}
		if sessionID == "" {
			return
		}
		m.onAgentOpened(sessionID)
	case contracts.TerminalMsgAgentError:
		var p contracts.TerminalErrorPayload
		_ = json.Unmarshal(msg.Payload, &p)
		m.onAgentError(msg.SessionID, p.Code, p.Message)
	case contracts.TerminalMsgAgentExit:
		var p contracts.TerminalExitPayload
		_ = json.Unmarshal(msg.Payload, &p)
		m.onAgentExit(msg.SessionID, p.Code)
	case contracts.TerminalMsgPong:
		// keep-alive: sin acción.
	case contracts.TerminalMsgPing:
		_ = m.sendJSON(ch, contracts.TerminalMessage{Type: contracts.TerminalMsgPong})
	}
}

// onAgentOpened marca la sesión como activa y notifica al navegador.
func (m *Manager) onAgentOpened(sessionID string) {
	s := m.getSession(sessionID)
	if s == nil {
		return
	}
	now := m.now().UTC()
	err := m.store.Update(context.Background(), func(st *store.State) error {
		ts := st.TerminalSessions[sessionID]
		if ts == nil {
			return store.ErrNotFound
		}
		if ts.State == contracts.TerminalSessionPending {
			ts.State = contracts.TerminalSessionActive
			ts.OpenedAt = &now
		}
		return nil
	})
	if err == nil {
		m.notifyBrowser(s, contracts.TerminalMessage{Type: contracts.TerminalMsgServerOpen})
	}
}

// onAgentError registra el fallo del agente al abrir la sesión.
func (m *Manager) onAgentError(sessionID, code, message string) {
	m.closeInternal(sessionID, contracts.TerminalSessionClosed,
		"el agente no pudo abrir la terminal: "+message)
}

// onAgentExit cierra la sesión cuando el shell termina por su cuenta.
func (m *Manager) onAgentExit(sessionID string, code int) {
	now := m.now().UTC()
	err := m.store.Update(context.Background(), func(st *store.State) error {
		ts := st.TerminalSessions[sessionID]
		if ts == nil {
			return store.ErrNotFound
		}
		if ts.State != contracts.TerminalSessionClosed {
			ts.State = contracts.TerminalSessionClosed
			ts.ClosedAt = &now
			ts.Reason = fmt.Sprintf("shell finalizó con código %d", code)
		}
		return nil
	})
	if err == nil {
		s := m.getSession(sessionID)
		if s != nil {
			m.notifyBrowser(s, contracts.TerminalMessage{
				Type:    contracts.TerminalMsgServerExit,
				Payload: mustJSON(contracts.TerminalExitPayload{Code: code}),
			})
			m.finish(s)
		}
	}
}

// handleAgentDisconnect cierra todas las sesiones del agente al perder el
// canal de control (política: terminar shell y marcar interrumpida).
func (m *Manager) handleAgentDisconnect(ch *agentChannel) {
	agentID := ch.agentID
	m.mu.Lock()
	var sessions []*session
	for _, s := range m.sessions {
		if s.agentID == agentID && !s.isDone() {
			sessions = append(sessions, s)
		}
	}
	m.mu.Unlock()

	for _, s := range sessions {
		m.interruptSession(s.id, "se perdió la conexión de control del agente")
	}
}

// relayOutput envía datos de terminal del agente al navegador de la sesión.
func (m *Manager) relayOutput(sessionID string, payload []byte) {
	s := m.getSession(sessionID)
	if s == nil || s.isDone() {
		return
	}
	s.browserMu.Lock()
	bc := s.browser
	s.browserMu.Unlock()
	if bc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := bc.write(ctx, websocket.MessageBinary, payload); err != nil {
		m.log.Debug("no se pudo escribir salida al navegador", "session_id", sessionID, "error", err.Error())
	}
}

// notifyBrowser envía un mensaje de control al navegador (si está conectado).
func (m *Manager) notifyBrowser(s *session, msg contracts.TerminalMessage) {
	s.browserMu.Lock()
	bc := s.browser
	s.browserMu.Unlock()
	if bc == nil {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := bc.write(ctx, websocket.MessageText, data); err != nil {
		m.log.Debug("no se pudo notificar al navegador", "session_id", s.id, "error", err.Error())
	}
}

// getSession devuelve la sesión en memoria (nil si no existe).
func (m *Manager) getSession(sessionID string) *session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID]
}

// isDone indica si la sesión ya se cerró.
func (s *session) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// CreateSession crea una sesión de terminal para el agente indicado.
func (m *Manager) CreateSession(ctx context.Context, agent store.Agent, op OperatorInfo, req contracts.CreateTerminalSessionRequest) (contracts.TerminalSessionView, error) {
	if agent.RevokedAt != nil {
		return contracts.TerminalSessionView{}, ErrAgentNotEnrolled
	}

	// El agente debe tener canal de terminal activo y no exceder el límite.
	m.mu.Lock()
	_, online := m.agents[agent.ID]
	if online {
		active := 0
		for _, s := range m.sessions {
			if s.agentID == agent.ID && !s.isDone() {
				active++
			}
		}
		if active >= maxSessionsPerAgent {
			m.mu.Unlock()
			return contracts.TerminalSessionView{}, ErrTooManySessions
		}
	}
	m.mu.Unlock()

	shell := req.Shell
	if shell == "" {
		shell = contracts.TerminalShellDefault
	}
	privilege := req.Privilege
	if privilege == "" {
		privilege = contracts.TerminalPrivilegeStandard
	}
	if privilege != contracts.TerminalPrivilegeStandard {
		return contracts.TerminalSessionView{}, fmt.Errorf("el nivel de privilegio %q no está disponible en esta versión", privilege)
	}
	cols := req.Cols
	if cols == 0 {
		cols = contracts.DefaultTerminalCols
	}
	rows := req.Rows
	if rows == 0 {
		rows = contracts.DefaultTerminalRows
	}

	id, err := newSessionID()
	if err != nil {
		return contracts.TerminalSessionView{}, err
	}
	now := m.now().UTC()
	rec := &store.TerminalSession{
		ID: id, AgentID: agent.ID, OperatorID: op.ID, OperatorName: op.Name,
		Shell: shell, Privilege: privilege, Cols: cols, Rows: rows,
		State: contracts.TerminalSessionPending, CreatedAt: now, ExpiresAt: now.Add(defaultSessionTTL()),
	}
	if err := m.store.Update(ctx, func(st *store.State) error {
		st.TerminalSessions[id] = rec
		return nil
	}); err != nil {
		return contracts.TerminalSessionView{}, err
	}

	s := &session{id: id, agentID: agent.ID, done: make(chan struct{})}
	ticket, err := randomHex(24)
	if err != nil {
		return contracts.TerminalSessionView{}, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.tickets[id] = ticket
	m.mu.Unlock()

	// Pide al agente que abra la pseudoterminal.
	msg := contracts.TerminalMessage{
		Type: contracts.TerminalMsgAgentOpen,
		Payload: mustJSON(contracts.TerminalOpenPayload{
			SessionID: id, Shell: shell, Cols: cols, Rows: rows, Privilege: privilege,
		}),
	}
	if online {
		m.mu.Lock()
		ch := m.agents[agent.ID]
		m.mu.Unlock()
		if err := m.sendJSON(ch, msg); err != nil {
			m.interruptSession(id, "no se pudo enviar la orden de apertura al agente")
			return contracts.TerminalSessionView{}, err
		}
	}

	agentCopy := agent
	return viewFromStore(rec, &agentCopy), nil
}

// GetSession devuelve la vista de una sesión.
func (m *Manager) GetSession(sessionID string) (*contracts.TerminalSessionView, error) {
	var view *contracts.TerminalSessionView
	err := m.store.View(context.Background(), func(st *store.State) error {
		ts := st.TerminalSessions[sessionID]
		if ts == nil {
			return store.ErrNotFound
		}
		v := viewFromStore(ts, st.Agents[ts.AgentID])
		view = &v
		return nil
	})
	if err != nil {
		return nil, ErrSessionNotFound
	}
	return view, nil
}

// ListSessions devuelve las sesiones ordenadas por creación (más recientes
// primero).
func (m *Manager) ListSessions() []contracts.TerminalSessionView {
	items := []contracts.TerminalSessionView{}
	_ = m.store.View(context.Background(), func(st *store.State) error {
		for _, ts := range st.TerminalSessions {
			items = append(items, viewFromStore(ts, st.Agents[ts.AgentID]))
		}
		return nil
	})
	return items
}

// viewFromStore convierte la metadata persistida en vista pública.
func viewFromStore(ts *store.TerminalSession, agent *store.Agent) contracts.TerminalSessionView {
	v := contracts.TerminalSessionView{
		SessionID:    ts.ID,
		AgentID:      ts.AgentID,
		Shell:        ts.Shell,
		Privilege:    ts.Privilege,
		State:        ts.State,
		Cols:         ts.Cols,
		Rows:         ts.Rows,
		CreatedAt:    ts.CreatedAt,
		OpenedAt:     ts.OpenedAt,
		ClosedAt:     ts.ClosedAt,
		ExpiresAt:    ts.ExpiresAt,
		Reason:       ts.Reason,
		OperatorID:   ts.OperatorID,
		OperatorName: ts.OperatorName,
	}
	if agent != nil {
		v.Hostname = agent.Hostname
		v.OSFamily = agent.OSFamily
	}
	return v
}

// AttachBrowser conecta el WebSocket del operador a la sesión y bloquea
// mientras el navegador siga conectado (el handler no debe retornar antes).
func (m *Manager) AttachBrowser(ctx context.Context, sessionID string, ws *websocket.Conn) error {
	s := m.getSession(sessionID)
	if s == nil {
		return ErrSessionNotFound
	}
	if s.isDone() {
		return ErrSessionClosed
	}

	s.browserMu.Lock()
	if s.browser != nil {
		s.browserMu.Unlock()
		return ErrSessionBusy
	}
	bc := &browserConn{ws: ws}
	s.browser = bc
	s.browserMu.Unlock()

	// Si la sesión ya está activa, indica al navegador que puede enviar datos.
	if m.sessionState(sessionID) == contracts.TerminalSessionActive {
		m.notifyBrowser(s, contracts.TerminalMessage{Type: contracts.TerminalMsgServerOpen})
	}

	m.browserReadLoop(ctx, s, bc)

	s.browserMu.Lock()
	if s.browser == bc {
		s.browser = nil
	}
	s.browserMu.Unlock()
	return nil
}

// browserReadLoop procesa los mensajes del navegador (entrada binaria y
// controles de texto).
func (m *Manager) browserReadLoop(ctx context.Context, s *session, bc *browserConn) {
	for {
		typ, data, err := bc.ws.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			m.forwardToAgent(s.agentID, s.id, data)
		case websocket.MessageText:
			var msg contracts.TerminalMessage
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case contracts.TerminalMsgBrowserResize:
				var p contracts.TerminalResizePayload
				if json.Unmarshal(msg.Payload, &p) == nil {
					m.forwardControl(s.agentID, contracts.TerminalMessage{
						Type:    contracts.TerminalMsgServerResize,
						Payload: mustJSON(p),
					})
				}
			case contracts.TerminalMsgBrowserSignal:
				var p contracts.TerminalSignalPayload
				if json.Unmarshal(msg.Payload, &p) == nil {
					m.forwardControl(s.agentID, contracts.TerminalMessage{
						Type:    contracts.TerminalMsgServerSignal,
						Payload: mustJSON(p),
					})
				}
			case contracts.TerminalMsgBrowserClose:
				_ = m.CloseSession(context.Background(), s.id, "cerrada por el operador")
			}
		}
	}
}

// forwardToAgent envía datos de entrada del operador al agente.
func (m *Manager) forwardToAgent(agentID, sessionID string, data []byte) {
	frame, err := encodeAgentData(sessionID, data)
	if err != nil {
		return
	}
	m.mu.Lock()
	ch := m.agents[agentID]
	m.mu.Unlock()
	if ch == nil {
		return
	}
	ch.sendMu.Lock()
	defer ch.sendMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = ch.ws.Write(ctx, websocket.MessageBinary, frame)
}

// forwardControl envía un mensaje de control del operador al agente.
func (m *Manager) forwardControl(agentID string, msg contracts.TerminalMessage) {
	m.mu.Lock()
	ch := m.agents[agentID]
	m.mu.Unlock()
	if ch == nil {
		return
	}
	_ = m.sendJSON(ch, msg)
}

// sendJSON serializa y envía un mensaje de texto al agente.
func (m *Manager) sendJSON(ch *agentChannel, msg contracts.TerminalMessage) error {
	if ch == nil {
		return fmt.Errorf("canal de agente no disponible")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ch.sendMu.Lock()
	defer ch.sendMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ch.ws.Write(ctx, websocket.MessageText, data)
}

// CloseSession cierra la sesión: avisa al agente, marca el estado y notifica
// al navegador. Es best-effort por el lado del agente.
func (m *Manager) CloseSession(ctx context.Context, sessionID, reason string) error {
	s := m.getSession(sessionID)
	if s == nil {
		return ErrSessionNotFound
	}
	if s.isDone() {
		return ErrSessionClosed
	}
	if reason == "" {
		reason = "cerrada por el operador"
	}

	// Avisa al agente para que termine el shell (best-effort).
	m.forwardControl(s.agentID, contracts.TerminalMessage{
		Type: contracts.TerminalMsgServerClose,
		Payload: mustJSON(contracts.TerminalClosePayload{
			SessionID: sessionID, Reason: reason,
		}),
	})

	m.closeInternal(sessionID, contracts.TerminalSessionClosed, reason)
	return nil
}

// interruptSession marca una sesión como interrumpida (pérdida de canal).
func (m *Manager) interruptSession(sessionID, reason string) {
	m.closeInternal(sessionID, contracts.TerminalSessionInterrupted, reason)
}

// closeInternal es la implementación común de cierre.
func (m *Manager) closeInternal(sessionID, state, reason string) {
	now := m.now().UTC()
	err := m.store.Update(context.Background(), func(st *store.State) error {
		ts := st.TerminalSessions[sessionID]
		if ts == nil {
			return store.ErrNotFound
		}
		if ts.State == contracts.TerminalSessionClosed || ts.State == contracts.TerminalSessionInterrupted {
			return nil
		}
		ts.State = state
		ts.ClosedAt = &now
		ts.Reason = reason
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		m.log.Warn("no se pudo actualizar el estado de la sesión", "session_id", sessionID, "error", err.Error())
	}

	s := m.getSession(sessionID)
	if s != nil {
		m.notifyBrowser(s, contracts.TerminalMessage{
			Type:    contracts.TerminalMsgServerClosed,
			Payload: mustJSON(contracts.TerminalClosePayload{SessionID: sessionID, Reason: reason}),
		})
		m.finish(s)
	}
}

// finish cierra el canal done de la sesión (una sola vez).
func (m *Manager) finish(s *session) {
	s.closeOnce.Do(func() {
		close(s.done)
		m.mu.Lock()
		delete(m.sessions, s.id)
		delete(m.tickets, s.id)
		m.mu.Unlock()
	})
}

// StreamTicket devuelve el ticket de stream de una sesión ("" si no existe).
// Va dirigido al operador: se presenta en el WebSocket como ?ticket=….
func (m *Manager) StreamTicket(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tickets[sessionID]
}

// ValidStreamTicket indica si el ticket presentado corresponde a la sesión.
// Comparación en tiempo constante para no filtrar el secreto.
func (m *Manager) ValidStreamTicket(sessionID, ticket string) bool {
	if sessionID == "" || ticket == "" {
		return false
	}
	m.mu.Lock()
	expected, ok := m.tickets[sessionID]
	m.mu.Unlock()
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(ticket)) == 1
}

// sessionState devuelve el estado persistido de una sesión.
func (m *Manager) sessionState(sessionID string) string {
	var state string
	_ = m.store.View(context.Background(), func(st *store.State) error {
		if ts := st.TerminalSessions[sessionID]; ts != nil {
			state = ts.State
		}
		return nil
	})
	return state
}

// reaperEntry es una sesión pendiente/expirada a cerrar.
type reaperEntry struct {
	id     string
	reason string
}

// ReapSessions cierra las sesiones expiradas o pendientes sin respuesta del
// agente y purga las ya cerradas. Pensado para ejecutarse periódicamente.
func (m *Manager) ReapSessions() {
	now := m.now().UTC()
	var toClose []reaperEntry
	var toPrune []string

	_ = m.store.View(context.Background(), func(st *store.State) error {
		for id, ts := range st.TerminalSessions {
			switch ts.State {
			case contracts.TerminalSessionClosed, contracts.TerminalSessionInterrupted:
				if ts.ClosedAt != nil && now.Sub(*ts.ClosedAt) > closedSessionTTL {
					toPrune = append(toPrune, id)
				}
				continue
			}
			if !ts.ExpiresAt.IsZero() && now.After(ts.ExpiresAt) {
				toClose = append(toClose, reaperEntry{id: id, reason: "sesión expirada"})
				continue
			}
			if ts.State == contracts.TerminalSessionPending && !ts.CreatedAt.IsZero() &&
				now.Sub(ts.CreatedAt) > agentAckTimeout {
				toClose = append(toClose, reaperEntry{id: id, reason: "agente no respondió a tiempo"})
			}
		}
		return nil
	})

	if len(toPrune) > 0 {
		_ = m.store.Update(context.Background(), func(st *store.State) error {
			for _, id := range toPrune {
				delete(st.TerminalSessions, id)
			}
			return nil
		})
	}

	for _, t := range toClose {
		m.interruptSession(t.id, t.reason)
	}
}

// defaultSessionTTL es la vida por defecto de una sesión de terminal.
func defaultSessionTTL() time.Duration {
	return 30 * time.Minute
}

// newSessionID genera un identificador de sesión impredecible.
func newSessionID() (string, error) {
	return randomHex(16)
}

// randomHex genera n bytes aleatorios codificados en hexadecimal.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// mustJSON serializa un valor; los tipos de payload son siempre serializables.
func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
