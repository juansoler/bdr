package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

func TestMemoryUpdateAndView(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC()

	err := st.Update(ctx, func(s *State) error {
		s.Agents["a1"] = &Agent{ID: "a1", Hostname: "h1", EnrolledAt: now}
		s.AppendJobEvent(contracts.JobEvent{JobID: "j1", Kind: "state", State: "pending", At: now})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = st.View(ctx, func(s *State) error {
		if s.Agents["a1"] == nil || s.Agents["a1"].Hostname != "h1" {
			t.Fatal("agente no visible")
		}
		if len(s.JobEvents["j1"]) != 1 || s.JobEvents["j1"][0].Seq != 1 {
			t.Fatal("evento no visible")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuditChain(t *testing.T) {
	l := NewMemoryAudit()
	for i := 0; i < 5; i++ {
		if _, err := l.Append(contracts.AuditEvent{Action: "test.op", ActorName: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	v := l.Verify()
	if !v.OK || v.Events != 5 || v.LastSeq != 5 {
		t.Fatalf("verify: %+v", v)
	}
	// Alterar un evento rompe la cadena.
	l.mu.Lock()
	l.events[2].Action = "tampered"
	l.mu.Unlock()
	if v := l.Verify(); v.OK || v.BrokenAt == 0 {
		t.Fatalf("esperaba cadena rota: %+v", v)
	}
}

func TestAuditFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l, err := OpenAuditFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := l.Append(contracts.AuditEvent{Action: "a", Result: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l2, err := OpenAuditFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	if v := l2.Verify(); !v.OK || v.Events != 3 {
		t.Fatalf("recarga: %+v", v)
	}
}

func TestFileMigrationFromLegacy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	legacy := `{
  "tokens": [{"ID":"t1","TokenHash":"abc","CreatedAt":"2026-01-01T00:00:00Z","ExpiresAt":"2026-01-02T00:00:00Z"}],
  "agents": [{"ID":"a1","Hostname":"legacy","Status":"online","EnrolledAt":"2026-01-01T00:00:00Z","APIKeyHash":"k"}],
  "plans": [{"ID":"p1","Name":"plan","Kind":"file","Paths":["/data"],"Enabled":true,"CreatedAt":"2026-01-01T00:00:00Z"}],
  "assignments": [{"ID":"as1","PlanID":"p1","AgentID":"a1","TargetType":"","Enabled":true,"CreatedAt":"2026-01-01T00:00:00Z"}],
  "jobs": [{"ID":"j1","Type":"BACKUP_FILE","State":"succeeded","AgentID":"a1","PlanID":"p1","Progress":100,"CreatedAt":"2026-01-01T00:00:00Z","UpdatedAt":"2026-01-01T00:00:00Z"}]
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := os.Stat(path + ".v1.bak"); err != nil {
		t.Fatal("falta la copia del formato antiguo")
	}
	ctx := context.Background()
	err = f.View(ctx, func(s *State) error {
		if len(s.Agents) != 1 || s.Agents["a1"] == nil {
			t.Fatal("agente no migrado")
		}
		if a := s.Assignments["as1"]; a.TargetType != contracts.TargetAgent || a.TargetID != "a1" || a.AgentID != "a1" {
			t.Fatalf("asignación mal normalizada: %+v", a)
		}
		if p := s.Plans["p1"]; p.Timezone == "" || p.UpdatedAt.IsZero() {
			t.Fatal("plan sin normalizar")
		}
		if j := s.Jobs["j1"]; j.MaxAttempts == 0 {
			t.Fatal("job sin max_attempts")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Segunda apertura: ya no es legacy.
	f.Close()
	f2, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	if err := f2.View(ctx, func(s *State) error {
		if len(s.Agents) != 1 {
			t.Fatal("estado perdido al reabrir")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFileRoundTripAndLazyFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	f, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()

	// Update persiste inmediatamente.
	if err := f.Update(ctx, func(s *State) error {
		s.Plans["p1"] = &Plan{ID: "p1", Name: "x", CreatedAt: now}
		s.Alerts["al1"] = &Alert{ID: "al1", Key: "k", State: contracts.AlertOpen, FirstAt: now, LastAt: now}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f2, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f2.View(ctx, func(s *State) error {
		if s.Plans["p1"] == nil || s.Alerts["al1"] == nil {
			t.Fatal("Update no persistió")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f2.Close()

	// UpdateLazy no persiste hasta Flush.
	if err := f.UpdateLazy(ctx, func(s *State) error {
		s.Plans["p2"] = &Plan{ID: "p2", Name: "y", CreatedAt: now}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f3, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f3.View(ctx, func(s *State) error {
		if s.Plans["p2"] != nil {
			t.Fatal("UpdateLazy no debió persistir aún")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f3.Close()
	if err := f.Flush(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	f4, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f4.Close()
	if err := f4.View(ctx, func(s *State) error {
		if s.Plans["p2"] == nil {
			t.Fatal("Flush no escribió los cambios diferidos")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateErrorNoPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	f, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx := context.Background()
	err = f.Update(ctx, func(s *State) error {
		s.Plans["px"] = &Plan{ID: "px", Name: "no"}
		return ErrConflict
	})
	if err == nil {
		t.Fatal("esperaba error")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("no debía existir state.json tras un update fallido")
	}
}

func TestAppendJobEventTrims(t *testing.T) {
	st := NewMemory()
	now := time.Now().UTC()
	if err := st.Update(context.Background(), func(s *State) error {
		for i := 0; i < MaxJobEvents+20; i++ {
			s.AppendJobEvent(contracts.JobEvent{JobID: "j", Kind: "log", Message: "m", At: now})
		}
		list := s.JobEvents["j"]
		if len(list) != MaxJobEvents {
			t.Fatalf("len=%d", len(list))
		}
		if list[len(list)-1].Seq != MaxJobEvents+20 {
			t.Fatalf("seq final=%d", list[len(list)-1].Seq)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFutureStateRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	b, _ := json.Marshal(map[string]any{"meta": map[string]any{"SchemaVersion": SchemaVersion + 5}})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFile(path); err == nil {
		t.Fatal("esperaba rechazo de un estado más nuevo")
	}
}
