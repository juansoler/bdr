//go:build windows

// Comando interno `screenhelper`: proceso de sesión para el acceso por
// pantalla en Windows. Lo lanza el servicio (nunca se usa a mano).
package main

import (
	"fmt"
	"os"

	"github.com/juansoler/bdr/apps/agent/internal/remoteaccess"
)

func cmdScreenHelper() {
	if err := remoteaccess.RunScreenHelper(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
