//go:build windows

// clipboard_windows.go sincroniza el portapapeles del equipo gestionado con
// el visor: el helper detecta cambios (GetClipboardSequenceNumber — lectura
// barata, sin abrir el portapapeles) y publica el texto al visor; y fija el
// portapapeles remoto con el texto que llega del visor (pegar del local).
//
// El portapapeles no es thread-affine: se puede usar desde el hilo de
// captura sin problema. OpenClipboard falla si otra app lo mantiene: se
// reintenta unas veces y si no, se pierde esa vuelta (la siguiente detección
// de secuencia lo recogerá).
package remoteaccess

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procOpenClipboard        = user32DLL.NewProc("OpenClipboard")
	procCloseClipboard       = user32DLL.NewProc("CloseClipboard")
	procEmptyClipboard       = user32DLL.NewProc("EmptyClipboard")
	procSetClipboardData     = user32DLL.NewProc("SetClipboardData")
	procGetClipboardData     = user32DLL.NewProc("GetClipboardData")
	procGetClipboardSequence = user32DLL.NewProc("GetClipboardSequenceNumber")
	procGlobalAlloc          = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalAlloc")
	procGlobalSize           = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalSize")
	procGlobalLock           = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalLock")
	procGlobalUnlock         = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalUnlock")
	procGlobalFree           = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// clipboardTracker vigila el portapapeles remoto y evita el eco: al fijar el
// portapapeles nosotros, la secuencia cambia también — se anota para no
// reenviar nuestro propio texto.
type clipboardTracker struct {
	lastSeq uint32
	logf    func(string, ...any) // nil = sin logs
}

// readNew devuelve el texto nuevo del portapapeles ("" si no cambió o está
// vacío/no es texto).
func (c *clipboardTracker) readNew() (string, bool) {
	seq, _, _ := procGetClipboardSequence.Call()
	if seq == 0 || uint32(seq) == c.lastSeq {
		return "", false
	}
	c.lastSeq = uint32(seq)
	text, ok := c.read()
	if c.logf != nil && !ok {
		c.logf("portapapeles remoto cambió a contenido no textual (ignorado)")
	}
	if !ok {
		return "", false
	}
	return text, true
}

// set fija el portapapeles remoto y anota la secuencia para no ecoar.
func (c *clipboardTracker) set(text string) error {
	if err := c.setImpl(text); err != nil {
		return err
	}
	if seq, _, _ := procGetClipboardSequence.Call(); seq != 0 {
		c.lastSeq = uint32(seq)
	}
	return nil
}

// openRetry abre el portapapeles reintentando (otras apps lo retienen).
func openRetry() bool {
	for i := 0; i < 10; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// read lee el texto actual (CF_UNICODETEXT). ok=false si no hay texto.
func (c *clipboardTracker) read() (string, bool) {
	if !openRetry() {
		return "", false
	}
	defer procCloseClipboard.Call(0)
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 || size > maxClipboardBytes*2+2 {
		return "", false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(h)
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(p)), size/2)
	for i, c := range u16 {
		if c == 0 {
			u16 = u16[:i]
			break
		}
	}
	return syscall.UTF16ToString(u16), true
}

// setImpl fija el texto (CF_UNICODETEXT). El handle del HGLOBAL pasa a ser
// propiedad del portapapeles tras SetClipboardData con éxito.
func (c *clipboardTracker) setImpl(text string) error {
	u16, err := syscall.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("portapapeles: texto inválido: %w", err)
	}
	size := len(u16) * 2
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %v", err)
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock falló")
	}
	slice := unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u16))
	copy(slice, u16)
	procGlobalUnlock.Call(h)

	if !openRetry() {
		procGlobalFree.Call(h)
		return fmt.Errorf("el portapapeles remoto está ocupado")
	}
	defer procCloseClipboard.Call(0)
	procEmptyClipboard.Call(0)
	r, _, err := procSetClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData: %v", err)
	}
	return nil // el portapapeles es dueño del handle: no liberar
}
