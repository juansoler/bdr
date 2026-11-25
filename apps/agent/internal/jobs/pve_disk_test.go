package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	pkgimg "github.com/juansoler/bdr/packages/image"
)

func TestDiskParamsAndSizeToG(t *testing.T) {
	p := diskParams("DATA1:vm-105-disk-1,size=64G,ssd=1,discard=on")
	if p["size"] != "64G" || p["ssd"] != "1" || p["discard"] != "on" {
		t.Fatalf("params: %v", p)
	}
	if p["volid"] != "" {
		t.Fatalf("volid no debe ser un parámetro: %v", p)
	}
	cases := map[string]string{
		"64G":  "64",
		"1T":   "1024",
		"8G":   "8",
		"512M": "1",
		"32G":  "32",
	}
	for in, want := range cases {
		if got := sizeToG(in); got != want {
			t.Errorf("sizeToG(%q) = %q, esperaba %q", in, got, want)
		}
	}
	if got := sizeToG(""); got != "" {
		t.Errorf("sizeToG vacío = %q", got)
	}
	// Argumento qm create para un disco regular con parámetros preservados.
	cfg := "DATA1:vm-105-disk-1,size=64G,ssd=1"
	params := diskParams(cfg)
	arg := fmt.Sprintf("--scsi0=DATA1:%s", sizeToG(params["size"]))
	if params["ssd"] == "1" {
		arg += ",ssd=1"
	}
	if arg != "--scsi0=DATA1:64,ssd=1" {
		t.Fatalf("arg: %q", arg)
	}
}

func sinEsperaReintentos(t *testing.T) {
	t.Helper()
	espera := restoreRetryDelay
	restoreRetryDelay = 0
	t.Cleanup(func() { restoreRetryDelay = espera })
}

// volumenTest crea un fichero de ceros que hace de volumen destino.
func volumenTest(t *testing.T, tam int64) *os.File {
	t.Helper()
	vol, err := os.CreateTemp(t.TempDir(), "volumen-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vol.Close() })
	if err := vol.Truncate(tam); err != nil {
		t.Fatal(err)
	}
	return vol
}

// segmentoTest sube al destino un segmento de 256 KiB y devuelve su stream.
func segmentoTest(t *testing.T, dest uploadDest, offset int64) (pkgimg.Stream, []byte) {
	t.Helper()
	datos := bytes.Repeat([]byte("datos-de-segmento,"), 16<<10)
	sum := sha256.Sum256(datos)
	s := pkgimg.Stream{
		Disk: 0, Name: "disk0-seg0000", Offset: offset, Bytes: int64(len(datos)),
		SHA256: hex.EncodeToString(sum[:]), Blob: dest.Prefix() + "disk0-seg0000-" + hex.EncodeToString(sum[:]),
	}
	if err := dest.putRaw(context.Background(), s.Blob, datos, time.Now()); err != nil {
		t.Fatal(err)
	}
	return s, datos
}

// destCortado simula un backend que corta la conexión a mitad de objeto: las
// primeras cortes aperturas sirven solo parcial bytes y luego fallan.
type destCortado struct {
	uploadDest
	cortes, parcial int
	fallo           error
	aperturas       int
}

func (d *destCortado) openObject(ctx context.Context, key string) (io.ReadCloser, error) {
	d.aperturas++
	obj, err := d.uploadDest.openObject(ctx, key)
	if err != nil || d.cortes <= 0 {
		return obj, err
	}
	d.cortes--
	return &lectorCortado{ReadCloser: obj, resto: d.parcial, fallo: d.fallo}, nil
}

// lectorCortado entrega bytes y después falla como un cuerpo HTTP truncado.
type lectorCortado struct {
	io.ReadCloser
	resto int
	fallo error
}

func (l *lectorCortado) Read(p []byte) (int, error) {
	if l.resto <= 0 {
		return 0, l.falloCorte()
	}
	if len(p) > l.resto {
		p = p[:l.resto]
	}
	n, err := l.ReadCloser.Read(p)
	l.resto -= n
	return n, err
}

func (l *lectorCortado) falloCorte() error {
	if l.fallo != nil {
		return l.fallo
	}
	return io.ErrUnexpectedEOF
}

// TestWriteStreamsReintentaDescargaCortada: un corte de la conexión a mitad
// del objeto no falla el job; el segmento se reescribe desde el principio.
func TestWriteStreamsReintentaDescargaCortada(t *testing.T) {
	sinEsperaReintentos(t)
	base, _ := destinoTest(t, "vm")
	stream, _ := segmentoTest(t, base, 64<<10)
	manifest := pkgimg.Manifest{Streams: []pkgimg.Stream{stream}}
	dest := &destCortado{uploadDest: base, cortes: 2, parcial: 100 << 10}
	resume := &restoreState{ManifestKey: "manifest-cap-1.json", dataDir: t.TempDir()}
	var avisos []string
	hechos := 0

	err := writeStreams(context.Background(), dest, manifest, 0, volumenTest(t, 1<<20), 1<<20, resume,
		func(n int) { hechos += n }, func(aviso string) { avisos = append(avisos, aviso) })
	if err != nil {
		t.Fatalf("writeStreams: %v", err)
	}
	if dest.aperturas != 3 || len(avisos) != 2 || hechos != 1 || !resume.Has(stream.Name) {
		t.Fatalf("aperturas=%d avisos=%v hechos=%d reanudable=%v", dest.aperturas, avisos, hechos, resume.Has(stream.Name))
	}
	for i, aviso := range avisos {
		if !strings.Contains(aviso, fmt.Sprintf("%s: descargar: unexpected EOF; reintento %d/%d", stream.Name, i+1, restoreCopyRetries)) {
			t.Fatalf("aviso de reintento %d: %q", i+1, aviso)
		}
	}
}

// TestWriteStreamsContenidoTrasReintentos: el volumen recibe el segmento
// entero desde su offset, no los bytes del intento cortado encadenados.
func TestWriteStreamsContenidoTrasReintentos(t *testing.T) {
	sinEsperaReintentos(t)
	base, _ := destinoTest(t, "vm")
	stream, datos := segmentoTest(t, base, 64<<10)
	dest := &destCortado{uploadDest: base, cortes: 2, parcial: 100 << 10}
	vol := volumenTest(t, 1<<20)

	err := writeStreams(context.Background(), dest, pkgimg.Manifest{Streams: []pkgimg.Stream{stream}},
		0, vol, 1<<20, nil, func(int) {}, func(string) {})
	if err != nil {
		t.Fatalf("writeStreams: %v", err)
	}
	leido := make([]byte, len(datos))
	if _, err := vol.ReadAt(leido, stream.Offset); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leido, datos) {
		t.Fatal("el volumen no contiene el segmento completo tras los reintentos")
	}
	antes := make([]byte, int(stream.Offset))
	if _, err := vol.ReadAt(antes, 0); err != nil {
		t.Fatal(err)
	}
	for _, b := range antes {
		if b != 0 {
			t.Fatal("la escritura invadió bytes anteriores al offset del segmento")
		}
	}
}

