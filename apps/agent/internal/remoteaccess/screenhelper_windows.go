//go:build windows

// screenhelper_windows.go implementa el modo `bdr-agent screenhelper`: un
// proceso lanzado por el servicio DENTRO de la sesión interactiva del usuario
// (WinSta0\Default) que captura el escritorio con GDI, inyecta entrada con
// SendInput y produce tiles JPEG por stdout.
//
// El servicio (sesión 0) no puede capturar ni inyectar en el escritorio del
// usuario: este helper es el mismo patrón que MeshCentral resuelve con su
// modo `--slave`, y el que usan TeamViewer/AnyDesk.
package remoteaccess

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// RunScreenHelper ejecuta el helper: lee mensajes del visor por stdin,
// emite tiles por stdout, hasta que el canal se cierre o la captura falle.
// Lo llama cmd/bdr-agent con `screenhelper`; no está pensado para uso manual.
func RunScreenHelper(stdin io.Reader, stdout io.Writer) error {
	engine := NewTileEngine()
	source, err := newGDICapture()
	if err != nil {
		return fmt.Errorf("helper: captura GDI no disponible: %w", err)
	}
	defer source.Close()

	inj := newSendInputInjector()
	var intervalMs atomic.Int64
	intervalMs.Store(int64(defaultFrameEvery / time.Millisecond))

	// Entrada del visor: stdin (mensajes enmarcados). Al cerrarse el canal,
	// se para el bucle de captura (sin visor no hay sesión).
	stop := make(chan struct{})
	var stopOnce sync.Once
	stopNow := func() { stopOnce.Do(func() { close(stop) }) }
	go func() {
		for {
			msg, err := nextStreamFrame(stdin)
			if err != nil {
				stopNow()
				return
			}
			if len(msg) == 0 {
				continue
			}
			switch msg[0] {
			case msgMouse:
				if ev, ok := decodeMouse(msg[1:]); ok {
					_ = inj.Mouse(ev.Action, ev.Buttons, ev.X, ev.Y)
				}
			case msgKey:
				if ev, ok := decodeKey(msg[1:]); ok {
					_ = inj.Key(ev.Down, ev.Keysym)
				}
			case msgQuality:
				if q, interval, ok := decodeQuality(msg[1:]); ok {
					engine.SetQuality(q)
					intervalMs.Store(int64(interval / time.Millisecond))
				}
			case msgRefresh:
				engine.Reset()
			}
		}
	}()

	return captureLoop(engine, source, func() time.Duration {
		return time.Duration(intervalMs.Load()) * time.Millisecond
	}, func(m []byte) error {
		_, werr := stdout.Write(frameStream(m))
		return werr
	}, stop)
}
