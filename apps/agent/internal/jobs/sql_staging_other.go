//go:build !windows

// Default de staging fuera de Windows: temp del agente (flujo de lab).
// SQL Server no corre aquí; el agente de prueba usa el staging local.
package jobs

import (
	"os"
	"path/filepath"
)

func sqlStagingDefault() string {
	return filepath.Join(os.TempDir(), "bdr-sql-staging")
}
