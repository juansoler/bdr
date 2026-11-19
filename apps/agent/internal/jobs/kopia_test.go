package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

const snapshotFixture = `{
  "id": "7bd6c0f1a2b3",
  "source": {"host": "equipo-01", "userName": "root", "path": "/srv/datos"},
  "description": "bdr:job=job-1",
  "startTime": "2026-11-16T10:00:00.123456Z",
  "endTime": "2026-11-16T10:00:05.654321Z",
  "rootEntry": {
    "name": "datos", "type": "d", "obj": "kc8e1234",
    "summ": {"size": 11, "files": 2, "symlinks": 0, "dirs": 2, "numFailed": 1}
  },
  "stats": {"excludedFileCount": 3, "excludedDirCount": 1, "errorCount": 0}
}`

func TestParseSnapshotJSON(t *testing.T) {
	// Kopia puede escribir una línea de progreso antes del objeto JSON.
	data := []byte("Snapshotting ...\n" + snapshotFixture + "\n")
	snap, err := parseSnapshotJSON(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap.ID != "7bd6c0f1a2b3" {
		t.Fatalf("id = %q", snap.ID)
	}
	if snap.RootEntry.Obj != "kc8e1234" {
		t.Fatalf("obj = %q", snap.RootEntry.Obj)
	}
	if snap.RootEntry.Summ.Size != 11 || snap.RootEntry.Summ.Files != 2 || snap.RootEntry.Summ.Dirs != 2 {
		t.Fatalf("summ inesperado: %+v", snap.RootEntry.Summ)
	}
	if snap.RootEntry.Summ.NumFailed != 1 {
		t.Fatalf("numFailed = %d", snap.RootEntry.Summ.NumFailed)
	}
	if !snap.StartTime.Equal(time.Date(2026, 11, 16, 10, 0, 0, 123456000, time.UTC)) {
		t.Fatalf("startTime = %v", snap.StartTime)
	}
	if snap.Stats == nil || snap.Stats.ExcludedFileCount != 3 || snap.Stats.ExcludedDirCount != 1 {
		t.Fatalf("stats = %+v", snap.Stats)
	}
}

func TestParseSnapshotJSONInvalido(t *testing.T) {
	for _, data := range []string{"", "no json", `{"source":{}}`} {
		if _, err := parseSnapshotJSON([]byte(data)); err == nil {
			t.Fatalf("esperaba error para %q", data)
		}
	}
}

func TestConnectArgsNoExponenPassword(t *testing.T) {
	creds := contracts.RepositoryCredentials{
		RepositoryID: "repo-1",
		Kind:         contracts.RepoKindServer,
		URL:          "https://kopia.example:51515",
		Fingerprint:  "sha256:ABCdef0123",
		Username:     "agent-1",
		Hostname:     "bdr",
		Password:     "super-secreto",
		Version:      3,
	}
	args, err := ConnectArgs(creds, "/var/lib/bdr-agent/kopia/kopia.config", "/var/lib/bdr-agent/kopia/cache")
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"repository connect server",
		"--url=https://kopia.example:51515",
		"--server-cert-fingerprint=abcdef0123",
		"--override-username=agent-1",
		"--override-hostname=bdr",
		"--config-file=/var/lib/bdr-agent/kopia/kopia.config",
		"--cache-directory=/var/lib/bdr-agent/kopia/cache",
		"--no-use-keychain",
		"--no-persist-credentials",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("faltan argumentos %q en %v", want, args)
		}
	}
	if strings.Contains(joined, creds.Password) {
		t.Fatalf("la contraseña aparece en argv: %v", args)
	}
	env := strings.Join(kopiaEnv(&creds), "\n")
	if !strings.Contains(env, "KOPIA_PASSWORD="+creds.Password) {
		t.Fatal("KOPIA_PASSWORD no está en el entorno del hijo")
	}
	if !strings.Contains(env, "KOPIA_CHECK_FOR_UPDATES=false") {
		t.Fatal("falta KOPIA_CHECK_FOR_UPDATES=false")
	}
}

