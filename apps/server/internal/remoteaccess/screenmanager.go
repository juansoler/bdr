// screenmanager.go implementa el gestor de sesiones de acceso por pantalla
// (VNC vía noVNC). Es el gemelo del gestor de terminal: registro del canal
// saliente del agente, ciclo de vida de sesiones, relay WebSocket bidireccional
// y auditoría. Los frames binarios transportan bytes crudos del protocolo RFB.
//
// El agente siempre inicia la conexión (canal saliente autenticado con mTLS y
// API key); el servidor solo autoriza y retransmite. El VNC del equipo
// gestionado escucha solo en localhost: nunca hay puertos VNC expuestos.
package remoteaccess

import (
	"context"
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

// maxScreenSessionsPerAgent limita las sesiones de pantalla simultáneas por
// agente (un VNC consume bastante ancho de banda: menos que terminal).
const maxScreenSessionsPerAgent = 2

// defaultScreenTTL es la vida por defecto de una sesión de pantalla.
const defaultScreenTTL = 30 * time.Minute

// ScreenManager coordina el registro de agentes conectados al canal de
// pantalla, el ciclo de vida de sesiones y el relay bidireccional entre el
// navegador (noVNC) y el agente.
type ScreenManager struct {
	store store.Store
	log   *logging.Logger
	now   func() time.Time

	mu       sync.Mutex
	agents   map[string]*agentChannel
	sessions map[string]*screenSession
	// tickets asocia cada sesión con el ticket de stream del operador: los
	// WebSockets del navegador no pueden enviar cabeceras de autorización,
	// así que presentan este secreto como query param (?ticket=…).
	tickets map[string]string
}

// screenSession mantiene el estado en memoria de una sesión de pantalla.
type screenSession struct {
	id      string
	agentID string

	browserMu sync.Mutex
	browser   *browserConn // nil si no hay operador conectado

	done      chan struct{} // se cierra al cerrarse la sesión
	closeOnce sync.Once
}

// NewScreenManager crea un gestor de acceso por pantalla.
func NewScreenManager(st store.Store, log *logging.Logger) *ScreenManager {
	if log == nil {
		log = logging.New("info")
	}
	return &ScreenManager{
		store:    st,
		log:      log,
		now:      time.Now,
		agents:   map[string]*agentChannel{},
		sessions: map[string]*screenSession{},
		tickets:  map[string]string{},
	}
}

// SetClock permite inyectar un reloj para tests.
func (m *ScreenManager) SetClock(now func() time.Time) { m.now = now }

// AgentOnline indica si el agente tiene canal de pantalla abierto.
func (m *ScreenManager) AgentOnline(agentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.agents[agentID]
	return ok
}

// ScreenCapabilities devuelve la disponibilidad de pantalla de un agente.
func (m *ScreenManager) ScreenCapabilities(agentID, osFamily string) contracts.ScreenCapabilities {
	return contracts.ScreenCapabilities{
		AgentID:         agentID,
		Online:          m.AgentOnline(agentID),
		OSFamily:        osFamily,
		Provider:        screenProviderFor(osFamily),
		DisplayRequired: osFamily != "",
	}
}

// screenProviderFor describe cómo obtiene el escritorio local cada familia
// de SO (ver contracts.ScreenCapabilities.Provider).
func screenProviderFor(osFamily string) string {
	switch osFamily {
	case "windows":
		return "vnc-external"
	case "darwin":
		return "vnc-built-in"
	case "linux":
		return "vnc-helper"
	default:
		return ""
	}
}

// RegisterAgent registra el canal saliente de un agente y bloquea mientras
// la conexión siga viva (el handler WebSocket no debe retornar hasta que
// termine). Al cerrarse el canal, desconecta al agente y marca sus sesiones
// como interrumpidas.
func (m *ScreenManager) RegisterAgent(ctx context.Context, agentID string, ws *websocket.Conn) {
	m.mu.Lock()
	if old, ok := m.agents[agentID]; ok {
		m.mu.Unlock()
		_ = old.ws.Close(websocket.StatusPolicyViolation, "canal duplicado")
		time.Sleep(50 * time.Millisecond)
		m.mu.Lock()
	}
	ch := &agentChannel{agentID: agentID, ws: ws}
	m.agents[agentID] = ch
	m.mu.Unlock()

	m.resumePendingScreenSessions(ch)

	m.screenAgentReadLoop(ctx, ch)

	m.mu.Lock()
	if m.agents[agentID] == ch {
		delete(m.agents, agentID)
	}
	m.mu.Unlock()
	m.handleScreenAgentDisconnect(ch)
}

// resumePendingScreenSessions reenvía la orden de apertura de las sesiones
// pendientes de un agente que acaba de conectar su canal.
func (m *ScreenManager) resumePendingScreenSessions(ch *agentChannel) {
	m.mu.Lock()
	var ids []string
	for _, s := range m.sessions {
		if s.agentID == ch.agentID && !s.isDone() {
			ids = append(ids, s.id)
		}
	}
	m.mu.Unlock()

	for _, id := range ids {
		var rec *store.ScreenSession
		_ = m.store.View(context.Background(), func(st *store.State) error {
			rec = st.ScreenSessions[id]
			return nil
		})
		if rec == nil || rec.State != contracts.ScreenSessionPending {
			continue
		}
		msg := contracts.ScreenMessage{
			Type: contracts.ScreenMsgAgentOpen,
			Payload: mustJSON(contracts.ScreenOpenPayload{
				SessionID: id, Quality: rec.Quality, AllowClipboard: rec.AllowClipboard,
			}),
		}
		if err := m.sendScreenJSON(ch, msg); err != nil {
			m.interruptScreenSession(id, "no se pudo reenviar la orden de apertura al agente")
		}
	}
}

// screenAgentReadLoop procesa los mensajes del agente: control (texto) y
// datos binarios multiplexados por sesión (bytes RFB).
func (m *ScreenManager) screenAgentReadLoop(ctx context.Context, ch *agentChannel) {
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
			m.relayScreenOutput(sessionID, payload)
		case websocket.MessageText:
			var msg contracts.ScreenMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			m.handleScreenAgentControl(ch, msg)
		}
	}
}

