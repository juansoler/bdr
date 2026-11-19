package control_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// createFSTarget crea un destino de ruta física contra un directorio temporal.
func createFSTarget(t *testing.T, ts *httptest.Server, root string) string {
	t.Helper()
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "nas-lab", Kind: "filesystem", Path: root,
			Prefix: "", SyncIntervalMinutes: 60, Enabled: true})
	if status != http.StatusCreated {
		t.Fatalf("crear target filesystem: %d %s", status, body)
	}
	var created map[string]string
	_ = json.Unmarshal(body, &created)
	if created["id"] == "" {
		t.Fatal("respuesta sin id")
	}
	return created["id"]
}

func TestStorageFilesystemTargetCRUD(t *testing.T) {
	ts, _ := newStorageTestAPI(t, t.TempDir(), "/bin/true")
	root := t.TempDir()

	id := createFSTarget(t, ts, root)

	// El listado muestra kind y path, y no exige credenciales.
	status, body := doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/storage/targets", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar: %d %s", status, body)
	}
	var list contracts.StorageTargetList
	_ = json.Unmarshal(body, &list)
	if len(list.Items) != 1 {
		t.Fatalf("targets: %d", len(list.Items))
	}
	tg := list.Items[0]
	if tg.Kind != "filesystem" || tg.Path != root {
		t.Fatalf("kind/path: %+v", tg)
	}
	if tg.Endpoint != "" || tg.Bucket != "" {
		t.Fatalf("un destino filesystem no debe tener campos S3: %+v", tg)
	}

	// Ruta relativa: rechazada.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "mala", Kind: "filesystem", Path: "backups-relativos"})
	if status != http.StatusBadRequest {
		t.Fatalf("ruta relativa aceptada: %d %s", status, body)
	}

	// Cambiar a S3 sin credenciales: rechazado.
	status, _ = doJSON(t, http.MethodPut, ts.URL+"/api/v1/admin/storage/targets/"+id, adminToken,
		contracts.StorageTargetRequest{Name: "nas-lab", Kind: "s3"})
	if status != http.StatusBadRequest {
		t.Fatalf("cambio a S3 sin credenciales aceptado: %d", status)
	}

	status, _ = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/admin/storage/targets/"+id, adminToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: %d", status)
	}
}

// TestStorageFilesystemVerifyAndSync: verify comprueba escritura real en la
// ruta y sync invoca `kopia repository sync-to filesystem`.
func TestStorageFilesystemVerifyAndSync(t *testing.T) {
	bin, callsLog := fakeKopia(t)
	ts, _ := newStorageTestAPI(t, t.TempDir(), bin)
	root := t.TempDir()

	id := createFSTarget(t, ts, root)

	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets/"+id+"/verify", adminToken, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "ok") {
		t.Fatalf("verify: %d %s", status, body)
	}
	if _, err := os.Stat(filepath.Join(root, "verify", "ok.txt")); !os.IsNotExist(err) {
		t.Fatal("el archivo de prueba de verify no se borró")
	}

	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets/"+id+"/sync", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("sync: %d %s", status, body)
	}
	calls := readFile(t, callsLog)
	if !strings.Contains(calls, "sync-to filesystem") || !strings.Contains(calls, "--path="+root) {
		t.Fatalf("kopia sync-to filesystem no invocado correctamente: %s", calls)
	}

	// Verify sobre una ruta que no existe: error descriptivo.
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "fantasma", Kind: "filesystem",
			Path: filepath.Join(root, "no-existe"), Enabled: true})
	if status != http.StatusCreated {
		t.Fatalf("crear destino fantasma: %d", status)
	}
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/storage/targets", adminToken, nil)
	var list contracts.StorageTargetList
	_ = json.Unmarshal(body, &list)
	var ghost string
	for _, tg := range list.Items {
		if tg.Name == "fantasma" {
			ghost = tg.ID
		}
	}
	if ghost == "" {
		t.Fatal("destino fantasma no listado")
	}
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets/"+ghost+"/verify", adminToken, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "error") {
		t.Fatalf("verify sobre ruta inexistente debe fallar: %d %s", status, body)
	}
}

