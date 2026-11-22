//go:build linux

// x11backend_linux.go implementa la captura X11 nativa del agente:
//
//   - Captura: XGetImage (ZPixmap, 24/32 bpp) contra la ventana raíz — cubre
//     todas las pantallas del servidor X. Sin CGO (jezek/xgb habla el
//     protocolo X por socket Unix).
//   - Entrada: extensión XTEST (XTestFakeInput) — teclado por keycode
//     resuelto contra el keymap del servidor, ratón por coordenada absoluta.
//
// La autenticación X del agente-servicio se resuelve probando XAUTHORITY
// candidatas (root, usuarios de /home, GDM/LightDM).
package remoteaccess

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// xtestDeviceID es el identificador del dispositivo XTEST (XTEST_DEVICE_ID).
const xtestDeviceID = 0

// x11Backend es la sesión de pantalla X11 nativa.
type x11Backend struct {
	conn        *xgb.Conn
	root        xproto.Window
	w, h        int
	keymap      map[uint32]xproto.Keycode // keysym → primer keycode
	engine      *TileEngine
	events      chan []byte
	done        chan struct{}
	stop        chan struct{}
	prevButtons byte // último estado de botones conocido (para el diff XTEST)
	closeOnce   sync.Once
	termErr     // motivo de terminación para el visor
}

// newX11Backend conecta al servidor X y prepara la sesión.
func newX11Backend() (*x11Backend, error) {
	conn, root, w, h, err := connectX11()
	if err != nil {
		return nil, err
	}
	b := &x11Backend{
		conn:   conn,
		root:   root,
		w:      w,
		h:      h,
		engine: NewTileEngine(),
		events: make(chan []byte, 64),
		done:   make(chan struct{}),
		stop:   make(chan struct{}),
	}
	if b.keymap, err = loadKeymap(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("X11: no se pudo leer el keymap: %w", err)
	}
	go b.run()
	return b, nil
}

// connectX11 prueba DISPLAY/XAUTHORITY candidatos hasta conectar.
func connectX11() (*xgb.Conn, xproto.Window, int, int, error) {
	displays := []string{os.Getenv("DISPLAY"), ":0", ":1"}
	var lastErr error
	for _, display := range displays {
		if display == "" {
			continue
		}
		for _, xauth := range xauthorityCandidates() {
			_ = os.Setenv("DISPLAY", display)
			_ = os.Setenv("XAUTHORITY", xauth)
			conn, err := xgb.NewConnDisplay(display)
			if err != nil {
				lastErr = err
				continue
			}
			setup := xproto.Setup(conn)
			screen := setup.DefaultScreen(conn)
			return conn, screen.Root, int(screen.WidthInPixels), int(screen.HeightInPixels), nil
		}
	}
	if lastErr == nil {
		lastErr = errNoX11
	}
	return nil, 0, 0, 0, fmt.Errorf("%w: %v", errNoX11, lastErr)
}

// x11Source adapta la conexión X a TileSource.
type x11Source struct {
	b *x11Backend
}

// Frame captura la ventana raíz completa (BGRA).
func (s *x11Source) Frame() ([]byte, int, int, error) {
	reply, err := xproto.GetImage(s.b.conn, xproto.ImageFormatZPixmap,
		xproto.Drawable(s.b.root), 0, 0, uint16(s.b.w), uint16(s.b.h), 0xFFFFFFFF).Reply()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("X11 GetImage: %w", err)
	}
	// Profundidad inesperada (16 bpp…) → el buffer no coincide con BGRA:
	// mejor fallar claro que pintar basura o desbordar.
	if len(reply.Data) < s.b.w*s.b.h*4 {
		return nil, 0, 0, fmt.Errorf(
			"X11: framebuffer inesperado (%d bytes para %dx%d; ¿profundidad != 24/32?)",
			len(reply.Data), s.b.w, s.b.h)
	}
	return reply.Data, s.b.w, s.b.h, nil
}

// Close no posee la conexión (la cierra el backend).
func (s *x11Source) Close() error { return nil }

