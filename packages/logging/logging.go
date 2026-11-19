// Package logging emite logs JSON sin dependencias externas.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Logger escribe una línea JSON por evento.
type Logger struct {
	mu    sync.Mutex
	out   io.Writer
	level int
}

const (
	levelDebug = iota
	levelInfo
	levelWarn
	levelError
)

// New crea un logger que escribe en stderr.
func New(level string) *Logger {
	return &Logger{out: os.Stderr, level: parseLevel(level)}
}

// NewWithWriter permite inyectar un writer (tests).
func NewWithWriter(w io.Writer, level string) *Logger {
	return &Logger{out: w, level: parseLevel(level)}
}

func parseLevel(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return levelDebug
	case "warn", "warning":
		return levelWarn
	case "error":
		return levelError
	default:
		return levelInfo
	}
}

// Debug registra un evento de depuración.
func (l *Logger) Debug(msg string, kv ...any) { l.log(levelDebug, "debug", msg, kv...) }

// Info registra un evento informativo.
func (l *Logger) Info(msg string, kv ...any) { l.log(levelInfo, "info", msg, kv...) }

// Warn registra una advertencia.
func (l *Logger) Warn(msg string, kv ...any) { l.log(levelWarn, "warn", msg, kv...) }

// Error registra un error.
func (l *Logger) Error(msg string, kv ...any) { l.log(levelError, "error", msg, kv...) }

func (l *Logger) log(level int, name, msg string, kv ...any) {
	if level < l.level {
		return
	}
	entry := map[string]any{
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
		"level": name,
		"msg":   msg,
	}
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			key = fmt.Sprint(kv[i])
		}
		entry[key] = redact(key, kv[i+1])
	}
	b, err := json.Marshal(entry)
	if err != nil {
		b = []byte(fmt.Sprintf(`{"level":"error","msg":"log marshal failed"}`))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(append(b, '\n'))
}

// redact oculta valores de claves sensibles para que nunca lleguen a los logs.
func redact(key string, value any) any {
	k := strings.ToLower(key)
	for _, s := range []string{"token", "password", "secret", "api_key", "apikey", "authorization"} {
		if strings.Contains(k, s) {
			return "***"
		}
	}
	return value
}
