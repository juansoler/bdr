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

// Flujo de elección de almacenamiento por plan: target S3 → plan con
// storage_target_id → job con payload marcado → credenciales de repositorio
// S3 entregadas al agente (y denegadas a quien no tiene el plan).
func TestPlanStorageTargetSelection(t *testing.T) {
	// Fake kopia: repository connect/create exit 0 → RepoReady true.
	bin, _ := fakeKopia(t)
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

	// 1. Target S3.
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "minio-casa", Endpoint: "203.0.113.5:9000",
			Bucket: "bdr-repo", AccessKey: "admin", SecretKey: "sk", NoTLS: true,
			ObjectLock: true, RetentionDays: 1, SyncIntervalMinutes: 0, Enabled: true})
	if status != http.StatusCreated {
		t.Fatalf("target: %d %s", status, body)
	}
	var target map[string]string
	_ = json.Unmarshal(body, &target)

	// 2. Agente enrolado.
	tok := createToken(t, ts, "casa-s3")
	enrolled := enrollAgent(t, ts, tok.Token, "casa-s3")

	// 3. Plan de imagen asignado al target.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("equipo-s3"), Kind: "image",
			StorageTargetID: strPtr(target["id"])})
	if status != http.StatusCreated {
		t.Fatalf("plan: %d %s", status, body)
	}
	var plan contracts.PlanView
	_ = json.Unmarshal(body, &plan)
	if plan.StorageTargetID != target["id"] {
		t.Fatalf("plan storage_target_id: %q", plan.StorageTargetID)
	}

	// Asignación del plan al agente (necesaria para las credenciales).
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans/"+plan.ID+"/assign",
		adminToken, contracts.AssignmentRequest{AgentID: enrolled.AgentID})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("assign: %d %s", status, body)
	}

	// 4. Backup manual → job con storage_target_id en el payload.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/backup",
		adminToken, contracts.BackupRequest{PlanID: plan.ID})
	if status != http.StatusAccepted {
		t.Fatalf("backup: %d %s", status, body)
	}
	var br contracts.BackupResponse
	_ = json.Unmarshal(body, &br)
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/jobs/"+br.JobID, adminToken, nil)
	var job contracts.JobView
	_ = json.Unmarshal(body, &job)
	var payload contracts.ImageJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.StorageTargetID != target["id"] {
		t.Fatalf("payload storage_target_id: %q", payload.StorageTargetID)
	}

	// 5. Credenciales del repo del target: el agente con el plan las recibe
	// (kind s3, bucket, y contraseña de repo generada); otro agente no.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/agent/repository-credentials?target="+target["id"],
		enrolled.APIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("creds con plan: %d %s", status, body)
	}
	var creds contracts.RepositoryCredentials
	_ = json.Unmarshal(body, &creds)
	if creds.Kind != contracts.RepoKindS3 || creds.Bucket != "bdr-repo" ||
		creds.Endpoint != "203.0.113.5:9000" || creds.Password == "" || creds.SecretKey == "" {
		t.Fatalf("creds s3 incompletas: %+v", creds)
	}

	// Agente sin el plan → denegado.
	tok2 := createToken(t, ts, "otro-s3")
	enrolled2 := enrollAgent(t, ts, tok2.Token, "otro-s3")
	status, _ = doJSON(t, http.MethodGet, ts.URL+"/api/v1/agent/repository-credentials?target="+target["id"],
		enrolled2.APIKey, nil)
	if status != http.StatusForbidden {
		t.Fatalf("creds sin plan debe ser 403: %d", status)
	}

	// 6. El target quedó marcado con repo listo y contraseña cifrada.
	var ready, hasPw bool
	_ = st.View(context.Background(), func(s *store.State) error {
		tg := s.Storage[target["id"]]
		ready = tg.RepoReady
		hasPw = tg.RepoPasswordEnc != ""
		return nil
	})
	if !ready || !hasPw {
		t.Fatalf("repo no aprovisionado: ready=%v pw=%v", ready, hasPw)
	}
}
