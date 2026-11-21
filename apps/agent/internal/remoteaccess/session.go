// Package remoteaccess implementa la gestión de sesiones en el agente.
package remoteaccess

import (
	"os"
	"sync"
)

// SessionManager coordina todas las sesiones activas del agente.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*PTYSession
	closed   map[string]bool
}

// NewSessionManager crea un nuevo manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*PTYSession),
		closed:   make(map[string]bool),
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
	Cmd       *os.Process
}

// NewSession crea una nueva sesión PTY y la registra.
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
		Cmd:       cmd.Process,
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

// CloseSession cierra la sesión y elimina de la lista.
func (m *SessionManager) CloseSession(id string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[id]; ok {
		s.PTY.Close()
		if s.Cmd != nil {
			s.Cmd.Kill()
		}
		delete(m.sessions, id)
		m.closed[id] = true
	}
}