type writerFalla struct{ err error }

func (w writerFalla) WriteAt(p []byte, off int64) (int, error) { return 0, w.err }

// TestWriteStreamsNoReintentaFalloDeEscritura: un error del disco no se
// reintenta y se informa como fallo de escritura.
func TestWriteStreamsNoReintentaFalloDeEscritura(t *testing.T) {
	sinEsperaReintentos(t)
	base, _ := destinoTest(t, "vm")
	stream, _ := segmentoTest(t, base, 0)
	dest := &destCortado{uploadDest: base}
	resume := &restoreState{ManifestKey: "manifest-cap-1.json", dataDir: t.TempDir()}

	err := writeStreams(context.Background(), dest, pkgimg.Manifest{Streams: []pkgimg.Stream{stream}},
		0, writerFalla{syscall.EIO}, 1<<20, resume, func(int) {}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "escribir:") || !errors.Is(err, syscall.EIO) {
		t.Fatalf("error: %v", err)
	}
	if dest.aperturas != 1 || resume.Has(stream.Name) {
		t.Fatalf("un fallo de escritura no debe reintentarse: aperturas=%d reanudable=%v", dest.aperturas, resume.Has(stream.Name))
	}

	err = writeStreams(context.Background(), dest, pkgimg.Manifest{Streams: []pkgimg.Stream{stream}},
		0, writerFalla{syscall.ENOSPC}, 1<<20, resume, func(int) {}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "sin espacio") {
		t.Fatalf("sin espacio: %v", err)
	}
}

// TestWriteStreamsAgotaReintentos: si el corte persiste, el job falla con el
// lado que falló y el segmento no queda marcado como completado.
func TestWriteStreamsAgotaReintentos(t *testing.T) {
	sinEsperaReintentos(t)
	base, _ := destinoTest(t, "vm")
	stream, _ := segmentoTest(t, base, 0)
	dest := &destCortado{uploadDest: base, cortes: 1 << 30, parcial: 32 << 10}
	resume := &restoreState{ManifestKey: "manifest-cap-1.json", dataDir: t.TempDir()}

	err := writeStreams(context.Background(), dest, pkgimg.Manifest{Streams: []pkgimg.Stream{stream}},
		0, volumenTest(t, 1<<20), 1<<20, resume, func(int) {}, func(string) {})
	if err == nil || err.Error() != "segmento "+stream.Name+": descargar: unexpected EOF" {
		t.Fatalf("error: %v", err)
	}
	if dest.aperturas != restoreCopyRetries+1 || resume.Has(stream.Name) {
		t.Fatalf("aperturas=%d reanudable=%v", dest.aperturas, resume.Has(stream.Name))
	}
}

// TestWriteStreamsNoReintentaErroresDefinitivos: un objeto ausente en el
// destino o un job cancelado no consumen reintentos.
func TestWriteStreamsNoReintentaErroresDefinitivos(t *testing.T) {
	sinEsperaReintentos(t)
	base, _ := destinoTest(t, "vm")
	manifest := pkgimg.Manifest{Streams: []pkgimg.Stream{{Disk: 0, Name: "disk0-seg0000", Blob: base.Prefix() + "ausente"}}}

	dest := &destCortado{uploadDest: base}
	err := writeStreams(context.Background(), dest, manifest, 0, volumenTest(t, 1<<20), 1<<20, nil,
		func(int) {}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "descargar:") || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("objeto ausente: %v", err)
	}
	if dest.aperturas != 1 {
		t.Fatalf("un objeto ausente no debe reintentarse: aperturas=%d", dest.aperturas)
	}

	stream, _ := segmentoTest(t, base, 0)
	dest = &destCortado{uploadDest: base, cortes: 5, parcial: 32 << 10, fallo: context.Canceled}
	err = writeStreams(context.Background(), dest, pkgimg.Manifest{Streams: []pkgimg.Stream{stream}},
		0, volumenTest(t, 1<<20), 1<<20, nil, func(int) {}, func(string) {})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelado: %v", err)
	}
	if dest.aperturas != 1 {
		t.Fatalf("un job cancelado no debe reintentarse: aperturas=%d", dest.aperturas)
	}
}
