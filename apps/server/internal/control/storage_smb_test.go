package control_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"

	"io"
)

// createSMBTarget crea un destino filesystem con compartida SMB y devuelve
// su ID. Requiere Box (la contraseña SMB se cifra).
func createSMBTarget(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "nas-smb", Kind: "filesystem",
			Path: "//nas.local/backups", Prefix: "lxc",
			SMBUser: "bdr-backup", SMBPassword: "s3cr3t-smb", SMBDomain: "EMPRESA.LOCAL",
			SyncIntervalMinutes: 0, Enabled: true})
	if status != http.StatusCreated {
		t.Fatalf("crear target SMB: %d %s", status, body)
	}
	var created map[string]string
	_ = json.Unmarshal(body, &created)
	return created["id"]
}

func TestStorageSMBTargetCRUD(t *testing.T) {
	ts, st := newStorageTestAPI(t, t.TempDir(), "/bin/true")

	id := createSMBTarget(t, ts)

	status, body := doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/storage/targets", adminToken, nil)
	var list contracts.StorageTargetList
	_ = json.Unmarshal(body, &list)
	if status != http.StatusOK || len(list.Items) != 1 {
		t.Fatalf("listar: %d %d items", status, len(list.Items))
	}
	tg := list.Items[0]
	if tg.Kind != "filesystem" || !tg.SMB || tg.SMBUser != "bdr-backup" || tg.SMBDomain != "EMPRESA.LOCAL" {
		t.Fatalf("view SMB: %+v", tg)
	}
	if strings.Contains(string(body), "s3cr3t-smb") {
		t.Fatal("el listado expone la contraseña SMB")
	}

	// La contraseña queda cifrada en el store.
	enc := ""
	_ = st.View(context.Background(), func(s *store.State) error {
		if t := s.Storage[id]; t != nil {
			enc = t.SMBPasswordEnc
		}
		return nil
	})
	if enc == "" {
		t.Fatal("contraseña SMB sin cifrar en el store")
	}

	// PUT sin smb_password: conserva la cifrada.
	status, _ = doJSON(t, http.MethodPut, ts.URL+"/api/v1/admin/storage/targets/"+id, adminToken,
		contracts.StorageTargetRequest{Name: "nas-smb", Kind: "filesystem", Path: "//nas.local/backups",
			SMBUser: "otro-user", SMBDomain: "EMPRESA.LOCAL", Enabled: true})
	if status != http.StatusNoContent {
		t.Fatalf("update: %d", status)
	}
	enc2 := ""
	var user2 string
	_ = st.View(context.Background(), func(s *store.State) error {
		if t := s.Storage[id]; t != nil {
			enc2 = t.SMBPasswordEnc
			user2 = t.SMBUser
		}
		return nil
	})
	if enc2 != enc {
		t.Fatal("el update perdió la contraseña SMB cifrada")
	}
	if user2 != "otro-user" {
		t.Fatalf("el update no aplicó el usuario SMB: %q", user2)
	}

	// Cambio a ruta local: limpia los campos SMB.
	status, _ = doJSON(t, http.MethodPut, ts.URL+"/api/v1/admin/storage/targets/"+id, adminToken,
		contracts.StorageTargetRequest{Name: "nas-local", Kind: "filesystem", Path: "/mnt/backups", Enabled: true})
	if status != http.StatusNoContent {
		t.Fatalf("update a local: %d", status)
	}
	_ = st.View(context.Background(), func(s *store.State) error {
		if x := s.Storage[id]; x != nil {
			if x.IsSMB() || x.SMBUser != "" || x.SMBPasswordEnc != "" {
				t.Fatalf("campos SMB no limpiados: %+v", x)
			}
		}
		return nil
	})

	// Ruta SMB sin recurso compartido: rechazada.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "mala", Kind: "filesystem", Path: "//naso", Enabled: true})
	if status != http.StatusBadRequest {
		t.Fatalf("ruta SMB sin recurso aceptada: %d %s", status, body)
	}

	// Verify sobre un servidor SMB inexistente: error descriptivo.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "fantasma-smb", Kind: "filesystem",
			Path: "//127.0.0.1:1/backups", SMBUser: "u", SMBPassword: "p", Enabled: true})
	if status != http.StatusCreated {
		t.Fatalf("crear fantasma: %d %s", status, body)
	}
	var created map[string]string
	_ = json.Unmarshal(body, &created)
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets/"+created["id"]+"/verify", adminToken, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "error") {
		t.Fatalf("verify offline debe fallar con detalle: %d %s", status, body)
	}
}

// TestFilePlanRejectsSMBTarget: los planes de archivo no aceptan destinos
// SMB (kopia no escribe en compartidas sin montar); los de imagen sí.
func TestFilePlanRejectsSMBTarget(t *testing.T) {
	ts, _ := newStorageTestAPI(t, t.TempDir(), "/bin/true")
	id := createSMBTarget(t, ts)

	// Plan de archivo con destino SMB: rechazado.
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("ficheros-smb"), Paths: []string{"/data"},
			StorageTargetID: &id})
	if status != http.StatusBadRequest || !strings.Contains(string(body), "SMB") {
		t.Fatalf("plan de archivo con destino SMB debe rechazarse: %d %s", status, body)
	}

	// Plan de imagen con destino SMB: aceptado.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("imagen-smb"), Kind: "image",
			StorageTargetID: &id})
	if status != http.StatusCreated {
		t.Fatalf("plan de imagen con destino SMB rechazado: %d %s", status, body)
	}
}

