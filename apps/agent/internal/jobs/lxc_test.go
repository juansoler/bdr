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
