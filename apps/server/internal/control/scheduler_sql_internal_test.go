// Test interno del scheduler para planes SQL: necesita llamar a
// runSchedulerTick, que no es accesible desde control_test.
package control

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

func newSQLSchedulerTest(t *testing.T) (*Server, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	s := New(st, "tok", logging.NewWithWriter(io.Discard, "error"), "test")
	return s, st
}

// recordSQLRestorePoint: medio cifrado SIN custodia -> alerta; CON
// custodia -> nil. Es la garantía RF-45 de "cifrado irrecuperable se
// detecta".
func TestRecordSQLRestorePointAlertaCustodia(t *testing.T) {
	s, st := newSQLSchedulerTest(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mkJob := func(id string, encrypted bool, certName string) *store.Job {
		payload, _ := json.Marshal(contracts.SQLBackupJobPayload{
			Server: "sqlhost", Database: "app", Type: "FULL",
			Encryption: func() *contracts.SQLEncryption {
				if encrypted {
					return &contracts.SQLEncryption{Algorithm: "AES_256", CertName: certName}
				}
				return nil
			}(),
		})
		result, _ := json.Marshal(contracts.SQLBackupJobResult{
			Database: "app", Type: "FULL", RepoURI: "s3://bdr/sql/app.bak",
			Encrypted: encrypted, CertName: certName,
		})
		return &store.Job{ID: id, AgentID: "a1", Type: contracts.JobTypeSQLBackup,
			State: store.JobSucceeded, Payload: payload, Result: result,
			CreatedAt: now, UpdatedAt: now}
	}

	_ = st.Update(ctx, func(st *store.State) error {
		st.Agents["a1"] = &store.Agent{ID: "a1", Hostname: "sqlhost", EnrolledAt: now}
		// 1. Cifrado con certificado SIN custodia -> alerta.
		st.Jobs["j1"] = mkJob("j1", true, "BDR_Cert")
		// 2. Cifrado CON custodia registrada -> sin alerta.
		st.SQLCerts["BDR_Custodiado"] = &store.SQLCertificate{
			Name: "BDR_Custodiado", CertURI: "s3://bdr/certs/c.cer", PvkURI: "s3://bdr/certs/c.pvk",
			CreatedAt: now, LastBackupAt: now,
		}
		st.Jobs["j2"] = mkJob("j2", true, "BDR_Custodiado")
		// 3. Sin cifrar -> sin alerta.
		st.Jobs["j3"] = mkJob("j3", false, "")
		return nil
	})

	_ = st.View(ctx, func(st *store.State) error {
		if al := s.recordSQLRestorePoint(st, st.Jobs["j1"], now); al == nil {
			t.Error("cifrado sin custodia debe generar alerta")
		} else if al.Kind != "sql_cert_missing" {
			t.Errorf("alerta inesperada: %+v", al)
		}
		if al := s.recordSQLRestorePoint(st, st.Jobs["j2"], now); al != nil {
			t.Errorf("cifrado con custodia no debe alertar: %+v", al)
		}
		if al := s.recordSQLRestorePoint(st, st.Jobs["j3"], now); al != nil {
			t.Errorf("backup sin cifrar no debe alertar: %+v", al)
		}
		return nil
	})
}

func TestSchedulerLanzaPlanSQLVencido(t *testing.T) {
	s, st := newSQLSchedulerTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Minute) // el tick usa s.Now(); sembramos vencido igualmente

	_ = st.Update(ctx, func(st *store.State) error {
		st.Agents["a1"] = &store.Agent{ID: "a1", Hostname: "sqlhost", EnrolledAt: now}
		st.Storage["tg1"] = &store.StorageTarget{ID: "tg1", Name: "s3", Enabled: true, RetentionDays: 7}
		st.SQLPlans["p1"] = &store.SQLPlan{
			ID: "p1", AgentID: "a1", Server: "sqlhost", Database: "app",
			Type: "FULL", Schedule: "0 3 * * *", StorageTargetID: "tg1",
			Enabled: true, NextRunAt: time.Now().UTC().Add(-time.Minute),
			CreatedAt: now, UpdatedAt: now,
		}
		return nil
	})

	s.runSchedulerTick(ctx)

	var jobID string
	_ = st.View(ctx, func(st *store.State) error {
		for _, j := range st.Jobs {
			if j.Type != contracts.JobTypeSQLBackup {
				continue
			}
			jobID = j.ID
			if j.PlanID != "p1" || j.AgentID != "a1" || j.State != store.JobPending || j.Trigger != "schedule" {
				t.Fatalf("job SQL inesperado: %+v", j)
			}
			var payload contracts.SQLBackupJobPayload
			if err := json.Unmarshal(j.Payload, &payload); err != nil {
				t.Fatalf("payload: %v", err)
			}
			if payload.Database != "app" || payload.StorageTargetID != "tg1" || payload.RetentionDays != 7 {
				t.Fatalf("payload SQL: %+v", payload)
			}
		}
		p := st.SQLPlans["p1"]
		if p.LastJobID != jobID {
			t.Fatalf("plan sin LastJobID: %q vs job %q", p.LastJobID, jobID)
		}
		if p.NextRunAt.Before(time.Now().UTC()) {
			t.Fatalf("NextRunAt no avanzó: %v", p.NextRunAt)
		}
		return nil
	})
	if jobID == "" {
		t.Fatal("el tick no creó el job SQL_BACKUP")
	}

	// Dedup: mientras el job está pendiente, un plan otra vez vencido no
	// crea un segundo job.
	_ = st.Update(ctx, func(st *store.State) error {
		st.SQLPlans["p1"].NextRunAt = time.Now().UTC().Add(-time.Second)
		return nil
	})
	s.runSchedulerTick(ctx)
	count := 0
	_ = st.View(ctx, func(st *store.State) error {
		for _, j := range st.Jobs {
			if j.Type == contracts.JobTypeSQLBackup {
				count++
			}
		}
		return nil
	})
	if count != 1 {
		t.Fatalf("dedup falló: %d jobs SQL_BACKUP", count)
	}
}

