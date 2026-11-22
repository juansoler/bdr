//go:build windows

// Integración del agente con el Service Control Manager de Windows: un
// servicio debe responder al SCM (StartServiceCtrlDispatcher) en los 30 s
// posteriores al arranque; si no, Windows lo declara fallido (error 1053) y
// mata el proceso. Por eso el bucle del agente corre bajo svc.Run cuando el
// proceso fue iniciado por el gestor de servicios, y directamente en consola
// cuando se ejecuta a mano.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/juansoler/bdr/apps/agent/internal/agent"
	"github.com/juansoler/bdr/packages/logging"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// serviceName es el nombre del servicio creado por el instalador.
const serviceName = "bdr-agent"

// isWindowsService indica si el SCM arrancó este proceso.
func isWindowsService() bool {
	inSvc, err := svc.IsWindowsService()
	return err == nil && inSvc
}

// serviceLogger en modo servicio la salida estándar no está atada a ninguna
// consola: los logs van a <data-dir>\agent.log para poder diagnosticar.
func serviceLogger(dataDir, level string, fallback *logging.Logger) *logging.Logger {
	f, err := os.OpenFile(filepath.Join(dataDir, "agent.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fallback
	}
	return logging.NewWithWriter(f, level)
}

// runAgentProcess ejecuta el bucle bajo el SCM o en consola. En modo servicio
// instala el canal de parada limpia que usa el reinicio por autoactualización
// (restartSelf → requestCleanStop).
func runAgentProcess(ctx context.Context, ag *agent.Agent, once bool) error {
	if !isWindowsService() {
		return ag.Run(ctx, once)
	}
	stop := make(chan struct{})
	var stopOnce sync.Once
	restartStopMu.Lock()
	restartStopFn = func() { stopOnce.Do(func() { close(stop) }) }
	restartStopMu.Unlock()
	defer func() {
		restartStopMu.Lock()
		restartStopFn = nil
		restartStopMu.Unlock()
	}()
	return svc.Run(serviceName, &bdrHandler{run: func(ctx context.Context) error {
		return ag.Run(ctx, once)
	}, stopForRestart: stop})
}

// bdrHandler implementa svc.Handler: reporta estados al SCM y traduce
// Stop/Shutdown a cancelación del contexto.
type bdrHandler struct {
	run func(ctx context.Context) error
	// stopForRestart: cierre limpio solicitado por restartSelf tras dejar el
	// binario nuevo en su sitio. Reporta SERVICE_STOPPED con código 0, que NO
	// consume las acciones de recuperación del SCM (quedan para crashes
	// reales) ni genera eventos 7031/7034.
	stopForRestart chan struct{}
}

func (h *bdrHandler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	changes <- svc.Status{State: svc.Running, Accepts: accepts}
	for {
		select {
		case <-h.stopForRestart:
			changes <- svc.Status{State: svc.StopPending}
			cancel()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
			}
			return false, 0
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(30 * time.Second):
				}
				return false, 0
			}
		case err := <-done:
			// El bucle terminó solo: error → el SCM lo ve como fallo y
			// aplicará las acciones de recuperación (restart).
			changes <- svc.Status{State: svc.StopPending}
			if err != nil {
				return false, 1
			}
			return false, 0
		}
	}
}

// restartStopFn conecta restartSelf con el handler del SCM. Lo instala
// runAgentProcess al entrar en modo servicio.
var (
	restartStopMu sync.Mutex
	restartStopFn func()
)

func requestCleanStop() {
	restartStopMu.Lock()
	fn := restartStopFn
	restartStopMu.Unlock()
	if fn != nil {
		fn()
	}
}

// restartOnce evita un doble helper si dos heartbeats consecutivos aplicaran
// la misma oferta antes de que la parada limpia llegue al handler.
var restartOnce sync.Once

