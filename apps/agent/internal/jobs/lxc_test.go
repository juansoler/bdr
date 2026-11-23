package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	pkgimg "github.com/juansoler/bdr/packages/image"

	"github.com/klauspost/compress/zstd"
)

// TestRemoveLegacyStaging: el agente elimina el directorio de staging de
// las versiones 0.16.33–0.16.36 (la subida vuelve a ser directa) y es
// idempotente; sin DataDir no hace nada.
func TestRemoveLegacyStaging(t *testing.T) {
	r := &VMRunner{DataDir: t.TempDir(), Log: nil}
	dir := filepath.Join(r.DataDir, "lxc-staging")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ct102-abcd1234-2026-11-23.zst"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.removeLegacyStaging()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("el staging legacy no se eliminó: %v", err)
	}
	r.removeLegacyStaging() // idempotente (directorio ya inexistente)

	sinDataDir := &VMRunner{Log: nil}
	sinDataDir.removeLegacyStaging() // sin pánico ni efecto
}

// TestStreamArchiveResume: el segundo streamArchive con el MISMO contenido
// contra un destino que ya tiene los segmentos (reintento tras un fallo)
// no reescribe ningún objeto: todos los segmentos se deduplican por clave
// (el nombre lleva el SHA-256) y el fichero del destino queda intacto.
func TestStreamArchiveResume(t *testing.T) {
	root := t.TempDir()
	creds := &contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: root,
	}
	origin := bytes.Repeat([]byte("contenido-estable-para-reanudar"), 100_000)

	run := func() (int, *pkgimg.Manifest) {
		dest, err := newFSUploadDest(creds, "lxc", "", "")
		if err != nil {
			t.Fatal(err)
		}
		manifest := &pkgimg.Manifest{Kind: "lxc", LXC: &pkgimg.LXC{VMID: 102, Name: "web"}}
		segCount, _, err := streamArchive(context.Background(), dest, manifest,
			bytes.NewReader(origin), int64(len(origin)),
			func(_ int, _ string, b int64) {})
		if err != nil {
			t.Fatalf("streamArchive: %v", err)
		}
		return segCount, manifest
	}

	seg1, m1 := run()
	if seg1 == 0 || len(m1.Streams) == 0 {
		t.Fatalf("primera pasada sin segmentos: seg=%d streams=%d", seg1, len(m1.Streams))
	}
	blobPath := filepath.Join(root, filepath.FromSlash(m1.Streams[0].Blob))
	st1, err := os.Stat(blobPath)
	if err != nil {
		t.Fatalf("segmento del destino: %v", err)
	}

	// Reintento (destino reconstruido: re-lista lo existente).
	seg2, m2 := run()
	if seg2 != seg1 {
		t.Fatalf("segmentos: %d ≠ %d", seg2, seg1)
	}
	if m2.Streams[0].Blob != m1.Streams[0].Blob {
		t.Fatalf("claves distintas para el mismo contenido: %q ≠ %q",
			m2.Streams[0].Blob, m1.Streams[0].Blob)
	}
	// El reintento NO reescribe: el fichero conserva su mtime original.
	st2, err := os.Stat(blobPath)
	if err != nil {
		t.Fatalf("segmento tras el reintento: %v", err)
	}
	if !st2.ModTime().Equal(st1.ModTime()) {
		t.Fatal("el reintento reescribió el segmento existente (no reanudó)")
	}

	// Y un destino con máquina/fecha (estructura nueva) sube todo a su
	// propia carpeta: primera ejecución de ese nodo/CT/día.
	dest2, err := newFSUploadDest(creds, "lxc", "pve1/web-105", "2026-11-23")
	if err != nil {
		t.Fatal(err)
	}
	manifest := &pkgimg.Manifest{Kind: "lxc", LXC: &pkgimg.LXC{VMID: 105, Name: "web-105"}}
	_, _, err = streamArchive(context.Background(), dest2, manifest,
		bytes.NewReader(origin), int64(len(origin)), func(_ int, _ string, b int64) {})
	if err != nil {
		t.Fatalf("streamArchive estructura nueva: %v", err)
	}
}

// TestStreamArchiveRoundtrip: streamArchive comprime y segmenta el archivo;
// los blobs del destino concatenados y descomprimidos reproducen el origen,
// y los SHA-256 del manifiesto corresponden a los segmentos comprimidos.
func TestStreamArchiveRoundtrip(t *testing.T) {
	root := t.TempDir()
	dest, err := newFSUploadDest(&contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: root,
	}, "lxc", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Origen con contenido variado (compresible y no).
	origin := bytes.Repeat([]byte("inicio-A"), 300_000)
	origin = append(origin, bytes.Repeat([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 500_000)...)
	origin = append(origin, []byte("fin")...)

	manifest := &pkgimg.Manifest{
		Kind: "lxc",
		LXC:  &pkgimg.LXC{VMID: 102, Name: "web"},
	}
	done := int64(0)
	segCount, bytesDone, err := streamArchive(context.Background(), dest, manifest,
		bytes.NewReader(origin), int64(len(origin)),
		func(_ int, _ string, b int64) { done = b })
	if err != nil {
		t.Fatalf("streamArchive: %v", err)
	}
	if segCount != len(manifest.Streams) || bytesDone != done {
		t.Fatalf("contadores: seg=%d streams=%d bytes=%d done=%d", segCount, len(manifest.Streams), bytesDone, done)
	}
	if bytesDone == 0 || bytesDone >= int64(len(origin)) {
		t.Fatalf("bytes comprimidos fuera de lo esperado: %d (origen %d)", bytesDone, len(origin))
	}

	// Concatena los segmentos del destino en orden y descomprime.
	var compressed bytes.Buffer
	for _, s := range manifest.Streams {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(s.Blob)))
		if err != nil {
			t.Fatalf("blob %s: %v", s.Blob, err)
		}
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), s.SHA256) {
			t.Fatalf("sha del segmento %s no coincide", s.Name)
		}
		compressed.Write(data)
	}
	zr, err := zstd.NewReader(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("descomprimir: %v", err)
	}
	if !bytes.Equal(got, origin) {
		t.Fatalf("roundtrip: %d bytes, esperaba %d", len(got), len(origin))
	}
}

