//go:build windows

// backend_windows.go conecta el helper de sesión con el protocolo de tiles:
// los tiles del helper (stdout) van al navegador; su entrada (WebSocket del
// servidor) se enmarca hacia el stdin del helper.
package remoteaccess

// helperBackend implementa screenBackend sobre el helper de sesión.
type helperBackend struct {
	helper  *helperProcess
	events  chan []byte
	done    chan struct{}
	stop    chan struct{}
	termErr // motivo de terminación para el visor
}

// newScreenBackend crea el backend Windows: lanza el helper de sesión.
func newScreenBackend() (screenBackend, error) {
	helperSpawnMu.Lock()
	defer helperSpawnMu.Unlock()
	h, err := spawnScreenHelper()
	if err != nil {
		return nil, err
	}
	b := &helperBackend{
		helper: h,
		events: make(chan []byte, 64),
		done:   make(chan struct{}),
		stop:   make(chan struct{}),
	}
	go b.readLoop()
	return b, nil
}

// readLoop bombea stdout del helper (tiles enmarcados) al canal de eventos.
func (b *helperBackend) readLoop() {
	defer close(b.done)
	defer close(b.events)
	for {
		msg, err := nextStreamFrame(b.helper.stdout)
		if err != nil {
			// El helper terminó: distingue fallo del propio helper (exit
			// code) de un cierre ordenado (stop nuestro).
			select {
			case <-b.stop:
			default:
				b.failAnnotate(b.helper.terminationReason())
			}
			return
		}
		select {
		case b.events <- msg:
		case <-b.stop:
			return
		}
	}
}

// Events devuelve el canal de mensajes para el navegador.
func (b *helperBackend) Events() <-chan []byte { return b.events }

// Done se cierra cuando el helper termina.
func (b *helperBackend) Done() <-chan struct{} { return b.done }

// Handle enmarca el mensaje del navegador hacia el stdin del helper.
func (b *helperBackend) Handle(msg []byte) {
	if err := b.helper.writeFrame(msg); err != nil {
		b.failf("no se pudo escribir al helper: %v", err)
	}
}

// writeFrame escribe un mensaje enmarcado al stdin del helper.
func (h *helperProcess) writeFrame(msg []byte) error {
	if h.stdin == nil {
		return nil
	}
	_, err := h.stdin.Write(frameStream(msg))
	return err
}

// Close mata el helper de sesión.
func (b *helperBackend) Close() {
	close(b.stop)
	b.helper.kill()
}
