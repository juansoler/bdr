//go:build windows

// input_windows.go inyecta ratón y teclado con SendInput (sin CGO).
//
// Teclas: dos vías, como en MeshCentral (KEY vs KEY_UNICODE):
//   - Teclas "especiales" (flechas, F1-12, modificadores…) → tabla
//     keysym→VK.
//   - Caracteres imprimibles → KEYEVENTF_UNICODE (independiente del layout).
//
// La inyección DEBE ejecutarse en el mismo hilo bloqueado que la captura
// (los escritorios y el estado de entrada son por hilo): el helper pasa
// los eventos del visor al bucle de captura, que los drena antes de cada
// frame — mismo patrón que el mainloop de MeshCentral.
package remoteaccess

import (
	"encoding/binary"
	"fmt"
	"sync"
	"unsafe"
)

// procSendInput vive en user32 (declarado junto al resto de procs GDI en
// capture_windows.go); este fichero solo lo usa.

// Flags de MOUSEINPUT.
const (
	mouseeventfMove       = 0x0001
	mouseeventfLeftDown   = 0x0002
	mouseeventfLeftUp     = 0x0004
	mouseeventfRightDown  = 0x0008
	mouseeventfRightUp    = 0x0010
	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040
	mouseeventfAbsolute   = 0x8000
)

// Flags de KEYBDINPUT.
const (
	keyeventfKeyup   = 0x0002
	keyeventfUnicode = 0x0004
)

// Tipos de INPUT.
const (
	inputMouse    = 0
	inputKeyboard = 1
)

// Layout del struct INPUT en 64 bits (el agente Windows se distribuye solo
// para amd64/arm64): type@0, unión@8, tamaño 40. MOUSEINPUT y KEYBDINPUT se
// escriben por offsets de byte exactos.
const (
	sizeofInput    = 40
	offUnion       = 8
	offMiDX        = offUnion + 0
	offMiDY        = offUnion + 4
	offMiMouseData = offUnion + 8
	offMiFlags     = offUnion + 12
	offKiVK        = offUnion + 0
	offKiScan      = offUnion + 2
	offKiFlags     = offUnion + 4
)

// keysymVK mapea keysyms X11 de teclas especiales a Virtual Keys de Windows.
var keysymVK = map[uint32]uint16{
	0xFF08: 0x08, // BackSpace
	0xFF09: 0x09, // Tab
	0xFF0D: 0x0D, // Return
	0xFF1B: 0x1B, // Escape
	0xFF50: 0x24, // Home
	0xFF51: 0x25, // Left
	0xFF52: 0x26, // Up
	0xFF53: 0x27, // Right
	0xFF54: 0x28, // Down
	0xFF55: 0x21, // Prior (Page Up)
	0xFF56: 0x22, // Next (Page Down)
	0xFF57: 0x23, // End
	0xFF63: 0x2D, // Insert
	0xFFFF: 0x2E, // Delete
	0xFFBE: 0x70, // F1
	0xFFBF: 0x71, // F2
	0xFFC0: 0x72, // F3
	0xFFC1: 0x73, // F4
	0xFFC2: 0x74, // F5
	0xFFC3: 0x75, // F6
	0xFFC4: 0x76, // F7
	0xFFC5: 0x77, // F8
	0xFFC6: 0x78, // F9
	0xFFC7: 0x79, // F10
	0xFFC8: 0x7A, // F11
	0xFFC9: 0x7B, // F12
	0xFFE1: 0xA0, // Shift_L
	0xFFE2: 0xA1, // Shift_R
	0xFFE3: 0xA2, // Control_L
	0xFFE4: 0xA3, // Control_R
	0xFFE5: 0x14, // Caps_Lock
	0xFFE9: 0xA4, // Alt_L
	0xFFEA: 0xA5, // Alt_R
	0xFFEB: 0x5B, // Super_L
	0xFFEC: 0x5C, // Super_R
}

// sendInputInjector inyecta entrada con SendInput en el hilo de captura.
type sendInputInjector struct {
	prevButtons byte

	logf    func(string, ...any) // nil = sin logs
	firstIn bool
	failN   int
	mu      sync.Mutex
}

// newSendInputInjector crea el inyector. logf puede ser nil.
func newSendInputInjector(logf func(string, ...any)) *sendInputInjector {
	return &sendInputInjector{logf: logf, firstIn: true}
}

