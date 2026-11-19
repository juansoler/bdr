package jobs

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

// TestIntegracionKopiaReal valida el runner contra el binario real y un
// repositorio filesystem temporal. Se omite con -short o si falta kopia.
func TestIntegracionKopiaReal(t *testing.T) {
	if testing.Short() {
		t.Skip("integración omitida con -short")
	}
	kopia, err := exec.LookPath("kopia")
	if err != nil {
		t.Skip("kopia no está en PATH")
	}

	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo")
	dataPath := filepath.Join(dir, "data")
	agentData := filepath.Join(dir, "agent-data")
	repoConfig := filepath.Join(dir, "create.config")
	password := "clave-de-prueba-integracion"

	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataPath, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "keep.txt"), []byte("contenido importante\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "skip.log"), []byte("no debe respaldarse\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "sub", "a.txt"), []byte("tampoco\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Crear el repositorio con una configuración aislada.
	create := exec.CommandContext(ctx, kopia, "repository", "create", "filesystem",
		"--path="+repoPath, "--config-file="+repoConfig, "--no-use-keychain")
	create.Env = append(os.Environ(), "KOPIA_PASSWORD="+password, "KOPIA_CHECK_FOR_UPDATES=false")
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("crear repositorio: %v\n%s", err, sanitizeKopia(out, password))
	}

	runner := NewKopiaRunnerWithOptions(KopiaOptions{Binary: kopia, DataDir: agentData})
	creds := &contracts.RepositoryCredentials{
		RepositoryID: "repo-integracion",
		Kind:         contracts.RepoKindFilesystem,
		Path:         repoPath,
		Password:     password,
		Version:      1,
	}

	backupPayload, _ := json.Marshal(contracts.BackupJobPayload{
		PlanID:   "plan-1",
		Paths:    []string{dataPath},
		Excludes: []string{"*.log", dataPath + "/sub"},
		Retention: &contracts.Retention{
			KeepLatest: 5, KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 6,
		},
	})
	ex := NewExecutor(runner, nil)
	out, err := ex.Execute(ctx, contracts.JobView{
		ID: "job-integracion", Type: contracts.JobTypeBackupFile, Payload: backupPayload,
	}, RunOptions{Credentials: creds})
	if err != nil {
		t.Fatalf("backup: %v (%s)", err, ErrorCode(err))
	}
	var backup contracts.BackupJobResult
	if err := json.Unmarshal(out, &backup); err != nil {
		t.Fatalf("json backup: %v", err)
	}
	if len(backup.Snapshots) != 1 {
		t.Fatalf("snapshots = %d", len(backup.Snapshots))
	}
	snap := backup.Snapshots[0]
	if snap.ID == "" || snap.RootObjectID == "" {
		t.Fatalf("snapshot incompleto: %+v", snap)
	}
	if snap.Files != 1 {
		t.Fatalf("files = %d, esperaba 1 (excluidos *.log y /sub): %+v", snap.Files, snap)
	}
	if snap.ExcludedFiles < 1 || snap.ExcludedDirs < 1 {
		t.Fatalf("contadores de excluidos inesperados: %+v", snap)
	}
	if backup.KopiaVersion == "" {
		t.Fatalf("kopia_version vacía")
	}

	// Restaurar un elemento y comparar contenido.
	destDir := t.TempDir()
	restorePayload, _ := json.Marshal(contracts.RestoreJobPayload{
		SnapshotID:  snap.ID,
		Items:       []string{"keep.txt"},
		Destination: destDir,
		Overwrite:   false,
	})
	out, err = ex.Execute(ctx, contracts.JobView{
		ID: "job-restore", Type: contracts.JobTypeRestoreFile, Payload: restorePayload,
	}, RunOptions{Credentials: creds})
	if err != nil {
		t.Fatalf("restore: %v (%s)", err, ErrorCode(err))
	}
	var restored contracts.RestoreJobResult
	if err := json.Unmarshal(out, &restored); err != nil {
		t.Fatalf("json restore: %v", err)
	}
	if len(restored.Restored) != 1 || restored.Restored[0].Files != 1 {
		t.Fatalf("resultado restore inesperado: %+v", restored)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "keep.txt"))
	if err != nil {
		t.Fatalf("leer restaurado: %v", err)
	}
	if string(got) != "contenido importante\n" {
		t.Fatalf("contenido restaurado inesperado: %q", got)
	}

	// Sin overwrite, restaurar de nuevo debe fallar sin tocar el archivo.
	_, err = ex.Execute(ctx, contracts.JobView{
		ID: "job-restore-2", Type: contracts.JobTypeRestoreFile, Payload: restorePayload,
	}, RunOptions{Credentials: creds})
	if ErrorCode(err) != "E_RESTORE_DEST" {
		t.Fatalf("esperaba E_RESTORE_DEST, got %v (%s)", err, ErrorCode(err))
	}
	got, _ = os.ReadFile(filepath.Join(destDir, "keep.txt"))
	if !strings.Contains(string(got), "contenido importante") {
		t.Fatalf("el archivo existente se modificó: %q", got)
	}
}
