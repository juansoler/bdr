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
