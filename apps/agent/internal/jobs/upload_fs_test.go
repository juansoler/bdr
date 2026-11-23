package jobs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

// TestFSUploadDest: el destino de ruta física escribe segmentos y
// manifiestos como ficheros atómicos y reanuda (salta segmentos existentes).
func TestFSUploadDest(t *testing.T) {
	root := t.TempDir()
	creds := &contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: root, Prefix: "equipo1",
	}
	dest, err := newFSUploadDest(creds, "image", "", "")
	if err != nil {
		t.Fatalf("newFSUploadDest: %v", err)
	}
	if dest.Prefix() != "equipo1/image/" {
		t.Fatalf("prefix: %q", dest.Prefix())
	}

	ctx := context.Background()
	now := time.Now().UTC()

	// Segmento nuevo: se escribe en <root>/<prefix>/<clave>.
	key, err := dest.putSegment(ctx, dest.Prefix()+"disk0-seg0000-abc", []byte("hola"), now)
	if err != nil {
		t.Fatalf("putSegment: %v", err)
	}
	if key != "equipo1/image/disk0-seg0000-abc" {
		t.Fatalf("key: %q", key)
	}
	data, err := os.ReadFile(filepath.Join(root, "equipo1", "image", "disk0-seg0000-abc"))
	if err != nil || string(data) != "hola" {
		t.Fatalf("fichero del segmento: %v %q", err, data)
	}

	// Reanudación: reanudar con el mismo destino salta el segmento existente.
	resumed, err := newFSUploadDest(creds, "image", "", "")
	if err != nil {
		t.Fatalf("reanudación: %v", err)
	}
	key2, err := resumed.putSegment(ctx, key, []byte("OTRO"), now)
	if err != nil {
		t.Fatalf("putSegment reanudado: %v", err)
	}
	if key2 != key {
		t.Fatalf("key reanudada: %q", key2)
	}
	data, _ = os.ReadFile(filepath.Join(root, "equipo1", "image", "disk0-seg0000-abc"))
	if string(data) != "hola" {
		t.Fatalf("la reanudación reescribió el segmento: %q", data)
	}

	// Manifiesto: idempotente sobrescribe (putRaw siempre escribe).
	mkey := dest.Prefix() + "manifest-img-1.json"
	if err := dest.putRaw(ctx, mkey, []byte(`{"a":1}`), now); err != nil {
		t.Fatalf("putRaw: %v", err)
	}
	mb, err := dest.getManifest(ctx, mkey)
	if err != nil || !strings.Contains(string(mb), `"a"`) {
		t.Fatalf("getManifest: %v %s", err, mb)
	}

	// Lectura de objetos (restauración).
	obj, err := dest.openObject(ctx, key)
	if err != nil {
		t.Fatalf("openObject: %v", err)
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(obj); err != nil {
		t.Fatal(err)
	}
	_ = obj.Close()
	if buf.String() != "hola" {
		t.Fatalf("openObject contenido: %q", buf.String())
	}

	// Claves con recorrido de directorio: rechazadas.
	if _, err := dest.putSegment(ctx, "../escape", []byte("x"), now); err == nil {
		t.Fatal("clave con ../ aceptada")
	}
	if _, err := dest.putSegment(ctx, "/absoluta", []byte("x"), now); err == nil {
		t.Fatal("clave absoluta aceptada")
	}

	// S3 con kind filesystem pero sin ruta: error claro.
	if _, err := newFSUploadDest(&contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: "",
	}, "vm", "", ""); err == nil {
		t.Fatal("ruta vacía aceptada")
	}
}

// TestFSUploadDestEstructuraBackup verifica la estructura de backups por
// máquina y fecha: el destino filesystem escribe los objetos como
// <ruta>/backup/<máquina>/<fecha>/<namespace>/..., respeta el prefijo del
// destino (creds.Prefix) y reanuda dentro de la misma carpeta.
func TestFSUploadDestEstructuraBackup(t *testing.T) {
	root := t.TempDir()
	creds := &contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: root, Prefix: "cliente-a",
	}
	dest, err := newFSUploadDest(creds, "vm", "servidor-01", "2026-11-23")
	if err != nil {
		t.Fatalf("newFSUploadDest: %v", err)
	}
	wantPrefix := "cliente-a/backup/servidor-01/2026-11-23/vm/"
	if dest.Prefix() != wantPrefix {
		t.Fatalf("prefix: %q ≠ %q", dest.Prefix(), wantPrefix)
	}

	ctx := context.Background()
	now := time.Now().UTC()

	// Segmento: la clave completa hereda el prefijo máquina/fecha.
	key, err := dest.putSegment(ctx, dest.Prefix()+"disk0-seg0000-abc123", []byte("datos"), now)
	if err != nil {
		t.Fatalf("putSegment: %v", err)
	}
	if key != wantPrefix+"disk0-seg0000-abc123" {
		t.Fatalf("key: %q", key)
	}
	data, err := os.ReadFile(filepath.Join(root, "cliente-a", "backup",
		"servidor-01", "2026-11-23", "vm", "disk0-seg0000-abc123"))
	if err != nil || string(data) != "datos" {
		t.Fatalf("fichero del segmento: %v %q", err, data)
	}

	// Reanudación el mismo día: mismo prefijo → salta el segmento.
	resumed, err := newFSUploadDest(creds, "vm", "servidor-01", "2026-11-23")
	if err != nil {
		t.Fatalf("reanudación: %v", err)
	}
	key2, err := resumed.putSegment(ctx, key, []byte("CAMBIADO"), now)
	if err != nil {
		t.Fatalf("putSegment reanudado: %v", err)
	}
	if key2 != key {
		t.Fatalf("key reanudada: %q", key2)
	}
	data, _ = os.ReadFile(filepath.Join(root, "cliente-a", "backup",
		"servidor-01", "2026-11-23", "vm", "disk0-seg0000-abc123"))
	if string(data) != "datos" {
		t.Fatalf("la reanudación reescribió el segmento: %q", data)
	}

	// Otro día: carpeta distinta, no colisiona con la anterior.
	otroDia, err := newFSUploadDest(creds, "vm", "servidor-01", "2026-11-24")
	if err != nil {
		t.Fatalf("otro día: %v", err)
	}
	if otroDia.Prefix() != "cliente-a/backup/servidor-01/2026-11-24/vm/" {
		t.Fatalf("prefix otro día: %q", otroDia.Prefix())
	}

	// Sin máquina/fecha (restauración): comportamiento clásico por namespace.
	restore, err := newFSUploadDest(creds, "vm", "", "")
	if err != nil {
		t.Fatalf("restauración: %v", err)
	}
	if restore.Prefix() != "cliente-a/vm/" {
		t.Fatalf("prefix restauración: %q", restore.Prefix())
	}
}

// TestSanitizeMachine cubre la normalización de nombres de máquina para las
// rutas del destino.
func TestSanitizeMachine(t *testing.T) {
	cases := map[string]string{
		"srv-web 01":    "srv-web_01",
		"servidor/nt":   "servidor_nt",
		"  Mi.PC  ":     "Mi.PC",
		"con:caracter?": "con_caracter",
		"":              "",
		"///":           "",
	}
	for in, want := range cases {
		if got := sanitizeMachine(in); got != want {
			t.Errorf("sanitizeMachine(%q) = %q; quiero %q", in, got, want)
		}
	}
}
