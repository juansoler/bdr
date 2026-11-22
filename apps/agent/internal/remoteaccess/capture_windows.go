//go:build windows

// capture_windows.go implementa la captura de escritorio con GDI (sin CGO):
// CreateDIBSection + BitBlt sobre el escritorio virtual (todas las pantallas).
// El DIB section entrega los píxeles BGRA directamente en memoria.
//
// REGLA CRÍTICA DE GDI SOBRE GO: los DC de GetDC y el binding de escritorio
// (SetThreadDesktop) son ESTADO POR HILO. Las goroutines migran entre hilos
// del SO, así que TODO el trabajo GDI de captura debe correr en un ÚNICO
// hilo bloqueado (runtime.LockOSThread) con el escritorio interactivo fijado
// — mismo motivo por el que MeshCentral re-adquiere el DC «in case the
// current desktop changes» (tile.cpp) y llama a CheckDesktopSwitch.
//
// Este código corre en el HELPER de sesión (bdr-agent screenhelper), lanzado
// por el servicio dentro de la sesión interactiva del usuario.
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

	procGetDC                    = user32DLL.NewProc("GetDC")
	procReleaseDC                = user32DLL.NewProc("ReleaseDC")
	procGetSystemMetrics         = user32DLL.NewProc("GetSystemMetrics")
	procOpenInputDesktop         = user32DLL.NewProc("OpenInputDesktop")
	procCloseDesktop             = user32DLL.NewProc("CloseDesktop")
	procGetUserObjectInformation = user32DLL.NewProc("GetUserObjectInformationW")
	procSetThreadDesktop         = user32DLL.NewProc("SetThreadDesktop") // escritorios = objetos USER: user32
	procGetThreadDesktop         = user32DLL.NewProc("GetThreadDesktop") // (el panic de 0.16.4 lo probó)
	procGetProcessWindowStation  = user32DLL.NewProc("GetProcessWindowStation")
	procSetProcessDPIAware       = user32DLL.NewProc("SetProcessDPIAware")
	procSendInput                = user32DLL.NewProc("SendInput")
	procCreateCompatibleDC       = gdi32DLL.NewProc("CreateCompatibleDC")
	procCreateDIBSection         = gdi32DLL.NewProc("CreateDIBSection")
	procSelectObject             = gdi32DLL.NewProc("SelectObject")
	procDeleteDC                 = gdi32DLL.NewProc("DeleteDC")
	procDeleteObject             = gdi32DLL.NewProc("DeleteObject")
	procBitBlt                   = gdi32DLL.NewProc("BitBlt")
)

// Índices de GetSystemMetrics para el escritorio virtual (multi-monitor).
const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

// uoiName es el índice de GetUserObjectInformation para el nombre del objeto.
const uoiName = 2

// desktopAll es DESKTOP_ALL: todos los derechos sobre el escritorio.
const desktopAll = 0x01FF

const (
	srccopy      = 0x00CC0020
	dibRGBColors = 0
)

// gdiCapture es el TileSource GDI: DIB section + DC compatible cacheados, y
// el DC de pantalla adquirido FRESCO en cada frame (estado por hilo). Todas
// sus llamadas deben ejecutarse en el hilo bloqueado del helper.
type gdiCapture struct {
	memDC  syscall.Handle
	bmp    syscall.Handle
	oldBmp syscall.Handle
	bits   unsafe.Pointer // memoria del DIB section (propiedad de GDI)
	pix    []byte         // vista sobre bits
	w, h   int
	vx, vy int
	bmi    [40]byte // BITMAPINFOHEADER + sin paleta (32bpp)
}

// newGDICapture prepara la captura GDI. Debe llamarse desde el hilo bloqueado
// con el escritorio interactivo ya fijado.
func newGDICapture() (*gdiCapture, error) {
	return &gdiCapture{}, nil
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
	return nil
}

