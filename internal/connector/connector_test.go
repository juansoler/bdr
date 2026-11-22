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
		{"cero", "0", "0", false},
		{"valor simple", "42", "42", false},
		{"con espacios", " 7 ", "7", false},
		{"vacío", "", "", false},
		{"máximo uint64", "18446744073709551615", "18446744073709551615", false},
		{"LSN real de 21 dígitos (P-26)", "338420000000652000001", "338420000000652000001", false},
		{"máximo numeric(25,0)", "9999999999999999999999999", "9999999999999999999999999", false},
		{"no numérico", "abc", "", true},
		{"negativo", "-1", "", true},
		{"con decimales", "12.5", "", true},
	}

	for _, c := range cases {
		t.Run(c.nombre, func(t *testing.T) {
			got, err := parseLSN(c.in)
			if c.falla {
				if err == nil {
					t.Fatalf("parseLSN(%q) debería fallar; devolvió %q", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLSN(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("parseLSN(%q) = %q; esperaba %q", c.in, got, c.want)
			}
		})
	}
}

func TestLSNCompare(t *testing.T) {
	cases := []struct {
		a, b LSN
		want int
	}{
		{"100", "200", -1},
		{"200", "100", 1},
		{"200", "200", 0},
		{"", "0", 0},  // vacío = 0
		{"", "1", -1}, // vacío = 0 < 1
		{"338420000000652000001", "338420000000652000002", -1}, // 21 dígitos
		{"9999999999999999999999999", "10000000000000000000000000", -1},
	}
	for _, c := range cases {
		if got := c.a.Compare(c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d; esperaba %d", c.a, c.b, got, c.want)
		}
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
