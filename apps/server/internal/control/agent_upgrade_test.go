package control_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/juansoler/bdr/apps/server/internal/selfupdate"
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

// Flujo «Forzar actualización»: POST /admin/agents/{id}/upgrade marca la
// solicitud; el próximo heartbeat entrega la oferta con Force y limpia el
// flag; el agente ya actualizado no recibe oferta y un nuevo force responde
// 409.
func TestAgentUpgradeForce(t *testing.T) {
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
	api.Now = func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	api.PublicURL = ts.URL

	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/enroll", "", contracts.EnrollRequest{
		Token: createToken(t, ts, "casa-02").Token, Hostname: "casa-02",
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

	// Force → 202.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/upgrade", adminToken, nil)
	if status != http.StatusAccepted {
		t.Fatalf("force upgrade: %d %s", status, body)
	}
	// Primer heartbeat: oferta forzada.
	resp := beat("0.0.1-test")
	if resp.Upgrade == nil || !resp.Upgrade.Force {
		t.Fatalf("esperaba oferta forzada: %+v", resp.Upgrade)
	}
	if resp.Upgrade.Version != version.Version || resp.Upgrade.SHA256 != wantSHA {
		t.Fatalf("upgrade forzado incorrecto: %+v", resp.Upgrade)
	}
	// Segundo heartbeat: la oferta sigue (versión distinta) pero sin Force.
	if r := beat("0.0.1-test"); r.Upgrade == nil || r.Upgrade.Force {
		t.Fatalf("la fuerza debe consumirse: %+v", r.Upgrade)
	}
	// Agente actualizado: sin oferta.
	if r := beat(version.Version); r.Upgrade != nil {
		t.Fatalf("no esperaba upgrade: %+v", r.Upgrade)
	}
	// Force con el agente al día → 409.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/upgrade", adminToken, nil)
	if status != http.StatusConflict {
		t.Fatalf("force con agente al día: %d %s", status, body)
	}
}

// Fallback sin agents.json: la oferta sale del canal firmado (último
// manifiesto verificado) cuando el binario servido coincide con su SHA.
func TestAgentUpgradeOfferSignedChannel(t *testing.T) {
	downloads := t.TempDir()
	bin := []byte("binario del agente canal")
	path := filepath.Join(downloads, "bdr-agent-windows-amd64.exe")
	if err := os.WriteFile(path, bin, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bin)
	wantSHA := hex.EncodeToString(sum[:])

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := selfupdate.Manifest{
		Version:    version.Version,
		Channel:    "stable",
		ReleasedAt: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC),
		Server:     map[string]selfupdate.Asset{"windows-amd64": {URL: "http://canal/bundle.zip", SHA256: strings.Repeat("a", 64)}},
		Agents:     map[string]selfupdate.Asset{"windows-amd64": {URL: "http://canal/agent.exe", SHA256: wantSHA}},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, raw)
	channel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "latest.json":
			_, _ = w.Write(raw)
		case "latest.json.sig":
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(sig)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(channel.Close)

	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	box, err := secrets.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	api.Box = box
	api.DownloadsDir = downloads
	api.PublicURL = "" // sin URL pública: solo el canal firmado puede ofrecer
	api.UpdateURL = channel.URL + "/latest.json"
	api.UpdatePublicKey = base64.StdEncoding.EncodeToString(pub)
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)

	// Chequeo del canal (POST /check) → manifiesto verificado en memoria.
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/system/update/check", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("check del canal: %d %s", status, body)
	}

	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/enroll", "", contracts.EnrollRequest{
		Token: createToken(t, ts, "casa-03").Token, Hostname: "casa-03",
		OSFamily: "windows", Arch: "amd64", AgentVersion: "0.0.1-test",
	})
	if status != http.StatusCreated {
		t.Fatalf("enroll: %d %s", status, body)
	}
	var enrolled contracts.EnrollResponse
	if err := json.Unmarshal(body, &enrolled); err != nil {
		t.Fatal(err)
	}
	s, b := doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/heartbeat", enrolled.APIKey,
		contracts.HeartbeatRequest{AgentVersion: "0.0.1-test", State: contracts.StateIdle})
	if s != http.StatusOK {
		t.Fatalf("heartbeat: %d %s", s, b)
	}
	var out contracts.HeartbeatResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Upgrade == nil {
		t.Fatal("esperaba oferta desde el canal firmado")
	}
	if out.Upgrade.Version != version.Version || !strings.EqualFold(out.Upgrade.SHA256, wantSHA) {
		t.Fatalf("oferta del canal incorrecta: %+v", out.Upgrade)
	}
	if !strings.HasPrefix(out.Upgrade.URL, "/api/v1/download/agent/") {
		t.Fatalf("la URL debe ser relativa al endpoint del agente: %s", out.Upgrade.URL)
	}

	// Binario servido que ya no coincide con el canal → sin oferta (sin bucles).
	other := []byte("binario corrupto")
	if err := os.WriteFile(path, other, 0o755); err != nil {
		t.Fatal(err)
	}
	s, b = doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/heartbeat", enrolled.APIKey,
		contracts.HeartbeatRequest{AgentVersion: "0.0.1-test", State: contracts.StateIdle})
	if s != http.StatusOK {
		t.Fatalf("heartbeat 2: %d %s", s, b)
	}
	out = contracts.HeartbeatResponse{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Upgrade != nil {
		t.Fatalf("binario ajeno al canal no debe ofrecerse: %+v", out.Upgrade)
	}
}
