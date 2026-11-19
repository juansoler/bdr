package control_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// fakeS3 simula un S3-compatible: HEAD/GET/PUT/DELETE de objetos y buckets.
func fakeS3(t *testing.T) *httptest.Server {
	t.Helper()
	store := map[string][]byte{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("fakeS3 <- %s %s", r.Method, r.URL.String())
		key := strings.TrimPrefix(r.URL.Path, "/")
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("x-amz-object-lock-mode", "GOVERNANCE")
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			store[key] = b
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			q := r.URL.Query()
			if q.Has("location") {
				_, _ = w.Write([]byte(`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Location>us-east-1</Location></LocationConstraint>`))
				return
			}
			if q.Has("versioning") {
				_, _ = w.Write([]byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/" status="Enabled"/>`))
				return
			}
			if b, ok := store[key]; ok {
				_, _ = w.Write(b)
				return
			}
			if !strings.Contains(key, "/") {
				w.Header().Set("x-amz-bucket-region", "us-east-1")
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			// delete objects (multi-delete): vacía el prefijo solicitado
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			delete(store, key)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// fakeKopia escribe un binario kopia que registra invocaciones.
func fakeKopia(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, "fake-kopia")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func newStorageTestAPI(t *testing.T, downloads string, kopiaBin string) (*httptest.Server, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	api.Box = testBox(t)
	api.DownloadsDir = downloads
	// El configFile debe existir y `repository status` debe funcionar para
	// que el control asuma conexión directa (serverSide); el binario fake
	// responde exit 0 a todo, así que basta con crear el fichero vacío.
	cfg := filepath.Join(t.TempDir(), "kopia.config")
	if err := os.WriteFile(cfg, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	api.Kopia = control.NewKopiaControl(kopiaBin, cfg, "pw", "", "", "", "", "", "")
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func TestStorageTargetsCRUD(t *testing.T) {
	ts, st := newStorageTestAPI(t, t.TempDir(), "/bin/true")

	req := contracts.StorageTargetRequest{
		Name: "minio-casa", Endpoint: "203.0.113.5:9000", Bucket: "bdr-test",
		AccessKey: "admin", SecretKey: "C@sA2025_m|n!o", NoTLS: true,
		ObjectLock: true, RetentionDays: 7, SyncIntervalMinutes: 60, Enabled: true,
	}
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken, req)
	if status != http.StatusCreated {
		t.Fatalf("crear target: %d %s", status, body)
	}

	// El secreto queda cifrado en el store.
	enc := ""
	_ = st.View(context.Background(), func(s *store.State) error {
		for _, tg := range s.Storage {
			enc = tg.SecretKeyEnc
		}
		return nil
	})
	if enc == "" {
		t.Fatal("secreto no cifrado en el store")
	}

	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/storage/targets", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar: %d", status)
	}
	if strings.Contains(string(body), "C@sA2025_m|n!o") {
		t.Fatal("el listado expone el secreto")
	}

	// PUT de actualización sin secret → conserva el cifrado existente.
	status, _ = doJSON(t, http.MethodPut, ts.URL+"/api/v1/admin/storage/targets/"+targetID(t, body), adminToken,
		contracts.StorageTargetRequest{Name: "minio-casa-2", Endpoint: "203.0.113.5:9000", Bucket: "bdr-test",
			AccessKey: "admin", NoTLS: true, ObjectLock: true, RetentionDays: 7, Enabled: true})
	if status != http.StatusNoContent {
		t.Fatalf("update: %d", status)
	}
	enc2 := ""
	_ = st.View(context.Background(), func(s *store.State) error {
		for _, tg := range s.Storage {
			enc2 = tg.SecretKeyEnc
		}
		return nil
	})
	if enc2 == "" {
		t.Fatal("el update perdió el secreto")
	}

	// DELETE.
	status, _ = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/storage/targets", adminToken, nil)
	var list contracts.StorageTargetList
	_ = json.Unmarshal(body, &list)
	id := list.Items[0].ID
	status, _ = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/admin/storage/targets/"+id, adminToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: %d", status)
	}
}

// helper: extrae el ID del target del listado.
func targetID(t *testing.T, body []byte) string {
	t.Helper()
	var list contracts.StorageTargetList
	_ = json.Unmarshal(body, &list)
	if len(list.Items) == 0 {
		t.Fatal("sin targets")
	}
	return list.Items[0].ID
}

// TestStorageVerifyAndSync: verify contra un S3 falso (minio-go) y sync con
// kopia falso (argumentos de réplica correctos y estado actualizado).
func TestStorageVerifyAndSync(t *testing.T) {
	s3 := fakeS3(t)
	bin, callsLog := fakeKopia(t)
	ts, _ := newStorageTestAPI(t, t.TempDir(), bin)

	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "minio", Endpoint: strings.TrimPrefix(s3.URL, "http://"),
			Bucket: "bdr-test", AccessKey: "admin", SecretKey: "sk-test", NoTLS: true,
			ObjectLock: true, RetentionDays: 1, SyncIntervalMinutes: 0, Enabled: true})
	if status != http.StatusCreated {
		t.Fatalf("crear: %d %s", status, body)
	}
	var created map[string]string
	_ = json.Unmarshal(body, &created)
	id := created["id"]
	if id == "" {
		t.Fatal("respuesta sin id")
	}

	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets/"+id+"/verify", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("verify: %d %s", status, body)
	}
	if !strings.Contains(string(body), "ok") {
		t.Fatalf("verify no ok: %s", body)
	}

	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets/"+id+"/sync", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("sync: %d %s", status, body)
	}
	calls := readFile(t, callsLog)
	if !strings.Contains(calls, "sync-to") || !strings.Contains(calls, "bdr-test") ||
		!strings.Contains(calls, "--endpoint=") {
		t.Fatalf("kopia sync-to no invocado correctamente: %s", calls)
	}
	if strings.Contains(calls, "--delete") {
		t.Fatal("la réplica por defecto no debe usar --delete (inmutabilidad)")
	}

	// Destino deshabilitado del sync: interval 0 nunca dispara el loop (aquí
	// se verifica que el estado quedó registrado tras el sync manual).
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/storage/targets", adminToken, nil)
	var list contracts.StorageTargetList
	_ = json.Unmarshal(body, &list)
	if list.Items[0].LastSyncStatus != "ok" {
		t.Fatalf("last_sync_status: %q", list.Items[0].LastSyncStatus)
	}
}
