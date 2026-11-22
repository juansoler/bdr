//go:build windows

// helper_windows.go lanza el helper de sesión desde el servicio: el agente
// (SYSTEM, sesión 0) localiza la sesión de consola activa, obtiene el token
// del usuario y crea `bdr-agent screenhelper` en WinSta0\Default con pipes
// de stdio. Mismo patrón que el `--slave` de MeshCentral.
package remoteaccess

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// helperProcess es el helper de sesión en marcha.
type helperProcess struct {
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	proc      windows.Handle
	done      chan struct{}
	exitCode  uint32 // código de salida del proceso (tras done)
	closeOnce sync.Once
}

// spawnScreenHelper crea el proceso helper en la sesión interactiva.
func spawnScreenHelper() (*helperProcess, error) {
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID == 0xFFFFFFFF || sessionID == 0 {
		return nil, fmt.Errorf("no hay sesión de consola activa (equipo sin usuario logado)")
	}
	var token windows.Token
	if err := windows.WTSQueryUserToken(sessionID, &token); err != nil {
		return nil, fmt.Errorf("no se pudo obtener el token del usuario de consola: %w", err)
	}
	defer token.Close()

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exe16, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	cmd16, err := windows.UTF16PtrFromString(`"` + exe + `" screenhelper`)
	if err != nil {
		return nil, err
	}
	desktop16, _ := windows.UTF16PtrFromString("WinSta0\\Default")

	// Pipes: los extremos del hijo deben ser heredables.
	var sa windows.SecurityAttributes
	sa.Length = uint32(unsafe.Sizeof(sa))
	sa.InheritHandle = 1

	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, &sa, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe stdin: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, &sa, 0); err != nil {
		_ = windows.CloseHandle(inRead)
		_ = windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("CreatePipe stdout: %w", err)
	}

	si := &windows.StartupInfo{
		Flags:     windows.STARTF_USESTDHANDLES,
		Desktop:   desktop16,
		StdInput:  inRead,
		StdOutput: outWrite,
		StdErr:    nulHandle(&sa), // stderr a NUL: un panic del helper NUNCA corrompe el stream de tiles
	}
	pi := &windows.ProcessInformation{}
	err = windows.CreateProcessAsUser(
		token, exe16, cmd16, nil, nil, true, /* hereda handles */
		windows.CREATE_NO_WINDOW, nil, nil, si, pi)
	// Los extremos del hijo sobran en el padre tras CreateProcess.
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	if err != nil {
		_ = windows.CloseHandle(inWrite)
		_ = windows.CloseHandle(outRead)
		_ = windows.CloseHandle(si.StdErr)
		return nil, fmt.Errorf("CreateProcessAsUser: %w", err)
	}
	_ = windows.CloseHandle(si.StdErr) // el hijo ya heredó su copia
	_ = windows.CloseHandle(pi.Thread)

	h := &helperProcess{
		stdin:  &handleWriter{h: inWrite},
		stdout: &handleReader{h: outRead},
		proc:   pi.Process,
		done:   make(chan struct{}),
	}
	go func() {
		_, _ = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		var code uint32
		_ = windows.GetExitCodeProcess(pi.Process, &code)
		h.exitCode = code
		close(h.done)
	}()
	return h, nil
}

// terminationReason devuelve por qué murió el helper ("" si sigue vivo).
func (h *helperProcess) terminationReason() string {
	select {
	case <-h.done:
		switch h.exitCode {
		case 0:
			return "el helper de sesión terminó sin error (canal cerrado)"
		case 1:
			return "el helper falló: captura GDI no disponible (¿sesión sin escritorio?)"
		case 2:
			return "el helper terminó de forma anómala (pánico): revisa %ProgramData%\\bdr\\screenhelper.log"
		default:
			return fmt.Sprintf("el helper de sesión terminó con código %d", h.exitCode)
		}
	default:
		return "el helper dejó de emitir datos y sigue vivo (posible bloqueo interno): revisa %ProgramData%\\bdr\\screenhelper.log"
	}
}

// nulHandle abre NUL (heredable) para redirigir el stderr del helper: si el
// helper entra en pánico, Go escribe en stderr y NO debe mezclarse con los
// tiles del stdout.
func nulHandle(sa *windows.SecurityAttributes) windows.Handle {
	nul, _ := windows.UTF16PtrFromString("NUL")
	h, err := windows.CreateFile(nul, windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, sa,
		windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0
	}
	return h
}

// kill termina el helper.
func (h *helperProcess) kill() {
	h.closeOnce.Do(func() {
		if h.proc != 0 {
			_ = windows.TerminateProcess(h.proc, 1)
		}
		select {
		case <-h.done:
		case <-time.After(2 * time.Second):
		}
		_ = h.stdin.Close()
		_ = h.stdout.Close()
		if h.proc != 0 {
			_ = windows.CloseHandle(h.proc)
		}
	})
}

// handleReader/handleWriter adaptan HANDLE de pipe a io.ReadCloser/io.WriteCloser.
type handleReader struct{ h windows.Handle }
type handleWriter struct{ h windows.Handle }

// Close cierra el handle del pipe.
func (r *handleReader) Close() error { return windows.CloseHandle(r.h) }

// Close cierra el handle del pipe.
func (w *handleWriter) Close() error { return windows.CloseHandle(w.h) }

func (r *handleReader) Read(p []byte) (int, error) {
	var n uint32
	if len(p) == 0 {
		return 0, nil
	}
	err := windows.ReadFile(r.h, p, &n, nil)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

func (w *handleWriter) Write(p []byte) (int, error) {
	var n uint32
	total := 0
	for total < len(p) {
		chunk := p[total:]
		if err := windows.WriteFile(w.h, chunk, &n, nil); err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
		total += int(n)
	}
	return total, nil
}

// helperWaitGroup evita dobles spawns concurrentes.
var helperSpawnMu sync.Mutex