func TestSchedulerRespetaPlanSQLDeshabilitado(t *testing.T) {
	s, st := newSQLSchedulerTest(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_ = st.Update(ctx, func(st *store.State) error {
		st.Agents["a1"] = &store.Agent{ID: "a1", Hostname: "sqlhost", EnrolledAt: now}
		st.Storage["tg1"] = &store.StorageTarget{ID: "tg1", Enabled: true}
		st.SQLPlans["p1"] = &store.SQLPlan{
			ID: "p1", AgentID: "a1", Server: "sqlhost", Database: "app",
			Type: "LOG", Schedule: "*/15 * * * *", StorageTargetID: "tg1",
			Enabled: false, NextRunAt: now.Add(-time.Minute),
		}
		return nil
	})

	s.runSchedulerTick(ctx)
	_ = st.View(ctx, func(st *store.State) error {
		if len(st.Jobs) != 0 {
			t.Fatalf("plan deshabilitado no debe lanzar jobs; hay %d", len(st.Jobs))
		}
		return nil
	})
}

func TestSchedulerVerificaSQLProgramada(t *testing.T) {
	s, st := newSQLSchedulerTest(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Plan con verify_cron vencido y último backup ya ingerido como punto
	// de restauración (con URI de repositorio).
	_ = st.Update(ctx, func(st *store.State) error {
		st.Agents["a1"] = &store.Agent{ID: "a1", Hostname: "sqlhost", EnrolledAt: now}
		st.Storage["tg1"] = &store.StorageTarget{ID: "tg1", Enabled: true}
		st.SQLPlans["p1"] = &store.SQLPlan{
			ID: "p1", AgentID: "a1", Server: "sqlhost", Database: "app",
			Type: "FULL", Schedule: "0 3 * * *", VerifyCron: "0 5 * * 6",
			StorageTargetID: "tg1", Enabled: true,
			NextRunAt:    now.Add(3 * time.Hour), // el backup aún no vence
			NextVerifyAt: now.Add(-time.Minute),  // la verificación sí
			CreatedAt:    now, UpdatedAt: now,
		}
		st.RestorePoints["rp1"] = &store.RestorePoint{
			ID: "rp1", AgentID: "a1", PlanID: "p1", JobID: "j0", Kind: "sql",
			SnapshotID: "s3://bdr/sql/app_FULL_1.bak", CreatedAt: now.Add(-time.Hour),
			SQL: &store.SQLBackupMeta{Instance: "sqlhost", Database: "app", BackupType: "FULL"},
		}
		return nil
	})

	s.runSchedulerTick(ctx)

	var verifyJobID string
	_ = st.View(ctx, func(st *store.State) error {
		for _, j := range st.Jobs {
			if j.Type == contracts.JobTypeSQLVerify {
				verifyJobID = j.ID
				if j.PlanID != "p1" || j.Trigger != "schedule" {
					t.Fatalf("job de verificación inesperado: %+v", j)
				}
				var payload contracts.SQLVerifyJobPayload
				if err := json.Unmarshal(j.Payload, &payload); err != nil {
					t.Fatalf("payload: %v", err)
				}
				if payload.RepoURI != "s3://bdr/sql/app_FULL_1.bak" {
					t.Fatalf("repo_uri: %q", payload.RepoURI)
				}
			}
			if j.Type == contracts.JobTypeSQLBackup {
				t.Fatal("el backup no debía lanzarse (NextRunAt futuro)")
			}
		}
		if p := st.SQLPlans["p1"]; !p.NextVerifyAt.After(now) {
			t.Fatalf("NextVerifyAt no avanzó: %v", p.NextVerifyAt)
		}
		return nil
	})
	if verifyJobID == "" {
		t.Fatal("el tick no creó el job SQL_VERIFY programado")
	}

	// Plan sin backups aún: no crea job ni avanza la verificación.
	_ = st.Update(ctx, func(st *store.State) error {
		st.SQLPlans["p2"] = &store.SQLPlan{
			ID: "p2", AgentID: "a1", Server: "sqlhost", Database: "otra",
			Type: "FULL", Schedule: "0 3 * * *", VerifyCron: "0 5 * * 6",
			StorageTargetID: "tg1", Enabled: true,
			NextVerifyAt: now.Add(-time.Minute),
		}
		return nil
	})
	s.runSchedulerTick(ctx)
	_ = st.View(ctx, func(st *store.State) error {
		p := st.SQLPlans["p2"]
		if !p.NextVerifyAt.Equal(now.Add(-time.Minute)) {
			t.Fatalf("plan sin backups avanzó NextVerifyAt: %v", p.NextVerifyAt)
		}
		return nil
	})
}
