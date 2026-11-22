// Staging del motor SQL: la carpeta donde SQL Server escribe los .bak/.trn
// y de la que el agente lee para transferir. Debe ser accesible por AMBAS
// cuentas: la del servicio SQL (escritura) y la del agente (lectura).
package jobs

import "os"

// sqlStagingRoot devuelve la carpeta de staging: la variable
// BDR_SQL_STAGING si está definida, o el valor por defecto de la
// plataforma (sqlStagingDefault).
func sqlStagingRoot() string {
	if p := os.Getenv("BDR_SQL_STAGING"); p != "" {
		return p
	}
	return sqlStagingDefault()
}

// pickSQLKey elige de una lista de claves de registro "MSSQL<n>.<instancia>"
// la que corresponde a la instancia pedida ("" = MSSQLSERVER). Si no está,
// devuelve la única disponible (host de una instancia). Función portable
// para poder testearla en cualquier plataforma.
func pickSQLKey(keys []string, instance string) string {
	want := instance
	if want == "" {
		want = "MSSQLSERVER"
	}
	for _, k := range keys {
		if i := lastIndexByte(k, '.'); i >= 0 && equalFoldASCII(k[i+1:], want) {
			return k
		}
	}
	if len(keys) == 1 {
		return keys[0]
	}
	return ""
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 32
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
