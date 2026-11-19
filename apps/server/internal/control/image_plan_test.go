package control_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/juansoler/bdr/packages/contracts"
)

// Plan kind=image exige destino de almacenamiento; con él, su backup manual
// genera un job BACKUP_IMAGE con payload marcado (target + retención).
func TestImagePlanAndJob(t *testing.T) {
	ts := newTestAPIFull(t, true) // necesita caja de secretos para el target
	tok := createToken(t, ts, "casa-img")
	enrolled := enrollAgent(t, ts, tok.Token, "casa-img")

	// Plan de imagen SIN destino → rechazado.
	status, _ := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("sin-destino"), Kind: "image"})
	if status != http.StatusBadRequest {
		t.Fatalf("plan image sin destino debe ser 400: %d", status)
	}

	// Crear plan de imagen CON destino y selección de discos (0 y 1, sin
	// ordenar para comprobar la normalización).
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/storage/targets", adminToken,
		contracts.StorageTargetRequest{Name: "minio-casa", Endpoint: "203.0.113.5:9000",
			Bucket: "bdr-repo", AccessKey: "admin", SecretKey: "sk", NoTLS: true,
			ObjectLock: true, RetentionDays: 1, SyncIntervalMinutes: 0, Enabled: true})
	if status != http.StatusCreated {
		t.Fatalf("target: %d %s", status, body)
	}
	var target map[string]string
	_ = json.Unmarshal(body, &target)

	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("equipo-completo"), Kind: "image",
			StorageTargetID: strPtr(target["id"]), Disks: []int{1, 0}})
	if status != http.StatusCreated {
		t.Fatalf("crear plan image: %d %s", status, body)
	}
	var plan contracts.PlanView
	_ = json.Unmarshal(body, &plan)
	if plan.Kind != "image" {
		t.Fatalf("kind: %q", plan.Kind)
	}
	if len(plan.Paths) != 0 {
		t.Fatalf("un plan de imagen no debe tener rutas: %v", plan.Paths)
	}
	if len(plan.Disks) != 2 || plan.Disks[0] != 0 || plan.Disks[1] != 1 {
		t.Fatalf("disks normalizados (ordenados, sin duplicados): %v", plan.Disks)
	}

	// Un plan file sin rutas sigue rechazándose.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("sin-rutas"), Kind: "file"})
	if status != http.StatusBadRequest {
		t.Fatalf("plan file sin rutas debe ser 400: %d %s", status, body)
	}

	// disks en un plan file se rechaza: solo aplica a imagen.
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("file-con-discos"), Kind: "file",
			Paths: []string{"C:\\Datos"}, Disks: []int{0}})
	if status != http.StatusBadRequest {
		t.Fatalf("plan file con disks debe ser 400: %d", status)
	}

	// Índices de disco fuera de rango se rechazan.
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("disco-negativo"), Kind: "image",
			StorageTargetID: strPtr(target["id"]), Disks: []int{-1}})
	if status != http.StatusBadRequest {
		t.Fatalf("plan image con disco negativo debe ser 400: %d", status)
	}
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("disco-alto"), Kind: "image",
			StorageTargetID: strPtr(target["id"]), Disks: []int{256}})
	if status != http.StatusBadRequest {
		t.Fatalf("plan image con disco 256 debe ser 400: %d", status)
	}

	// Backup manual sobre el plan de imagen → job BACKUP_IMAGE.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/backup",
		adminToken, contracts.BackupRequest{PlanID: plan.ID})
	if status != http.StatusAccepted {
		t.Fatalf("backup manual: %d %s", status, body)
	}
	var br contracts.BackupResponse
	_ = json.Unmarshal(body, &br)

	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/jobs/"+br.JobID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("job: %d %s", status, body)
	}
	if !strings.Contains(string(body), contracts.JobTypeBackupImage) {
		t.Fatalf("el job debe ser %s: %s", contracts.JobTypeBackupImage, body)
	}
	var job contracts.JobView
	_ = json.Unmarshal(body, &job)
	var payload contracts.ImageJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("payload image: %v (%s)", err, job.Payload)
	}
	if payload.StorageTargetID != target["id"] {
		t.Fatalf("payload storage_target_id: %q", payload.StorageTargetID)
	}
	if payload.RetentionDays != 1 {
		t.Fatalf("payload retention_days (del destino): %d", payload.RetentionDays)
	}
	if len(payload.Disks) != 2 || payload.Disks[0] != 0 || payload.Disks[1] != 1 {
		t.Fatalf("payload disks: %v", payload.Disks)
	}

	// PATCH con disks: [] limpia la selección (vuelve a capturar todos).
	status, body = doJSON(t, http.MethodPatch, ts.URL+"/api/v1/admin/backup-plans/"+plan.ID,
		adminToken, contracts.PlanRequest{Disks: []int{}})
	if status != http.StatusOK {
		t.Fatalf("patch limpiar disks: %d %s", status, body)
	}
	// La respuesta omite "disks" (omitempty) cuando la selección queda
	// vacía; unmarshaling en una variable nueva lo verifica.
	var patched contracts.PlanView
	if err := json.Unmarshal(body, &patched); err != nil {
		t.Fatalf("patch plan: %v (%s)", err, body)
	}
	if len(patched.Disks) != 0 {
		t.Fatalf("disks tras limpiar: %v", patched.Disks)
	}

	// Y el payload del siguiente backup ya no lleva filtro de discos. El
	// primer job sigue en cola (no hay agente que lo procese): se cancela
	// para poder lanzar otro sobre el mismo plan.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/jobs/"+br.JobID+"/cancel",
		adminToken, nil)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("cancel job 1: %d %s", status, body)
	}
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/backup",
		adminToken, contracts.BackupRequest{PlanID: plan.ID})
	if status != http.StatusAccepted {
		t.Fatalf("backup manual 2: %d %s", status, body)
	}
	_ = json.Unmarshal(body, &br)
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/jobs/"+br.JobID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("job 2: %d %s", status, body)
	}
	_ = json.Unmarshal(body, &job)
	var payload2 contracts.ImageJobPayload
	if err := json.Unmarshal(job.Payload, &payload2); err != nil {
		t.Fatalf("payload image 2: %v (%s)", err, job.Payload)
	}
	if len(payload2.Disks) != 0 {
		t.Fatalf("payload disks tras limpiar: %v", payload2.Disks)
	}
}

// kind desconocido sigue rechazándose.
func TestPlanKindUnknown(t *testing.T) {
	ts := newTestAPI(t)
	status, _ := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans", adminToken,
		contracts.PlanRequest{Name: strPtr("raro"), Kind: "qcow2"})
	if status != http.StatusBadRequest {
		t.Fatalf("kind desconocido debe ser 400: %d", status)
	}
}
