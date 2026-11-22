// Tests de regresión del stream WebSocket de pantalla (VNC/noVNC): el mismo
// contrato que la terminal — creación con ticket, handshake 101 con ticket
// válido, 401 con ticket inválido — y ciclo de cierre ordenado.
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

func newScreenAPI(t *testing.T) *httptest.Server {
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

func TestScreenStreamHandshakeWithTicket(t *testing.T) {
	ts := newScreenAPI(t)

	// 1. Crear sesión (cuerpo vacío: campos opcionales).
	req, err := http.NewRequest(http.MethodPost,
		ts.URL+"/api/v1/admin/agents/agent-1/screen/sessions",
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
		t.Fatalf("crear sesión de pantalla: status %d, cuerpo: %s", res.StatusCode, body)
	}
	var view struct {
		SessionID    string `json:"session_id"`
		StreamTicket string `json:"stream_ticket"`
		StreamURL    string `json:"stream_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.SessionID == "" || view.StreamTicket == "" {
		t.Fatalf("respuesta sin session_id o stream_ticket: %+v", view)
	}
	if !strings.Contains(view.StreamURL, "/api/v1/admin/screen/sessions/"+view.SessionID+"/stream") {
		t.Fatalf("stream_url inesperada: %q", view.StreamURL)
	}

	// 2. Handshake WebSocket CON ticket: debe negociar (101), no 501/401.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/api/v1/admin/screen/sessions/" + view.SessionID + "/stream?ticket=" + view.StreamTicket
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("handshake WebSocket con ticket: %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "fin del test")

	// 3. Ticket inválido → 401 (nunca 501).
	badRes, err := http.Get(ts.URL + "/api/v1/admin/screen/sessions/" + view.SessionID + "/stream?ticket=invalido")
	if err != nil {
		t.Fatal(err)
	}
	defer badRes.Body.Close()
	if badRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ticket inválido: esperado 401, obtenido %d", badRes.StatusCode)
	}
}

func TestScreenCloseSession(t *testing.T) {
	ts := newScreenAPI(t)

	// Crear sesión.
	req, err := http.NewRequest(http.MethodPost,
		ts.URL+"/api/v1/admin/agents/agent-1/screen/sessions", strings.NewReader("{}"))
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
	var view struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}

	// Cerrarla (screen:terminate; el admin token tiene todas las caps).
	closeReq, err := http.NewRequest(http.MethodPost,
		ts.URL+"/api/v1/admin/screen/sessions/"+view.SessionID+"/close?reason=prueba", nil)
	if err != nil {
		t.Fatal(err)
	}
	closeReq.Header.Set("Authorization", "Bearer "+adminToken)
	closeRes, err := http.DefaultClient.Do(closeReq)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRes.Body.Close()
	if closeRes.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(closeRes.Body)
		t.Fatalf("cerrar sesión: status %d, cuerpo: %s", closeRes.StatusCode, body)
	}

	// El estado persistido debe ser closed con el motivo.
	getReq, err := http.NewRequest(http.MethodGet,
		ts.URL+"/api/v1/admin/screen/sessions/"+view.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	getReq.Header.Set("Authorization", "Bearer "+adminToken)
	getRes, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getRes.Body.Close()
	if getRes.StatusCode != http.StatusOK {
		t.Fatalf("consultar sesión cerrada: status %d", getRes.StatusCode)
	}
	var got struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(getRes.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.State != "closed" {
		t.Fatalf("estado tras cierre: %q (esperado closed)", got.State)
	}
	if got.Reason != "prueba" {
		t.Fatalf("motivo tras cierre: %q (esperado prueba)", got.Reason)
	}
}

func TestScreenQualityValidation(t *testing.T) {
	ts := newScreenAPI(t)

	req, err := http.NewRequest(http.MethodPost,
		ts.URL+"/api/v1/admin/agents/agent-1/screen/sessions",
		strings.NewReader(`{"quality":"ultra"}`))
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
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("quality inválida: esperado 400, obtenido %d", res.StatusCode)
	}
}
