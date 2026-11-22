//go:build windows

// backend_windows.go conecta el helper de sesión con el protocolo de tiles:
// los tiles del helper (stdout) van al navegador; su entrada (WebSocket del
// servidor) se enmarca hacia el stdin del helper.
package remoteaccess

import (
	"sync"
	"time"
)

// helperBackend implements screenBackend sobre el helper de sesión.
type helperBackend struct {
	helper  *helperProcess
	events  chan []byte
	done    chan struct{}
	stop    chan struct{}
	termErr // motivo de terminación para el visor

	helperReasonMu sync.Mutex
	helperReason   string // motivo enviado por el helper vía msgHelperError
}

// helperError anota el motivo estructurado enviado por el helper (el más
// fiable: describe el fallo concreto, no el exit code).
func (b *helperBackend) helperError(text string) {
	b.helperReasonMu.Lock()
	if b.helperReason == "" {
		b.helperReason = text
	}
	b.helperReasonMu.Unlock()
}

// helperReasonIfAny devuelve el motivo estructurado ("" si no llegó).
func (b *helperBackend) helperReasonIfAny() string {
	b.helperReasonMu.Lock()
	defer b.helperReasonMu.Unlock()
	return b.helperReason
}

// newScreenBackend crea el backend Windows: lanza el helper de sesión.
func newScreenBackend() (screenBackend, error) {
	helperSpawnMu.Lock()
	defer helperSpawnMu.Unlock()
	h, err := spawnScreenHelper()
	if err != nil {
		return nil, err
	}
	log := screenBackendLogger()
	if log != nil {
		log.Info("pantalla: helper de sesión lanzado")
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
			// El helper terminó (o dejó de emitir). Prioridad del motivo: el
			// texto estructurado msgHelperError (describe el fallo real);
			// fallback al exit code. Espera acotada al código: el EOF del
			// pipe llega ANTES de que WaitForSingleObject lo publique.
			select {
			case <-b.stop:
			default:
				if reason := b.helperReasonIfAny(); reason != "" {
					b.failAnnotate(reason)
				} else {
					select {
					case <-b.helper.done:
					case <-time.After(3 * time.Second):
					}
					reason := b.helper.terminationReason()
					if log := screenBackendLogger(); log != nil {
						log.Warn("pantalla: helper terminó", "reason", reason)
					}
					b.failAnnotate(reason)
				}
			}
			return
		}
		if len(msg) > 0 && msg[0] == msgHelperError {
			reason := string(msg[1:])
			if log := screenBackendLogger(); log != nil {
				log.Warn("pantalla: helper reporta fallo", "reason", reason)
			}
			b.helperError(reason)
			continue // NUNCA reenviar al navegador: no es parte del protocolo
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
