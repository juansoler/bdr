package connector

import (
	"net/url"
	"strings"
	"testing"
)

func TestParseLSN(t *testing.T) {
	cases := []struct {
		nombre string
		in     string
		want   LSN
		falla  bool
	}{
		{"cero", "0", 0, false},
		{"valor simple", "42", 42, false},
		{"con espacios", " 7 ", 7, false},
		{"vacío", "", 0, false},
		{"máximo uint64", "18446744073709551615", 18446744073709551615, false},
		{"desborda uint64", "18446744073709551616", 0, true},
		{"no numérico", "abc", 0, true},
		{"negativo", "-1", 0, true},
	}

	for _, c := range cases {
		t.Run(c.nombre, func(t *testing.T) {
			got, err := parseLSN(c.in)
			if c.falla {
				if err == nil {
					t.Fatalf("parseLSN(%q) debería fallar; devolvió %d", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLSN(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("parseLSN(%q) = %d; esperaba %d", c.in, got, c.want)
			}
		})
	}
}

// Lab P-24 (hallazgo real): MSSQLSERVER es la instancia por defecto y no
// se nombra; "host\instancia" en server también debe funcionar.
func TestNormalizeParamsTrampasDeInstancia(t *testing.T) {
	cases := []struct {
		nombre       string
		server, inst string
		wantHost     string
		wantInstance string
	}{
		{"MSSQLSERVER en instance es default", "localhost", "MSSQLSERVER", "localhost", ""},
		{"MSSQLSERVER lowercase", "localhost", "mssqlserver", "localhost", ""},
		{"host\\MSSQLSERVER en server", `localhost\MSSQLSERVER`, "", "localhost", ""},
		{"host\\SQLEXPRESS en server", `localhost\SQLEXPRESS`, "", "localhost", "SQLEXPRESS"},
		{"instancia normal intacta", "10.0.0.5", "SQLEXPRESS", "10.0.0.5", "SQLEXPRESS"},
		{"por defecto ya limpio", "sqlhost", "", "sqlhost", ""},
	}
	for _, c := range cases {
		t.Run(c.nombre, func(t *testing.T) {
			got := ConnectionParams{Server: c.server, Instance: c.inst}.normalized()
			if got.Server != c.wantHost || got.Instance != c.wantInstance {
				t.Fatalf("got host=%q inst=%q; want %q/%q", got.Server, got.Instance, c.wantHost, c.wantInstance)
			}
		})
	}

	// El DSN de la instancia por defecto no lleva nombre de instancia.
	dsn := buildDSN(ConnectionParams{Server: "localhost", Instance: "MSSQLSERVER"})
	if strings.Contains(dsn, "MSSQLSERVER") {
		t.Errorf("el DSN no debe llevar MSSQLSERVER: %s", dsn)
	}
}

func TestBuildDSNEscapaCredenciales(t *testing.T) {
	dsn := buildDSN(ConnectionParams{
		Server:   "10.0.0.5",
		Instance: "SQLEXPRESS",
		User:     "sa",
		Password: "p@ss/w:rd", // caracteres que romperían una URL sin escapar
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN no parseable %q: %v", dsn, err)
	}
	if u.User.Username() != "sa" {
		t.Errorf("usuario = %q; esperaba sa", u.User.Username())
	}
	pass, ok := u.User.Password()
	if !ok || pass != "p@ss/w:rd" {
		t.Errorf("contraseña tras round-trip = %q (ok=%v); esperaba p@ss/w:rd", pass, ok)
	}
	if !strings.Contains(u.Host, "10.0.0.5") || !strings.Contains(u.Path, "SQLEXPRESS") {
		t.Errorf("host/instancia incorrectos en %q", dsn)
	}
	if !strings.Contains(u.RawQuery, "encrypt=disable") {
		t.Errorf("falta encrypt=disable en %q", dsn)
	}
}

func TestBuildDSNWindowsAuth(t *testing.T) {
	dsn := buildDSN(ConnectionParams{Server: "sqlhost", Instance: "SQLEXPRESS"})
	if strings.Contains(dsn, "@") {
		t.Errorf("Windows Auth no debe llevar userinfo: %s", dsn)
	}
	if !strings.Contains(dsn, "trusted=yes") {
		t.Errorf("Windows Auth sin trusted=yes: %s", dsn)
	}
}
