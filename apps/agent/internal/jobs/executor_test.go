package jobs

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

type fakeRunner struct {
	paths    []string
	restores []RestoreSpec
}

func (f *fakeRunner) Snapshot(_ context.Context, req SnapshotRequest) (SnapshotResult, error) {
	f.paths = append(f.paths, req.Path)
	return SnapshotResult{
		Summary:      contracts.SnapshotSummary{Path: req.Path, ID: "snap-" + req.Path, Files: 1, SizeBytes: 1},
		KopiaVersion: "0.23.1",
	}, nil
}

func (f *fakeRunner) Restore(_ context.Context, spec RestoreSpec) (RestoreResult, error) {
	f.restores = append(f.restores, spec)
	return RestoreResult{Files: 1, Bytes: 11}, nil
}

func (f *fakeRunner) Version(context.Context) string { return "0.23.1" }

func TestExecuteBackupJob(t *testing.T) {
	payload, _ := json.Marshal(contracts.BackupJobPayload{
		PlanID: "p1", Paths: []string{"/data/a", "/data/b"},
		Excludes: []string{"*.tmp"}, Retention: &contracts.Retention{KeepLatest: 3},
	})
	job := contracts.JobView{ID: "j1", Type: contracts.JobTypeBackupFile, Payload: payload}
	fr := &fakeRunner{}
	ex := NewExecutor(fr, nil)

	var last Progress
	out, err := ex.Execute(context.Background(), job, RunOptions{Progress: func(p Progress) { last = p }})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(fr.paths) != 2 || last.Percent != 100 {
		t.Fatalf("paths=%v last=%+v", fr.paths, last)
	}
	var result contracts.BackupJobResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(result.Snapshots) != 2 || result.Snapshots[0].ID == "" {
		t.Fatalf("resultado inesperado: %+v", result)
	}
	if result.KopiaVersion != "0.23.1" || result.BytesTotal != 2 {
		t.Fatalf("resultado inesperado: %+v", result)
	}
}

func TestExecuteInvalidPayload(t *testing.T) {
	ex := NewExecutor(&fakeRunner{}, nil)
	_, err := ex.Execute(context.Background(), contracts.JobView{
		ID: "j1", Type: contracts.JobTypeBackupFile, Payload: json.RawMessage(`{}`),
	}, RunOptions{})
	if err == nil || ErrorCode(err) != "E_PLAN_EMPTY" {
		t.Fatalf("esperaba E_PLAN_EMPTY, got %v (%s)", err, ErrorCode(err))
	}
}

func TestExecuteTipoNoSoportado(t *testing.T) {
	ex := NewExecutor(&fakeRunner{}, nil)
	_, err := ex.Execute(context.Background(), contracts.JobView{ID: "j1", Type: "OTRO"}, RunOptions{})
	if ErrorCode(err) != "E_JOB_TYPE" {
		t.Fatalf("esperaba E_JOB_TYPE, got %v", err)
	}
}

type blockingRunner struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingRunner) Snapshot(ctx context.Context, _ SnapshotRequest) (SnapshotResult, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return SnapshotResult{}, ctx.Err()
}

func (b *blockingRunner) Restore(ctx context.Context, _ RestoreSpec) (RestoreResult, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return RestoreResult{}, ctx.Err()
}

func (b *blockingRunner) Version(context.Context) string { return "fake" }

