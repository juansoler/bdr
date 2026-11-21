// Package remoteaccess implementa la gestión de sesiones PTY en el agente.
package remoteaccess

import (
	"os"
	"os/exec"
	"sync"
)

// SessionManager coordina todas las sesiones de terminal activas del agente.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*PTYSession
}

// NewSessionManager crea un nuevo gestor de sesiones.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*PTYSession),
	}
}

// PTYSession representa una sesión de terminal activa.
type PTYSession struct {
	ID        string
	PTY       *os.File
	Shell     string
	Cols      uint16
	Rows      uint16
	Privilege string
	Cmd       *exec.Cmd
}

// NewSession abre una nueva pseudoterminal con el shell indicado y la
// registra bajo el ID de sesión dado por el servidor.
func (m *SessionManager) NewSession(id, shell string, cols, rows uint16, privilege string) (*PTYSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	master, cmd, err := StartShell(shell, cols, rows)
	if err != nil {
		return nil, err
	}

	s := &PTYSession{
		ID:        id,
		PTY:       master,
		Shell:     shell,
		Cols:      cols,
		Rows:      rows,
		Privilege: privilege,
		Cmd:       cmd,
	}
	m.sessions[id] = s
	return s, nil
}

// GetSession devuelve una sesión por ID.
func (m *SessionManager) GetSession(id string) (*PTYSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// CloseSession cierra la sesión y elimina su registro.
func (m *SessionManager) CloseSession(id string, reason string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !ok {
		return
	}
	_ = reason
	if s.PTY != nil {
		_ = s.PTY.Close() // desbloquea la bomba de salida (EOF)
	}
	if s.Cmd != nil && s.Cmd.Process != nil {
		_ = s.Cmd.Process.Kill()
	}
}

// CloseAll cierra todas las sesiones (pérdida del canal con el servidor).
func (m *SessionManager) CloseAll(reason string) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.CloseSession(id, reason)
	}
}