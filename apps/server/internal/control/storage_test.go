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
	"time"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/secrets"
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

// fakeKopiaConnectFails escribe un kopia falso que falla `connect` con el
// mensaje de contraseña inválida (repo ajeno) y acepta todo lo demás.
func fakeKopiaConnectFails(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, "fake-kopia")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\ncase \"$*\" in *connect*) echo 'ERROR: invalid repository password' >&2; exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

// TestStorageAdoptRepoPassword: un destino creado con repo_password adopta
// el repositorio existente — si la contraseña no conecta, NUNCA se formatea
// el destino (sin create), con error claro; con la contraseña correcta
// conecta, la contraseña se revela por el endpoint break-glass y las
// credenciales al agente llevan esa contraseña.
func TestStorageAdoptRepoPassword(t *testing.T) {
	bin, callsLog := fakeKopiaConnectFails(t)
	downloads := t.TempDir()

	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	box, err := secrets.New([]byte(strings.Repeat("t", 32)))
	if err != nil {
		t.Fatal(err)
	}
	api.Box = box
	api.DownloadsDir = downloads
	cfg := filepath.Join(t.TempDir(), "kopia.config")
	if err := os.WriteFile(cfg, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	api.Kopia = control.NewKopiaControl(bin, cfg, "pw", "", "", "", "", "", "")
	fixed := time.Date(2026, 11, 18, 21, 0, 0, 0, time.UTC)
	api.Now = func() time.Time { return fixed }
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	api.PublicURL = ts.URL

	// Target con contraseña de repo aportada (adopción).
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "adoptado", Endpoint: "203.0.113.5:9000",
			Bucket: "bdr-ajeno", AccessKey: "admin", SecretKey: "sk", NoTLS: true,
			RepoPassword: "clave-adoptada", SyncIntervalMinutes: 0, Enabled: true})
	if status != http.StatusCreated {
		t.Fatalf("target: %d %s", status, body)
	}
	var target map[string]string
	_ = json.Unmarshal(body, &target)

	// Agente + plan asignado al target.
	tok := createToken(t, ts, "casa-adopt")
	enrolled := enrollAgent(t, ts, tok.Token, "casa-adopt")
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("plan-adopt"), Kind: "image",
			StorageTargetID: strPtr(target["id"])})
	if status != http.StatusCreated {
		t.Fatalf("plan: %d %s", status, body)
	}
	var plan struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &plan)
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans/"+plan.ID+"/assign",
		adminToken, contracts.AssignmentRequest{AgentID: enrolled.AgentID})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("assign: %d", status)
	}

	// Credenciales: connect falla con contraseña inválida y el destino es
	// adoptado → error claro y SIN create (no se formatea nada ajeno).
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/agent/repository-credentials?target="+target["id"],
		enrolled.APIKey, nil)
	if status != http.StatusBadGateway {
		t.Fatalf("creds sobre repo ajeno: %d %s", status, body)
	}
	if !strings.Contains(string(body), "no corresponde") {
		t.Fatalf("mensaje de adopción ausente: %s", body)
	}
	calls := readFile(t, callsLog)
	if strings.Contains(calls, "create") {
		t.Fatalf("un destino adoptado no debe formatearse nunca: %s", calls)
	}
	if !strings.Contains(calls, "connect") {
		t.Fatalf("esperaba intento de connect: %s", calls)
	}
	var adopted, ready bool
	_ = st.View(context.Background(), func(s *store.State) error {
		tg := s.Storage[target["id"]]
		adopted, ready = tg.RepoAdopted, tg.RepoReady
		return nil
	})
	if !adopted || ready {
		t.Fatalf("marca de adopción: adopted=%v ready=%v", adopted, ready)
	}

	// Contraseña correcta: el fake acepta todo → connect OK y contraseña
	// adoptada entregada al agente y revelable por break-glass.
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	status, _ = doJSON(t, http.MethodPut, ts.URL+"/api/v1/admin/storage/targets/"+target["id"], adminToken,
		contracts.StorageTargetRequest{Name: "adoptado", Endpoint: "203.0.113.5:9000",
			Bucket: "bdr-ajeno", AccessKey: "admin", SecretKey: "sk", NoTLS: true,
			RepoPassword: "clave-adoptada", SyncIntervalMinutes: 0, Enabled: true})
	if status != http.StatusNoContent {
		t.Fatalf("re-adopt: %d", status)
	}
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/agent/repository-credentials?target="+target["id"],
		enrolled.APIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("creds con contraseña correcta: %d %s", status, body)
	}
	var creds contracts.RepositoryCredentials
	_ = json.Unmarshal(body, &creds)
	if creds.Password != "clave-adoptada" {
		t.Fatalf("el agente debe recibir la contraseña adoptada: %q", creds.Password)
	}
	if !strings.Contains(readFile(t, callsLog), "connect") {
		t.Fatal("esperaba connect con la contraseña adoptada")
	}

	// Revelado break-glass (admin, auditado).
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/storage/targets/"+target["id"]+"/repo-password", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("reveal: %d %s", status, body)
	}
	var revealed struct {
		RepoPassword string `json:"repo_password"`
	}
	_ = json.Unmarshal(body, &revealed)
	if revealed.RepoPassword != "clave-adoptada" {
		t.Fatalf("contraseña revelada: %q", revealed.RepoPassword)
	}
}

