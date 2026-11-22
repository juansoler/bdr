// tileloop.go define la interfaz de backend de pantalla y el bucle de
// captura compartido por las tres plataformas.
//
// Backends (selección por build tags en backend_*.go):
//   - Windows: helper de sesión nativo (captura GDI + SendInput) por stdio.
//   - Linux:   captura X11 nativa (XGetImage + XTEST), fallback x11vnc+bridge.
//   - darwin:  bridge cliente VNC contra el «Compartir pantalla» del sistema.
package remoteaccess

import (
	"time"
)

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
