package control_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juansoler/bdr/packages/contracts"
)

// newSQLTestAPI monta la API con destino de storage y un agente enrolado,
// listos para los endpoints SQL.
func newSQLTestAPI(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	srv, _ := newStorageTestAPI(t, t.TempDir(), "/bin/true")

	req := contracts.StorageTargetRequest{
		Name: "s3-sql", Endpoint: "203.0.113.5:9000", Bucket: "bdr-sql",
		AccessKey: "ak", SecretKey: "sk", NoTLS: true,
		ObjectLock: true, RetentionDays: 7, Enabled: true,
	}
	status, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/storage/targets", adminToken, req)
	if status != http.StatusCreated {
		t.Fatalf("crear target: %d %s", status, body)
	}
	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("respuesta de target: %s", body)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("target sin id: %s", body)
	}

	token := createToken(t, srv, "sqlhost").Token
	enrolled := enrollAgent(t, srv, token, "sqlhost")
	return srv, id, enrolled.AgentID
}

func TestSQLBackupValidaciones(t *testing.T) {
	srv, targetID, agentID := newSQLTestAPI(t)
	defer srv.Close()

	// Faltan campos obligatorios.
	status, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/backup", adminToken,
		map[string]any{"agent_id": agentID})
	if status != http.StatusBadRequest {
		t.Fatalf("backup sin server/database: %d", status)
	}

	// Agente desconocido.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/backup", adminToken,
		map[string]any{"agent_id": "nope", "server": "sqlhost", "database": "app", "storage_target_id": targetID})
	if status != http.StatusBadRequest {
		t.Fatalf("backup con agente desconocido: %d", status)
	}

	// Tipo no soportado.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/backup", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app",
			"type": "incremental", "storage_target_id": targetID})
	if status != http.StatusBadRequest {
		t.Fatalf("backup tipo incremental: %d", status)
	}

	// Restore destructivo sin confirm.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/restore", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app",
			"files": []string{"s3://bdr/sql/app_FULL.bak"}})
	if status != http.StatusBadRequest {
		t.Fatalf("restore sin confirm: %d", status)
	}

	// Restore con modo inválido (aun con confirm).
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/restore", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app", "confirm": true,
			"files": []string{"s3://bdr/sql/app_FULL.bak"}, "mode": "Standby"})
	if status != http.StatusBadRequest {
		t.Fatalf("restore modo inválido: %d", status)
	}

	// Verify sin repo_uri.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/verify", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost"})
	if status != http.StatusBadRequest {
		t.Fatalf("verify sin repo_uri: %d", status)
	}

	// Inventory sin server.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/inventory", adminToken,
		map[string]any{"agent_id": agentID})
	if status != http.StatusBadRequest {
		t.Fatalf("inventory sin server: %d", status)
	}

	// CheckDB sin database.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/checkdb", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost"})
	if status != http.StatusBadRequest {
		t.Fatalf("checkdb sin database: %d", status)
	}
}

