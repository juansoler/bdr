//go:build !windows

package jobs

import (
	"fmt"

	pkgimg "github.com/juansoler/bdr/packages/image"
)

// enumerateDisksWindows no existe fuera de Windows: la enumeración nativa
// con DeviceIoControl solo compila en GOOS=windows. El dispatcher de
// enumerateDisks solo la llama en Windows.
func enumerateDisksWindows() ([]pkgimg.Disk, error) {
	return nil, fmt.Errorf("enumeración de discos Windows no disponible en este sistema")
}
