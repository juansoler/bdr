//go:build windows

// Default de staging en Windows: el directorio de backups propio de SQL
// Server (BackupDirectory del registro). SQL Server escribe ahí por
// definición, lo que evita el "Acceso denegado" (error 3201/OS 5) cuando
// el agente corre como LocalSystem y el staging cae en su temp
// (C:\WINDOWS\SystemTemp no es escribible por la cuenta del servicio SQL).
// Hallazgo del lab P-24 (Win11 + SQL Server 2022).
package jobs

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func sqlStagingDefault() string {
	if dir := sqlBackupDirFromRegistry(""); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "bdr-sql-staging")
}

// sqlBackupDirFromRegistry lee el BackupDirectory de la instancia pedida
// ("" = la por defecto). Devuelve "" si no la encuentra.
func sqlBackupDirFromRegistry(instance string) string {
	keys, err := sqlInstanceKeys()
	if err != nil || len(keys) == 0 {
		return ""
	}
	key := pickSQLKey(keys, instance)
	if key == "" {
		return ""
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Microsoft SQL Server\`+key+`\MSSQLServer`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("BackupDirectory")
	if err != nil || strings.TrimSpace(v) == "" {
		return ""
	}
	return strings.TrimSpace(v)
}

// sqlInstanceKeys enumera las claves "MSSQL<n>.<instancia>" del registro
// (p. ej. MSSQL16.MSSQLSERVER, MSSQL16.SQLEXPRESS).
func sqlInstanceKeys() ([]string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Microsoft SQL Server`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, "MSSQL.") {
			out = append(out, n)
		}
	}
	return out, nil
}
