package client

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juansoler/bdr/packages/contracts"
)

func TestEnrollAndHeartbeatContract(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/enroll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req contracts.EnrollRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Token != "tok" || req.Hostname != "lab-01" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusCreated, contracts.EnrollResponse{
			AgentID: "agent-1", APIKey: "key-1", HeartbeatSeconds: 30,
		})
	})
	mux.HandleFunc("/api/v1/agent/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, contracts.HeartbeatResponse{PendingJobs: []string{}})
	})
	mux.HandleFunc("/api/v1/agent/inventory", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/v1/agent/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, contracts.AgentView{ID: "agent-1", Status: contracts.AgentOnline})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	anon, err := New(ts.URL, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	enrolled, err := anon.Enroll(context.Background(), contracts.EnrollRequest{
		Token: "tok", Hostname: "lab-01", OSFamily: "linux", Arch: "amd64",
	})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if enrolled.APIKey != "key-1" {
		t.Fatalf("api key: %q", enrolled.APIKey)
	}

	authed, err := New(ts.URL, enrolled.APIKey)
	if err != nil {
		t.Fatalf("new authed: %v", err)
	}
	if _, err := authed.Heartbeat(context.Background(), contracts.HeartbeatRequest{}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if err := authed.PutInventory(context.Background(), contracts.Inventory{Hostname: "lab-01"}); err != nil {
		t.Fatalf("inventory: %v", err)
	}
	me, err := authed.Me(context.Background())
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if me.ID != "agent-1" {
		t.Fatalf("me id: %s", me.ID)
	}
}

func TestAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusGone)
		_ = json.NewEncoder(w).Encode(contracts.Problem{
			Status: http.StatusGone, Code: "E_ENROLL_USED", Detail: "usado",
		})
	}))
	defer ts.Close()

	c, err := New(ts.URL, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = c.Enroll(context.Background(), contracts.EnrollRequest{})
	if err == nil {
		t.Fatal("esperaba error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("tipo de error inesperado: %T", err)
	}
	if apiErr.Status != http.StatusGone || apiErr.Code != "E_ENROLL_USED" {
		t.Fatalf("error inesperado: %+v", apiErr)
	}
}

func TestInvalidURL(t *testing.T) {
	for _, u := range []string{"", "ftp://x", "://bad"} {
		if _, err := New(u, ""); err == nil {
			t.Fatalf("esperaba error para %q", u)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestPinVerification(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, contracts.HeartbeatResponse{})
	}))
	defer ts.Close()

	pin := FingerprintDER(ts.Certificate().Raw)
	ok, err := NewWithOptions(Options{BaseURL: ts.URL, ServerFingerprint: "sha256:" + pin})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := ok.Heartbeat(context.Background(), contracts.HeartbeatRequest{}); err != nil {
		t.Fatalf("la huella correcta fue rechazada: %v", err)
	}

	bad, err := NewWithOptions(Options{BaseURL: ts.URL, ServerFingerprint: "sha256:" + strings.Repeat("00", 32)})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := bad.Heartbeat(context.Background(), contracts.HeartbeatRequest{}); err == nil {
		t.Fatal("una huella incorrecta debe fallar")
	}
}

func TestCAConfiada(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, contracts.HeartbeatResponse{})
	}))
	defer ts.Close()

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw})
	c, err := NewWithOptions(Options{BaseURL: ts.URL, CAPEM: caPEM})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := c.Heartbeat(context.Background(), contracts.HeartbeatRequest{}); err != nil {
		t.Fatalf("la CA indicada debería validar el certificado: %v", err)
	}

	// Sin CA ni huella, un certificado autofirmado no es de confianza.
	untrusted, err := NewWithOptions(Options{BaseURL: ts.URL})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := untrusted.Heartbeat(context.Background(), contracts.HeartbeatRequest{}); err == nil {
		t.Fatal("esperaba fallo de verificación TLS")
	}
}

func TestPinVerificaCadena(t *testing.T) {
	if err := VerifyPinnedChain(nil, "00", "host"); err == nil {
		t.Fatal("una cadena vacía no puede validar")
	}
	if err := VerifyPinnedChain([][]byte{[]byte("no-importa")}, "00", "host"); err == nil {
		t.Fatal("una huella que no coincide debe fallar")
	}
}

func TestJobProgressAcepta204Y200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/jobs/j1/progress", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/v1/agent/jobs/j2/progress", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, contracts.JobProgressResponse{CancelRequested: true})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	c, err := New(ts.URL, "k")
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.JobProgress(context.Background(), "j1", contracts.JobProgressRequest{Progress: 10})
	if err != nil || p.CancelRequested {
		t.Fatalf("204: %+v %v", p, err)
	}
	p, err = c.JobProgress(context.Background(), "j2", contracts.JobProgressRequest{Progress: 20})
	if err != nil || !p.CancelRequested {
		t.Fatalf("200: %+v %v", p, err)
	}
}

func TestRepositoryCredentialsNoConfigurado(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/repository-credentials", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(contracts.Problem{
			Status: http.StatusNotFound, Code: CodeRepoUnconfigured, Detail: "sin repositorio",
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	c, _ := New(ts.URL, "k")
	creds, err := c.RepositoryCredentials(context.Background())
	if err != nil || creds != nil {
		t.Fatalf("creds=%+v err=%v", creds, err)
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	for _, in := range []string{"sha256:ABCDEF", "SHA256:abcdef", "abcdef", "sha256:ab:cd:ef"} {
		if got := NormalizeFingerprint(in); got != "abcdef" {
			t.Errorf("NormalizeFingerprint(%q) = %q", in, got)
		}
	}
}

// Las rutas con query (?target=...) deben llegar al servidor como query,
// no como parte del path (un path con "?" produce 404 en el servidor).
func TestRepositoryCredentialsForQuery(t *testing.T) {
	var gotPath, gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/repository-credentials", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Get("target")
		if r.Header.Get("Authorization") != "Bearer key-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, contracts.RepositoryCredentials{
			RepositoryID: "target-1", Kind: contracts.RepoKindS3,
			Endpoint: "203.0.113.5:9000", Bucket: "bdr-repo", Password: "pw",
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := New(ts.URL, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	creds, err := c.RepositoryCredentialsFor(context.Background(), "target-1")
	if err != nil {
		t.Fatalf("RepositoryCredentialsFor: %v", err)
	}
	if creds.Kind != contracts.RepoKindS3 || creds.Bucket != "bdr-repo" {
		t.Fatalf("creds: %+v", creds)
	}
	if gotPath != "/api/v1/agent/repository-credentials" || gotQuery != "target-1" {
		t.Fatalf("servidor recibió path=%q query=%q", gotPath, gotQuery)
	}
}