func TestStorageTargetHetznerAutoRegionAndPreserve(t *testing.T) {
	ts, st := newStorageTestAPI(t, t.TempDir(), "/bin/true")

	// Crear target Hetzner sin especificar región → se deduce fsn1
	req := contracts.StorageTargetRequest{
		Name:      "hetzner-backup",
		Endpoint:  "https://fsn1.your-objectstorage.com/",
		Bucket:    "bdr-fsn1",
		AccessKey: "hetzner-ak",
		SecretKey: "hetzner-sk",
		Enabled:   true,
	}
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken, req)
	if status != http.StatusCreated {
		t.Fatalf("crear target Hetzner: %d %s", status, body)
	}
	var created map[string]string
	_ = json.Unmarshal(body, &created)
	id := created["id"]

	// GET: StorageTargetView debe exponer la región deducida
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/storage/targets", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar: %d", status)
	}
	var list contracts.StorageTargetList
	_ = json.Unmarshal(body, &list)
	if len(list.Items) == 0 {
		t.Fatal("sin targets en listado")
	}
	item := list.Items[0]
	if item.Region != "fsn1" {
		t.Fatalf("esperaba region 'fsn1', obtuvo: %q", item.Region)
	}
	if item.Endpoint != "fsn1.your-objectstorage.com" {
		t.Fatalf("endpoint esperado 'fsn1.your-objectstorage.com', obtuvo: %q", item.Endpoint)
	}
	if item.NoTLS {
		t.Fatalf("no_tls no debe ser true para endpoint https://: %v", item.NoTLS)
	}

	// PUT update sin especificar región (como envía el panel Next.js cuando region="") → debe conservarse fsn1
	updateReq := contracts.StorageTargetRequest{
		Name:      "hetzner-backup-editado",
		Endpoint:  "fsn1.your-objectstorage.com",
		Bucket:    "bdr-fsn1",
		AccessKey: "hetzner-ak",
		Enabled:   true,
	}
	status, _ = doJSON(t, http.MethodPut, ts.URL+"/api/v1/admin/storage/targets/"+id, adminToken, updateReq)
	if status != http.StatusNoContent {
		t.Fatalf("update: %d", status)
	}

	_ = st.View(context.Background(), func(s *store.State) error {
		tg := s.Storage[id]
		if tg == nil {
			t.Fatal("target no encontrado en store")
		}
		if tg.Region != "fsn1" {
			t.Fatalf("la actualización perdió la región: %q", tg.Region)
		}
		return nil
	})
}
