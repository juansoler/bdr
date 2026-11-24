package control_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// TestJobCleanupIncomplete cubre la limpieza de objetos a medias de un
// backup fallido: GET previa y POST borrado. Regresión: el POST respondía
// 405 aunque ejecutaba el borrado, porque el handler llamaba dos veces a
// allowMethod (GET y POST por separado) y la primera escribía el 405.
func TestJobCleanupIncomplete(t *testing.T) {
	ts, st := newStorageTestAPI(t, t.TempDir(), "/bin/true")
	root := t.TempDir()
	targetID := createFSTarget(t, ts, root)

	prefix := "backup/Proxmox-VE/SQLSERVER/2026-11-23/vm"
	orphans := []string{"disk1-seg2446-aaa", "disk1-seg2447-bbb"}
	for _, k := range orphans {
		p := filepath.Join(root, filepath.FromSlash(prefix), k)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC()
	payload, _ := json.Marshal(map[string]string{"storage_target_id": targetID})
	result, _ := json.Marshal(map[string]any{
		"dest_prefix": prefix + "/",
		"error":       "E_IMAGE_UPLOAD: disco 1 seg 2446: respuesta del backend",
	})
	if err := st.Update(context.Background(), func(s *store.State) error {
		s.Jobs["j-clean"] = &store.Job{
			ID: "j-clean", Type: contracts.JobTypeBackupImage, State: store.JobFailed,
			AgentID: "a1", PlanID: "p1", Payload: payload, Result: result,
			CreatedAt: now, UpdatedAt: now,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	url := ts.URL + "/api/v1/admin/jobs/j-clean/cleanup-incomplete"

	// GET: vista previa con el número de objetos y bytes.
	status, body := doJSON(t, http.MethodGet, url, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET preview: %d %s", status, body)
	}
	var preview struct {
		Objects int   `json:"objects"`
		Bytes   int64 `json:"bytes"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatalf("preview ilegible: %s", body)
	}
	if preview.Objects != 2 || preview.Bytes != 8 {
		t.Fatalf("preview inesperada: %+v", preview)
	}

	// POST: debe responder 200 (regresión: 405) y borrar los objetos.
	status, body = doJSON(t, http.MethodPost, url, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("POST cleanup: %d %s", status, body)
	}
	if !strings.Contains(string(body), "2 objetos") {
		t.Fatalf("detalle del borrado inesperado: %s", body)
	}
	for _, k := range orphans {
		p := filepath.Join(root, filepath.FromSlash(prefix), k)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("el objeto %s no se borró", k)
		}
	}

	// El job queda marcado como limpiado.
	_ = st.View(context.Background(), func(s *store.State) error {
		var res struct {
			Cleaned bool `json:"cleaned"`
		}
		_ = json.Unmarshal(s.Jobs["j-clean"].Result, &res)
		if !res.Cleaned {
			t.Fatal("el job no quedó marcado como limpiado")
		}
		return nil
	})

	// GET tras limpiar: conflicto (ya se limpió).
	status, _ = doJSON(t, http.MethodGet, url, adminToken, nil)
	if status != http.StatusConflict {
		t.Fatalf("GET tras limpiar: esperaba 409, got %d", status)
	}
}
