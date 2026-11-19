//go:build !windows

// Equivalente Unix del modo servicio: sin SCM que atender, runControl corre
// directamente en el proceso actual (systemd o launchd gestionan el ciclo).
package main

import (
	"errors"

	"github.com/juansoler/bdr/packages/logging"
)

func isWindowsService() bool { return false }

func serviceLogger(_ string, _ string, fallback *logging.Logger) *logging.Logger {
	return fallback
}

// runWindowsService no aplica en Unix.
func runWindowsService(_ controlOpts) error {
	return errors.New("modo servicio solo disponible en Windows")
}
