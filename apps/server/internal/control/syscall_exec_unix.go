//go:build !windows

// Re-ejecución del binario en Unix: reemplaza la imagen del proceso
// manteniendo el PID (systemd/launchd ni siquiera perciben el cambio).
package control

import (
	"syscall"
)

func syscallExec(path string, args, env []string) error {
	return syscall.Exec(path, args, env)
}
