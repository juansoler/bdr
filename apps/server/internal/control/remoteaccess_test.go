// Tests de regresión del stream WebSocket de terminal: el middleware de logs
// envuelve el ResponseWriter y, si no delega http.Hijacker, el handshake
// responde 501 en vez de negociar.
package control_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/logging"
)

func newTerminalAPI(t *testing.T) *httptest.Server {
	t.Helper()
	st := store.NewMemory()
	if err := st.Update(context.Background(), func(s *store.State) error {
		s.Agents["agent-1"] = &store.Agent{ID: "agent-1", Hostname: "test-host", Status: "online"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestTerminalStreamHandshakeWithTicket(t *testing.T) {
	ts := newTerminalAPI(t)

	// 1. Crear sesión (cuerpo vacío: campos opcionales).
	req, err := http.NewRequest(http.MethodPost,
		ts.URL+"/api/v1/admin/agents/agent-1/terminal/sessions",
		strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("crear sesión: status %d, cuerpo: %s", res.StatusCode, body)
	}
	var view struct {
		SessionID    string `json:"session_id"`
		StreamTicket string `json:"stream_ticket"`
	}
	if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.SessionID == "" || view.StreamTicket == "" {
		t.Fatalf("respuesta sin session_id o stream_ticket: %+v", view)
	}

	// 2. Handshake WebSocket CON ticket: debe negociar (101), no 501/401.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/api/v1/admin/terminal/sessions/" + view.SessionID + "/stream?ticket=" + view.StreamTicket
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("handshake WebSocket con ticket: %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "fin del test")

	// 3. Ticket inválido → 401 (nunca 501).
	badRes, err := http.Get(ts.URL + "/api/v1/admin/terminal/sessions/" + view.SessionID + "/stream?ticket=invalido")
	if err != nil {
		t.Fatal(err)
	}
	defer badRes.Body.Close()
	_ = badRes.Body.Close()
	if badRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ticket inválido: esperado 401, obtenido %d", badRes.StatusCode)
	}
}
