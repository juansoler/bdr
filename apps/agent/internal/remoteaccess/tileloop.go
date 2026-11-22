// tileloop.go define la interfaz de backend de pantalla y el bucle de
// captura compartido por las tres plataformas.
//
// Backends (selección por build tags en backend_*.go):
//   - Windows: helper de sesión nativo (captura GDI + SendInput) por stdio.
//   - Linux:   captura X11 nativa (XGetImage + XTEST), fallback x11vnc+bridge.
//   - darwin:  bridge cliente VNC contra el «Compartir pantalla» del sistema.
package remoteaccess

import (
	"fmt"
	"sync"
	"time"

	"github.com/juansoler/bdr/packages/logging"
)

// helperBackendLog es el logger del agente para el ciclo de los backends de
// pantalla (spawn/terminación del helper). Se inyecta desde el runtime; nil
// = sin logs.
var helperBackendLog *logging.Logger

func screenBackendLogger() *logging.Logger { return helperBackendLog }

func setScreenBackendLogger(l *logging.Logger) { helperBackendLog = l }

// termErr registra el motivo de terminación de un backend para que el
// runtime lo informe al servidor (y llegue legible al visor del panel).
type termErr struct {
	mu     sync.Mutex
	reason string
}

// failAnnotate registra el motivo (solo el primero: el que causó la salida).
func (t *termErr) failAnnotate(reason string) {
	if reason == "" {
		return
	}
	t.mu.Lock()
	if t.reason == "" {
		t.reason = reason
	}
	t.mu.Unlock()
}

// failf registra el motivo formateado.
func (t *termErr) failf(format string, args ...any) {
	t.failAnnotate(fmt.Sprintf(format, args...))
}

// Err devuelve el motivo registrado ("" si fue un cierre ordenado).
func (t *termErr) Err() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.reason
}

// screenBackend es una sesión de pantalla activa: produce eventos para el
// navegador y consume su entrada.
type screenBackend interface {
	// Events devuelve el canal de mensajes para el navegador (tiles, info).
	// Se cierra al terminar el backend.
	Events() <-chan []byte
	// Handle aplica un mensaje del navegador (mouse/key/quality/refresh).
	Handle(msg []byte)
	// Done se cierra cuando el backend termina (fallo de captura o cierre).
	Done() <-chan struct{}
	// Err devuelve el motivo de la terminación ("" si fue ordenada o
	// desconocida). Consultar tras Done.
	Err() string
	// Close detiene el backend y libera recursos.
	Close()
}

// captureLoop es el bucle de captura compartido: captura frames, codifica
// tiles con el motor y los emite. Termina cuando stop se cierra o la captura
// falla de forma persistente (p. ej. sesión gráfica cerrada). El intervalo se
// lee en cada vuelta (el visor puede cambiarlo en caliente con msgQuality).
func captureLoop(engine *TileEngine, source TileSource, intervalFn func() time.Duration, emit func([]byte) error, stop <-chan struct{}) error {
	const maxConsecutiveErrors = 30
	consecutive := 0
	force := true
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		pix, w, h, err := source.Frame()
		if err != nil {
			consecutive++
			if consecutive >= maxConsecutiveErrors {
				return err
			}
			select {
			case <-stop:
				return nil
			case <-time.After(intervalFn()):
			}
			continue
		}
		consecutive = 0
		for _, m := range engine.EncodeScreen(pix, w, h, force) {
			if emitErr := emit(m); emitErr != nil {
				return emitErr
			}
		}
		force = false
		select {
		case <-stop:
			return nil
		case <-time.After(intervalFn()):
		}
	}
}
