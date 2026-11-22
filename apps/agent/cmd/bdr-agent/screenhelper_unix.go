//go:build !windows

// Comando interno `screenhelper`: solo existe en Windows (helper de sesión
// GDI/SendInput). En las demás plataformas no tiene función.
package main

import (
	"fmt"
	"os"
)

func cmdScreenHelper() {
	fmt.Fprintln(os.Stderr, "screenhelper solo está disponible en Windows")
	os.Exit(2)
}
