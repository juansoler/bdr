package store

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

func osReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func containsRaw(b []byte, sub string) bool { return bytes.Contains(b, []byte(sub)) }

// storeFixture describe un backend para la batería de contrato del Store.
// reopen cierra el almacén previo y devuelve uno nuevo sobre el mismo
// almacenamiento; durable indica si lo persistido sobrevive a reopen.
// peek comprueba el almacenamiento SIN pasar por el almacén (debe reflejar
// solo lo escrito, no lo diferido); nil si el backend no persiste.
type storeFixture struct {
	make    func(t *testing.T) Store
	reopen  func(t *testing.T, prev Store) Store
	peek    func(t *testing.T, key string) bool
	durable bool
}

// runStoreContract ejecuta los comportamientos que todo Store debe cumplir.
// La batería es la garantía de paridad entre memory, file y postgres: cada
// implementación nueva debe pasarla sin cambios.
func runStoreContract(t *testing.T, fx storeFixture) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("update_view_y_persistencia", func(t *testing.T) {
		st := fx.make(t)
		if err := st.Update(ctx, func(s *State) error {
			s.Agents["a1"] = &Agent{ID: "a1", Hostname: "h1", OSFamily: "linux", EnrolledAt: now}
			s.Plans["p1"] = &Plan{ID: "p1", Name: "diario", Kind: "file", Paths: []string{"/data"}, CreatedAt: now, UpdatedAt: now}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if fx.durable {
			if !fx.peek(t, "p1") {
				t.Fatal("Update debió escribir en el almacenamiento")
			}
			st = fx.reopen(t, st)
		}
		defer func() {
			if st != nil {
				_ = st.Close()
			}
		}()
		if err := st.View(ctx, func(s *State) error {
			if a := s.Agents["a1"]; a == nil || a.Hostname != "h1" {
				t.Fatal("agente no visible")
			}
			if p := s.Plans["p1"]; p == nil || len(p.Paths) != 1 {
				t.Fatal("plan no visible")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("update_con_error_no_persiste", func(t *testing.T) {
		st := fx.make(t)
		if err := st.Update(ctx, func(s *State) error {
			s.Plans["px"] = &Plan{ID: "px", Name: "no"}
			return ErrConflict
		}); err == nil {
			t.Fatal("esperaba error")
		}
		// La mutación puede quedar en RAM (documentado), pero nunca en el
		// almacenamiento: al reabrir, px no existe.
		if fx.durable {
			if fx.peek(t, "px") {
				t.Fatal("un update fallido no debió escribir en el almacenamiento")
			}
			st = fx.reopen(t, st)
			defer st.Close()
			if err := st.View(ctx, func(s *State) error {
				if s.Plans["px"] != nil {
					t.Fatal("un update fallido no debió sobrevivir al reinicio")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return
		}
		_ = st.Close()
	})

	t.Run("lazy_flush", func(t *testing.T) {
		st := fx.make(t)
		if err := st.Update(ctx, func(s *State) error {
			s.Plans["p1"] = &Plan{ID: "p1", Name: "x", CreatedAt: now, UpdatedAt: now}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.UpdateLazy(ctx, func(s *State) error {
			s.Plans["p2"] = &Plan{ID: "p2", Name: "y", CreatedAt: now, UpdatedAt: now}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if fx.durable {
			// p2 va diferido: en el almacenamiento aún no está.
			if fx.peek(t, "p2") {
				t.Fatal("UpdateLazy no debía escribir antes de Flush")
			}
			if err := st.Flush(); err != nil {
				t.Fatal(err)
			}
			if !fx.peek(t, "p2") {
				t.Fatal("Flush debió escribir los cambios diferidos")
			}
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("auditoria_persiste_y_verifica", func(t *testing.T) {
		st := fx.make(t)
		for i := 0; i < 3; i++ {
			if _, err := st.Audit().Append(contracts.AuditEvent{Action: "test.op", ActorName: "x"}); err != nil {
				t.Fatal(err)
			}
		}
		if fx.durable {
			st = fx.reopen(t, st)
		}
		defer func() {
			if st != nil {
				_ = st.Close()
			}
		}()
		if got := st.Audit().Len(); got != 3 {
			t.Fatalf("eventos tras reabrir: %d", got)
		}
		if v := st.Audit().Verify(); !v.OK || v.LastSeq != 3 {
			t.Fatalf("verify: %+v", v)
		}
	})

	t.Run("secuencia_de_eventos_continua", func(t *testing.T) {
		st := fx.make(t)
		if err := st.Update(ctx, func(s *State) error {
			for i := 0; i < 2; i++ {
				s.AppendJobEvent(contracts.JobEvent{JobID: "j1", Kind: "state", State: "pending", At: now})
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if fx.durable {
			st = fx.reopen(t, st)
			defer st.Close()
		}
		var lastSeq int
		if err := st.Update(ctx, func(s *State) error {
			ev := s.AppendJobEvent(contracts.JobEvent{JobID: "j1", Kind: "log", Message: "m", At: now})
			lastSeq = ev.Seq
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if lastSeq != 3 {
			t.Fatalf("seq tras reabrir: %d, esperaba 3", lastSeq)
		}
	})
}

// memoryFixture: sin persistencia; reopen devuelve un almacén vacío.
func memoryFixture() storeFixture {
	return storeFixture{
		make: func(t *testing.T) Store { return NewMemory() },
		reopen: func(t *testing.T, prev Store) Store {
			_ = prev.Close()
			return NewMemory()
		},
		durable: false,
	}
}

func TestContractMemory(t *testing.T) {
	runStoreContract(t, memoryFixture())
}

func TestContractFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/state.json"
	runStoreContract(t, storeFixture{
		make: func(t *testing.T) Store {
			st, err := NewFile(path)
			if err != nil {
				t.Fatal(err)
			}
			return st
		},
		reopen: func(t *testing.T, prev Store) Store {
			_ = prev.Close()
			st, err := NewFile(path)
			if err != nil {
				t.Fatal(err)
			}
			return st
		},
		peek: func(t *testing.T, key string) bool {
			b, err := osReadFile(path)
			if err != nil {
				return false
			}
			return containsRaw(b, `"ID":"`+key+`"`)
		},
		durable: true,
	})
}
