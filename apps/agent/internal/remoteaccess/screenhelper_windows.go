//go:build windows

// screenhelper_windows.go implementa el modo `bdr-agent screenhelper`: un
// proceso lanzado por el servicio DENTRO de la sesión interactiva del usuario
// (WinSta0\Default) que captura el escritorio con GDI, inyecta entrada con
// SendInput y produce tiles JPEG por stdout.
//
// El servicio (sesión 0) no puede capturar ni inyectar en el escritorio del
// usuario: este helper es el mismo patrón que MeshCentral resuelve con su
// modo `--slave`, y el que usan TeamViewer/AnyDesk.
//
// Diagnóstico: el helper escribe su bitácora en
// %ProgramData%\bdr\screenhelper.log (su stderr va a NUL para no contaminar
// el stream de tiles; un panic del helper debe poder verse en el fichero).
package remoteaccess

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juansoler/bdr/packages/version"
)

// versionString devuelve la versión del binario para el log del helper.
func versionString() string { return version.Version }

// screenHelperLog abre (append) el fichero de bitácora del helper. Si no se
// puede abrir, devuelve nil y el helper corre sin log — nunca bloquea.
func screenHelperLog() *os.File {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = os.TempDir()
	}
	path := filepath.Join(base, "bdr", "screenhelper.log")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return f
}

// logLine escribe una línea con timestamp (ignora errores: best-effort).
func logLine(f *os.File, format string, args ...any) {
	if f == nil {
		return
	}
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		fmt.Sprintf(format, args...))
}

// RunScreenHelper ejecuta el helper: lee mensajes del visor por stdin,
// emite tiles por stdout, hasta que el canal se cierre o la captura falle.
// Lo llama cmd/bdr-agent con `screenhelper`; no está pensado para uso manual.
func RunScreenHelper(stdin io.Reader, stdout io.Writer) error {
	logFile := screenHelperLog()
	logLine(logFile, "screenhelper iniciado (pid %d, version %s)", os.Getpid(), versionString())

	// REGLA GDI/Go: los DC de GetDC y el binding de escritorio son estado
	// POR HILO y las goroutines migran — bloquea este goroutine a su hilo
	// para TODO el ciclo de captura, y fija el escritorio interactivo.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	bindInteractiveDesktop(func(format string, args ...any) {
		logLine(logFile, format, args...)
	})

	defer func() {
		if r := recover(); r != nil {
			logLine(logFile, "PANIC: %v\n%s", r, debug.Stack())
		}
	}()

	engine := NewTileEngine()
	source, err := newGDICapture()
	if err != nil {
		logLine(logFile, "ERROR: captura GDI no disponible: %v", err)
		return fmt.Errorf("helper: captura GDI no disponible: %w", err)
	}
	logLine(logFile, "captura GDI inicializada")
	defer source.Close()

	inj := newSendInputInjector()
	var intervalMs atomic.Int64
	intervalMs.Store(int64(defaultFrameEvery / time.Millisecond))
	firstFrame := atomic.Bool{}
	firstFrame.Store(true)

	// Entrada del visor: stdin (mensajes enmarcados). Al cerrarse el canal,
	// se para el bucle de captura (sin visor no hay sesión).
	stop := make(chan struct{})
	var stopOnce sync.Once
	stopNow := func() { stopOnce.Do(func() { close(stop) }) }
	go func() {
		for {
			msg, err := nextStreamFrame(stdin)
			if err != nil {
				logLine(logFile, "stdin cerrado (%v): fin del helper", err)
				stopNow()
				return
			}
			if len(msg) == 0 {
				continue
			}
			switch msg[0] {
			case msgMouse:
				if ev, ok := decodeMouse(msg[1:]); ok {
					_ = inj.Mouse(ev.Action, ev.Buttons, ev.X, ev.Y)
				}
			case msgKey:
				if ev, ok := decodeKey(msg[1:]); ok {
					_ = inj.Key(ev.Down, ev.Keysym)
				}
			case msgQuality:
				if q, interval, ok := decodeQuality(msg[1:]); ok {
					engine.SetQuality(q)
					intervalMs.Store(int64(interval / time.Millisecond))
				}
			case msgRefresh:
				engine.Reset()
			}
		}
	}()

	err = captureLoop(engine, source, func() time.Duration {
		return time.Duration(intervalMs.Load()) * time.Millisecond
	}, func(m []byte) error {
		if firstFrame.CompareAndSwap(true, false) {
			if m[0] == msgScreenInfo {
				w := int(m[1])<<8 | int(m[2])
				h := int(m[3])<<8 | int(m[4])
				logLine(logFile, "primer frame capturado: %dx%d", w, h)
			}
		}
		_, werr := stdout.Write(frameStream(m))
		return werr
	}, stop)
	if err != nil {
		logLine(logFile, "bucle de captura terminó con error: %v", err)
	} else {
		logLine(logFile, "bucle de captura terminó ordenadamente")
	}
	return err
}
