package store

import (
	"context"
	"sync"
	"time"
)

// persister escribe el estado serializado. Se llama con el cerrojo tomado.
type persister func(*State) error

// Memory mantiene el estado en RAM. Con un persister (File) se vuelve duradero.
type Memory struct {
	mu      sync.RWMutex
	state   *State
	persist persister
	dirty   bool
	audit   *AuditLog
	closed  chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
}

// NewMemory crea un almacenamiento vacío y no persistente (tests y desarrollo).
func NewMemory() *Memory {
	return newMemory(NewState(time.Now().UTC()), nil, NewMemoryAudit())
}

func newMemory(st *State, p persister, audit *AuditLog) *Memory {
	st.normalize()
	m := &Memory{state: st, persist: p, audit: audit, closed: make(chan struct{})}
	if p != nil {
		m.wg.Add(1)
		go m.flusher()
	}
	return m
}

func (m *Memory) flusher() {
	defer m.wg.Done()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.closed:
			return
		case <-t.C:
			_ = m.Flush()
		}
	}
}

// Ping siempre responde si el proceso está vivo.
func (m *Memory) Ping(context.Context) error { return nil }

// View ejecuta fn en modo lectura.
func (m *Memory) View(_ context.Context, fn func(*State) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fn(m.state)
}

// Update ejecuta fn en exclusiva y persiste.
func (m *Memory) Update(_ context.Context, fn func(*State) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := fn(m.state); err != nil {
		return err
	}
	if m.persist == nil {
		return nil
	}
	if err := m.persist(m.state); err != nil {
		m.dirty = true
		return err
	}
	m.dirty = false
	return nil
}

// UpdateLazy ejecuta fn y marca el estado como pendiente de escribir.
func (m *Memory) UpdateLazy(_ context.Context, fn func(*State) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := fn(m.state); err != nil {
		return err
	}
	m.dirty = m.persist != nil
	return nil
}

// Flush escribe los cambios diferidos.
func (m *Memory) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.dirty || m.persist == nil {
		return nil
	}
	if err := m.persist(m.state); err != nil {
		return err
	}
	m.dirty = false
	return nil
}

// Audit devuelve el registro de auditoría.
func (m *Memory) Audit() *AuditLog { return m.audit }

// Close detiene el volcado periódico y escribe lo pendiente.
func (m *Memory) Close() error {
	m.once.Do(func() { close(m.closed) })
	m.wg.Wait()
	err := m.Flush()
	if cerr := m.audit.Close(); err == nil {
		err = cerr
	}
	return err
}