// handleScreenAgentControl procesa un mensaje de control del agente.
func (m *ScreenManager) handleScreenAgentControl(ch *agentChannel, msg contracts.ScreenMessage) {
	switch msg.Type {
	case contracts.ScreenMsgAgentOpened:
		var p contracts.ScreenOpenedPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			p.SessionID = ""
		}
		sessionID := p.SessionID
		if sessionID == "" {
			sessionID = msg.SessionID
		}
		if sessionID == "" {
			return
		}
		m.onScreenAgentOpened(sessionID)
	case contracts.ScreenMsgAgentError:
		var p contracts.ScreenErrorPayload
		_ = json.Unmarshal(msg.Payload, &p)
		m.onScreenAgentError(msg.SessionID, p.Message)
	case contracts.ScreenMsgAgentExit:
		var p contracts.ScreenExitPayload
		_ = json.Unmarshal(msg.Payload, &p)
		m.onScreenAgentExit(msg.SessionID, p.Code)
	case contracts.ScreenMsgAgentAckClose:
		// Acuse del agente: sin acción adicional (la sesión ya se cerró).
	case contracts.ScreenMsgPong:
		// keep-alive: sin acción.
	case contracts.ScreenMsgPing:
		_ = m.sendScreenJSON(ch, contracts.ScreenMessage{Type: contracts.ScreenMsgPong})
	}
}

// onScreenAgentOpened marca la sesión como activa. No se envía ningún
// mensaje de control al navegador: el stream de pantalla transporta solo
// bytes RFB (noVNC interpretaría un frame de texto como datos corruptos) y
// el visor detecta la sesión activa cuando completa el handshake RFB.
func (m *ScreenManager) onScreenAgentOpened(sessionID string) {
	s := m.getScreenSession(sessionID)
	if s == nil {
		return
	}
	now := m.now().UTC()
	_ = m.store.Update(context.Background(), func(st *store.State) error {
		ss := st.ScreenSessions[sessionID]
		if ss == nil {
			return store.ErrNotFound
		}
		if ss.State == contracts.ScreenSessionPending {
			ss.State = contracts.ScreenSessionActive
			ss.OpenedAt = &now
		}
		return nil
	})
}

// onScreenAgentError registra el fallo del agente al abrir la sesión.
func (m *ScreenManager) onScreenAgentError(sessionID, message string) {
	m.closeScreenInternal(sessionID, contracts.ScreenSessionClosed,
		"el agente no pudo atender la pantalla: "+message)
}

