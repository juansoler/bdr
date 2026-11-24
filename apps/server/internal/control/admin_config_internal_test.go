package control_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/juansoler/bdr/packages/contracts"
)

// TestConfigBackupExportAndRestore verifica el ciclo completo de copia de
// seguridad de configuración: exportar un plan SQL programado + agente +
// destino y restaurarlo en un segundo servidor limpio.
func TestConfigBackupExportAndRestore(t *testing.T) {
	srv, targetID, agentID := newSQLTestAPI(t)
	defer srv.Close()

	// Crear un plan SQL programado para que la exportación lo recoja.
	status, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/plans", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app",
			"type": "full", "schedule": "0 3 * * *", "timezone": "Europe/Madrid",
			"storage_target_id": targetID, "retention_days": 14, "verify": true})
	if status != http.StatusCreated {
		t.Fatalf("crear plan: %d %s", status, body)
	}

	status, body = doJSON(t, http.MethodGet, srv.URL+"/api/v1/admin/config/backup", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("backup config: %d %s", status, body)
	}
	var snap contracts.ConfigBackup
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("json backup config: %v (%s)", err, body)
	}
	if snap.SchemaVersion != contracts.ConfigBackupSchemaVersion {
		t.Fatalf("schema_version: %d", snap.SchemaVersion)
	}
	if len(snap.SQLPlans) != 1 {
		t.Fatalf("SQLPlans exportados: %d", len(snap.SQLPlans))
	}
	if snap.SQLPlans[0].Schedule != "0 3 * * *" || snap.SQLPlans[0].Database != "app" {
		t.Fatalf("plan exportado incorrecto: %+v", snap.SQLPlans[0])
	}
	if len(snap.Storage) == 0 {
		t.Fatal("backup sin destinos de almacenamiento")
	}
	if len(snap.Agents) == 0 {
		t.Fatal("backup sin agentes enrolados")
	}
	if len(snap.Plans) != 0 {
		t.Fatalf("planes genéricos inesperados: %d", len(snap.Plans))
	}

	// Restaurar en un segundo servidor limpio (simula migración).
	srv2, _, _ := newSQLTestAPI(t)
	defer srv2.Close()
	status, body = doJSON(t, http.MethodPost, srv2.URL+"/api/v1/admin/config/restore", adminToken, snap)
	if status != http.StatusOK {
		t.Fatalf("restore config: %d %s", status, body)
	}
	var restored struct {
		Restored bool `json:"restored"`
	}
	if err := json.Unmarshal(body, &restored); err != nil || !restored.Restored {
		t.Fatalf("respuesta de restore: %s (%v)", body, err)
	}

	status, body = doJSON(t, http.MethodGet, srv2.URL+"/api/v1/admin/config/backup", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("backup config (destino): %d %s", status, body)
	}
	var snap2 contracts.ConfigBackup
	if err := json.Unmarshal(body, &snap2); err != nil {
		t.Fatalf("json backup config (destino): %v (%s)", err, body)
	}
	if len(snap2.SQLPlans) != 1 || snap2.SQLPlans[0].Schedule != "0 3 * * *" {
		t.Fatalf("el restore no aplicó el plan: %+v", snap2.SQLPlans)
	}
	if len(snap2.Storage) == 0 {
		t.Fatal("el restore no aplicó los destinos de almacenamiento")
	}
}

func TestConfigRestoreSchemaVersion(t *testing.T) {
	srv, _, _ := newSQLTestAPI(t)
	defer srv.Close()

	bad := contracts.ConfigBackup{SchemaVersion: 999}
	status, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/config/restore", adminToken, bad)
	if status != http.StatusBadRequest {
		t.Fatalf("restore con schema_version inválido: %d", status)
	}
}
