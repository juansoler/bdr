package image_test

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	imgagent "github.com/juansoler/bdr/apps/agent/internal/image"
	pkgimg "github.com/juansoler/bdr/packages/image"
)

// memSink acumula segmentos en memoria para verificar integridad.
type memSink struct {
	mu       sync.Mutex
	segments map[string][]byte
}

func newMemSink() *memSink {
	return &memSink{segments: map[string][]byte{}}
}

func (s *memSink) WriteSegment(name string, data io.Reader) (string, error) {
	b, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.segments[name] = b
	s.mu.Unlock()
	return "kopia-id-" + name, nil
}

func (s *memSink) get(name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.segments[name]
}

func (s *memSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.segments)
}

// TestSegmenterFullCapture captura completa de un fichero en segmentos y
// verifica que la reconstrucción SHA-256 coincide con el origen.
func TestSegmenterFullCapture(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "image.raw")
	data := make([]byte, 10<<20) // 10 MB
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	segSize := int64(1 << 20) // 1 MB
	sink := newMemSink()
	sg := &imgagent.Segmenter{
		Source:      f,
		TotalBytes:  int64(len(data)),
		SegmentSize: segSize,
		JournalPath: filepath.Join(dir, "journal.jsonl"),
		Sink:        sink,
	}

	manifest := &pkgimg.Manifest{
		SchemaVersion: 1, ManifestID: "m-1", RunID: "r-1", AgentID: "a-1",
		Kind: "image", CapturedAt: "2026-11-18T20:00:00Z", Consistency: "crash_consistent",
		Disks: []pkgimg.Disk{{Index: 0}},
	}
	res, err := sg.Run(manifest, 0)
	if err != nil {
		t.Fatal(err)
	}

	totalSegs := int(len(data)) / int(segSize)
	if int64(len(data))%segSize != 0 {
		totalSegs++
	}
	if res.SegmentsTotal != int(totalSegs) {
		t.Fatalf("total segmentos: %d ≠ %d", res.SegmentsTotal, totalSegs)
	}
	if res.SegmentsCompleted != res.SegmentsTotal || res.SegmentsSkipped != 0 {
		t.Fatalf("completados=%d skip=%d", res.SegmentsCompleted, res.SegmentsSkipped)
	}

	// Reconstruir y comparar SHA-256.
	var rebuilt []byte
	for _, s := range res.Segments {
		rebuilt = append(rebuilt, sink.get(s.Name)...)
	}
	origSum := sha256.Sum256(data)
	revSum := sha256.Sum256(rebuilt)
	if origSum != revSum {
		t.Fatal("reconstrucción no idéntica")
	}

	// Verificar manifiesto.
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifiesto: %v", err)
	}
	if len(manifest.Streams) != int(totalSegs) {
		t.Fatalf("streams en manifiesto: %d ≠ %d", len(manifest.Streams), totalSegs)
	}
}

// TestSegmenterResume: tras un corte a mitad, reanuda sin re-enviar los
// segmentos ya completados (journal) y el resultado final es íntegro.
func TestSegmenterResume(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "image.raw")
	data := make([]byte, 8<<20) // 8 MB
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(src)
	defer f.Close()

	segSize := int64(1 << 20)
	journalPath := filepath.Join(dir, "journal.jsonl")

	// Primer run: solo 3 de 8 segmentos (simulación de corte).
	sink1 := newMemSink()
	sg1 := &imgagent.Segmenter{
		Source:      io.NewSectionReader(f, 0, 3*int64(segSize)),
		TotalBytes:  3 * int64(segSize),
		SegmentSize: segSize,
		JournalPath: journalPath,
		Sink:        sink1,
	}
	res1, err := sg1.Run(&pkgimg.Manifest{Kind: "image", Disks: []pkgimg.Disk{{Index: 0}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res1.SegmentsCompleted != 3 {
		t.Fatalf("primera pasada: %d segmentos", res1.SegmentsCompleted)
	}
	if sink1.count() != 3 {
		t.Fatalf("sink tras primera pasada: %d", sink1.count())
	}

	// Segundo run: full reanudación — los primeros 3 se saltan.
	sink2 := newMemSink()
	sg2 := &imgagent.Segmenter{
		Source:      f,
		TotalBytes:  int64(len(data)),
		SegmentSize: segSize,
		JournalPath: journalPath,
		Sink:        sink2,
	}
	res2, err := sg2.Run(&pkgimg.Manifest{Kind: "image", Disks: []pkgimg.Disk{{Index: 0}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res2.SegmentsSkipped != 3 {
		t.Fatalf("reanudación: esperaba 3 saltados, got %d", res2.SegmentsSkipped)
	}
	if res2.SegmentsCompleted != int(len(data))/int(segSize) {
		t.Fatalf("total tras reanudación: %d", res2.SegmentsCompleted)
	}
	// Reconstrucción desde ambos sinks = datos completos.
	var rebuilt []byte
	for i := 0; i < int(len(data))/int(segSize); i++ {
		name := fmt.Sprintf("disk0-seg%04d", i)
		if b := sink1.get(name); len(b) > 0 {
			rebuilt = append(rebuilt, b...)
		} else if b := sink2.get(name); len(b) > 0 {
			rebuilt = append(rebuilt, b...)
		}
	}
	if len(rebuilt) != len(data) {
		t.Fatalf("reconstrucción: %d ≠ %d", len(rebuilt), len(data))
	}
	origSum := sha256.Sum256(data)
	revSum := sha256.Sum256(rebuilt)
	if origSum != revSum {
		t.Fatal("reconstrucción no idéntica")
	}
}

// TestManifestValidation: manifiesto con streams generados valida y serializa.
func TestManifestValidation(t *testing.T) {
	m := pkgimg.Manifest{
		SchemaVersion: 1,
		ManifestID:    "550e8400-e29b-41d4-a716-446655440000",
		RunID:         "550e8400-e29b-41d4-a716-446655440001",
		AgentID:       "550e8400-e29b-41d4-a716-446655440002",
		Kind:          "image",
		ImageMode:     pkgimg.ModeRawDisk,
		Consistency:   "crash_consistent",
		CapturedAt:    "2026-11-18T20:00:00Z",
		Disks: []pkgimg.Disk{
			{Index: 0, Table: "gpt", SectorSize: 512, TotalBytes: 8 << 20,
				Partitions: []pkgimg.Partition{{Index: 0, Offset: 0, Size: 8 << 20, Role: pkgimg.RoleOS}}},
		},
		Streams: []pkgimg.Stream{
			{Disk: 0, Segment: 0, Name: "disk0-seg0000", Offset: 0, Bytes: 1 << 20,
				SHA256: "abc", State: pkgimg.SegmentCompleted},
		},
		Host:         pkgimg.Host{Hostname: "test", OSFamily: "linux", Arch: "amd64"},
		Security:     pkgimg.Security{BootMode: "uefi"},
		RestoreHints: pkgimg.RestoreHints{SystemDisk: 0, BootPartitionIndex: 0, ExpectedFirmware: "uefi"},
		Tooling:      pkgimg.Tooling{AgentVersion: "0.5.0-dev"},
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	m2, err := pkgimg.UnManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if m2.ManifestID != m.ManifestID {
		t.Fatal("round-trip falló")
	}
	m3 := m
	m3.SchemaVersion = 99
	if err := m3.Validate(); err == nil {
		t.Fatal("schema_version=99 debía rechazarse")
	}
	m4 := m
	m4.Kind = "file"
	if err := m4.Validate(); err == nil {
		t.Fatal("kind=file debía rechazarse")
	}
}
