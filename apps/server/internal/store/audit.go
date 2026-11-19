package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/juansoler/bdr/packages/contracts"
)

// AuditLog es un registro append-only encadenado por hash: cada evento
// incluye el hash del anterior y su propio hash SHA-256 sobre el contenido.
// Un hueco o una modificación rompe la cadena y Verify lo detecta.
//
// La persistencia va detrás de un sink: el archivo (audit.jsonl), una tabla
// de PostgreSQL o nada (memoria). El sink se invoca con el cerrojo tomado y
// antes de añadir el evento en memoria; si falla, el evento no se añade y
// Append devuelve error, de modo que memoria y almacenamiento nunca divergen.
type AuditLog struct {
	mu     sync.RWMutex
	events []contracts.AuditEvent
	file   *os.File
	path   string
	sink   func(contracts.AuditEvent) error
}

// NewMemoryAudit crea un registro solo en memoria.
func NewMemoryAudit() *AuditLog { return &AuditLog{} }

// NewAudit crea un registro con eventos precargados y un sink de persistencia.
// Se usa al reabrir un almacén (los eventos cargados ya están en el disco).
func NewAudit(events []contracts.AuditEvent, sink func(contracts.AuditEvent) error) *AuditLog {
	return &AuditLog{events: events, sink: sink}
}

// OpenAuditFile abre (o crea) audit.jsonl y carga sus eventos.
func OpenAuditFile(path string) (*AuditLog, error) {
	l := &AuditLog{path: path}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 4<<20)
		line := 0
		for sc.Scan() {
			line++
			if len(sc.Bytes()) == 0 {
				continue
			}
			var ev contracts.AuditEvent
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("auditoría corrupta en %s línea %d: %w", path, line, err)
			}
			l.events = append(l.events, ev)
		}
		_ = f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	l.file = f
	return l, nil
}

// HashEvent calcula el hash de un evento (sin el campo Hash).
func HashEvent(ev contracts.AuditEvent) string {
	ev.Hash = ""
	b, _ := json.Marshal(ev)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Append añade un evento, rellenando Seq, PrevHash y Hash.
func (l *AuditLog) Append(ev contracts.AuditEvent) (contracts.AuditEvent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ev.Seq = 1
	ev.PrevHash = ""
	if n := len(l.events); n > 0 {
		ev.Seq = l.events[n-1].Seq + 1
		ev.PrevHash = l.events[n-1].Hash
	}
	ev.Hash = HashEvent(ev)
	if l.file != nil {
		b, err := json.Marshal(ev)
		if err != nil {
			return ev, err
		}
		if _, err := l.file.Write(append(b, '\n')); err != nil {
			return ev, err
		}
		if err := l.file.Sync(); err != nil {
			return ev, err
		}
	}
	if l.sink != nil {
		if err := l.sink(ev); err != nil {
			return ev, err
		}
	}
	l.events = append(l.events, ev)
	return ev, nil
}

// AuditFilter filtra eventos en List.
type AuditFilter struct {
	ActorID   string
	Action    string
	TargetID  string
	Result    string
	BeforeSeq int64 // 0 = desde el último
	Limit     int
}

// List devuelve eventos del más reciente al más antiguo.
func (l *AuditLog) List(f AuditFilter) (items []contracts.AuditEvent, nextBefore int64) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	for i := len(l.events) - 1; i >= 0; i-- {
		ev := l.events[i]
		if f.BeforeSeq > 0 && ev.Seq >= f.BeforeSeq {
			continue
		}
		if f.ActorID != "" && ev.ActorID != f.ActorID {
			continue
		}
		if f.Action != "" && ev.Action != f.Action && !hasPrefixDot(ev.Action, f.Action) {
			continue
		}
		if f.TargetID != "" && ev.TargetID != f.TargetID {
			continue
		}
		if f.Result != "" && ev.Result != f.Result {
			continue
		}
		if len(items) == f.Limit {
			return items, items[len(items)-1].Seq
		}
		items = append(items, ev)
	}
	return items, 0
}

func hasPrefixDot(action, prefix string) bool {
	return len(action) > len(prefix) && action[:len(prefix)] == prefix && action[len(prefix)] == '.'
}

// Export escribe todos los eventos en orden cronológico.
func (l *AuditLog) Export(fn func(contracts.AuditEvent) error) error {
	l.mu.RLock()
	events := append([]contracts.AuditEvent(nil), l.events...)
	l.mu.RUnlock()
	for _, ev := range events {
		if err := fn(ev); err != nil {
			return err
		}
	}
	return nil
}

// Verify recorre la cadena y comprueba hashes y secuencia.
func (l *AuditLog) Verify() contracts.AuditVerify {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := contracts.AuditVerify{OK: true, Events: int64(len(l.events))}
	prev := ""
	var prevSeq int64
	for i, ev := range l.events {
		if i == 0 {
			out.FirstSeq = ev.Seq
		}
		switch {
		case i > 0 && ev.Seq != prevSeq+1:
			out.OK, out.BrokenAt, out.BrokenWhy = false, ev.Seq, "hueco en la secuencia"
		case ev.PrevHash != prev:
			out.OK, out.BrokenAt, out.BrokenWhy = false, ev.Seq, "prev_hash no coincide"
		case HashEvent(ev) != ev.Hash:
			out.OK, out.BrokenAt, out.BrokenWhy = false, ev.Seq, "hash alterado"
		}
		if !out.OK {
			return out
		}
		prev, prevSeq = ev.Hash, ev.Seq
		out.LastSeq, out.LastHash = ev.Seq, ev.Hash
	}
	return out
}

// Len devuelve el número de eventos.
func (l *AuditLog) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.events)
}

// Close cierra el archivo.
func (l *AuditLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

var _ io.Closer = (*AuditLog)(nil)
