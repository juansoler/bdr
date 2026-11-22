//go:build windows

// capture_windows.go implementa la captura de escritorio con GDI (sin CGO):
// BitBlt sobre el escritorio virtual (todas las pantallas) y GetDIBits para
// obtener los píxeles en BGRA. Es el mismo enfoque que MeshCentral (su código
// DXGI Desktop Duplication está comentado: GDI es compatible con todo).
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
	procCreateCompatBitmap = gdi32DLL.NewProc("CreateCompatibleBitmap")
	procSelectObject       = gdi32DLL.NewProc("SelectObject")
	procDeleteDC           = gdi32DLL.NewProc("DeleteDC")
	procDeleteObject       = gdi32DLL.NewProc("DeleteObject")
	procBitBlt             = gdi32DLL.NewProc("BitBlt")
	procGetDIBits          = gdi32DLL.NewProc("GetDIBits")
)

// Índices de GetSystemMetrics para el escritorio virtual (multi-monitor).
const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

const (
	srccopy        = 0x00CC0020
	dibRGBColors   = 0
	biTopDownDelta = -1 // biHeight negativo = filas top-down
)

// gdiCapture es el TileSource GDI con DCs cacheados.
type gdiCapture struct {
	screenDC  syscall.Handle
	memDC     syscall.Handle
	bmp       syscall.Handle
	oldBmp    syscall.Handle
	w, h      int
	vx, vy    int
	pix       []byte
	bmi       [40]byte // BITMAPINFOHEADER + paleta vacía (32bpp: sin paleta)
	lastError int
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
func (g *gdiCapture) metrics() error {
	v, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	g.vx = int(int32(v))
	v, _, _ = procGetSystemMetrics.Call(smYVirtualScreen)
	g.vy = int(int32(v))
	v, _, _ = procGetSystemMetrics.Call(smCXVirtualScreen)
	w := int(int32(v))
	v, _, _ = procGetSystemMetrics.Call(smCYVirtualScreen)
	h := int(int32(v))
	if w <= 0 || h <= 0 {
		return fmt.Errorf("GDI: escritorio virtual inválido %dx%d (¿sin sesión interactiva?)", w, h)
	}
	g.w, g.h = w, h
	return nil
}

// resize reconstruye DC+bitmap cuando cambia el tamaño del escritorio.
func (g *gdiCapture) resize() error {
	if g.memDC != 0 {
		procSelectObject.Call(uintptr(g.memDC), uintptr(g.oldBmp))
		procDeleteObject.Call(uintptr(g.bmp))
		procDeleteDC.Call(uintptr(g.memDC))
		g.memDC, g.bmp, g.oldBmp = 0, 0, 0
	}
	hmem, _, _ := procCreateCompatibleDC.Call(uintptr(g.screenDC))
	if hmem == 0 {
		return fmt.Errorf("GDI: CreateCompatibleDC falló")
	}
	g.memDC = syscall.Handle(hmem)
	hbmp, _, _ := procCreateCompatBitmap.Call(uintptr(g.screenDC), uintptr(g.w), uintptr(g.h))
	if hbmp == 0 {
		return fmt.Errorf("GDI: CreateCompatibleBitmap falló")
	}
	g.bmp = syscall.Handle(hbmp)
	old, _, _ := procSelectObject.Call(uintptr(g.memDC), uintptr(hbmp))
	g.oldBmp = syscall.Handle(old)
	g.pix = make([]byte, g.w*g.h*4)
	return nil
}

// fillBMI escribe el BITMAPINFOHEADER (40 bytes, filas top-down).
func (g *gdiCapture) fillBMI() {
	b := g.bmi[:]
	for i := range b {
		b[i] = 0
	}
	binary.LittleEndian.PutUint32(b[0:], 40)                                // biSize
	binary.LittleEndian.PutUint32(b[4:], uint32(g.w))                       // biWidth
	binary.LittleEndian.PutUint32(b[8:], uint32(int32(biTopDownDelta*g.h))) // biHeight (negativo)
	binary.LittleEndian.PutUint16(b[12:], 1)                                // biPlanes
	binary.LittleEndian.PutUint16(b[14:], 32)                               // biBitCount
	// biCompression = BI_RGB (0); el resto a cero.
}

// Frame captura el escritorio completo (BGRA, top-down).
func (g *gdiCapture) Frame() ([]byte, int, int, error) {
	prevW, prevH := g.w, g.h
	if err := g.metrics(); err != nil {
		return nil, 0, 0, err
	}
	if g.memDC == 0 || g.w != prevW || g.h != prevH {
		if err := g.resize(); err != nil {
			return nil, 0, 0, err
		}
		g.fillBMI()
	} else {
		g.fillBMI() // refresca por si cambió el origen del escritorio virtual
	}
	r1, _, err := procBitBlt.Call(
		uintptr(g.memDC), 0, 0, uintptr(g.w), uintptr(g.h),
		uintptr(g.screenDC), uintptr(int32(g.vx)), uintptr(int32(g.vy)), srccopy)
	if r1 == 0 {
		return nil, 0, 0, fmt.Errorf("GDI: BitBlt falló: %v", err)
	}
	got, _, err := procGetDIBits.Call(
		uintptr(g.memDC), uintptr(g.bmp), 0, uintptr(g.h),
		uintptr(unsafe.Pointer(&g.pix[0])), uintptr(unsafe.Pointer(&g.bmi[0])), dibRGBColors)
	if got == 0 || got == 0xFFFFFFFF {
		return nil, 0, 0, fmt.Errorf("GDI: GetDIBits falló: %v", err)
	}
	return g.pix, g.w, g.h, nil
}
