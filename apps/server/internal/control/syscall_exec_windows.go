//go:build windows

// En Windows un binario no puede sustituir su propio ejecutable: Apply
// devuelve error antes de llegar aquí. El stub existe para compilar.
package control

import "errors"

func syscallExec(path string, args, env []string) error {
	return errors.New("la re-ejecución in-place no está disponible en Windows; ejecuta el instalador de la release")
}