// metrics lee la bounding box del escritorio virtual.
func (g *gdiCapture) metrics() (w, h int, err error) {
	v, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	g.vx = int(int32(v))
	v2, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	g.vy = int(int32(v2))
	v3, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	w = int(int32(v3))
	v4, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	h = int(int32(v4))
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

// resize reconstruye DIB section + DC compatible. sdc es el DC de pantalla
// fresco del frame actual (solo se usa para crear el DIB).
func (g *gdiCapture) resize(sdc uintptr, w, h int) error {
	if g.bmp != 0 {
		procSelectObject.Call(uintptr(g.memDC), uintptr(g.oldBmp))
		procDeleteObject.Call(uintptr(g.bmp))
		procDeleteDC.Call(uintptr(g.memDC))
		g.memDC, g.bmp, g.bits, g.pix = 0, 0, nil, nil
	}
	g.fillBMI(w, h)

	hmem, _, err := procCreateCompatibleDC.Call(sdc)
	if hmem == 0 {
		return fmt.Errorf("GDI: CreateCompatibleDC falló: %v", err)
	}
	g.memDC = syscall.Handle(hmem)

	var bits uintptr
	hbmp, _, err := procCreateDIBSection.Call(
		sdc, uintptr(unsafe.Pointer(&g.bmi[0])), dibRGBColors,
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
// Adquiere el DC de pantalla FRESCO en el hilo actual (thread-affine) y lo
// libera al terminar: inmune a cambios de escritorio y a la migración de
// goroutines (el llamador bloquea el hilo).
func (g *gdiCapture) Frame() ([]byte, int, int, error) {
	w, h, err := g.metrics()
	if err != nil {
		return nil, 0, 0, err
	}
	sdc, _, errDC := procGetDC.Call(0)
	if sdc == 0 {
		return nil, 0, 0, fmt.Errorf("GDI: GetDC(NULL) falló: %v", errDC)
	}
	defer procReleaseDC.Call(0, sdc)

	if g.memDC == 0 || g.w != w || g.h != h {
		if err := g.resize(sdc, w, h); err != nil {
			return nil, 0, 0, err
		}
	}
	r1, _, err := procBitBlt.Call(
		uintptr(g.memDC), 0, 0, uintptr(w), uintptr(h),
		sdc, uintptr(int32(g.vx)), uintptr(int32(g.vy)), srccopy)
	if r1 == 0 {
		return nil, 0, 0, fmt.Errorf(
			"GDI: BitBlt falló: %v (memDC=%#x bmp=%#x oldBmp=%#x vx=%d vy=%d %dx%d)",
			err, uintptr(g.memDC), uintptr(g.bmp), uintptr(g.oldBmp), g.vx, g.vy, w, h)
	}
	return g.pix, g.w, g.h, nil
}

// logSessionContext registra el contexto de seguridad del helper: sesión,
// window station, escritorio del hilo y usuario. Es EL dato decisivo para
// diagnosticar el «BitBlt: handle invalid» — si el helper no aterriza en la
// sesión interactiva del usuario (sesión ≠ consola, station ≠ WinSta0), la
// captura no puede funcionar.
func logSessionContext(logf func(string, ...any)) {
	pid := windows.GetCurrentProcessId()
	var sess uint32
	if err := windows.ProcessIdToSessionId(pid, &sess); err == nil {
		logf("contexto: pid=%d sesión=%d", pid, sess)
	} else {
		logf("contexto: pid=%d (sesión desconocida: %v)", pid, err)
	}
	if ws, _, _ := procGetProcessWindowStation.Call(); ws != 0 {
		name := userObjectName(ws)
		if name == "" {
			name = "?"
		}
		logf("contexto: window station=%s", name)
	}
	if dn := currentThreadDesktopName(); dn != "" {
		logf("contexto: escritorio del hilo=%s", dn)
	}
	var nb uint32
	if err := windows.GetUserNameEx(windows.NameSamCompatible, nil, &nb); err == nil && nb > 0 {
		buf := make([]uint16, nb)
		if err2 := windows.GetUserNameEx(windows.NameSamCompatible, &buf[0], &nb); err2 == nil {
			logf("contexto: usuario=%s", syscall.UTF16ToString(buf[:nb]))
		}
	}
}

// userObjectName devuelve el nombre de un objeto USER (window station o
// escritorio) vía GetUserObjectInformationW(UOI_NAME).
func userObjectName(h uintptr) string {
	var needed uint32
	procGetUserObjectInformation.Call(h, uoiName, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 || needed > 1024 {
		return ""
	}
	buf := make([]uint16, needed/2+1)
	r1, _, _ := procGetUserObjectInformation.Call(h, uoiName,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)))
	if r1 == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// bindInteractiveDesktop fija el escritorio interactivo ACTIVO al hilo
// actual (OpenInputDesktop + SetThreadDesktop) y devuelve su nombre para el
// log. Es estado por hilo: llamar ANTES de cualquier GetDC, en el hilo
// bloqueado de captura. Si falla (p. ej. escritorio seguro de UAC activo),
// se continúa con el escritorio heredado del proceso (se registra en el log).
func bindInteractiveDesktop(logf func(string, ...any)) string {
	// Nombre del escritorio actual del hilo (diagnóstico).
	name := currentThreadDesktopName()

	hdesk, _, _ := procOpenInputDesktop.Call(0, 0, uintptr(desktopAll))
	if hdesk == 0 {
		logf("escritorio interactivo no abrible ahora (¿UAC/pantalla de bloqueo?); se usa el heredado del proceso: %s", name)
		return name
	}
	r1, _, _ := procSetThreadDesktop.Call(hdesk)
	if r1 == 0 {
		procCloseDesktop.Call(hdesk)
		logf("SetThreadDesktop falló; se usa el heredado del proceso: %s", name)
		return name
	}
	procCloseDesktop.Call(hdesk) // el binding persiste; el handle no hace falta
	if bound := currentThreadDesktopName(); bound != "" {
		name = bound
	}
	logf("escritorio del hilo de captura: %s", name)
	return name
}

// currentThreadDesktopName devuelve el nombre del escritorio del hilo actual.
func currentThreadDesktopName() string {
	h, _, _ := procGetThreadDesktop.Call(uintptr(windows.GetCurrentThreadId()))
	if h == 0 {
		return ""
	}
	var needed uint32
	procGetUserObjectInformation.Call(h, uoiName, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 || needed > 1024 {
		return ""
	}
	buf := make([]uint16, needed/2+1)
	r1, _, _ := procGetUserObjectInformation.Call(h, uoiName,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)))
	if r1 == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
