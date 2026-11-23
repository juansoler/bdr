// El auto-actualizador no debe depender de /tmp: en despliegues endurecidos
// (ProtectSystem=strict, contenedores read-only) es de solo lectura y los
// temporales van junto al binario del control.
package selfupdate

import (
	"path/filepath"
	"testing"
)

func TestWritableTempDir(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bdr-control")
	if got := writableTempDir(exe); got != dir {
		t.Fatalf("writableTempDir = %q; quiero el directorio del binario %q", got, dir)
	}
	// Binario en un directorio inexistente: cae al /tmp del sistema.
	if got := writableTempDir("/no-existe/bdr-control"); got == "" {
		t.Fatal("debería caer al /tmp del sistema")
	}
}