// TestRestorePointDeleteFilesystemVM: un punto de VM con destino de ruta
// física borra el manifiesto y sus segmentos del directorio.
func TestRestorePointDeleteFilesystemVM(t *testing.T) {
	ts, st := newStorageTestAPI(t, t.TempDir(), "/bin/true")
	root := t.TempDir()
	id := createFSTarget(t, ts, root)

	// Manifiesto + segmento en el destino.
	manifestKey := "vm/manifest-vm1.json"
	segmentKey := "vm/seg-001"
	if err := os.MkdirAll(filepath.Join(root, "vm"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schema_version":1,"manifest_id":"vm-1","kind":"vm","streams":[` +
		`{"disk":0,"segment":0,"name":"disk0-seg0000","blob":"` + segmentKey + `","sha256":"x","bytes":4}]}`
	if err := os.WriteFile(filepath.Join(root, manifestKey), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, segmentKey), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	if err := st.Update(context.Background(), func(s *store.State) error {
		s.RestorePoints["rp-vm"] = &store.RestorePoint{
			ID: "rp-vm", AgentID: "a1", JobID: "j1", Kind: "vm",
			SnapshotID: manifestKey, TargetID: id, SourcePath: "pve/inst/1",
			CreatedAt: now, StartTime: now, EndTime: now, VerifyState: "pending",
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	status, body := doJSON(t, http.MethodDelete, ts.URL+"/api/v1/admin/restore-points/rp-vm", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("delete: %d %s", status, body)
	}
	if !strings.Contains(string(body), "2 objetos") {
		t.Fatalf("detalle del borrado inesperado: %s", body)
	}
	if _, err := os.Stat(filepath.Join(root, manifestKey)); !os.IsNotExist(err) {
		t.Fatal("el manifiesto no se borró de la ruta")
	}
	if _, err := os.Stat(filepath.Join(root, segmentKey)); !os.IsNotExist(err) {
		t.Fatal("el segmento no se borró de la ruta")
	}

	// El catálogo ya no incluye el punto.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/restore-points", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar restore-points: %d %s", status, body)
	}
	if strings.Contains(string(body), `"rp-vm"`) {
		t.Fatal("el punto sigue en el catálogo")
	}

	// Segundo borrado: 404.
	status, _ = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/admin/restore-points/rp-vm", adminToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("segundo delete: %d", status)
	}
}

// TestRestorePointDeleteFileKind: un punto de archivo borra el snapshot con
// `kopia snapshot delete --delete`, en el repo central o en el del destino.
func TestRestorePointDeleteFileKind(t *testing.T) {
	bin, callsLog := fakeKopia(t)
	ts, st := newStorageTestAPI(t, t.TempDir(), bin)
	root := t.TempDir()
	targetID := createFSTarget(t, ts, root)

	now := time.Now().UTC()
	if err := st.Update(context.Background(), func(s *store.State) error {
		s.RestorePoints["rp-central"] = &store.RestorePoint{
			ID: "rp-central", AgentID: "a1", JobID: "j1", Kind: "file",
			SnapshotID: "k1234", SourcePath: "/data",
			CreatedAt: now, StartTime: now, EndTime: now, VerifyState: "pending",
		}
		s.RestorePoints["rp-target"] = &store.RestorePoint{
			ID: "rp-target", AgentID: "a1", JobID: "j2", Kind: "file",
			SnapshotID: "k5678", TargetID: targetID, SourcePath: "/data",
			CreatedAt: now, StartTime: now, EndTime: now, VerifyState: "pending",
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Punto del repo central.
	status, body := doJSON(t, http.MethodDelete, ts.URL+"/api/v1/admin/restore-points/rp-central", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("delete central: %d %s", status, body)
	}
	calls := readFile(t, callsLog)
	if !strings.Contains(calls, "snapshot delete k1234 --delete") {
		t.Fatalf("snapshot delete no invocado: %s", calls)
	}

	// Punto en el repo del destino: aprovisiona (connect filesystem) y borra.
	status, body = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/admin/restore-points/rp-target", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("delete en destino: %d %s", status, body)
	}
	calls = readFile(t, callsLog)
	if !strings.Contains(calls, "connect filesystem --path="+root) ||
		!strings.Contains(calls, "snapshot delete k5678 --delete") {
		t.Fatalf("borrado en el repo del destino incorrecto: %s", calls)
	}
}

// TestRestorePointDeleteLXC: un punto kind=lxc borra el manifiesto y sus
// segmentos del destino (mismo camino que vm/image).
func TestRestorePointDeleteLXC(t *testing.T) {
	ts, st := newStorageTestAPI(t, t.TempDir(), "/bin/true")
	root := t.TempDir()
	id := createFSTarget(t, ts, root)

	manifestKey := "lxc/manifest-ct1.json"
	segmentKey := "lxc/ct103-seg0000-abc"
	if err := os.MkdirAll(filepath.Join(root, "lxc"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schema_version":1,"manifest_id":"lxc-1","kind":"lxc","streams":[` +
		`{"disk":0,"segment":0,"name":"ct103-seg0000","blob":"` + segmentKey + `","sha256":"x","bytes":4}],` +
		`"lxc":{"vmid":103,"name":"ad"}}`
	if err := os.WriteFile(filepath.Join(root, manifestKey), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, segmentKey), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	if err := st.Update(context.Background(), func(s *store.State) error {
		s.RestorePoints["rp-lxc"] = &store.RestorePoint{
			ID: "rp-lxc", AgentID: "a1", JobID: "j1", Kind: "lxc",
			SnapshotID: manifestKey, TargetID: id, SourcePath: "pve/inst/103",
			CreatedAt: now, StartTime: now, EndTime: now, VerifyState: "pending",
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	status, body := doJSON(t, http.MethodDelete, ts.URL+"/api/v1/admin/restore-points/rp-lxc", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("delete lxc: %d %s", status, body)
	}
	if !strings.Contains(string(body), "2 objetos") {
		t.Fatalf("detalle inesperado: %s", body)
	}
	if _, err := os.Stat(filepath.Join(root, manifestKey)); !os.IsNotExist(err) {
		t.Fatal("el manifiesto LXC no se borró")
	}
	if _, err := os.Stat(filepath.Join(root, segmentKey)); !os.IsNotExist(err) {
		t.Fatal("el segmento LXC no se borró")
	}
}
