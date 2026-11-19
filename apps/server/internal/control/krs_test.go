package control_test

import (
	"bytes"
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
)

// newFakeKopia escribe un binario «kopia» que registra sus invocaciones en un
// log y simula el ciclo de vida de los usuarios del repositorio (list/add/set
// sobre un fichero de usuarios). Devuelve la ruta del binario y rutas de apoyo.
func newFakeKopia(t *testing.T) (bin, callsLog, usersFile string) {
	t.Helper()
	dir := t.TempDir()
	callsLog = filepath.Join(dir, "calls.log")
	usersFile = filepath.Join(dir, "users.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + callsLog + "\n"
	script += `
cmd=""
for a in "$@"; do
  case "$a" in
    server|users|list|add|set|delete|info) cmd="$cmd $a" ;;
  esac
done
case "$cmd" in
  *"users list"*)
    if [ -f ` + usersFile + ` ]; then
      printf '['
      first=yes
      while IFS= read -r u; do
        [ $first = yes ] || printf ','
        printf '{"username":"%s"}' "$u"
        first=no
      done < ` + usersFile + `
      printf ']'
    else
      printf '[]'
    fi
    exit 0
    ;;
  *"users add"*|*"users set"*)
    u=""
    for a in "$@"; do case "$a" in *@*) u="$a" ;; esac; done
    echo "$u" >> ` + usersFile + `
    exit 0
    ;;
  *"users delete"*)
    u=""; for a in "$@"; do case "$a" in *@*) u="$a" ;; esac; done
    grep -v "^$u$" ` + usersFile + ` > ` + usersFile + `.tmp 2>/dev/null
    mv ` + usersFile + `.tmp ` + usersFile + `
    exit 0
    ;;
esac
exit 0
`
	bin = filepath.Join(dir, "fake-kopia")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, callsLog, usersFile
}

// krsEnv agrupa el servidor de pruebas y su estado.
type krsEnv struct {
	ts  *httptest.Server
	st  store.Store
	api *control.Server
}

// newKRSTestAPI monta un servidor de pruebas en modo server con kopia falso.
func newKRSTestAPI(t *testing.T, bin, kind, repoPath string) *krsEnv {
	t.Helper()
	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	fixed := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	api.Now = func() time.Time { return fixed }
	box, err := secrets.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	api.Box = box
	api.Kopia = control.NewKopiaControl(bin, filepath.Join(t.TempDir(), "kopia.config"), "repo-pw", kind,
		repoPath, "https://kopia.test:51515", "sha256:aa", "", "")
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	return &krsEnv{ts: ts, st: st, api: api}
}

// TestRepoCredentialsProvisionsServerUser: en modo server, la entrega de
// credenciales crea el usuario en el repositorio; la rotación lo actualiza
// sin cambiar el nombre; revocar lo elimina.
func TestRepoCredentialsProvisionsServerUser(t *testing.T) {
	bin, callsLog, usersFile := newFakeKopia(t)
	env := newKRSTestAPI(t, bin, "server", "")

	tok := createToken(t, env.ts, "lab-01")
	enrolled := enrollAgent(t, env.ts, tok.Token, "lab-01")

	status, body := doJSON(t, http.MethodGet, env.ts.URL+"/api/v1/agent/repository-credentials", enrolled.APIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("credentials: %d %s", status, body)
	}
	var creds contracts.RepositoryCredentials
	_ = json.Unmarshal(body, &creds)
	if creds.Kind != "server" || creds.Username == "" || creds.Password == "" || creds.URL == "" {
		t.Fatalf("credenciales incompletas: %+v", creds)
	}
	if creds.Version != 1 {
		t.Fatalf("versión: %d", creds.Version)
	}
	if id := creds.Username + "@" + creds.Hostname; !userInFile(t, usersFile, id) {
		t.Fatalf("usuario %s no aprovisionado en el repositorio", id)
	}

	// Segunda entrega: mismo usuario, misma versión (idempotente).
	status, body = doJSON(t, http.MethodGet, env.ts.URL+"/api/v1/agent/repository-credentials", enrolled.APIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("segunda credentials: %d %s", status, body)
	}
	var again contracts.RepositoryCredentials
	_ = json.Unmarshal(body, &again)
	if again.Username != creds.Username || again.Version != 1 {
		t.Fatalf("credencial no estable: %+v", again)
	}

	// Vista administrativa sin contraseña.
	status, body = doJSON(t, http.MethodGet, env.ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/repo-credentials", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("vista admin: %d %s", status, body)
	}
	if strings.Contains(string(body), creds.Password) {
		t.Fatal("la vista administrativa no debe incluir la contraseña")
	}

	// Rotación: versión 2 y `users set` en el log.
	status, body = doJSON(t, http.MethodPost, env.ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/repo-credentials/rotate", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("rotate: %d %s", status, body)
	}
	var rotated contracts.RepoCredsView
	_ = json.Unmarshal(body, &rotated)
	if rotated.Version != 2 || rotated.PendingApply {
		t.Fatalf("rotación: %+v", rotated)
	}
	status, body = doJSON(t, http.MethodGet, env.ts.URL+"/api/v1/agent/repository-credentials", enrolled.APIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("credentials tras rotar: %d %s", status, body)
	}
	var after contracts.RepositoryCredentials
	_ = json.Unmarshal(body, &after)
	if after.Version != 2 || after.Password == creds.Password {
		t.Fatalf("credencial tras rotar: versión=%d", after.Version)
	}

	// Revocación: el usuario desaparece del repositorio.
	status, _ = doJSON(t, http.MethodPost, env.ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/revoke", adminToken,
		contracts.RevokeRequest{Reason: "prueba"})
	if status != http.StatusNoContent {
		t.Fatalf("revoke: %d", status)
	}
	if userInFile(t, usersFile, creds.Username+"@"+creds.Hostname) {
		t.Fatal("el usuario del repositorio debió eliminarse al revocar")
	}

	// En modo server debe haber llamadas a users add y users set.
	calls := readFile(t, callsLog)
	if !strings.Contains(calls, "users add") || !strings.Contains(calls, "users set") {
		t.Fatalf("llamadas esperadas ausentes: %s", calls)
	}
}

// TestRepoCredentialsFilesystemNoProvisioning: en modo filesystem no se
// gestionan usuarios del repositorio.
func TestRepoCredentialsFilesystemNoProvisioning(t *testing.T) {
	bin, callsLog, usersFile := newFakeKopia(t)
	env := newKRSTestAPI(t, bin, "filesystem", "/tmp/repo")

	tok := createToken(t, env.ts, "lab-01")
	enrolled := enrollAgent(t, env.ts, tok.Token, "lab-01")

	status, body := doJSON(t, http.MethodGet, env.ts.URL+"/api/v1/agent/repository-credentials", enrolled.APIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("credentials: %d %s", status, body)
	}
	var creds contracts.RepositoryCredentials
	_ = json.Unmarshal(body, &creds)
	if creds.Kind != "filesystem" || creds.Username != "" {
		t.Fatalf("credenciales filesystem: %+v", creds)
	}
	if userInFile(t, usersFile, "@") {
		t.Fatal("filesystem no debe aprovisionar usuarios")
	}
	if strings.Contains(readFile(t, callsLog), "users") {
		t.Fatal("filesystem no debe invocar server users")
	}
}

func userInFile(t *testing.T, path, id string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if l == id {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