// run es el bucle de captura del backend.
func (b *x11Backend) run() {
	defer close(b.done)
	defer close(b.events)
	interval := defaultFrameEvery
	src := &x11Source{b: b}
	err := captureLoop(b.engine, src, func() time.Duration { return interval }, func(m []byte) error {
		select {
		case b.events <- m:
			return nil
		case <-b.stop:
			return errStopped
		}
	}, nil, b.stop)
	if err != nil && err != errStopped {
		// Fallo persistente de captura: el motivo llega al visor.
		b.failf("la captura X11 falló de forma persistente: %v", err)
	}
}

// errStopped marca la parada del bucle de emisión.
var errStopped = fmt.Errorf("backend detenido")

// Events devuelve el canal de mensajes para el navegador.
func (b *x11Backend) Events() <-chan []byte { return b.events }

// Done se cierra cuando el backend termina.
func (b *x11Backend) Done() <-chan struct{} { return b.done }

// Handle aplica un mensaje del navegador (entrada XTEST).
func (b *x11Backend) Handle(msg []byte) {
	if len(msg) == 0 {
		return
	}
	switch msg[0] {
	case msgMouse:
		if ev, ok := decodeMouse(msg[1:]); ok {
			b.injectMouse(ev)
		}
	case msgKey:
		if ev, ok := decodeKey(msg[1:]); ok {
			b.injectKey(ev)
		}
	case msgQuality:
		// El ritmo del backend X11 es de sondeo: sí se puede ajustar aquí
		// vía engine… el intervalo se fija al arrancar; cambio de calidad sí.
		if q, _, ok := decodeQuality(msg[1:]); ok {
			b.engine.SetQuality(q)
		}
	case msgRefresh:
		b.engine.Reset()
	}
}

// injectMouse inyecta movimiento y botones con XTEST. El visor envía el
// estado COMPLETO de botones (bitmask): se compara con el anterior para
// generar ButtonPress/ButtonRelease por botón (detail 1=izq, 2=centro, 3=der).
func (b *x11Backend) injectMouse(ev MouseEvent) {
	// Movimiento (también en mensajes de botón: arrastra el cursor).
	_ = xtest.FakeInputChecked(b.conn, xproto.MotionNotify, 0 /*absoluto*/, 0,
		b.root, int16(ev.X), int16(ev.Y), xtestDeviceID)

	prev := b.prevButtons
	for bit := byte(0); bit < 3; bit++ {
		mask := byte(1 << bit)
		if ev.Buttons&mask == prev&mask {
			continue
		}
		detail := bit + 1
		typ := byte(xproto.ButtonPress)
		if ev.Buttons&mask == 0 {
			typ = xproto.ButtonRelease
		}
		_ = xtest.FakeInputChecked(b.conn, typ, detail, 0,
			b.root, int16(ev.X), int16(ev.Y), xtestDeviceID)
	}
	b.prevButtons = ev.Buttons
}

// injectKey inyecta teclas con XTEST vía keycode del keymap.
func (b *x11Backend) injectKey(ev KeyEvent) {
	keycode, ok := b.keymap[ev.Keysym]
	if !ok {
		return
	}
	typ := byte(xproto.KeyPress)
	if !ev.Down {
		typ = xproto.KeyRelease
	}
	_ = xtest.FakeInputChecked(b.conn, typ, byte(keycode), 0, b.root, 0, 0, xtestDeviceID)
}

// Close cierra la conexión X.
func (b *x11Backend) Close() {
	b.closeOnce.Do(func() {
		close(b.stop)
		b.conn.Close()
	})
}

// loadKeymap construye el mapa keysym → keycode desde GetKeyboardMapping.
func loadKeymap(conn *xgb.Conn) (map[uint32]xproto.Keycode, error) {
	setup := xproto.Setup(conn)
	first := setup.MinKeycode
	count := setup.MaxKeycode - setup.MinKeycode + 1
	reply, err := xproto.GetKeyboardMapping(conn, first, byte(count)).Reply()
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]xproto.Keycode)
	for i := 0; i < len(reply.Keysyms); i += int(reply.KeysymsPerKeycode) {
		keycode := first + xproto.Keycode(i/int(reply.KeysymsPerKeycode))
		// El primer keysym (sin modificadores) gana si no estaba mapeado.
		for j := 0; j < int(reply.KeysymsPerKeycode) && j < 2; j++ {
			ks := uint32(reply.Keysyms[i+j])
			if ks == 0 {
				continue
			}
			if _, exists := out[ks]; !exists {
				out[ks] = keycode
			}
		}
	}
	return out, nil
}
