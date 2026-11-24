package jobs

import (
	"strings"
	"testing"
	"time"
)

// sqlRelDir estructura el destino de los medios SQL como
// <máquina-agente>/<instancia>/<fecha>, normalizando la instancia
// predeterminada a MSSQLSERVER y sanitizando los componentes.
func TestSQLRelDir(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 30, 0, 0, time.UTC)

	t.Run("instancia vacía → MSSQLSERVER", func(t *testing.T) {
		dir := sqlRelDir("", now)
		parts := strings.Split(dir, "/")
		if len(parts) != 3 {
			t.Fatalf("dir = %q; esperaba 3 segmentos", dir)
		}
		if parts[0] == "" {
			t.Error("máquina vacía")
		}
		if parts[1] != "MSSQLSERVER" {
			t.Errorf("instancia = %q; esperaba MSSQLSERVER", parts[1])
		}
		if parts[2] != "2026-11-24" {
			t.Errorf("fecha = %q; esperaba 2026-11-24", parts[2])
		}
	})

	t.Run("instancia con nombre", func(t *testing.T) {
		dir := sqlRelDir("SQLEXPRESS", now)
		if !strings.HasSuffix(dir, "/SQLEXPRESS/2026-11-24") {
			t.Errorf("dir = %q; esperaba sufijo /SQLEXPRESS/2026-11-24", dir)
		}
	})

	t.Run("caracteres inválidos se sanitizan", func(t *testing.T) {
		dir := sqlRelDir("INST?1", now)
		if !strings.Contains(dir, "/INST_1/") {
			t.Errorf("dir = %q; esperaba segmento INST_1", dir)
		}
	})
}