// onScreenAgentExit cierra la sesión cuando el VNC local termina por su cuenta.
func (m *ScreenManager) onScreenAgentExit(sessionID string, code int) {
	now := m.now().UTC()
	reason := fmt.Sprintf("el escritorio remoto finalizó (código %d)", code)
	err := m.store.Update(context.Background(), func(st *store.State) error {
		ss := st.ScreenSessions[sessionID]
		if ss == nil {
			return store.ErrNotFound
		}
		if ss.State != contracts.ScreenSessionClosed && ss.State != contracts.ScreenSessionInterrupted {
			ss.State = contracts.ScreenSessionClosed
			ss.ClosedAt = &now
			ss.Reason = reason
		}
		return nil
	})
	if err == nil {
		s := m.getScreenSession(sessionID)
		if s != nil {
			m.notifyScreenBrowser(s, reason)
			m.finishScreen(s)
		}
	}
}

// handleScreenAgentDisconnect cierra todas las sesiones del agente al perder
// el canal de control (política: cortar el VNC local y marcar interrumpida).
func (m *ScreenManager) handleScreenAgentDisconnect(ch *agentChannel) {
	agentID := ch.agentID
	m.mu.Lock()
	var sessions []*screenSession
	for _, s := range m.sessions {
		if s.agentID == agentID && !s.isDone() {
			sessions = append(sessions, s)
		}
	}
	m.mu.Unlock()

	for _, s := range sessions {
		m.interruptScreenSession(s.id, "se perdió la conexión de control del agente")
	}
}

// relayScreenOutput envía bytes RFB del agente al navegador de la sesión.
func (m *ScreenManager) relayScreenOutput(sessionID string, payload []byte) {
	s := m.getScreenSession(sessionID)
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
		m.log.Debug("no se pudo escribir salida de pantalla al navegador", "session_id", sessionID, "error", err.Error())
	}
}

// notifyScreenBrowser cierra el WebSocket del navegador con el motivo dado.
//
// El stream de pantalla transporta solo bytes RFB (noVNC no tolera frames de
// texto), así que los estados de «sesión terminada» se comunican con el
// frame de cierre de WebSocket: el visor muestra el motivo capturado por su
// propio listener de close.
func (m *ScreenManager) notifyScreenBrowser(s *screenSession, reason string) {
	s.browserMu.Lock()
	bc := s.browser
	s.browserMu.Unlock()
	if bc == nil {
		return
	}
	r := reason
	if len(r) > 120 {
		r = r[:120]
	}
	_ = bc.ws.Close(websocket.StatusNormalClosure, r)
}

// getScreenSession devuelve la sesión en memoria (nil si no existe).
func (m *ScreenManager) getScreenSession(sessionID string) *screenSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID]
}

