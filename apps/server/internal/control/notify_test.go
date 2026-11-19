package control_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/secrets"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

func testBox(t *testing.T) *secrets.Box {
	t.Helper()
	box, err := secrets.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

// TestNotifyRetryBackoff: un webhook que falla dos veces y luego acepta se
// reintenta con backoff hasta entregarse.
func TestNotifyRetryBackoff(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(nil, "error"), "test")
	api.Box = testBox(t)
	// Settings con el webhook de prueba y severidad mínima baja.
	err := st.Update(context.Background(), func(s *store.State) error {
		s.Settings.WebhookURL = srv.URL
		s.Settings.AlertMinSeverity = contracts.SeverityLow
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	n := control.NewNotifier(api)
	n.RetryBackoff = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}

	// deliver es interno; se ejercita a través del canal: encolar y esperar.
	done := make(chan struct{})
	go func() {
		n.Run(context.Background())
		close(done)
	}()
	n.Enqueue(contracts.AlertView{Key: "k", Kind: "test", Severity: "low", Title: "t", FirstAt: time.Now().UTC()})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if hits.Load() >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := hits.Load(); got < 3 {
		t.Fatalf("esperaba al menos 3 intentos con backoff, got %d", got)
	}
	// El segundo intento no debía producirse sin backoff: con backoff corto,
	// el primer fallo ocurre antes de 50 ms (el test base dura ~30 ms).
}

// TestNotifyRetryExhausted: tras agotar los reintentos no se cuelga y no
// vuelve a intentar (1 + 2 reintentos = 3 peticiones).
func TestNotifyRetryExhausted(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(nil, "error"), "test")
	api.Box = testBox(t)
	err := st.Update(context.Background(), func(s *store.State) error {
		s.Settings.WebhookURL = srv.URL
		s.Settings.AlertMinSeverity = contracts.SeverityLow
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	n := control.NewNotifier(api)
	n.RetryBackoff = []time.Duration{5 * time.Millisecond, 10 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(done)
	}()
	n.Enqueue(contracts.AlertView{Key: "k", Kind: "test", Severity: "low", Title: "t", FirstAt: time.Now().UTC()})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if hits.Load() >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if got := hits.Load(); got != 3 {
		t.Fatalf("esperaba exactamente 3 intentos (1+2 reintentos), got %d", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("el notificador no terminó")
	}
}
