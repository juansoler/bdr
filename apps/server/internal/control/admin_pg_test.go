package control_test

import (
	"net/http"
	"testing"
)

func TestPGEndpointsValidaciones(t *testing.T) {
	srv, targetID, agentID := newSQLTestAPI(t) // mismo harness: target + agente
	defer srv.Close()

	// Dump sin database.
	status, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/pg/dump", adminToken,
		map[string]any{"agent_id": agentID, "storage_target_id": targetID})
	if status != http.StatusBadRequest {
		t.Fatalf("dump sin database: %d", status)
	}

	// Dump con agente desconocido.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/pg/dump", adminToken,
		map[string]any{"agent_id": "nope", "database": "app", "storage_target_id": targetID})
	if status != http.StatusBadRequest {
		t.Fatalf("dump agente desconocido: %d", status)
	}

	// Dump feliz.
	status, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/pg/dump", adminToken,
		map[string]any{"agent_id": agentID, "host": "localhost", "database": "app",
			"storage_target_id": targetID})
	if status != http.StatusAccepted {
		t.Fatalf("dump enqueue: %d %s", status, body)
	}

	// Restore destructivo sin confirm.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/pg/restore", adminToken,
		map[string]any{"agent_id": agentID, "database": "app", "repo_uri": "s3://bdr/pg/app.dump"})
	if status != http.StatusBadRequest {
		t.Fatalf("restore sin confirm: %d", status)
	}

	// Restore con confirm.
	status, body = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/pg/restore", adminToken,
		map[string]any{"agent_id": agentID, "database": "app", "repo_uri": "s3://bdr/pg/app.dump", "confirm": true})
	if status != http.StatusAccepted {
		t.Fatalf("restore enqueue: %d %s", status, body)
	}

	// Verify de un URI inexistente.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/pg/verify", adminToken,
		map[string]any{"repo_uri": "s3://bdr/pg/nope.dump"})
	if status != http.StatusNotFound {
		t.Fatalf("verify URI inexistente: %d", status)
	}

	// Inventory sin agent_id.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/admin/pg/inventory", adminToken,
		map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("inventory sin agent_id: %d", status)
	}

	// Lista de puntos vacía pero OK.
	status, body = doJSON(t, http.MethodGet, srv.URL+"/api/v1/admin/pg/restore-points", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("restore-points: %d %s", status, body)
	}
}
