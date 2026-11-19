// Segmentador con journal y reanudación para captura de imágenes completas.
//
// Lee un origen (disco, partición o fichero para tests) en segmentos de
// tamaño fijo, calcula el SHA-256 de cada uno y los entrega al sink (kopia o
// el sustituto de tests). El journal registra el estado de cada segmento en
// JSON lines para que un corte a mitad de captura reanude sin re-enviar
// segmentos ya completados (hallazgo G0.2: kopia stdin-file no tiene
// checkpoint de snapshot → el checkpoint es responsabilidad del motor).
package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	pkgimg "github.com/juansoler/bdr/packages/image"
)

// DefaultSegmentSize es el tamaño por defecto de segmento (32 MiB).
const DefaultSegmentSize = 32 * 1024 * 1024

// journalEntry es una línea del journal.
type journalEntry struct {
	Segment   int    `json:"segment"`
	Offset    int64  `json:"offset"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
	State     string `json:"state"`
	Timestamp string `json:"timestamp"`
}

// journal registra el progreso en un fichero JSON lines.
type journal struct {
	mu   sync.Mutex
	path string
	file *os.File
	enc  *json.Encoder
	done map[int]journalEntry
}

func newJournal(path string) (*journal, error) {
	j := &journal{path: path, done: map[int]journalEntry{}}
	if path == "" {
		return j, nil
	}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range splitLines(data) {
			var e journalEntry
			if json.Unmarshal(line, &e) == nil && e.State == string(pkgimg.SegmentCompleted) {
				j.done[e.Segment] = e
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	j.file = f
	j.enc = json.NewEncoder(f)
	return j, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := data[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func (j *journal) append(e journalEntry) error {
	if j.enc == nil {
		return nil
	}
	e.Timestamp = time.Now().UTC().Format(time.RFC3339)
	return j.enc.Encode(e)
}

func (j *journal) recordDone(e journalEntry) {
	j.done[e.Segment] = e
	_ = j.append(e)
}

func (j *journal) isDone(segment int) (journalEntry, bool) {
	e, ok := j.done[segment]
	return e, ok
}

func (j *journal) close() error {
	if j.file != nil {
		return j.file.Close()
	}
	return nil
}

// SegmentSink es el destino de los datos de cada segmento (kopia, disco o
// el sustituto de tests). Los segmentos se identifican por nombre.
type SegmentSink interface {
	WriteSegment(name string, data io.Reader) (kopiaID string, err error)
}

// Segmenter captura un origen en segmentos con journal y reanudación.
type Segmenter struct {
	Source      io.ReaderAt
	TotalBytes  int64
	SegmentSize int64
	JournalPath string
	Sink        SegmentSink
}

// Result resume la captura (o reanudación).
type Result struct {
	SegmentsTotal     int
	SegmentsCompleted int
	SegmentsSkipped   int
	BytesTotal        int64
	Segments          []pkgimg.Stream
	Manifest          *pkgimg.Manifest
}

// Run ejecuta la captura completa o la reanudación.
func (sg *Segmenter) Run(manifest *pkgimg.Manifest, diskIndex int) (*Result, error) {
	if sg.SegmentSize <= 0 {
		sg.SegmentSize = DefaultSegmentSize
	}
	j, err := newJournal(sg.JournalPath)
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	defer j.close()

	totalSegments := int((sg.TotalBytes + sg.SegmentSize - 1) / sg.SegmentSize)
	res := &Result{SegmentsTotal: totalSegments, BytesTotal: sg.TotalBytes}

	for seg := 0; seg < totalSegments; seg++ {
		offset := int64(seg) * sg.SegmentSize
		size := sg.SegmentSize
		if offset+size > sg.TotalBytes {
			size = sg.TotalBytes - offset
		}
		name := fmt.Sprintf("disk%d-seg%04d", diskIndex, seg)

		// Reanudación: si el segmento ya está completado en el journal,
		// reutiliza el resultado sin re-leer el origen.
		if prev, ok := j.isDone(seg); ok && prev.SHA256 != "" {
			res.SegmentsSkipped++
			res.SegmentsCompleted++
			res.Segments = append(res.Segments, pkgimg.Stream{
				Disk: diskIndex, Segment: seg, Name: name,
				Offset: offset, Bytes: prev.Bytes,
				SHA256: prev.SHA256, State: pkgimg.SegmentCompleted,
			})
			continue
		}

		buf := make([]byte, size)
		if _, err := sg.Source.ReadAt(buf, offset); err != nil && err != io.EOF {
			return nil, fmt.Errorf("segmento %d: lectura: %w", seg, err)
		}
		sum := sha256.Sum256(buf)
		sha := hex.EncodeToString(sum[:])

		entry := journalEntry{
			Segment: seg, Offset: offset, Bytes: int64(size),
			SHA256: sha, State: string(pkgimg.SegmentCompleted),
		}
		j.recordDone(entry)

		res.SegmentsCompleted++
		res.Segments = append(res.Segments, pkgimg.Stream{
			Disk: diskIndex, Segment: seg, Name: name,
			Offset: offset, Bytes: int64(size), SHA256: sha,
			State: pkgimg.SegmentCompleted,
		})
		if sg.Sink != nil {
			if _, err := sg.Sink.WriteSegment(name, &segmentReader{buf: buf}); err != nil {
				return nil, fmt.Errorf("segmento %d: sink: %w", seg, err)
			}
		}
	}
	// Los streams completados se copian al manifiesto del disco.
	manifest.Streams = append(manifest.Streams, res.Segments...)
	return res, nil
}

// segmentReader adapta un buffer []byte a io.Reader.
type segmentReader struct{ buf []byte }

func (r *segmentReader) Read(p []byte) (int, error) {
	if len(r.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

var _ = fmt.Sprintf
