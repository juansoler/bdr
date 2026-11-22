//go:build windows

// capture_windows.go implementa la captura de escritorio con GDI (sin CGO):
// CreateDIBSection + BitBlt sobre el escritorio virtual (todas las pantallas).
// El DIB section entrega los píxeles BGRA directamente en memoria — sin el
// paso GetDIBits, que falla si el bitmap está seleccionado en un DC (como lo
// está durante el BitBlt). Es el mismo enfoque que MeshCentral.
//
// Este código corre en el HELPER de sesión (bdr-agent screenhelper), lanzado
// por el servicio dentro de la sesión interactiva del usuario: capturar o
// inyectar entrada desde la sesión 0 del servicio no llega al escritorio real.
package remoteaccess

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32DLL = windows.NewLazySystemDLL("user32.dll")
	gdi32DLL  = windows.NewLazySystemDLL("gdi32.dll")

	procGetDC              = user32DLL.NewProc("GetDC")
	procReleaseDC          = user32DLL.NewProc("ReleaseDC")
	procGetSystemMetrics   = user32DLL.NewProc("GetSystemMetrics")
	procCreateCompatibleDC = gdi32DLL.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32DLL.NewProc("CreateDIBSection")
	procSelectObject       = gdi32DLL.NewProc("SelectObject")
	procDeleteDC           = gdi32DLL.NewProc("DeleteDC")
	procDeleteObject       = gdi32DLL.NewProc("DeleteObject")
	procBitBlt             = gdi32DLL.NewProc("BitBlt")
)

// Índices de GetSystemMetrics para el escritorio virtual (multi-monitor).
const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

const (
	srccopy      = 0x00CC0020
	dibRGBColors = 0
)

// gdiCapture es el TileSource GDI con DC + DIB section cacheados.
type gdiCapture struct {
	screenDC syscall.Handle
	memDC    syscall.Handle
	bmp      syscall.Handle
	oldBmp   syscall.Handle
	bits     unsafe.Pointer // memoria del DIB section (propiedad de GDI)
	pix      []byte         // vista sobre bits
	w, h     int
	vx, vy   int
	bmi      [40]byte // BITMAPINFOHEADER + sin paleta (32bpp)
}

// newGDICapture prepara la captura GDI.
func newGDICapture() (*gdiCapture, error) {
	sdc, _, err := procGetDC.Call(0)
	if sdc == 0 {
		return nil, fmt.Errorf("GDI: GetDC(NULL) falló: %v", err)
	}
	return &gdiCapture{screenDC: syscall.Handle(sdc)}, nil
}

// Close libera los recursos GDI.
func (g *gdiCapture) Close() error {
	if g.bmp != 0 {
		procSelectObject.Call(uintptr(g.memDC), uintptr(g.oldBmp))
		procDeleteObject.Call(uintptr(g.bmp))
	}
	if g.memDC != 0 {
		procDeleteDC.Call(uintptr(g.memDC))
	}
	if g.screenDC != 0 {
		procReleaseDC.Call(0, uintptr(g.screenDC))
	}
	return nil
}

// metrics lee la bounding box del escritorio virtual.
func (g *gdiCapture) metrics() (w, h int, err error) {
	v, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	g.vx = int(int32(v))
	v, _, _ = procGetSystemMetrics.Call(smYVirtualScreen)
	g.vy = int(int32(v))
	v, _, _ = procGetSystemMetrics.Call(smCXVirtualScreen)
	w = int(int32(v))
	v, _, _ = procGetSystemMetrics.Call(smCYVirtualScreen)
	h = int(int32(v))
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("GDI: escritorio virtual inválido %dx%d (¿sesión bloqueada o sin escritorio?)", w, h)
	}
	return w, h, nil
}

// fillBMI escribe el BITMAPINFOHEADER (40 bytes, filas top-down).
func (g *gdiCapture) fillBMI(w, h int) {
	b := g.bmi[:]
	for i := range b {
		b[i] = 0
	}
	binary.LittleEndian.PutUint32(b[0:], 40)         // biSize
	binary.LittleEndian.PutUint32(b[4:], uint32(w))  // biWidth
	binary.LittleEndian.PutUint32(b[8:], uint32(-h)) // biHeight negativo = top-down
	binary.LittleEndian.PutUint16(b[12:], 1)         // biPlanes
	binary.LittleEndian.PutUint16(b[14:], 32)        // biBitCount
	// biCompression = BI_RGB (0); el resto a cero.
}

// resize reconstruye DIB section + DC cuando cambia el tamaño.
func (g *gdiCapture) resize(w, h int) error {
	if g.bmp != 0 {
		procSelectObject.Call(uintptr(g.memDC), uintptr(g.oldBmp))
		procDeleteObject.Call(uintptr(g.bmp))
		procDeleteDC.Call(uintptr(g.memDC))
		g.memDC, g.bmp, g.bits, g.pix = 0, 0, nil, nil
	}
	g.fillBMI(w, h)

	hmem, _, err := procCreateCompatibleDC.Call(uintptr(g.screenDC))
	if hmem == 0 {
		return fmt.Errorf("GDI: CreateCompatibleDC falló: %v", err)
	}
	g.memDC = syscall.Handle(hmem)

	var bits uintptr
	hbmp, _, err := procCreateDIBSection.Call(
		uintptr(g.screenDC), uintptr(unsafe.Pointer(&g.bmi[0])), dibRGBColors,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbmp == 0 {
		return fmt.Errorf("GDI: CreateDIBSection falló: %v", err)
	}
	g.bmp = syscall.Handle(hbmp)
	g.bits = unsafe.Pointer(bits)
	g.pix = unsafe.Slice((*byte)(g.bits), w*h*4)

	old, _, _ := procSelectObject.Call(uintptr(g.memDC), uintptr(hbmp))
	g.oldBmp = syscall.Handle(old)
	g.w, g.h = w, h
	return nil
}

// Frame captura el escritorio completo (BGRA, top-down) en el DIB section.
func (g *gdiCapture) Frame() ([]byte, int, int, error) {
	w, h, err := g.metrics()
	if err != nil {
		return nil, 0, 0, err
	}
	if g.memDC == 0 || g.w != w || g.h != h {
		if err := g.resize(w, h); err != nil {
			return nil, 0, 0, err
		}
	}
	r1, _, err := procBitBlt.Call(
		uintptr(g.memDC), 0, 0, uintptr(w), uintptr(h),
		uintptr(g.screenDC), uintptr(int32(g.vx)), uintptr(int32(g.vy)), srccopy)
	if r1 == 0 {
		return nil, 0, 0, fmt.Errorf("GDI: BitBlt falló: %v", err)
	}
	return g.pix, g.w, g.h, nil
}
