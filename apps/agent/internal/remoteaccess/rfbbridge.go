// rfbbridge.go implementa el backend de pantalla basado en bridge VNC: se
// conecta como cliente RFB a un servidor VNC local (macOS «Compartir
// pantalla», o x11vnc bajo demanda en Linux), y convierte los updates RAW
// del framebuffer en tiles JPEG para el navegador. La entrada del operador
// viaja como eventos RFB (keysyms nativos).
package remoteaccess

import (
	"sync"
	"sync/atomic"
	"time"
)

// vncLocalAddr es el servidor VNC local del sistema (solo loopback).
const vncLocalAddr = "127.0.0.1:5900"

// rfbBridgeBackend une un rfbClient con el motor de tiles.
type rfbBridgeBackend struct {
	client    *rfbClient
	engine    *TileEngine
	events    chan []byte
	done      chan struct{}
	stop      chan struct{}
	interval  atomic.Int64 // ms; ritmo máximo entre updates
	closeOnce sync.Once
	termErr   // motivo de terminación para el visor
}

// newRFBBridgeBackend dializa el VNC local y arranca el puente.
func newRFBBridgeBackend(addr string) (*rfbBridgeBackend, error) {
	client, err := dialRFB(addr, 3*time.Second)
	if err != nil {
		return nil, err
	}
	b := &rfbBridgeBackend{
		client: client,
		engine: NewTileEngine(),
		events: make(chan []byte, 64),
		done:   make(chan struct{}),
		stop:   make(chan struct{}),
	}
	b.interval.Store(int64(defaultFrameEvery / time.Millisecond))
	go b.run()
	return b, nil
}

// Events devuelve el canal de mensajes para el navegador.
func (b *rfbBridgeBackend) Events() <-chan []byte { return b.events }

// Done se cierra cuando el puente termina.
func (b *rfbBridgeBackend) Done() <-chan struct{} { return b.done }

// run es el bucle peticiones-update → tiles. Event-driven: el servidor VNC
// solo responde cuando la pantalla cambia; entre updates esperamos el
// intervalo como ritmo máximo.
func (b *rfbBridgeBackend) run() {
	defer close(b.done)
	defer close(b.events)
	force := true
	for {
		select {
		case <-b.stop:
			return
		default:
		}
		if err := b.client.RequestUpdate(force); err != nil {
			b.failf("conexión VNC rota (RequestUpdate): %v", err)
			return
		}
		if err := b.client.ReadUpdate(); err != nil {
			b.failf("el servidor VNC cerró o envió algo inesperado: %v", err)
			return // conexión o protocolo roto: termina la sesión
		}
		for _, m := range b.engine.EncodeScreen(b.client.fb, b.client.w, b.client.h, force) {
			select {
			case b.events <- m:
			case <-b.stop:
				return
			}
		}
		if force {
			force = false
		}
		select {
		case <-b.stop:
			return
		case <-time.After(time.Duration(b.interval.Load()) * time.Millisecond):
		}
	}
}

// Handle aplica un mensaje del navegador.
func (b *rfbBridgeBackend) Handle(msg []byte) {
	if len(msg) == 0 {
		return
	}
	switch msg[0] {
	case msgMouse:
		if ev, ok := decodeMouse(msg[1:]); ok {
			_ = b.client.SendPointer(ev.Buttons, ev.X, ev.Y)
		}
	case msgKey:
		if ev, ok := decodeKey(msg[1:]); ok {
			_ = b.client.SendKey(ev.Down, ev.Keysym)
		}
	case msgQuality:
		if q, interval, ok := decodeQuality(msg[1:]); ok {
			b.engine.SetQuality(q)
			b.interval.Store(int64(interval / time.Millisecond))
		}
	case msgRefresh:
		b.engine.Reset()
	}
}

// Close detiene el puente y cierra la conexión VNC.
func (b *rfbBridgeBackend) Close() {
	b.closeOnce.Do(func() {
		close(b.stop)
		_ = b.client.Close()
	})
}
