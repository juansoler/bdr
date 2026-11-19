//go:build windows

// Integración del control con el Service Control Manager de Windows: el
// servicio debe responder al SCM (StartServiceCtrlDispatcher) en los 30 s
// posteriores al arranque; si no, Windows lo declara fallido (error 1053).
// Por eso runControl corre bajo svc.Run cuando el proceso fue iniciado por
// el gestor de servicios, y directamente en consola cuando se ejecuta a mano.
package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/juansoler/bdr/packages/logging"

	"golang.org/x/sys/windows/svc"
)

// controlServiceName es el nombre del servicio creado por el instalador.
const controlServiceName = "bdr-control"

// isWindowsService indica si el SCM arrancó este proceso.
func isWindowsService() bool {
	inSvc, err := svc.IsWindowsService()
	return err == nil && inSvc
}

// serviceLogger en modo servicio la salida estándar no está atada a ninguna
// consola: los logs van a <data-dir>\control.log para poder diagnosticar.
func serviceLogger(dataDir, level string, fallback *logging.Logger) *logging.Logger {
	f, err := os.OpenFile(filepath.Join(dataDir, "control.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fallback
	}
	return logging.NewWithWriter(f, level)
}

// runWindowsService corre el plano de control bajo el SCM.
func runWindowsService(o controlOpts) error {
	return svc.Run(controlServiceName, &controlHandler{opts: o})
}

// controlHandler implementa svc.Handler: reporta estados al SCM y traduce
// Stop/Shutdown a cancelación del contexto de runControl.
type controlHandler struct {
	opts controlOpts
}

func (h *controlHandler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runControl(ctx, h.opts) }()
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
			// El servidor terminó solo: error → el SCM lo ve como fallo y
			// aplicará las acciones de recuperación (restart).
			changes <- svc.Status{State: svc.StopPending}
			if err != nil {
				return false, 1
			}
			return false, 0
		}
	}
}
