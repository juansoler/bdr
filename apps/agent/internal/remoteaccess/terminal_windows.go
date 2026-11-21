//go:build windows

// Package remoteaccess implementa un stub para Windows (ConPTY).
// En Fase 1 no hay implementación real; se proporciona solo para que el
// código compile en Windows y las sesiones fallen con un error explícito.
package remoteaccess

import (
	"errors"
	"os"
	"os/exec"
)

// StartShell devuelve un error indicando que ConPTY no está implementado
// en Fase 1.
func StartShell(shell string, cols, rows uint16) (*os.File, *exec.Cmd, error) {
	return nil, nil, errors.New("ConPTY no implementado en Fase 1; terminal remota disponible en Linux/macOS")
}

// ResizeTerminal devuelve error en Fase 1.
func ResizeTerminal(master *os.File, cols, rows uint16) error {
	return errors.New("ConPTY no implementado en Fase 1")
}

// signalFromName mapea un nombre de señal a su equivalente en Windows.
// Solo INT (console Ctrl-C) está soportada; el resto devuelve nil y el
// emisor lo ignora.
func signalFromName(name string) os.Signal {
	if name == "INT" {
		return os.Interrupt
	}
	return nil
}
