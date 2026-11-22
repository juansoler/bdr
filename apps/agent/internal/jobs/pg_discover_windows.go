//go:build windows

// Detección PostgreSQL en Windows: claves de servicio del registro con
// nombre postgresql-* (los instaladores EDB crean postgresql-x64-<ver>).
package jobs

import (
	"context"
	"strings"

	"github.com/juansoler/bdr/packages/contracts"

	"golang.org/x/sys/windows/registry"
)

func pgDiscoverLocal(ctx context.Context) ([]contracts.PGInstanceView, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var items []contracts.PGInstanceView
	for _, n := range names {
		if !strings.Contains(strings.ToLower(n), "postgres") {
			continue
		}
		sk, err := registry.OpenKey(registry.LOCAL_MACHINE,
			`SYSTEM\CurrentControlSet\Services\`+n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		image, _, _ := sk.GetStringValue("ImagePath")
		sk.Close()
		// Versión del directorio de instalación (…\16\bin\pg_ctl.exe).
		version := ""
		for _, part := range strings.Split(image, `\`) {
			if len(part) == 2 && allDigits(part) {
				version = part
			}
		}
		items = append(items, contracts.PGInstanceView{
			Service: n,
			Port:    portFromServiceName(n),
			Version: version,
		})
	}
	return items, nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
