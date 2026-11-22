// screenaccess.go expone los endpoints HTTP y WebSocket del subsistema de
// acceso por pantalla (VNC en el navegador vía noVNC). Cada operación es
// independiente del ciclo de vida de jobs de backup.
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

// handleAgentScreenCapabilities devuelve la disponibilidad de pantalla del
// agente (proveedor VNC local y estado del canal).
// GET /api/v1/admin/agents/{id}/screen  (screen:read)
func (s *Server) handleAgentScreenCapabilities(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	r, _, ok := s.authorize(w, r, contracts.CapScreenRead)
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
	writeJSON(w, http.StatusOK, s.ScreenAccess.ScreenCapabilities(agentID, osFamily))
}

// handleCreateScreenSession crea una sesión de pantalla para un agente.
// POST /api/v1/admin/agents/{id}/screen/sessions  (screen:connect)
func (s *Server) handleCreateScreenSession(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	r, a, ok := s.authorize(w, r, contracts.CapScreenConnect)
	if !ok {
		return
	}
	// Todos los campos son opcionales: un cuerpo vacío (POST sin JSON)
	// equivale a la petición por defecto.
	var req contracts.CreateScreenSessionRequest
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
	if !contracts.ValidScreenQuality(req.Quality) {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION",
			fmt.Sprintf("quality debe ser %s, %s o %s", contracts.ScreenQualityLow,
				contracts.ScreenQualityMedium, contracts.ScreenQualityHigh))
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

	view, err := s.ScreenAccess.CreateSession(r.Context(), agent,
		remoteaccess.OperatorInfo{ID: a.id, Name: a.auditName()}, req)
	if err != nil {
		switch err {
		case remoteaccess.ErrAgentNotEnrolled:
			s.problem(w, r, http.StatusForbidden, "E_AGENT_REVOKED", "El agente está revocado")
		case remoteaccess.ErrTooManySessions:
			s.problem(w, r, http.StatusConflict, "E_SCREEN_BUSY", "El agente ya tiene el máximo de sesiones de pantalla simultáneas")
		default:
			s.problem(w, r, http.StatusInternalServerError, "E_SCREEN_CREATE", err.Error())
		}
		return
	}
	view.StreamURL = s.screenStreamURL(view.SessionID)
	out := contracts.CreateScreenSessionResponse{
		SessionID: view.SessionID, AgentID: view.AgentID, State: view.State,
		ExpiresAt: view.ExpiresAt, StreamURL: view.StreamURL,
		StreamTicket: s.ScreenAccess.ScreenStreamTicket(view.SessionID),
	}
	s.audit(r, a, "screen.session.created", "screen_session", view.SessionID, "ok",
		map[string]string{"agent_id": agentID, "quality": view.Quality,
			"allow_clipboard": fmt.Sprintf("%t", view.AllowClipboard)})
	writeJSON(w, http.StatusCreated, out)
}

// handleListScreenSessions lista todas las sesiones de pantalla.
// GET /api/v1/admin/screen/sessions  (screen:read)
func (s *Server) handleListScreenSessions(w http.ResponseWriter, r *http.Request) {
	r, _, ok := s.authorize(w, r, contracts.CapScreenRead)
	if !ok {
		return
	}
	items := s.ScreenAccess.ListScreenSessions()
	writeJSON(w, http.StatusOK, contracts.ScreenSessionList{Items: items})
}

// handleGetScreenSession devuelve el estado de una sesión de pantalla.
// GET /api/v1/admin/screen/sessions/{id}  (screen:read)
func (s *Server) handleGetScreenSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	r, _, ok := s.authorize(w, r, contracts.CapScreenRead)
	if !ok {
		return
	}
	view, err := s.ScreenAccess.GetScreenSession(sessionID)
	if err != nil {
		s.problem(w, r, http.StatusNotFound, "E_SESSION_NOT_FOUND", "Sesión de pantalla no encontrada")
		return
	}
	view.StreamURL = s.screenStreamURL(view.SessionID)
	writeJSON(w, http.StatusOK, view)
}

// handleCloseScreenSession cierra una sesión de pantalla.
// POST /api/v1/admin/screen/sessions/{id}/close  (screen:terminate)
func (s *Server) handleCloseScreenSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		s.problem(w, r, http.StatusNotFound, "E_NOT_FOUND", "Ruta no encontrada")
		return
	}
	r, a, ok := s.authorize(w, r, contracts.CapScreenTerminate)
	if !ok {
		return
	}
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if err := s.ScreenAccess.CloseScreenSession(r.Context(), sessionID, reason); err != nil {
		switch err {
		case remoteaccess.ErrSessionNotFound:
			s.problem(w, r, http.StatusNotFound, "E_SESSION_NOT_FOUND", "Sesión de pantalla no encontrada")
		case remoteaccess.ErrSessionClosed:
			s.problem(w, r, http.StatusConflict, "E_SESSION_CLOSED", "La sesión ya está cerrada")
		default:
			s.internalError(w, r, err)
		}
		return
	}
	s.audit(r, a, "screen.session.closed", "screen_session", sessionID, "ok",
		map[string]string{"reason": reason})
	w.WriteHeader(http.StatusNoContent)
}

// handleScreenStream es el WebSocket de datos entre el navegador (noVNC) y la
// sesión. GET /api/v1/admin/screen/sessions/{id}/stream  (screen:connect)
//
// Los WebSockets del navegador no pueden enviar cabeceras de autorización:
// se acepta un ticket impredecible entregado al crear la sesión
// (?ticket=…) como credencial alternativa a la autenticación estándar.
func (s *Server) handleScreenStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if ticket := r.URL.Query().Get("ticket"); ticket != "" {
		if !s.ScreenAccess.ValidScreenStreamTicket(sessionID, ticket) {
			s.problem(w, r, http.StatusUnauthorized, "E_AUTH_TOKEN", "Ticket de stream inválido o sesión inexistente")
			return
		}
	} else {
		var ok bool
		r, _, ok = s.authorize(w, r, contracts.CapScreenConnect)
		if !ok {
			return
		}
	}
	if _, err := s.ScreenAccess.GetScreenSession(sessionID); err != nil {
		s.problem(w, r, http.StatusNotFound, "E_SESSION_NOT_FOUND", "Sesión de pantalla no encontrada")
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.ScreenAccess.AttachScreenBrowser(r.Context(), sessionID, conn); err != nil {
		s.Logger.Debug("stream de pantalla rechazado", "session_id", sessionID, "error", err.Error())
		s.problem(w, r, http.StatusConflict, "E_STREAM_FAILED", err.Error())
		return
	}
}

// handleAgentScreenConnect es el canal persistente del agente para pantalla
// (mTLS + API key). GET /api/v1/agent/screen/connect  (auth de agente)
func (s *Server) handleAgentScreenConnect(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.requireAgent(w, r)
	if !ok {
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.ScreenAccess.RegisterAgent(r.Context(), agent.ID, conn)
}

// screenStreamURL construye la URL de WebSocket del stream de datos de pantalla.
func (s *Server) screenStreamURL(sessionID string) string {
	base := s.PublicURL
	path := "/api/v1/admin/screen/sessions/" + sessionID + "/stream"
	if base == "" {
		return path
	}
	scheme := "ws"
	switch strings.ToLower(strings.SplitN(base, "://", 2)[0]) {
	case "https":
		scheme = "wss"
	case "http":
		scheme = "ws"
	}
	host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	return scheme + "://" + host + path
}
