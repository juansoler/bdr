package control_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/secrets"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
	"github.com/juansoler/bdr/packages/version"
)

// Flujo de autoactualización: agente enrolado con versión distinta de la del
// servidor + binario publicado en el directorio de descargas → el heartbeat
// responde upgrade con URL y SHA-256 correctos. Misma versión o sin binario →
// sin oferta.
func TestAgentUpgradeOffer(t *testing.T) {
	downloads := t.TempDir()
	bin := []byte("binario del agente v2")
	path := filepath.Join(downloads, "bdr-agent-windows-amd64.exe")
	if err := os.WriteFile(path, bin, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bin)
	wantSHA := hex.EncodeToString(sum[:])

	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	box, err := secrets.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	api.Box = box
	api.DownloadsDir = downloads
	fixed := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	api.Now = func() time.Time { return fixed }
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	// PublicURL se fija tras crear el servidor: los handlers lo leen en vivo.
	api.PublicURL = ts.URL

	// Enrolamiento de un agente Windows amd64 con versión antigua.
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/enroll", "", contracts.EnrollRequest{
		Token: createToken(t, ts, "casa-01").Token, Hostname: "casa-01",
		OSFamily: "windows", Arch: "amd64", AgentVersion: "0.0.1-test",
	})
	if status != http.StatusCreated {
		t.Fatalf("enroll: %d %s", status, body)
	}
	var enrolled contracts.EnrollResponse
	if err := json.Unmarshal(body, &enrolled); err != nil {
		t.Fatal(err)
	}

	beat := func(reported string) contracts.HeartbeatResponse {
		s, b := doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/heartbeat", enrolled.APIKey,
			contracts.HeartbeatRequest{AgentVersion: reported, State: contracts.StateIdle})
		if s != http.StatusOK {
			t.Fatalf("heartbeat: %d %s", s, b)
		}
		var out contracts.HeartbeatResponse
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Versión distinta → oferta con SHA-256 correcto.
	resp := beat("0.0.1-test")
	if resp.Upgrade == nil {
		t.Fatal("esperaba oferta de upgrade")
	}
	if resp.Upgrade.Version != version.Version || resp.Upgrade.SHA256 != wantSHA {
		t.Fatalf("upgrade incorrecto: %+v (esperaba version %s sha %s)", resp.Upgrade, version.Version, wantSHA)
	}
	if !strings.Contains(resp.Upgrade.URL, "/api/v1/download/agent/windows-amd64") {
		t.Fatalf("url de upgrade incorrecta: %s", resp.Upgrade.URL)
	}

	// La URL del binario debe servir exactamente lo prometido.
	res, err := http.Get(resp.Upgrade.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(got) != string(bin) {
		t.Fatalf("descarga del binario: %d %q", res.StatusCode, got)
	}

	// Misma versión que el binario del servidor → sin oferta.
	if r := beat(version.Version); r.Upgrade != nil {
		t.Fatalf("no esperaba upgrade: %+v", r.Upgrade)
	}

	// Sin binario publicado para el osarch → sin oferta.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if r := beat("0.0.1-test"); r.Upgrade != nil {
		t.Fatalf("sin binario no debe haber upgrade: %+v", r.Upgrade)
	}
}
