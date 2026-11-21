// Package control expone los endpoints HTTP y WebSocket del subsistema de
// Remote Access (terminales remotas). Cada operación es independiente del
// ciclo de vida de jobs de backup.
package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/juansoler/bdr/apps/server/internal/remoteaccess"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// handleAgentTerminalCapabilities devuelve qué puede hacer el autenticado con
// el agente (capacidades y estado del canal de terminal).
// GET /api/v1/admin/agents/{id}/terminal  (terminal:read)
func (s *Server) handleAgentTerminalCapabilities(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	r, _, ok := s.authorize(w, r, contracts.CapTerminalRead)
	if !ok {
		return
	}
	osFamily := r.URL.Query().Get("os_family") // opcional hint del navegador
	if err := s.Store.View(r.Context(), func(st *store.State) error {
		if st.Agents[agentID] == nil {
			return store.ErrNotFound
		}
		return nil
	}); err != nil {
		s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado")
		return
	}
	writeJSON(w, http.StatusOK, s.RemoteAccess.AgentCapabilities(agentID, osFamily))
}

// handleCreateTerminalSession crea una sesión de terminal para un agente.
// POST /api/v1/admin/agents/{id}/terminal/sessions  (terminal:connect)
func (s *Server) handleCreateTerminalSession(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	r, a, ok := s.authorize(w, r, contracts.CapTerminalConnect)
	if !ok {
		return
	}
	// Todos los campos son opcionales: un cuerpo vacío (POST sin JSON)
	// equivale a la petición por defecto.
	var req contracts.CreateTerminalSessionRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyAdmin))
	if err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "No se pudo leer el cuerpo")
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "El cuerpo no es JSON válido")
			return
		}
	}
	if err := validateCreateRequest(req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", err.Error())
		return
	}

	var agent store.Agent
	err = s.Store.View(r.Context(), func(st *store.State) error {
		a0 := st.Agents[agentID]
		if a0 == nil || a0.RevokedAt != nil {
			return store.ErrNotFound
		}
		agent = *a0
		return nil
	})
	if err != nil {
		s.problem(w, r, http.StatusNotFound, "E_AGENT_NOT_FOUND", "Agente no encontrado o revocado")
		return
	}

	view, err := s.RemoteAccess.CreateSession(r.Context(), agent,
		remoteaccess.OperatorInfo{ID: a.id, Name: a.auditName()}, req)
	if err != nil {
		switch err {
		case remoteaccess.ErrAgentNotEnrolled:
			s.problem(w, r, http.StatusForbidden, "E_AGENT_REVOKED", "El agente está revocado")
		case remoteaccess.ErrAgentOffline:
			s.problem(w, r, http.StatusConflict, "E_AGENT_OFFLINE", "El agente no tiene conexión de terminal activa")
		case remoteaccess.ErrTooManySessions:
			s.problem(w, r, http.StatusConflict, "E_TERMINAL_BUSY", "El agente ya tiene el máximo de sesiones simultáneas")
		default:
			s.problem(w, r, http.StatusInternalServerError, "E_TERMINAL_CREATE", err.Error())
		}
		return
	}
	view.StreamURL = s.streamURL(view.SessionID)
	view.StreamTicket = s.RemoteAccess.StreamTicket(view.SessionID)
	s.audit(r, a, "terminal.session.created", "terminal_session", view.SessionID, "ok",
		map[string]string{"agent_id": agentID, "shell": view.Shell, "privilege": view.Privilege})
	writeJSON(w, http.StatusCreated, view)
}

// handleListTerminalSessions lista todas las sesiones de terminal.
// GET /api/v1/admin/terminal/sessions  (terminal:read)
func (s *Server) handleListTerminalSessions(w http.ResponseWriter, r *http.Request) {
	r, _, ok := s.authorize(w, r, contracts.CapTerminalRead)
	if !ok {
		return
	}
	items := s.RemoteAccess.ListSessions()
	writeJSON(w, http.StatusOK, contracts.TerminalSessionList{Items: items})
}

// handleGetTerminalSession devuelve el estado de una sesión.
// GET /api/v1/admin/terminal/sessions/{id}  (terminal:read)
func (s *Server) handleGetTerminalSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	r, _, ok := s.authorize(w, r, contracts.CapTerminalRead)
	if !ok {
		return
	}
	view, err := s.RemoteAccess.GetSession(sessionID)
	if err != nil {
		s.problem(w, r, http.StatusNotFound, "E_SESSION_NOT_FOUND", "Sesión de terminal no encontrada")
		return
	}
	view.StreamURL = s.streamURL(view.SessionID)
	writeJSON(w, http.StatusOK, view)
}