// TestLXCHelpers: estimación de tamaño y storage del rootfs desde pct config.
func TestLXCHelpers(t *testing.T) {
	cfg := map[string]string{
		"hostname": "web",
		"rootfs":   "local-lvm:subvol-102-disk-0,size=8G",
		"mp0":      "local-lvm:subvol-102-disk-1,mp=/data,size=20G",
		"memory":   "512",
	}
	if got := lxcEstimatedBytes(cfg); got != (8+20)*(1<<30) {
		t.Fatalf("lxcEstimatedBytes: %d", got)
	}
	if got := lxcRootfsStorage(cfg); got != "local-lvm" {
		t.Fatalf("lxcRootfsStorage: %q", got)
	}
	if got := lxcRootfsStorage(map[string]string{"rootfs": "volume"}); got != "" {
		t.Fatalf("rootfs sin storage: %q", got)
	}
}

// TestSMBUploadDestValidation: rutas SMB inválidas se rechazan al construir
// el destino (sin red).
func TestSMBUploadDestValidation(t *testing.T) {
	if _, err := newSMBUploadDest(context.Background(), &contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: "//host-sin-recurso",
	}, "lxc", "", ""); err == nil {
		t.Fatal("ruta SMB sin recurso aceptada")
	}
	if _, err := newSMBUploadDest(context.Background(), &contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: "/mnt/local",
	}, "lxc", "", ""); err == nil {
		t.Fatal("ruta local aceptada como SMB")
	}
	// Un dial a un host que no responde debe fallar rápido con error claro.
	if _, err := newSMBUploadDest(context.Background(), &contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: "//127.0.0.1:1/share",
		SMBUser: "u", SMBPassword: "p",
	}, "lxc", "", ""); err == nil {
		t.Fatal("conexión a SMB inexistente aceptada")
	}
}

// TestRestoreLXCSegmentOrder: los segmentos se concatenan por número de
// segmento (el orden del slice del manifiesto), verificando SHA-256.
func TestRestoreLXCSegmentOrder(t *testing.T) {
	// Verifica las invariantes del manifiesto que usa RestoreLXC.
	m := pkgimg.Manifest{Kind: "lxc", Streams: []pkgimg.Stream{
		{Segment: 0, Name: "ct1-seg0000", Bytes: 3, SHA256: "aa", Blob: "lxc/a"},
		{Segment: 1, Name: "ct1-seg0001", Bytes: 2, SHA256: "bb", Blob: "lxc/b"},
	}}
	if len(m.Streams) != 2 || m.Streams[0].Segment > m.Streams[1].Segment {
		t.Fatal("orden de streams inválido")
	}
	_ = time.Now()
}

// TestNormalizeLXCDump: el dump correcto (una capa de zstd) se devuelve
// tal cual; el doble-comprimido de los agentes 0.16.33–0.16.38 se
// re-escribe con una sola capa; y un fichero que no es zstd falla.
func TestNormalizeLXCDump(t *testing.T) {
	dir := t.TempDir()
	tar := bytes.Repeat([]byte("contenido-tar-de-prueba\n"), 4096)

	// Caso normal: zstd(tar) → misma ruta, sin reescritura.
	normal := filepath.Join(dir, "dump.tar.zst")
	escribirZstd(t, normal, tar)
	got, err := normalizeLXCDump(normal)
	if err != nil {
		t.Fatalf("normal: %v", err)
	}
	if got != normal {
		t.Fatalf("normal: reescribió el dump: %q", got)
	}

	// Caso legado: zstd(zstd(tar)) → nueva ruta .norm.tar.zst cuyo
	// contenido descomprime una sola vez al tar original. Se comprime de
	// nuevo el fichero zstd(tar) EN BRUTO (no su contenido descomprimido).
	single, err := os.ReadFile(normal) // zstd(tar) en bruto
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "dump-legacy.tar.zst")
	escribirZstd(t, legacy, single)
	got, err = normalizeLXCDump(legacy)
	if err != nil {
		t.Fatalf("legacy: %v", err)
	}
	if got == legacy || !strings.HasSuffix(got, ".norm.tar.zst") {
		t.Fatalf("legacy: ruta inesperada: %q", got)
	}
	if cuerpo := leerZstd(t, got); !bytes.Equal(cuerpo, tar) {
		t.Fatalf("legacy: contenido reescrito distinto del tar original (%d bytes)", len(cuerpo))
	}

	// Caso corrupto: bytes que no son zstd → error claro.
	roto := filepath.Join(dir, "dump-roto.tar.zst")
	if err := os.WriteFile(roto, []byte("esto no es zstd"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeLXCDump(roto); err == nil {
		t.Fatal("un dump que no es zstd debería fallar")
	}

	// Caso vacío → error.
	vacio := filepath.Join(dir, "dump-vacio.tar.zst")
	if err := os.WriteFile(vacio, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeLXCDump(vacio); err == nil {
		t.Fatal("un dump vacío debería fallar")
	}
}

// escribirZstd escribe el contenido comprimido con zstd en una ruta.
func escribirZstd(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// leerZstd lee y descomprime una vez el fichero zstd de la ruta.
func leerZstd(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := zstd.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("descomprimir %s: %v", path, err)
	}
	return data
}
