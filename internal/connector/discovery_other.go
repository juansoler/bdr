//go:build !windows

package connector

import (
	"context"
	"fmt"
	"runtime"
)

// Discover no está soportado fuera de Windows: el registro no existe.
// En esos entornos el operador registra las instancias manualmente.
func Discover(_ context.Context) ([]Instance, error) {
	return nil, fmt.Errorf("el descubrimiento por registro solo está disponible en Windows (plataforma actual: %s)", runtime.GOOS)
}
