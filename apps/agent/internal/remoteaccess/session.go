// Package remoteaccess implementa la gestión de sesiones PTY en el agente.
package remoteaccess

import (
	"io"
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

// PTYSession representa una sesión de terminal activa. Los campos de
// plataforma: unix usa PTY (maestro bidireccional) + Cmd; Windows usa
// In/Out (pipes del ConPTY), Resize/WaitExit/Terminate (callbacks) y Cmd nil.
type PTYSession struct {
	ID        string
	PTY       *os.File // unix: maestro de la PTY (read+write); windows: nil
	In        io.WriteCloser
	Out       io.ReadCloser
	Resize    func(cols, rows uint16) // cambio de dimensiones (plataforma)
	WaitExit  func() int              // windows: bloquea hasta la salida del shell
	Terminate func()                  // windows: mata el shell y cierra el ConPTY
	Shell     string
	Cols      uint16
	Rows      uint16
	Privilege string
	Cmd       *exec.Cmd // unix
}

// NewSession abre una nueva pseudoterminal con el shell indicado y la
// registra bajo el ID de sesión dado por el servidor.
func (m *SessionManager) NewSession(id, shell string, cols, rows uint16, privilege string) (*PTYSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ps, err := StartShell(shell, cols, rows)
	if err != nil {
		return nil, err
	}
	ps.ID = id
	ps.Shell = shell
	ps.Cols = cols
	ps.Rows = rows
	ps.Privilege = privilege
	m.sessions[id] = ps
	return ps, nil
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
	if s.Out != nil {
		_ = s.Out.Close() // desbloquea la bomba de salida (EOF)
	}
	if s.In != nil {
		_ = s.In.Close()
	}
	if s.Terminate != nil {
		s.Terminate() // windows: mata el shell y cierra el ConPTY
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
