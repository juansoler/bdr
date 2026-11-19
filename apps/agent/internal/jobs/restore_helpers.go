// Helpers de escritura por offset para restaurar segmentos en volúmenes.
package jobs

import (
	"io"
)

// offsetWriter adapta un io.WriterAt a io.Writer avanzando la posición.
type offsetWriter struct {
	dst io.WriterAt
	pos int64
}

func newWriterAt(dst io.WriterAt, base int64) io.Writer {
	return &offsetWriter{dst: dst, pos: base}
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	n, err := w.dst.WriteAt(p, w.pos)
	w.pos += int64(n)
	return n, err
}