// isDone indica si la sesión ya se cerró.
func (s *screenSession) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// CreateSession crea una sesión de pantalla para el agente indicado.
func (m *ScreenManager) CreateSession(ctx context.Context, agent store.Agent, op OperatorInfo, req contracts.CreateScreenSessionRequest) (contracts.ScreenSessionView, error) {
	if agent.RevokedAt != nil {
		return contracts.ScreenSessionView{}, ErrAgentNotEnrolled
	}

	// El agente debe tener canal de pantalla activo y no exceder el límite.
	m.mu.Lock()
	_, online := m.agents[agent.ID]
	if online {
		active := 0
		for _, s := range m.sessions {
			if s.agentID == agent.ID && !s.isDone() {
				active++
			}
		}
		if active >= maxScreenSessionsPerAgent {
			m.mu.Unlock()
			return contracts.ScreenSessionView{}, ErrTooManySessions
		}
	}
	m.mu.Unlock()

	quality := req.Quality
	if quality == "" {
		quality = contracts.ScreenQualityMedium
	}

	id, err := newSessionID()
	if err != nil {
		return contracts.ScreenSessionView{}, err
	}
	now := m.now().UTC()
	rec := &store.ScreenSession{
		ID: id, AgentID: agent.ID, OperatorID: op.ID, OperatorName: op.Name,
		Quality: quality, AllowClipboard: req.AllowClipboard,
		State: contracts.ScreenSessionPending, CreatedAt: now, ExpiresAt: now.Add(defaultScreenTTL),
	}
	if err := m.store.Update(ctx, func(st *store.State) error {
		st.ScreenSessions[id] = rec
		return nil
	}); err != nil {
		return contracts.ScreenSessionView{}, err
	}

	s := &screenSession{id: id, agentID: agent.ID, done: make(chan struct{})}
	ticket, err := randomHex(24)
	if err != nil {
		return contracts.ScreenSessionView{}, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.tickets[id] = ticket
	m.mu.Unlock()

	// Pide al agente que atienda el VNC local.
	msg := contracts.ScreenMessage{
		Type: contracts.ScreenMsgAgentOpen,
		Payload: mustJSON(contracts.ScreenOpenPayload{
			SessionID: id, Quality: quality, AllowClipboard: req.AllowClipboard,
		}),
	}
	if online {
		m.mu.Lock()
		ch := m.agents[agent.ID]
		m.mu.Unlock()
		if err := m.sendScreenJSON(ch, msg); err != nil {
			m.interruptScreenSession(id, "no se pudo enviar la orden de apertura al agente")
			return contracts.ScreenSessionView{}, err
		}
	}

	return screenViewFromStore(rec, agent), nil
}

// screenViewFromStore convierte la metadata persistida en vista pública.
func screenViewFromStore(ss *store.ScreenSession, agent store.Agent) contracts.ScreenSessionView {
	v := contracts.ScreenSessionView{
		SessionID:      ss.ID,
		AgentID:        ss.AgentID,
		Quality:        ss.Quality,
		AllowClipboard: ss.AllowClipboard,
		State:          ss.State,
		CreatedAt:      ss.CreatedAt,
		OpenedAt:       ss.OpenedAt,
		ClosedAt:       ss.ClosedAt,
		ExpiresAt:      ss.ExpiresAt,
		Reason:         ss.Reason,
		OperatorID:     ss.OperatorID,
		OperatorName:   ss.OperatorName,
	}
	if agent.Hostname != "" {
		v.Hostname = agent.Hostname
		v.OSFamily = agent.OSFamily
	}
	return v
}

// GetScreenSession devuelve la vista de una sesión de pantalla.
func (m *ScreenManager) GetScreenSession(sessionID string) (*contracts.ScreenSessionView, error) {
	var view *contracts.ScreenSessionView
	err := m.store.View(context.Background(), func(st *store.State) error {
		ss := st.ScreenSessions[sessionID]
		if ss == nil {
			return store.ErrNotFound
		}
		v := contracts.ScreenSessionView{}
		if agent := st.Agents[ss.AgentID]; agent != nil {
			v = screenViewFromStore(ss, *agent)
		} else {
			v = screenViewFromStore(ss, store.Agent{})
		}
		view = &v
		return nil
	})
	if err != nil {
		return nil, ErrSessionNotFound
	}
	return view, nil
}

// ListScreenSessions devuelve las sesiones ordenadas por creación (más
// recientes primero).
func (m *ScreenManager) ListScreenSessions() []contracts.ScreenSessionView {
	items := []contracts.ScreenSessionView{}
	_ = m.store.View(context.Background(), func(st *store.State) error {
		for _, ss := range st.ScreenSessions {
			agent := st.Agents[ss.AgentID]
			if agent == nil {
				agent = &store.Agent{}
			}
			items = append(items, screenViewFromStore(ss, *agent))
		}
		return nil
	})
	return items
}

// AttachScreenBrowser conecta el WebSocket del operador a la sesión de
// pantalla y bloquea mientras el navegador siga conectado.
func (m *ScreenManager) AttachScreenBrowser(ctx context.Context, sessionID string, ws *websocket.Conn) error {
	s := m.getScreenSession(sessionID)
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

	m.screenBrowserReadLoop(ctx, s, bc)

	s.browserMu.Lock()
	if s.browser == bc {
		s.browser = nil
	}
	s.browserMu.Unlock()

	// El operador cerró el visor: termina la sesión de pantalla (una sesión
	// de VNC sin visor solo consume ancho de banda y expone la pantalla).
	if !s.isDone() {
		_ = m.CloseScreenSession(context.Background(), sessionID, "el operador cerró el visor")
	}
	return nil
}

// screenBrowserReadLoop procesa los mensajes del navegador: entrada binaria
// (bytes RFB) y cierre. No hay mensajes de control de texto en este canal:
// el visor noVNC interpreta todo el stream como protocolo RFB.
func (m *ScreenManager) screenBrowserReadLoop(ctx context.Context, s *screenSession, bc *browserConn) {
	for {
		typ, data, err := bc.ws.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			m.forwardScreenToAgent(s.agentID, s.id, data)
		case websocket.MessageText:
			// Frame de texto en un canal RFB: solo se acepta la orden de
			// cierre explícita (enviada por el visor antes de desconectar).
			var msg contracts.ScreenMessage
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			if msg.Type == contracts.ScreenMsgBrowserClose {
				_ = m.CloseScreenSession(context.Background(), s.id, "cerrada por el operador")
			}
		}
	}
}

