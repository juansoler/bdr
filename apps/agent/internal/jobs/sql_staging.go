// Staging del motor SQL: la carpeta donde SQL Server escribe los .bak/.trn
// y de la que el agente lee para transferir. Debe ser accesible por AMBAS
// cuentas: la del servicio SQL (escritura) y la del agente (lectura).
//
// Orden de resolución (resolveSQLStaging, en sql_runner.go):
//  1. BDR_SQL_STAGING (elección explícita; recomendada en producción para
//     separar el I/O de backups del disco del sistema).
//  2. El BackupDirectory que reporta la PROPIA instancia vía
//     connector.DB.BackupDirectory (xp_instance_regread + ruta de datos
//     como fallback): SQL escribe ahí por definición.
//  3. Temp del proceso (último recurso; en Windows puede no ser escribible
//     por la cuenta del servicio SQL — hallazgo del lab P-24).
package jobs

import (
	"os"
	"path/filepath"
)

// sqlStagingDefault es el último recurso cuando ni la env BDR_SQL_STAGING
// ni la instancia reportan un directorio: el temp del proceso.
func sqlStagingDefault() string {
	return filepath.Join(os.TempDir(), "bdr-sql-staging")
}