func TestSQLBackupEnqueueYRestorePoints(t *testing.T) {
	srv, targetID, agentID := newSQLTestAPI(t)
	defer srv.Close()

	// Enqueue feliz: copy-only se normaliza y el job queda pendiente.
	status, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/backup", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app",
			"type": "copy-only", "storage_target_id": targetID})
	if status != http.StatusAccepted {
		t.Fatalf("backup enqueue: %d %s", status, body)
	}
	var resp struct {
		JobID string `json:"job_id"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.JobID == "" || resp.State != "pending" {
		t.Fatalf("respuesta de enqueue: %s (%v)", body, err)
	}

	// Restore aceptado con confirm y modo explícito.
	status, body = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/restore", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app", "confirm": true,
			"files": []string{"s3://bdr/sql/app_FULL_1.bak", "s3://bdr/sql/app_LOG_2.trn"},
			"mode":  "norecovery"})
	if status != http.StatusAccepted {
		t.Fatalf("restore enqueue: %d %s", status, body)
	}

	// Verify e inventory también encolan.
	status, body = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/verify", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "repo_uri": "s3://bdr/sql/app_FULL_1.bak"})
	if status != http.StatusAccepted {
		t.Fatalf("verify enqueue: %d %s", status, body)
	}
	status, body = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/inventory", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost"})
	if status != http.StatusAccepted {
		t.Fatalf("inventory enqueue: %d %s", status, body)
	}

	// Los puntos de restauración SQL empiezan vacíos (el punto se crea al
	// ingestar el resultado del agente, ver recordSQLRestorePoint).
	status, body = doJSON(t, http.MethodGet, srv.URL+"/api/v1/admin/sql/restore-points?agent_id="+agentID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("restore-points: %d %s", status, body)
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("restore-points parse: %s", body)
	}
	if len(list.Items) != 0 {
		t.Fatalf("restore-points debería estar vacío; hay %d", len(list.Items))
	}
}

func TestSQLPlanesCRUD(t *testing.T) {
	srv, targetID, agentID := newSQLTestAPI(t)
	defer srv.Close()

	// Crear con cron válido.
	status, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/plans", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app",
			"type": "diff", "schedule": "0 3 * * *", "timezone": "UTC",
			"storage_target_id": targetID, "retention_days": 14})
	if status != http.StatusCreated {
		t.Fatalf("crear plan: %d %s", status, body)
	}
	var plan contracts.SQLPlanView
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("plan parse: %s", body)
	}
	if plan.Type != "DIFFERENTIAL" || !plan.Enabled || !plan.Verify {
		t.Fatalf("plan con defaults incorrectos: %+v", plan)
	}
	if plan.NextRunAt == nil || plan.ID == "" {
		t.Fatalf("plan sin id o next_run_at: %+v", plan)
	}

	// Cron inválido rechazado.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/plans", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app",
			"schedule": "todos los días", "storage_target_id": targetID})
	if status != http.StatusBadRequest {
		t.Fatalf("cron inválido aceptado: %d", status)
	}

	// Destino inexistente rechazado.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/sql/plans", adminToken,
		map[string]any{"agent_id": agentID, "server": "sqlhost", "database": "app",
			"schedule": "0 3 * * *", "storage_target_id": "nope"})
	if status != http.StatusBadRequest {
		t.Fatalf("destino inexistente aceptado: %d", status)
	}

	// Listado contiene el plan.
	status, body = doJSON(t, http.MethodGet, srv.URL+"/api/v1/admin/sql/plans", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar planes: %d %s", status, body)
	}
	var list struct {
		Items []contracts.SQLPlanView `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("listado: %d items (%v)", len(list.Items), err)
	}

	// Update: deshabilitar y cambiar horario.
	status, body = doJSON(t, http.MethodPut, srv.URL+"/api/v1/admin/sql/plans/"+plan.ID, adminToken,
		map[string]any{"enabled": false, "schedule": "30 4 * * *"})
	if status != http.StatusOK {
		t.Fatalf("update plan: %d %s", status, body)
	}
	var upd contracts.SQLPlanView
	if err := json.Unmarshal(body, &upd); err != nil {
		t.Fatalf("update parse: %s", body)
	}
	if upd.Enabled || upd.Schedule != "30 4 * * *" || upd.Type != "DIFFERENTIAL" {
		t.Fatalf("update no aplicado: %+v", upd)
	}

	// Delete + listado vacío.
	status, _ = doJSON(t, http.MethodDelete, srv.URL+"/api/v1/admin/sql/plans/"+plan.ID, adminToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete plan: %d", status)
	}
	status, _ = doJSON(t, http.MethodDelete, srv.URL+"/api/v1/admin/sql/plans/"+plan.ID, adminToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("delete repetido: %d", status)
	}
	status, body = doJSON(t, http.MethodGet, srv.URL+"/api/v1/admin/sql/plans", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar tras delete: %d %s", status, body)
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Items) != 0 {
		t.Fatalf("listado tras delete: %d items (%v)", len(list.Items), err)
	}
}