func TestExecuteCancelacionBackup(t *testing.T) {
	payload, _ := json.Marshal(contracts.BackupJobPayload{Paths: []string{"/data"}})
	job := contracts.JobView{ID: "j1", Type: contracts.JobTypeBackupFile, Payload: payload}
	br := &blockingRunner{started: make(chan struct{})}
	ex := NewExecutor(br, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ex.Execute(ctx, job, RunOptions{})
		done <- err
	}()
	select {
	case <-br.started:
	case <-time.After(5 * time.Second):
		t.Fatal("el runner no arrancó")
	}
	cancel()
	select {
	case err := <-done:
		if ErrorCode(err) != "E_CANCELED" {
			t.Fatalf("esperaba E_CANCELED, got %v (%s)", err, ErrorCode(err))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("el executor no terminó tras cancelar")
	}
}

func TestExecuteCancelacionRestore(t *testing.T) {
	payload, _ := json.Marshal(contracts.RestoreJobPayload{
		SnapshotID: "abc", Items: []string{"a.txt"}, Destination: t.TempDir(),
	})
	job := contracts.JobView{ID: "j2", Type: contracts.JobTypeRestoreFile, Payload: payload}
	br := &blockingRunner{started: make(chan struct{})}
	ex := NewExecutor(br, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ex.Execute(ctx, job, RunOptions{})
		done <- err
	}()
	<-br.started
	cancel()
	select {
	case err := <-done:
		if ErrorCode(err) != "E_CANCELED" {
			t.Fatalf("esperaba E_CANCELED, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("el executor no terminó tras cancelar")
	}
}

func TestExecuteRestoreItems(t *testing.T) {
	dest := t.TempDir()
	payload, _ := json.Marshal(contracts.RestoreJobPayload{
		SnapshotID: "snap1", Items: []string{"a.txt", "sub/b.txt"},
		Destination: dest, Overwrite: true,
	})
	job := contracts.JobView{ID: "j3", Type: contracts.JobTypeRestoreFile, Payload: payload}
	fr := &fakeRunner{}
	ex := NewExecutor(fr, nil)
	out, err := ex.Execute(context.Background(), job, RunOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(fr.restores) != 2 {
		t.Fatalf("restauraciones = %d", len(fr.restores))
	}
	if fr.restores[0].Source != "snap1/a.txt" || fr.restores[1].Source != "snap1/sub/b.txt" {
		t.Fatalf("orígenes inesperados: %+v", fr.restores)
	}
	if fr.restores[0].Destination != dest+"/a.txt" {
		t.Fatalf("destino inesperado: %s", fr.restores[0].Destination)
	}
	var result contracts.RestoreJobResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(result.Restored) != 2 || result.Restored[0].Files != 1 {
		t.Fatalf("resultado inesperado: %+v", result)
	}
}

func TestExecuteRestoreDestinoExistenteSinOverwrite(t *testing.T) {
	dest := t.TempDir()
	if err := os.WriteFile(dest+"/a.txt", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(contracts.RestoreJobPayload{
		SnapshotID: "snap1", Items: []string{"a.txt"}, Destination: dest, Overwrite: false,
	})
	job := contracts.JobView{ID: "j4", Type: contracts.JobTypeRestoreFile, Payload: payload}
	ex := NewExecutor(&fakeRunner{}, nil)
	_, err := ex.Execute(context.Background(), job, RunOptions{})
	if ErrorCode(err) != "E_RESTORE_DEST" {
		t.Fatalf("esperaba E_RESTORE_DEST, got %v", err)
	}
}

func TestExecuteRestoreEnSitio(t *testing.T) {
	base := t.TempDir()
	payload, _ := json.Marshal(contracts.RestoreJobPayload{
		SnapshotID: "snap1", Items: []string{"a.txt"}, SourcePath: base,
		InPlace: true, Overwrite: true,
	})
	job := contracts.JobView{ID: "j5", Type: contracts.JobTypeRestoreFile, Payload: payload}
	fr := &fakeRunner{}
	ex := NewExecutor(fr, nil)
	if _, err := ex.Execute(context.Background(), job, RunOptions{}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fr.restores[0].Destination != base+"/a.txt" {
		t.Fatalf("destino en sitio = %s", fr.restores[0].Destination)
	}
}

func TestKopiaMissing(t *testing.T) {
	r := NewKopiaRunner("kopia-que-no-existe-12345", "")
	_, err := r.Snapshot(context.Background(), SnapshotRequest{Path: "/tmp", JobID: "j1"})
	if err == nil || ErrorCode(err) != "E_KOPIA_MISSING" {
		t.Fatalf("esperaba E_KOPIA_MISSING, got %v", err)
	}
}
