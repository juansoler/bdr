// tileengine.go implementa el protocolo de tiles compartido por todos los
// backends de pantalla del agente (estilo MeshCentral):
//
//   - El escritorio se divide en una grilla de tiles de 64×64 px.
//   - Cada frame se compara con el anterior por CRC de tile: solo los tiles
//     cambiados se codifican en JPEG y se envían.
//   - El visor (canvas del panel) recibe screen_info + tiles y pinta.
//
// Framing: sobre WebSocket cada mensaje ES un frame (primer byte = tipo);
// sobre flujos (stdio con el helper de Windows) se prefixa [u16 BE longitud].
package remoteaccess

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"io"
	"time"
)

// Tipos de mensaje del protocolo de tiles (primer byte del frame).
const (
	// Agente → navegador.
	msgScreenInfo byte = 0x01 // [u16 w][u16 h]
	msgTile       byte = 0x02 // [u16 x][u16 y][u16 w][u16 h][JPEG…]
	// Helper → agente (solo en el flujo interno del helper de sesión
	// Windows; el backend lo captura como motivo de terminación y NUNCA lo
	// reenvía al navegador).
	msgHelperError byte = 0x03 // [texto UTF-8 del fallo]
	// Navegador → agente.
	msgMouse   byte = 0x10 // [u8 acción][u8 botones][u16 x][u16 y]
	msgKey     byte = 0x11 // [u8 acción][u32 keysym]
	msgQuality byte = 0x12 // [u8 calidad JPEG][u16 intervalo_ms]
	msgRefresh byte = 0x13 // sin payload

	// Acciones de ratón.
	mouseMove byte = 0
	mouseDown byte = 1
	mouseUp   byte = 2
	// Acciones de teclado.
	keyDown byte = 0
	keyUp   byte = 1
)

// screenTileSize es el lado del tile en píxeles (MeshCentral usa 32; 64
// reduce el número de mensajes JPEG sin perder granularidad).
const screenTileSize = 64

// Calidad y ritmo por defecto; el visor puede ajustarlos (msgQuality).
const (
	defaultJPEGQuality = 60
	defaultFrameEvery  = 120 * time.Millisecond
	minFrameEvery      = 40 * time.Millisecond
	maxFrameEvery      = 1000 * time.Millisecond
)

// TileSource produce frames del escritorio: píxeles BGRA (byte alto = X),
// fila a fila sin padding, framebuffer completo (todas las pantallas).
type TileSource interface {
	// Frame devuelve el framebuffer actual. Puede reutilizar el slice entre
	// llamadas (el motor no lo conserva tras codificar).
	Frame() (pix []byte, width, height int, err error)
	// Close libera recursos de captura.
	Close() error
}

// TileInjector inyecta entrada del operador en el equipo (nativa o vía
// bridge). Recibe el payload sin el byte de tipo.
type TileInjector interface {
	Mouse(action, buttons byte, x, y uint16) error
	Key(down bool, keysym uint32) error
}

// TileEngine mantiene la grilla de CRCs y convierte frames en mensajes.
// No es thread-safe: úsalo desde la goroutine del bucle de captura.
type TileEngine struct {
	prevCRC []uint32 // fila a fila; len = tilesX * tilesY
	prevW   int
	prevH   int
	quality int
}

// NewTileEngine crea el motor con los valores por defecto.
func NewTileEngine() *TileEngine {
	return &TileEngine{quality: defaultJPEGQuality}
}

// SetQuality ajusta la calidad JPEG (10-95).
func (e *TileEngine) SetQuality(q int) {
	if q < 10 {
		q = 10
	}
	if q > 95 {
		q = 95
	}
	e.quality = q
}

// Reset olvida el frame anterior (todo se reenvía).
func (e *TileEngine) Reset() {
	e.prevCRC = nil
}

// screenInfoMsg codifica el tamaño del framebuffer.
func screenInfoMsg(w, h int) []byte {
	m := make([]byte, 5)
	m[0] = msgScreenInfo
	binary.BigEndian.PutUint16(m[1:3], uint16(w))
	binary.BigEndian.PutUint16(m[3:5], uint16(h))
	return m
}

// tileMsg codifica un tile JPEG en (x,y) de tamaño w×h.
func tileMsg(x, y, w, h int, jpegData []byte) []byte {
	m := make([]byte, 9+len(jpegData))
	m[0] = msgTile
	binary.BigEndian.PutUint16(m[1:3], uint16(x))
	binary.BigEndian.PutUint16(m[3:5], uint16(y))
	binary.BigEndian.PutUint16(m[5:7], uint16(w))
	binary.BigEndian.PutUint16(m[7:9], uint16(h))
	copy(m[9:], jpegData)
	return m
}

// ChangedTiles compara pix (BGRA, w×h) con el frame anterior y devuelve los
// rectángulos de los tiles cambiados. Si el tamaño cambió, resetea la grilla.
func (e *TileEngine) ChangedTiles(pix []byte, w, h int) []image.Rectangle {
	tilesX := (w + screenTileSize - 1) / screenTileSize
	tilesY := (h + screenTileSize - 1) / screenTileSize
	if e.prevW != w || e.prevH != h || len(e.prevCRC) != tilesX*tilesY {
		e.prevCRC = make([]uint32, tilesX*tilesY)
		for i := range e.prevCRC {
			e.prevCRC[i] = 0xFFFFFFFF // fuerza el primer envío completo
		}
		e.prevW, e.prevH = w, h
	}

	var changed []image.Rectangle
	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			x0 := tx * screenTileSize
			y0 := ty * screenTileSize
			tw := min(screenTileSize, w-x0)
			th := min(screenTileSize, h-y0)
			crc := tileCRC(pix, w, x0, y0, tw, th)
			idx := ty*tilesX + tx
			if crc == e.prevCRC[idx] {
				continue
			}
			e.prevCRC[idx] = crc
			changed = append(changed, image.Rect(x0, y0, x0+tw, y0+th))
		}
	}
	return changed
}

