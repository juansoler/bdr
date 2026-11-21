// Package remoteaccess implementa un stub para Windows (ConPTY).
// En Fase 1 no hay implementación real; se proporciona solo para que el
// código compile en Windows.
//
//go:build windows
// +build windows

package remoteaccess

import (
	"errors"
)

// StartShell devuelve un error indicando que ConPTY no está implementado
// en Fase 1.
func StartShell(shell string, cols, rows uint16) (*os.File, *exec.Cmd, error) {
	return nil, nil, errors.New("ConPTY no implementado en Fase 1; use terminal:standard en Linux/macOS")
}

// ResizeTerminal devuelve error en Fase 1.
func ResizeTerminal(master *os.File, cols, rows uint16) error {
	return errors.New("ConPTY no implementado en Fase 1")
}