func TestConnectArgsFilesystem(t *testing.T) {
	creds := contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: "/mnt/repo", Password: "pw",
	}
	args, err := ConnectArgs(creds, "", "")
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "repository connect filesystem") || !strings.Contains(joined, "--path=/mnt/repo") {
		t.Fatalf("args inesperados: %v", args)
	}
	if strings.Contains(joined, "pw") {
		t.Fatalf("contraseña en argv: %v", args)
	}
}

func TestValidateRestoreItem(t *testing.T) {
	valid := []string{"archivo.txt", "sub/dir/a.txt", "con espacio.txt"}
	for _, item := range valid {
		if err := ValidateRestoreItem(item); err != nil {
			t.Errorf("ValidateRestoreItem(%q) = %v", item, err)
		}
	}
	invalid := []string{"", "/etc/passwd", "../secreto", "sub/../../x", "sub/../a.txt", `..\x`, `C:\Windows\x`, "C:/Windows"}
	for _, item := range invalid {
		if err := ValidateRestoreItem(item); err == nil {
			t.Errorf("ValidateRestoreItem(%q) debía fallar", item)
		} else if ErrorCode(err) != "E_PAYLOAD" {
			t.Errorf("código para %q = %s", item, ErrorCode(err))
		}
	}
}

func TestValidateRestoreDestination(t *testing.T) {
	if err := ValidateRestoreDestination("/tmp/restore", false); err != nil {
		t.Fatalf("destino absoluto rechazado: %v", err)
	}
	for _, dest := range []string{"", "relativo/destino"} {
		if err := ValidateRestoreDestination(dest, false); ErrorCode(err) != "E_RESTORE_DEST" {
			t.Fatalf("destino %q: %v", dest, err)
		}
	}
	if err := ValidateRestoreDestination("", true); ErrorCode(err) != "E_RESTORE_DEST" {
		t.Fatalf("in_place sin source_path debía fallar: %v", err)
	}
}

func TestClassifyErroresKopia(t *testing.T) {
	cases := []struct {
		stderr string
		code   string
	}{
		{"error connecting to repository: unable to create format manager: invalid repository password", "E_KOPIA_AUTH"},
		{"open repository: repository is not connected.", "E_KOPIA_AUTH"},
		{"repository is locked by another process", "E_KOPIA_LOCK"},
		{"no space left on device", "E_KOPIA_QUOTA"},
		{"algo inesperado", "E_KOPIA_FAILED"},
	}
	r := NewKopiaRunner("kopia", "")
	for _, c := range cases {
		err := r.classify(context.Background(), context.DeadlineExceeded, []byte(c.stderr),
			&contracts.RepositoryCredentials{Password: "pw"}, "capturar")
		if got := ErrorCode(err); got != c.code {
			t.Errorf("stderr %q → %s, esperaba %s", c.stderr, got, c.code)
		}
		if strings.Contains(err.Error(), "pw") {
			t.Errorf("la contraseña no debe aparecer: %v", err)
		}
	}
	// Contexto cancelado gana a cualquier otro diagnóstico.
	err := r.classify(canceledCtx(), context.Canceled, []byte("invalid repository password"), nil, "capturar")
	if ErrorCode(err) != "E_CANCELED" {
		t.Fatalf("esperaba E_CANCELED, got %s", ErrorCode(err))
	}
}

func TestSnapshotSummaryDesdeFixture(t *testing.T) {
	snap, err := parseSnapshotJSON([]byte(snapshotFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sum := contracts.SnapshotSummary{
		Path: "/srv/datos", ID: snap.ID, RootObjectID: snap.RootEntry.Obj,
		SourceHost: snap.Source.Host, SourceUser: snap.Source.UserName,
		SizeBytes: snap.RootEntry.Summ.Size, Files: snap.RootEntry.Summ.Files,
		Dirs: snap.RootEntry.Summ.Dirs, ErrorCount: snap.RootEntry.Summ.NumFailed,
	}
	b, _ := json.Marshal(sum)
	if !strings.Contains(string(b), `"root_object_id":"kc8e1234"`) {
		t.Fatalf("json inesperado: %s", b)
	}
}

func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