// tileCRC calcula el CRC32 del rectángulo del tile sin copiar memoria
// (crc32.Update es incremental, fila a fila).
func tileCRC(pix []byte, fw, x, y, w, h int) uint32 {
	rowLen := fw * 4
	var crc uint32
	for yy := 0; yy < h; yy++ {
		start := (y+yy)*rowLen + x*4
		crc = crc32.Update(crc, crc32.IEEETable, pix[start:start+w*4])
	}
	return crc
}

// encodeTileJPEG recorta el tile (x,y,w,h) del framebuffer BGRA y lo codifica
// en JPEG con la calidad del motor.
func (e *TileEngine) encodeTileJPEG(pix []byte, fw, x, y, w, h int) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rowLen := fw * 4
	for yy := 0; yy < h; yy++ {
		src := pix[(y+yy)*rowLen+x*4:]
		dst := img.Pix[yy*img.Stride:]
		for xx := 0; xx < w; xx++ {
			// BGRA → RGBA (A de relleno: JPEG ignora el alfa).
			dst[xx*4+0] = src[xx*4+2]
			dst[xx*4+1] = src[xx*4+1]
			dst[xx*4+2] = src[xx*4+0]
			dst[xx*4+3] = 0xFF
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: e.quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EncodeScreen produce los mensajes de un frame: screen_info + los tiles
// cambiados. Con force (refresh del visor o primer frame) reenvía todo.
func (e *TileEngine) EncodeScreen(pix []byte, w, h int, force bool) [][]byte {
	if force {
		e.Reset()
	}
	changed := e.ChangedTiles(pix, w, h)
	if len(changed) == 0 {
		return nil // nada nuevo: sin mensajes
	}
	msgs := make([][]byte, 0, 1+len(changed))
	msgs = append(msgs, screenInfoMsg(w, h))
	for _, r := range changed {
		jd, err := e.encodeTileJPEG(pix, w, r.Min.X, r.Min.Y, r.Dx(), r.Dy())
		if err != nil {
			continue
		}
		msgs = append(msgs, tileMsg(r.Min.X, r.Min.Y, r.Dx(), r.Dy(), jd))
	}
	return msgs
}

// --- Decodificación de entrada del visor -------------------------------------

// MouseEvent decodificado del mensaje msgMouse.
type MouseEvent struct {
	Action  byte
	Buttons byte // bit0 izq, bit1 centro, bit2 der
	X, Y    uint16
}

func decodeMouse(payload []byte) (MouseEvent, bool) {
	if len(payload) != 6 {
		return MouseEvent{}, false
	}
	return MouseEvent{
		Action:  payload[0],
		Buttons: payload[1],
		X:       binary.BigEndian.Uint16(payload[2:4]),
		Y:       binary.BigEndian.Uint16(payload[4:6]),
	}, true
}

// KeyEvent decodificado del mensaje msgKey.
type KeyEvent struct {
	Down   bool
	Keysym uint32
}

func decodeKey(payload []byte) (KeyEvent, bool) {
	if len(payload) != 5 {
		return KeyEvent{}, false
	}
	return KeyEvent{Down: payload[0] == keyDown, Keysym: binary.BigEndian.Uint32(payload[1:5])}, true
}

// decodeQuality decodifica msgQuality: calidad JPEG + intervalo de frame.
func decodeQuality(payload []byte) (quality int, interval time.Duration, ok bool) {
	if len(payload) != 3 {
		return 0, 0, false
	}
	quality = int(payload[0])
	interval = time.Duration(binary.BigEndian.Uint16(payload[1:3])) * time.Millisecond
	if interval < minFrameEvery {
		interval = minFrameEvery
	}
	if interval > maxFrameEvery {
		interval = maxFrameEvery
	}
	return quality, interval, true
}

// mouseMsg codifica el mensaje de ratón que el visor envía.
func mouseMsg(action, buttons byte, x, y uint16) []byte {
	m := make([]byte, 7)
	m[0] = msgMouse
	m[1] = action
	m[2] = buttons
	binary.BigEndian.PutUint16(m[3:5], x)
	binary.BigEndian.PutUint16(m[5:7], y)
	return m
}

// --- Framing sobre flujos (stdio del helper de Windows) ----------------------

// frameStream prefixa un mensaje para enviarlo por un flujo binario.
func frameStream(msg []byte) []byte {
	f := make([]byte, 2+len(msg))
	binary.BigEndian.PutUint16(f[:2], uint16(len(msg)))
	copy(f[2:], msg)
	return f
}

// nextStreamFrame lee el siguiente mensaje de un flujo binario.
func nextStreamFrame(r io.Reader) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	if n == 0 || n > 1<<20 {
		return nil, errors.New("frame de helper malformado")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
