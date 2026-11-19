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
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/juansoler/bdr/apps/agent/internal/agent"
	"github.com/juansoler/bdr/packages/logging"

	"golang.org/x/sys/windows/svc"
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

// runAgentProcess ejecuta el bucle bajo el SCM o en consola.
func runAgentProcess(ctx context.Context, ag *agent.Agent, once bool) error {
	if !isWindowsService() {
		return ag.Run(ctx, once)
	}
	return svc.Run(serviceName, &bdrHandler{run: func(ctx context.Context) error {
		return ag.Run(ctx, once)
	}})
}

// bdrHandler implementa svc.Handler: reporta estados al SCM y traduce
// Stop/Shutdown a cancelación del contexto.
type bdrHandler struct {
	run func(ctx context.Context) error
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

// restartSelf reemplaza el proceso por el binario nuevo. En servicio:
// sale con error sin reportar Stopped → el SCM lo considera fallo y su
// acción de recuperación (restart 5 s) levanta el binario nuevo. En consola:
// arranca el nuevo desacoplado y termina.
func restartSelf(newExe string) error {
	if isWindowsService() {
		os.Exit(1)
	}
	cmd := exec.Command(newExe, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