// restartSelf reemplaza el proceso por el binario nuevo.
//
// En servicio: el binario nuevo ya está en disco. Se lanza un helper
// desacoplado (`bdr-agent service-restart`) que espera la muerte de este
// proceso y arranca el servicio vía SCM; después se pide la parada limpia
// (STOPPED, código 0). Antes se salía con exit(1): el SCM lo contaba como
// fallo y consumía el contador de recuperación (5 s/10 s/5 min, reset 24 h) —
// a la tercera autoactualización del día el agente quedaba caído hasta 5
// minutos con la versión vieja en el panel.
//
// En consola/tarea: arranca el binario nuevo desacoplado y termina. Sin
// stdio heredado: en procesos sin consola los handles estándar son inválidos
// y cmd.Start falla con «The handle is invalid», dejando al agente vivo con
// el binario viejo y el nuevo ya en disco.
func restartSelf(newExe string) error {
	if isWindowsService() {
		restartOnce.Do(func() {
			if err := spawnServiceRestartHelper(newExe); err != nil {
				// Sin helper no hay quien arranque el servicio tras una
				// salida limpia (STOPPED no dispara la recuperación del SCM):
				// salida abrupta para que la recuperación lo levante — el
				// camino de antes, con sus retardos.
				os.Exit(1)
			}
			requestCleanStop()
		})
		return nil
	}
	cmd := exec.Command(newExe, os.Args[1:]...)
	if stdioValid(os.Stdout) {
		cmd.Stdout = os.Stdout
	}
	if stdioValid(os.Stderr) {
		cmd.Stderr = os.Stderr
	}
	if stdioValid(os.Stdin) {
		cmd.Stdin = os.Stdin
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// stdioValid detecta si un handle estándar es utilizable: en servicios y
// tareas programadas GetStdHandle devuelve NULL (o un handle muerto) y
// heredarlo rompe cmd.Start.
func stdioValid(f *os.File) bool {
	if f == nil {
		return false
	}
	_, err := f.Stat()
	return err == nil
}

// spawnServiceRestartHelper lanza el helper de reinicio sobre el binario
// nuevo: hereda un handle waitable de este proceso para que espere nuestra
// salida sin depender de PIDs (que Windows recicla).
func spawnServiceRestartHelper(newExe string) error {
	var h windows.Handle
	if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.CurrentProcess(),
		windows.CurrentProcess(), &h, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return fmt.Errorf("handle heredable del proceso: %w", err)
	}
	cmd := exec.Command(newExe, "service-restart",
		"--service", serviceName,
		"--parent", strconv.FormatUint(uint64(h), 16))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:                 true,
		CreationFlags:              windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
		AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(h)},
	}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(h)
		return fmt.Errorf("helper service-restart: %w", err)
	}
	// El hijo ya tiene su copia heredada del handle; esta se cierra. El
	// objeto del proceso se señala al terminar aunque queden handles abiertos.
	_ = windows.CloseHandle(h)
	return nil
}

// cmdServiceRestart es el helper interno de autoactualización: espera la
// salida del proceso antiguo y arranca el servicio con el binario nuevo.
// Vive en el propio binario del agente para no añadir otro ejecutable.
func cmdServiceRestart(args []string) {
	fs := flag.NewFlagSet("service-restart", flag.ExitOnError)
	svcName := fs.String("service", serviceName, "nombre del servicio a arrancar")
	parent := fs.String("parent", "", "handle heredable del proceso a esperar (hex)")
	_ = fs.Parse(args)

	if *parent != "" {
		if v, err := strconv.ParseUint(*parent, 16, 64); err == nil && v != 0 {
			_, _ = windows.WaitForSingleObject(windows.Handle(v), uint32((60 * time.Second).Milliseconds()))
		}
	} else {
		// Sin handle: margen fijo para que el proceso antiguo termine.
		time.Sleep(2 * time.Second)
	}
	startServiceRetry(*svcName, 30*time.Second)
	os.Exit(0)
}

// startServiceRetry arranca el servicio con reintentos: entre la muerte del
// proceso y el estado STOPPED del SCM puede haber un instante, y Start puede
// fallar por estado transitorio. «Ya está en marcha» también es éxito.
func startServiceRetry(name string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		m, err := mgr.Connect()
		if err == nil {
			s, serr := m.OpenService(name)
			if serr == nil {
				err = s.Start()
				_ = s.Close()
				m.Disconnect()
				if err == nil || isAlreadyRunning(err) {
					return
				}
			} else {
				m.Disconnect()
				err = serr
			}
		}
		if time.Now().After(deadline) {
			// El helper corre desacoplado y sin logger: si no lo consigue,
			// sale con error y las acciones de recuperación del SCM (para
			// crashes reales) cubren el caso residual.
			os.Exit(1)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func isAlreadyRunning(err error) bool {
	var errno windows.Errno
	if errors.As(err, &errno) {
		return errno == windows.ERROR_SERVICE_ALREADY_RUNNING
	}
	return false
}
