package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/juansoler/bdr/packages/contracts"
	pkgimg "github.com/juansoler/bdr/packages/image"
)

// patrón del origen sintético (en runs de 256 KiB dentro de cada segmento
// de 2 MiB → 8 runs por segmento):
//
//	seg 0: todo a ceros                      → 1 stream Zero
//	seg 1: denso (aleatorio estable)         → 1 blob del tamaño del segmento
//	seg 2: disperso (3 de 8 runs sucios)     → 3 blobs + 3 Zero coalescidos
//	seg 3: un byte sucio en el primer run    → 1 blob + 1 Zero
const (
	testSegmentSize = 2 << 20 // 2 MiB
	testRunSize     = 256 << 10
	testSegments    = 4
)

// origenesSintetico construye el origen de test y su mapa de runs sucios
// por segmento: dirty[seg] lista los índices de run no nulos (contenido
// pseudo-aleatorio estable, no comprimible ni confundible con ceros).
func origenSintetico(t *testing.T) (src *bytes.Reader, buf []byte, total int64) {
	t.Helper()
	total = testSegmentSize * testSegments
	buf = make([]byte, total)
	dirty := map[int][]int{
		0: {},
		1: {0, 1, 2, 3, 4, 5, 6, 7}, // denso
		2: {0, 2, 5},                // disperso (37.5%)
		3: {0},                      // un run sucio con un solo byte
	}
	patron := []byte("DATOS-QUE-NO-SON-CEROS-")
	for seg, runs := range dirty {
		for _, r := range runs {
			off := int64(seg)*testSegmentSize + int64(r)*testRunSize
			for b := int64(0); b < testRunSize; b++ {
				buf[off+b] = patron[(int(seg)*7+int(r)*13+int(b))%len(patron)]
			}
		}
	}
	// seg 3: un único byte al principio del primer run.
	buf[int64(3)*testSegmentSize] = 0xA5
	return bytes.NewReader(buf), buf, total
}

// ajustaTamañosTest encoge los tamaños de segmento/run para los tests y los
// restaura al terminar.
func ajustaTamañosTest(t *testing.T) {
	t.Helper()
	seg, run := imageSegmentSize, captureRunSize
	imageSegmentSize, captureRunSize = testSegmentSize, testRunSize
	t.Cleanup(func() { imageSegmentSize, captureRunSize = seg, run })
}

func destinoTest(t *testing.T, ns string) (uploadDest, string) {
	t.Helper()
	root := t.TempDir()
	dest, err := newFSUploadDest(&contracts.RepositoryCredentials{
		Kind: contracts.RepoKindFilesystem, Path: root,
	}, ns, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return dest, root
}

// TestCaptureSourceHibrida verifica la segmentación híbrida: los segmentos
// vacíos no suben nada, los densos suben enteros y los dispersos suben solo
// los runs no nulos; los streams cubren el disco sin huecos ni solapes.
func TestCaptureSourceHibrida(t *testing.T) {
	ajustaTamañosTest(t)
	src, _, total := origenSintetico(t)
	dest, root := destinoTest(t, "vm")

	manifest := &pkgimg.Manifest{
		SchemaVersion: 1, ManifestID: "cap-test", Kind: "vm",
	}
	disk := pkgimg.Disk{Index: 0, DevicePath: "/dev/fake", TotalBytes: total}
	done := int64(0)
	stats, err := captureSource(context.Background(), dest, src, disk, manifest,
		func(int64, int64, string) {}, &done)
	if err != nil {
		t.Fatalf("captureSource: %v", err)
	}

	// Lectura: todo el disco. Subida: solo los runs no nulos
	// (2 MiB densos + 3×256 KiB dispersos + 256 KiB del byte suelto).
	wantUploaded := int64(testSegmentSize) + 4*testRunSize
	if stats.BytesRead != total {
		t.Fatalf("bytes leídos: %d ≠ %d", stats.BytesRead, total)
	}
	if stats.BytesUploaded != wantUploaded {
		t.Fatalf("bytes subidos: %d ≠ %d", stats.BytesUploaded, wantUploaded)
	}
	if done != total {
		t.Fatalf("contador de progreso: %d ≠ %d", done, total)
	}

	// Cobertura: los streams, ordenados por offset, cubren el disco
	// contiguamente (sin huecos ni solapes).
	streams := append([]pkgimg.Stream(nil), manifest.Streams...)
	sort.Slice(streams, func(i, j int) bool { return streams[i].Offset < streams[j].Offset })
	cursor := int64(0)
	var blobBytes int64
	for _, s := range streams {
		if s.Offset != cursor {
			t.Fatalf("hueco o solape en %s: offset %d, esperaba %d", s.Name, s.Offset, cursor)
		}
		if s.Bytes <= 0 {
			t.Fatalf("stream %s sin bytes", s.Name)
		}
		cursor += s.Bytes
		if s.Zero {
			if s.Blob != "" {
				t.Fatalf("stream Zero %s no debe tener blob", s.Name)
			}
			continue
		}
		if s.Blob == "" {
			t.Fatalf("stream %s sin Zero ni blob", s.Name)
		}
		blobBytes += s.Bytes
	}
	if cursor != total {
		t.Fatalf("cobertura: %d ≠ %d", cursor, total)
	}
	if blobBytes != wantUploaded {
		t.Fatalf("bytes en blobs: %d ≠ %d", blobBytes, wantUploaded)
	}

	// Estructura esperada por segmento: 1 + 1 + 6 + 2 = 10 streams.
	if len(manifest.Streams) != 10 || stats.Streams != 10 {
		t.Fatalf("streams: manifiesto %d, stats %d, esperaba 10", len(manifest.Streams), stats.Streams)
	}

	// Cada blob del destino coincide en SHA-256 y contenido con el origen.
	for _, s := range manifest.Streams {
		if s.Blob == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(s.Blob)))
		if err != nil {
			t.Fatalf("blob %s: %v", s.Blob, err)
		}
		sum := sha256.Sum256(data)
		if !bytes.EqualFold([]byte(hex.EncodeToString(sum[:])), []byte(s.SHA256)) {
			t.Fatalf("sha del blob %s no coincide", s.Name)
		}
		if int64(len(data)) != s.Bytes {
			t.Fatalf("blob %s: %d bytes, manifiesto dice %d", s.Name, len(data), s.Bytes)
		}
	}
}

