package jobs

import "testing"

func TestPickSQLKey(t *testing.T) {
	keys := []string{"110", "MSSQL16.MSSQLSERVER", "MSSQL16.SQLEXPRESS", "SQLServerManager16"}
	cases := []struct {
		nombre   string
		instance string
		want     string
	}{
		{"por defecto", "", "MSSQL16.MSSQLSERVER"},
		{"por defecto mayúsculas", "mssqlserver", "MSSQL16.MSSQLSERVER"},
		{"instancia con nombre", "SQLEXPRESS", "MSSQL16.SQLEXPRESS"},
		{"instancia con nombre lowercase", "sqlexpress", "MSSQL16.SQLEXPRESS"},
	}
	for _, c := range cases {
		t.Run(c.nombre, func(t *testing.T) {
			if got := pickSQLKey(keys, c.instance); got != c.want {
				t.Fatalf("got %q; want %q", got, c.want)
			}
		})
	}

	// Host de una única instancia: la elige aunque no coincida el nombre.
	solo := []string{"MSSQL16.MIFARMA"}
	if got := pickSQLKey(solo, "MSSQLSERVER"); got != "MSSQL16.MIFARMA" {
		t.Fatalf("instancia única no elegida: %q", got)
	}

	// Varias sin coincidencia: sin decisión.
	if got := pickSQLKey(keys, "NOEXISTE"); got != "" {
		t.Fatalf("debería quedar vacío con múltiples y sin match: %q", got)
	}
}
