//go:build windows

package connector

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// Discover localiza las instancias de SQL Server registradas en Windows.
// Fuente: HKLM\SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL
func Discover(_ context.Context) ([]Instance, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL`,
		registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("no hay instancias SQL registradas (¿SQL Server instalado?): %w", err)
	}
	defer key.Close()

	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, fmt.Errorf("leer nombres de instancia del registro: %w", err)
	}

	instances := make([]Instance, 0, len(names))
	for _, name := range names {
		inst := Instance{Name: name}
		// El valor asociado es la clave interna (ej. MSSQL16.MSSQLSERVER)
		if v, _, err := key.GetStringValue(name); err == nil {
			inst.Key = v
		}
		inst.Default = inst.Name == "MSSQLSERVER"

		if inst.Key != "" {
			inst.Version, inst.Edition, inst.SQLPath = readSetup(inst.Key)
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// readSetup lee versión, edición y ruta de instalación de una instancia.
func readSetup(instanceKey string) (version, edition, sqlPath string) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Microsoft SQL Server\`+instanceKey+`\Setup`,
		registry.QUERY_VALUE)
	if err != nil {
		return "", "", ""
	}
	defer k.Close()
	version, _, _ = k.GetStringValue("Version")
	edition, _, _ = k.GetStringValue("Edition")
	sqlPath, _, _ = k.GetStringValue("SQLPath")
	return version, edition, sqlPath
}
