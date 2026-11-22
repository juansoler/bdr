// Tests del motor de tiles: detección de cambios por CRC, refresh completo,
// reset al cambiar de tamaño y framing de streams.
package remoteaccess

import (
	"bytes"
	"testing"
)

// frameSintetico genera un framebuffer BGRA w×h con el tile (tx,ty) pintado
// del color indicado (o negro si color==0).
func frameSintetico(w, h int, coloredTile int, color byte) []byte {
	pix := make([]byte, w*h*4)
	tilesPerRow := (w + screenTileSize - 1) / screenTileSize
	row := coloredTile / tilesPerRow
	col := coloredTile % tilesPerRow
	x0 := col * screenTileSize
	y0 := row * screenTileSize
	for y := y0; y < min2(y0+screenTileSize, h); y++ {
		for x := x0; x < min2(x0+screenTileSize, w); x++ {
			pix[(y*w+x)*4] = color
			pix[(y*w+x)*4+1] = color
			pix[(y*w+x)*4+2] = color
		}
	}
	return pix
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestTileEnginePrimerFrameCompleto(t *testing.T) {
	e := NewTileEngine()
	pix := frameSintetico(128, 128, 0, 0) // todo negro
	msgs := e.EncodeScreen(pix, 128, 128, false)
	// 4 tiles (2×2) + screen_info.
	if len(msgs) != 1+4 {
		t.Fatalf("primer frame: esperados 5 mensajes (info+4 tiles), obtenidos %d", len(msgs))
	}
	if msgs[0][0] != msgScreenInfo {
		t.Fatalf("primer mensaje no es screen_info: %d", msgs[0][0])
	}
}

func TestTileEngineSoloTilesCambiados(t *testing.T) {
	e := NewTileEngine()
	pix := frameSintetico(128, 128, 0, 0)
	if msgs := e.EncodeScreen(pix, 128, 128, false); len(msgs) != 5 {
		t.Fatalf("setup inicial: %d mensajes", len(msgs))
	}
	// Mismo frame: nada que enviar.
	if msgs := e.EncodeScreen(pix, 128, 128, false); msgs != nil {
		t.Fatalf("frame idéntico generó %d mensajes (esperados 0)", len(msgs))
	}
	// Cambia UN tile (tile 3 = fila 1, col 1): info + 1 tile.
	pix2 := frameSintetico(128, 128, 3, 200)
	msgs := e.EncodeScreen(pix2, 128, 128, false)
	if len(msgs) != 2 {
		t.Fatalf("un tile cambiado: esperados 2 mensajes (info+tile), obtenidos %d", len(msgs))
	}
	if msgs[1][0] != msgTile {
		t.Fatalf("segundo mensaje no es tile: %d", msgs[1][0])
	}
	// Tile del cambio en (64,64) tamaño 64×64.
	if x := int(msgs[1][1])<<8 | int(msgs[1][2]); x != 64 {
		t.Fatalf("tile x = %d (esperado 64)", x)
	}
	if y := int(msgs[1][3])<<8 | int(msgs[1][4]); y != 64 {
		t.Fatalf("tile y = %d (esperado 64)", y)
	}
}

func TestTileEngineRefreshResiendeTodo(t *testing.T) {
	e := NewTileEngine()
	pix := frameSintetico(128, 128, 0, 0)
	e.EncodeScreen(pix, 128, 128, false)
	if msgs := e.EncodeScreen(pix, 128, 128, false); msgs != nil {
		t.Fatalf("frame idéntico generó mensajes")
	}
	// force=true: aunque no cambie nada, reenvía los 4 tiles.
	msgs := e.EncodeScreen(pix, 128, 128, true)
	if len(msgs) != 5 {
		t.Fatalf("refresh: esperados 5 mensajes, obtenidos %d", len(msgs))
	}
}

func TestTileEngineResizeResetea(t *testing.T) {
	e := NewTileEngine()
	pix := frameSintetico(128, 128, 0, 0)
	e.EncodeScreen(pix, 128, 128, false)
	// Cambia a 128×64: los CRC se invalidan y se reenvía la grilla nueva (2×1).
	pix2 := frameSintetico(128, 64, 0, 0)
	msgs := e.EncodeScreen(pix2, 128, 64, false)
	if len(msgs) != 1+2 {
		t.Fatalf("tras resize: esperados 3 mensajes (info+2 tiles), obtenidos %d", len(msgs))
	}
	// Ancho/hora del screen_info.
	w := int(msgs[0][1])<<8 | int(msgs[0][2])
	h := int(msgs[0][3])<<8 | int(msgs[0][4])
	if w != 128 || h != 64 {
		t.Fatalf("screen_info %dx%d (esperado 128x64)", w, h)
	}
}

func TestTileEngineJPEGValido(t *testing.T) {
	e := NewTileEngine()
	pix := frameSintetico(64, 64, 0, 128)
	msgs := e.EncodeScreen(pix, 64, 64, false)
	if len(msgs) != 2 {
		t.Fatalf("esperados 2 mensajes, obtenidos %d", len(msgs))
	}
	// El payload del tile debe ser un JPEG (magic FFD8…FFD9).
	jd := msgs[1][9:]
	if len(jd) < 4 || jd[0] != 0xFF || jd[1] != 0xD8 || jd[len(jd)-2] != 0xFF || jd[len(jd)-1] != 0xD9 {
		t.Fatalf("payload no parece JPEG (len=%d)", len(jd))
	}
}

func TestStreamFraming(t *testing.T) {
	msg := []byte{msgScreenInfo, 0x05, 0x00, 0x10, 0x02}
	frame := frameStream(msg)
	if !bytes.HasPrefix(frame, []byte{0x00, byte(len(msg))}) {
		t.Fatalf("prefijo de longitud incorrecto: %v", frame[:2])
	}
	got, err := nextStreamFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("nextStreamFrame: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("mensaje desestado en el framing: %v", got)
	}
}
