package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pkgimg "github.com/juansoler/bdr/packages/image"
)

// staging con 3 segmentos simulando un disco de 10 MiB: manifest.json +
// disk0-segNNNN con SHA-256. WriteImage reconstruye el contenido en orden.
func TestWriteImageRoundtrip(t *testing.T) {
	// Disco "original": contenido pseudo-aleatorio estable.
	disk := make([]byte, 10<<20)
	for i := range disk {
		disk[i] = byte(i * 7 % 251)
	}

	const seg = 4 << 20 // 4 MiB
	dir := t.TempDir()
	var streams []pkgimg.Stream
	for off, segIdx := 0, 0; off < len(disk); off, segIdx = off+seg, segIdx+1 {
		end := off + seg
		if end > len(disk) {
			end = len(disk)
		}
		chunk := disk[off:end]
		sum := sha256.Sum256(chunk)
		name := "disk0-seg" + string(rune('0'+segIdx))
		if err := os.WriteFile(filepath.Join(dir, name), chunk, 0o600); err != nil {
			t.Fatal(err)
		}
		streams = append(streams, pkgimg.Stream{
			Disk: 0, Segment: segIdx, Name: name,
			Offset: int64(off), Bytes: int64(len(chunk)),
			SHA256: hex.EncodeToString(sum[:]), State: pkgimg.SegmentCompleted,
		})
	}
	manifest := pkgimg.Manifest{
		SchemaVersion: 1, ManifestID: "img-test", Kind: "image",
		Disks:   []pkgimg.Disk{{Index: 0, TotalBytes: int64(len(disk)), DevicePath: "/dev/fake"}},
		Streams: streams,
	}
	mb, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o600); err != nil {
		t.Fatal(err)
	}

	// Escritura a un fichero destino.
	target := filepath.Join(dir, "target.img")
	out, err := WriteImage(WriteOptions{SegmentsDir: dir, Target: target, Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Streams) != len(streams) {
		t.Fatalf("streams: %d", len(out.Streams))
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(disk) {
		t.Fatalf("tamaño: %d ≠ %d", len(got), len(disk))
	}
	for i := range disk {
		if got[i] != disk[i] {
			t.Fatalf("byte %d: %d ≠ %d", i, got[i], disk[i])
		}
	}
}

// Un segmento con SHA-256 alterado aborta la escritura (destino no queda
// "bien" pero tampoco silenciosamente corrupto: el error es explícito).
func TestWriteImageDetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	chunk := []byte("contenido de segmento")
	sum := sha256.Sum256(chunk)
	if err := os.WriteFile(filepath.Join(dir, "disk0-seg0000"), chunk, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := pkgimg.Manifest{
		SchemaVersion: 1, Kind: "image",
		Streams: []pkgimg.Stream{{
			Disk: 0, Segment: 0, Name: "disk0-seg0000",
			Offset: 0, Bytes: int64(len(chunk)),
			SHA256: hex.EncodeToString(sum[:]) + "00", // SHA inválido
			State:  pkgimg.SegmentCompleted,
		}},
	}
	mb, _ := json.Marshal(manifest)
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o600)

	target := filepath.Join(dir, "target.img")
	_, err := WriteImage(WriteOptions{SegmentsDir: dir, Target: target, Verify: true})
	if err == nil {
		t.Fatal("esperaba error de verificación")
	}
}