// TestLXCTargetCredentials: el agente de un nodo con un job BACKUP_LXC
// activo obtiene credenciales del destino (allowlist ad-hoc incluye LXC);
// para destinos SMB no se aprovisiona repo Kopia y las credenciales llevan
// la ruta SMB y las credenciales NTLM descifradas.
func TestLXCTargetCredentials(t *testing.T) {
	ts, st := newStorageTestAPI(t, t.TempDir(), "/bin/true")
	id := createSMBTarget(t, ts)

	tok := createToken(t, ts, "pve-node")
	enrolled := enrollAgent(t, ts, tok.Token, "pve-node")

	payload, _ := json.Marshal(contracts.BackupLXCJobPayload{
		InstanceID: "inst1", VMID: 102, Name: "web", Node: "pve",
		StorageTargetID: id,
	})
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	if err := st.Update(context.Background(), func(s *store.State) error {
		s.Jobs["job-lxc-2"] = &store.Job{
			ID: "job-lxc-2", Type: contracts.JobTypeBackupLXC, State: store.JobLeased,
			AgentID: enrolled.AgentID, Trigger: "manual",
			Payload: payload, MaxAttempts: 3,
			CreatedAt: now, UpdatedAt: now,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	status, body := doJSON(t, http.MethodGet,
		ts.URL+"/api/v1/agent/repository-credentials?target="+id, enrolled.APIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("credenciales para job LXC: %d %s", status, body)
	}
	var creds contracts.RepositoryCredentials
	_ = json.Unmarshal(body, &creds)
	if creds.Kind != contracts.RepoKindFilesystem {
		t.Fatalf("kind: %q", creds.Kind)
	}
	if creds.Path != "//nas.local/backups/lxc" {
		t.Fatalf("path SMB: %q", creds.Path)
	}
	if creds.SMBUser != "bdr-backup" || creds.SMBPassword != "s3cr3t-smb" || creds.SMBDomain != "EMPRESA.LOCAL" {
		t.Fatalf("credenciales SMB: %+v", creds)
	}

	// Un RESTORE_LXC activo también autoriza.
	if err := st.Update(context.Background(), func(s *store.State) error {
		if j := s.Jobs["job-lxc-2"]; j != nil {
			j.Type = contracts.JobTypeRestoreLXC
			j.Payload, _ = json.Marshal(contracts.RestoreLXCJobPayload{
				InstanceID: "inst1", ManifestKey: "lxc/manifest.json", StorageTargetID: id,
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, body = doJSON(t, http.MethodGet,
		ts.URL+"/api/v1/agent/repository-credentials?target="+id, enrolled.APIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("credenciales para RESTORE_LXC: %d %s", status, body)
	}
}

// TestLXCRestorePointRecorded: un BACKUP_LXC completado crea un punto de
// restauración kind=lxc con la clave del manifiesto y el destino.
func TestLXCRestorePointRecorded(t *testing.T) {
	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	fixed := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	api.Now = func() time.Time { return fixed }
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)

	tok := createToken(t, ts, "pve-node")
	enrolled := enrollAgent(t, ts, tok.Token, "pve-node")

	payload, _ := json.Marshal(contracts.BackupLXCJobPayload{
		InstanceID: "inst1", VMID: 102, Name: "web", Node: "pve",
		StorageTargetID: "tgt-smb",
	})
	now := fixed
	if err := st.Update(context.Background(), func(s *store.State) error {
		s.Jobs["job-lxc-1"] = &store.Job{
			ID: "job-lxc-1", Type: contracts.JobTypeBackupLXC, State: store.JobPending,
			AgentID: enrolled.AgentID, Trigger: "manual",
			Payload: payload, MaxAttempts: 3,
			CreatedAt: now, UpdatedAt: now,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	result := map[string]any{
		"manifest_id":  "lxc-102-1",
		"manifest_key": "lxc/manifest-lxc-102-1.json",
		"vmid":         102,
		"name":         "web",
		"bytes_total":  4096,
		"mode":         "snapshot",
	}
	rb, _ := json.Marshal(result)
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/jobs/job-lxc-1/complete", enrolled.APIKey,
		contracts.JobCompleteRequest{State: "succeeded", Result: rb})
	if status != http.StatusNoContent {
		t.Fatalf("complete: %d %s", status, body)
	}

	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/restore-points?agent_id="+enrolled.AgentID, adminToken, nil)
	var rps contracts.RestorePointList
	_ = json.Unmarshal(body, &rps)
	if status != http.StatusOK || len(rps.Items) != 1 {
		t.Fatalf("restore points: %d %d items (%s)", status, len(rps.Items), body)
	}
	rp := rps.Items[0]
	if rp.Kind != "lxc" || rp.SnapshotID != "lxc/manifest-lxc-102-1.json" || rp.TargetID != "tgt-smb" {
		t.Fatalf("rp lxc: %+v", rp)
	}
	if rp.SizeBytes != 4096 {
		t.Fatalf("size: %d", rp.SizeBytes)
	}

	// El listado por VMID de la instancia incluye el punto (pve/inst1/102).
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/proxmox/instances/inst1/lxc/102/restore-points", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("rps por lxc: %d %s", status, body)
	}
	var vl contracts.VMRestorePointList
	_ = json.Unmarshal(body, &vl)
	if len(vl.Items) != 1 || vl.Items[0].Kind != "lxc" {
		t.Fatalf("rps por lxc: %+v", vl.Items)
	}
}