// TestCaptureSourceUmbralDenso: un segmento con exactamente la mitad de
// runs con datos se sube entero (una petición), no fragmentado en runs.
func TestCaptureSourceUmbralDenso(t *testing.T) {
	ajustaTamañosTest(t)
	dirtyRuns := []int{0, 2, 4, 6} // 4 de 8 = 50%
	buf := make([]byte, testSegmentSize)
	for _, r := range dirtyRuns {
		for b := 0; b < testRunSize; b++ {
			buf[int64(r)*testRunSize+int64(b)] = byte('D')
		}
	}
	dest, _ := destinoTest(t, "vm")
	manifest := &pkgimg.Manifest{SchemaVersion: 1, Kind: "vm"}
	disk := pkgimg.Disk{Index: 0, TotalBytes: testSegmentSize}
	done := int64(0)
	stats, err := captureSource(context.Background(), dest, bytes.NewReader(buf), disk,
		manifest, func(int64, int64, string) {}, &done)
	if err != nil {
		t.Fatalf("captureSource: %v", err)
	}
	if len(manifest.Streams) != 1 {
		t.Fatalf("umbral denso: %d streams, esperaba 1 (segmento entero)", len(manifest.Streams))
	}
	s := manifest.Streams[0]
	if s.Zero || s.Blob == "" || s.Bytes != testSegmentSize {
		t.Fatalf("stream del umbral denso inválido: %+v", s)
	}
	if stats.BytesUploaded != testSegmentSize {
		t.Fatalf("subidos: %d ≠ %d", stats.BytesUploaded, testSegmentSize)
	}
}

// memDisk es un destino de escritura en memoria para el roundtrip del
// restore (equivalente al dispositivo NBD de writeStreams).
type memDisk struct{ b []byte }

func (m *memDisk) WriteAt(p []byte, off int64) (int, error) {
	end := off + int64(len(p))
	if end > int64(len(m.b)) {
		m.b = append(m.b, make([]byte, end-int64(len(m.b)))...)
	}
	copy(m.b[off:], p)
	return len(p), nil
}

// TestCaptureSourceRoundtripRestore: lo capturado se restaura byte a byte
// sobre un disco destino que contiene datos viejos (como un volumen
// reutilizado), verificando que los streams Zero reponen ceros.
func TestCaptureSourceRoundtripRestore(t *testing.T) {
	ajustaTamañosTest(t)
	src, wantBuf, total := origenSintetico(t)
	dest, _ := destinoTest(t, "vm")

	manifest := &pkgimg.Manifest{SchemaVersion: 1, ManifestID: "rt-test", Kind: "vm"}
	disk := pkgimg.Disk{Index: 0, TotalBytes: total}
	done := int64(0)
	if _, err := captureSource(context.Background(), dest, src, disk, manifest,
		func(int64, int64, string) {}, &done); err != nil {
		t.Fatalf("captureSource: %v", err)
	}

	// Destino con datos viejos distintos del origen.
	target := &memDisk{b: bytes.Repeat([]byte{0xFF}, int(total))}
	warns := 0
	if err := writeStreams(context.Background(), dest, *manifest, 0, target, total, nil,
		func(int) {}, func(string) { warns++ }); err != nil {
		t.Fatalf("writeStreams: %v", err)
	}
	if warns != 0 {
		t.Fatalf("avisos inesperados del restore: %d", warns)
	}
	if !bytes.Equal(target.b, wantBuf) {
		t.Fatalf("roundtrip: el disco restaurado no coincide con el origen")
	}
}