// forwardScreenToAgent envía bytes RFB del operador al agente.
func (m *ScreenManager) forwardScreenToAgent(agentID, sessionID string, data []byte) {
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

// sendScreenJSON serializa y envía un mensaje de texto al agente.
func (m *ScreenManager) sendScreenJSON(ch *agentChannel, msg contracts.ScreenMessage) error {
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

// CloseScreenSession cierra la sesión: avisa al agente, marca el estado y
// notifica al navegador. Es best-effort por el lado del agente.
func (m *ScreenManager) CloseScreenSession(ctx context.Context, sessionID, reason string) error {
	s := m.getScreenSession(sessionID)
	if s == nil {
		return ErrSessionNotFound
	}
	if s.isDone() {
		return ErrSessionClosed
	}
	if reason == "" {
		reason = "cerrada por el operador"
	}

	// Avisa al agente para que corte el VNC local (best-effort).
	m.mu.Lock()
	ch := m.agents[s.agentID]
	m.mu.Unlock()
	_ = m.sendScreenJSON(ch, contracts.ScreenMessage{
		Type: contracts.ScreenMsgServerClose,
		Payload: mustJSON(contracts.ScreenClosePayload{
			SessionID: sessionID, Reason: reason,
		}),
	})

	m.closeScreenInternal(sessionID, contracts.ScreenSessionClosed, reason)
	return nil
}

// interruptScreenSession marca una sesión como interrumpida (pérdida de canal).
func (m *ScreenManager) interruptScreenSession(sessionID, reason string) {
	m.closeScreenInternal(sessionID, contracts.ScreenSessionInterrupted, reason)
}

// closeScreenInternal es la implementación común de cierre.
func (m *ScreenManager) closeScreenInternal(sessionID, state, reason string) {
	now := m.now().UTC()
	err := m.store.Update(context.Background(), func(st *store.State) error {
		ss := st.ScreenSessions[sessionID]
		if ss == nil {
			return store.ErrNotFound
		}
		if ss.State == contracts.ScreenSessionClosed || ss.State == contracts.ScreenSessionInterrupted {
			return nil
		}
		ss.State = state
		ss.ClosedAt = &now
		ss.Reason = reason
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		m.log.Warn("no se pudo actualizar el estado de la sesión de pantalla", "session_id", sessionID, "error", err.Error())
	}

	s := m.getScreenSession(sessionID)
	if s != nil {
		m.notifyScreenBrowser(s, reason)
		m.finishScreen(s)
	}
}

// finishScreen cierra el canal done de la sesión (una sola vez).
func (m *ScreenManager) finishScreen(s *screenSession) {
	s.closeOnce.Do(func() {
		close(s.done)
		m.mu.Lock()
		delete(m.sessions, s.id)
		delete(m.tickets, s.id)
		m.mu.Unlock()
	})
}

// ScreenStreamTicket devuelve el ticket de stream de una sesión ("" si no existe).
func (m *ScreenManager) ScreenStreamTicket(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tickets[sessionID]
}

// ValidScreenStreamTicket indica si el ticket presentado corresponde a la
// sesión. Comparación en tiempo constante para no filtrar el secreto.
func (m *ScreenManager) ValidScreenStreamTicket(sessionID, ticket string) bool {
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

// ReapScreenSessions cierra las sesiones expiradas o pendientes sin respuesta
// del agente y purga las ya cerradas. Pensado para ejecutarse periódicamente.
func (m *ScreenManager) ReapScreenSessions() {
	now := m.now().UTC()
	var toClose []reaperEntry
	var toPrune []string

	_ = m.store.View(context.Background(), func(st *store.State) error {
		for id, ss := range st.ScreenSessions {
			switch ss.State {
			case contracts.ScreenSessionClosed, contracts.ScreenSessionInterrupted:
				if ss.ClosedAt != nil && now.Sub(*ss.ClosedAt) > closedSessionTTL {
					toPrune = append(toPrune, id)
				}
				continue
			}
			if !ss.ExpiresAt.IsZero() && now.After(ss.ExpiresAt) {
				toClose = append(toClose, reaperEntry{id: id, reason: "sesión expirada"})
				continue
			}
			if ss.State == contracts.ScreenSessionPending && !ss.CreatedAt.IsZero() &&
				now.Sub(ss.CreatedAt) > agentAckTimeout {
				toClose = append(toClose, reaperEntry{id: id, reason: "agente no respondió a tiempo"})
			}
		}
		return nil
	})

	if len(toPrune) > 0 {
		_ = m.store.Update(context.Background(), func(st *store.State) error {
			for _, id := range toPrune {
				delete(st.ScreenSessions, id)
			}
			return nil
		})
	}

	for _, t := range toClose {
		m.interruptScreenSession(t.id, t.reason)
	}
}