// sendOne envía un INPUT ya construido en buf (40 bytes).
func (s *sendInputInjector) sendOne(buf []byte, what string) error {
	r1, _, err := procSendInput.Call(uintptr(unsafe.Pointer(&buf[0])), 1, sizeofInput)
	if r1 != 1 {
		s.mu.Lock()
		s.failN++
		if s.logf != nil && s.failN <= 5 {
			s.logf("SendInput falló (%s, rc=%d): %v", what, r1, err)
		}
		s.mu.Unlock()
		return fmt.Errorf("SendInput %s: %v", what, err)
	}
	if s.logf != nil {
		s.mu.Lock()
		first := s.firstIn
		s.firstIn = false
		s.mu.Unlock()
		if first {
			s.logf("primera entrada inyectada con éxito (%s)", what)
		}
	}
	return nil
}

// Mouse inyecta movimiento (absoluto sobre el escritorio virtual) y el diff
// de botones respecto al estado anterior. DEBE llamarse desde el hilo de
// captura (mismo binding de escritorio que la captura).
func (s *sendInputInjector) Mouse(action, buttons byte, x, y uint16) error {
	_ = action // el estado de botones gobierna el diff (el visor manda ambos)
	vx, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	vy, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	vw, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	vh, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	if vw == 0 || vh == 0 {
		return nil
	}
	ax := int32(int(x) + int(int32(vx)))
	ay := int32(int(y) + int(int32(vy)))
	nx := int32(uint32(ax) * 65535 / uint32(vw-1))
	ny := int32(uint32(ay) * 65535 / uint32(vh-1))

	// Movimiento absoluto.
	buf := make([]byte, sizeofInput)
	buf[0] = inputMouse
	binary.LittleEndian.PutUint32(buf[offMiDX:], uint32(nx))
	binary.LittleEndian.PutUint32(buf[offMiDY:], uint32(ny))
	binary.LittleEndian.PutUint32(buf[offMiFlags:], mouseeventfMove|mouseeventfAbsolute)
	if err := s.sendOne(buf, "mouse-move"); err != nil {
		return err
	}

	// Diff de botones (bit0 izq, bit1 centro, bit2 der).
	prev := s.prevButtons
	for bit := byte(0); bit < 3; bit++ {
		mask := byte(1 << bit)
		if buttons&mask == prev&mask {
			continue
		}
		downFlag := [3]uint32{mouseeventfLeftDown, mouseeventfMiddleDown, mouseeventfRightDown}[bit]
		upFlag := [3]uint32{mouseeventfLeftUp, mouseeventfMiddleUp, mouseeventfRightUp}[bit]
		flag := upFlag
		what := "mouse-up"
		if buttons&mask != 0 {
			flag = downFlag
			what = "mouse-down"
		}
		buf := make([]byte, sizeofInput)
		buf[0] = inputMouse
		binary.LittleEndian.PutUint32(buf[offMiFlags:], flag)
		if err := s.sendOne(buf, what); err != nil {
			return err
		}
	}
	s.prevButtons = buttons
	return nil
}

// Key inyecta una tecla: especiales por VK, imprimibles por UNICODE.
func (s *sendInputInjector) Key(down bool, keysym uint32) error {
	buf := make([]byte, sizeofInput)
	buf[0] = inputKeyboard
	flags := uint32(0)
	if !down {
		flags = keyeventfKeyup
	}
	if vk, ok := keysymVK[keysym]; ok {
		binary.LittleEndian.PutUint16(buf[offKiVK:], vk)
		binary.LittleEndian.PutUint32(buf[offKiFlags:], flags)
	} else {
		// Imprimibles: Latin-1 directo; keysyms unicode (0x01000000+cp).
		ch := keysym
		if ch >= 0x01000000 {
			ch -= 0x01000000
		}
		if ch < 0x20 || ch > 0xFFFF {
			return nil // keysym desconocido fuera de rango: ignorar
		}
		binary.LittleEndian.PutUint16(buf[offKiScan:], uint16(ch))
		binary.LittleEndian.PutUint32(buf[offKiFlags:], flags|keyeventfUnicode)
	}
	what := "key-down"
	if !down {
		what = "key-up"
	}
	return s.sendOne(buf, what)
}
