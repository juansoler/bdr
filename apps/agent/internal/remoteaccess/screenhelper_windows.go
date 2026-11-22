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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows/registry"

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
//
// Si termina con error, avisa al agente por stdout (frame msgHelperError con
// el texto del fallo) antes de salir: el stderr del helper va a NUL para no
// contaminar el stream de tiles, así que este es el único canal del motivo.
func RunScreenHelper(stdin io.Reader, stdout io.Writer) (err error) {
	logFile := screenHelperLog()
	logLine(logFile, "screenhelper iniciado (pid %d, version %s)", os.Getpid(), versionString())

	// El recover cubre TODO el cuerpo (incluido el binding de escritorio):
	// un panic del helper debe dejar huella en el log, no un silencio.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic interno del helper (ver screenhelper.log): %v", r)
			logLine(logFile, "PANIC: %v\n%s", r, debug.Stack())
		}
		// Motivo estructurado por el pipe: el agente lo muestra verbatim en
		// el panel, sin depender del exit code.
		if err != nil {
			text := err.Error()
			if len(text) > 400 {
				text = text[:400]
			}
			frame := frameStream(append([]byte{msgHelperError}, []byte(text)...))
			_, _ = stdout.Write(frame)
			logLine(logFile, "helper terminó con error: %v", err)
		}
	}()

	// REGLA GDI/Go: los DC de GetDC y el binding de escritorio son estado
	// POR HILO y las goroutines migran — bloquea este goroutine a su hilo
	// para TODO el ciclo de captura, y fija el escritorio interactivo.
	logLine(logFile, "paso 1: bloqueando hilo de captura (LockOSThread)")
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	logLine(logFile, "paso 2: fijando escritorio interactivo del hilo")
	bindInteractiveDesktop(func(format string, args ...any) {
		logLine(logFile, format, args...)
	})
	logSessionContext(func(format string, args ...any) {
		logLine(logFile, format, args...)
	})

	// DPI-aware desde el primer GDI: sin esto, Windows virtualiza las
	// métricas en pantallas escaladas (125/150 %) — la captura sale
	// reescalada y las coordenadas de SendInput no coinciden con lo que el
	// visor envía. Debe llamarse antes de cualquier GetDC.
	if r1, _, _ := procSetProcessDPIAware.Call(); r1 != 0 {
		logLine(logFile, "paso 2b: SetProcessDPIAware aplicado (métricas físicas)")
	} else {
		logLine(logFile, "paso 2b: SetProcessDPIAware no aplicado (¿ya aware?)")
	}

	// Ediciones sin escritorio gráfico (Server Core, Nano): la captura no
	// puede funcionar — error honesto en vez de un «BitBlt handle invalid».
	if reason := desktopUnavailableReason(); reason != "" {
		logLine(logFile, "ERROR: %s", reason)
		return fmt.Errorf("%s", reason)
	}

	engine := NewTileEngine()
	logLine(logFile, "paso 3: inicializando captura GDI")
	source, err := newGDICapture()
	if err != nil {
		logLine(logFile, "ERROR: captura GDI no disponible: %v", err)
		return fmt.Errorf("helper: captura GDI no disponible: %w", err)
	}
	logLine(logFile, "paso 4: captura GDI lista (GetDC por frame)")
	defer source.Close()

	inj := newSendInputInjector(func(format string, args ...any) {
		logLine(logFile, format, args...)
	})
	var intervalMs atomic.Int64
	intervalMs.Store(int64(defaultFrameEvery / time.Millisecond))
	firstFrame := atomic.Bool{}
	firstFrame.Store(true)

	// Entrada del visor: stdin (mensajes enframedos → encolados). La
	// INYECCIÓN la hace el hilo de captura (drena la cola antes de cada
	// frame): SendInput y GDI comparten hilo y binding de escritorio.
	stop := make(chan struct{})
	var stopOnce sync.Once
	stopNow := func() { stopOnce.Do(func() { close(stop) }) }
	inputCh := make(chan []byte, 128)
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
			case msgMouse, msgKey:
				select {
				case inputCh <- msg:
				default:
					// Saturado: se descarta (el visor genera eventos muy rápido).
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

	src := &inputDrainSource{inner: source, ch: inputCh, inj: inj}
	err = captureLoop(engine, src, func() time.Duration {
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

// inputDrainSource envuelve el TileSource de captura: drena la entrada del
// visor ANTES de cada frame. Así la inyección (SendInput) corre en el MISMO
// hilo bloqueado que la captura (GDI) — mismo escritorio, mismo binding.
type inputDrainSource struct {
	inner TileSource
	ch    chan []byte
	inj   *sendInputInjector
}

// Frame drena la entrada pendiente y captura un frame.
func (s *inputDrainSource) Frame() ([]byte, int, int, error) {
	for {
		select {
		case msg := <-s.ch:
			switch msg[0] {
			case msgMouse:
				if ev, ok := decodeMouse(msg[1:]); ok {
					_ = s.inj.Mouse(ev.Action, ev.Buttons, ev.X, ev.Y) // fallos logueados por el inyector
				}
			case msgKey:
				if ev, ok := decodeKey(msg[1:]); ok {
					_ = s.inj.Key(ev.Down, ev.Keysym)
				}
			}
			continue
		default:
		}
		return s.inner.Frame()
	}
}

// Close libera el origen interno.
func (s *inputDrainSource) Close() error { return s.inner.Close() }

// desktopUnavailableReason detecta ediciones de Windows SIN escritorio
// gráfico (Server Core, Nano Server): «Ver pantalla» no puede funcionar y
// el fallo GDI posterior solo confundiría. "" = edición con escritorio.
func desktopUnavailableReason() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	val, _, err := k.GetStringValue("InstallationType")
	if err != nil || val == "" {
		return ""
	}
	if strings.Contains(val, "Core") || strings.EqualFold(val, "Nano Server") {
		return fmt.Sprintf(
			"esta edición de Windows (%s) no tiene escritorio gráfico que compartir: «Ver pantalla» no está disponible en ella", val)
	}
	return ""
}