// handleCloseTerminalSession cierra una sesión.
// POST /api/v1/admin/terminal/sessions/{id}/close  (terminal:terminate)
func (s *Server) handleCloseTerminalSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	r, a, ok := s.authorize(w, r, contracts.CapTerminalTerminate)
	if !ok {
		return
	}
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if err := s.RemoteAccess.CloseSession(r.Context(), sessionID, reason); err != nil {
		switch err {
		case remoteaccess.ErrSessionNotFound:
			s.problem(w, r, http.StatusNotFound, "E_SESSION_NOT_FOUND", "Sesión de terminal no encontrada")
		case remoteaccess.ErrSessionClosed:
			s.problem(w, r, http.StatusConflict, "E_SESSION_CLOSED", "La sesión ya está cerrada")
		default:
			s.internalError(w, r, err)
		}
		return
	}
	s.audit(r, a, "terminal.session.closed", "terminal_session", sessionID, "ok",
		map[string]string{"reason": reason})
	w.WriteHeader(http.StatusNoContent)
}

// handleTerminalStream es el WebSocket de datos entre el navegador y la sesión.
// GET /api/v1/admin/terminal/sessions/{id}/stream  (terminal:connect)
//
// Los WebSockets del navegador no pueden enviar cabeceras de autorización:
// se acepta un ticket impredecible entregado al crear la sesión
// (?ticket=…) como credencial alternativa a la autenticación estándar.
func (s *Server) handleTerminalStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if ticket := r.URL.Query().Get("ticket"); ticket != "" {
		if !s.RemoteAccess.ValidStreamTicket(sessionID, ticket) {
			s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Ticket de stream inválido o sesión inexistente")
			return
		}
	} else {
		var ok bool
		r, _, ok = s.authorize(w, r, contracts.CapTerminalConnect)
		if !ok {
			return
		}
	}
	if _, err := s.RemoteAccess.GetSession(sessionID); err != nil {
		s.problem(w, r, http.StatusNotFound, "E_SESSION_NOT_FOUND", "Sesión de terminal no encontrada")
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.RemoteAccess.AttachBrowser(r.Context(), sessionID, conn); err != nil {
		s.Logger.Debug("terminal stream rechazada", "session_id", sessionID, "error", err.Error())
		s.problem(w, r, http.StatusConflict, "E_STREAM_FAILED", err.Error())
		return
	}
}

// handleAgentTerminalConnect es el canal persistente del agente (mTLS + API key).
// GET /api/v1/agent/terminal/connect  (auth de agente)
func (s *Server) handleAgentTerminalConnect(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.RemoteAccess.RegisterAgent(r.Context(), agent.ID, conn)
}

// streamURL construye la URL de WebSocket del stream de datos.
func (s *Server) streamURL(sessionID string) string {
	base := s.PublicURL
	if base == "" {
		return "/api/v1/admin/terminal/sessions/" + sessionID + "/stream"
	}
	scheme := "ws"
	switch strings.ToLower(strings.SplitN(base, "://", 2)[0]) {
	case "https":
		scheme = "wss"
	case "http":
		scheme = "ws"
	}
	host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	if host == base {
		host = strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	}
	return scheme + "://" + host + "/api/v1/admin/terminal/sessions/" + sessionID + "/stream"
}

// validateCreateRequest comprueba la petición de creación.
func validateCreateRequest(req contracts.CreateTerminalSessionRequest) error {
	if req.Cols > 0 && req.Cols > 4096 {
		return fmt.Errorf("cols demasiado grandes (%d)", req.Cols)
	}
	if req.Rows > 0 && req.Rows > 1024 {
		return fmt.Errorf("rows demasiado grandes (%d)", req.Rows)
	}
	if req.Privilege != "" && req.Privilege != contracts.TerminalPrivilegeStandard &&
		req.Privilege != contracts.TerminalPrivilegeElevated {
		return fmt.Errorf("privilege debe ser %s o %s", contracts.TerminalPrivilegeStandard,
			contracts.TerminalPrivilegeElevated)
	}
	return nil
}
