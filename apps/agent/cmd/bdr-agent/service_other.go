//go:build !windows

// Equivalente Unix de la integración con servicios: sin SCM que atender,
// el bucle corre directamente y el reinicio tras autoactualización es una
// re-ejecución in-place (syscall.Exec) — mismo PID, systemd ni se entera.
package main

import (
	"context"
	"os"
	"syscall"

	"github.com/juansoler/bdr/apps/agent/internal/agent"
	"github.com/juansoler/bdr/packages/logging"
)

func isWindowsService() bool { return false }

func serviceLogger(_ string, _ string, fallback *logging.Logger) *logging.Logger {
	return fallback
}

func runAgentProcess(ctx context.Context, ag *agent.Agent, once bool) error {
	return ag.Run(ctx, once)
}

// restartSelf reemplaza la imagen del proceso por el binario nuevo: mismo
// PID, mismo descriptor del servicio systemd. Si exec falla (raro), se
// sale con error y systemd lo reinicia (Restart=always).
func restartSelf(newExe string) error {
	return syscall.Exec(newExe, os.Args, os.Environ())
}

// cmdServiceRestart es el helper interno de autoactualización de Windows:
// en Unix no tiene sentido (la re-ejecución es in-place).
func cmdServiceRestart([]string) {}
