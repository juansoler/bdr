package agent

import (
	"context"
	"sync"
	"time"

	"github.com/juansoler/bdr/apps/agent/internal/client"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

const (
	logBatchSize     = 20
	logFlushInterval = 2 * time.Second
	logQueueSize     = 256
)

// logBatcher agrupa las líneas de log de un job y las envía en lotes.
// Es best-effort: si la cola se llena, se descartan líneas.
type logBatcher struct {
	client *client.Client
	jobID  string
	log    *logging.Logger

	ch        chan contracts.JobLogLine
	done      chan struct{}
	closeOnce sync.Once
}

func newLogBatcher(c *client.Client, jobID string, log *logging.Logger) *logBatcher {
	b := &logBatcher{
		client: c,
		jobID:  jobID,
		log:    log,
		ch:     make(chan contracts.JobLogLine, logQueueSize),
		done:   make(chan struct{}),
	}
	go b.loop()
	return b
}

// Add encola una línea sin bloquear.
func (b *logBatcher) Add(level, message string) {
	if message == "" {
		return
	}
	line := contracts.JobLogLine{At: time.Now().UTC(), Level: level, Message: message}
	select {
	case b.ch <- line:
	default:
		if b.log != nil {
			b.log.Debug("log de job descartado: cola llena", "job_id", b.jobID)
		}
	}
}

// Close vacía la cola y espera al envío final.
func (b *logBatcher) Close() {
	b.closeOnce.Do(func() { close(b.ch) })
	<-b.done
}

func (b *logBatcher) loop() {
	defer close(b.done)
	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()

	buf := make([]contracts.JobLogLine, 0, logBatchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := b.client.JobLog(ctx, b.jobID, contracts.JobLogRequest{Lines: buf})
		cancel()
		if err != nil && b.log != nil {
			b.log.Debug("no se pudo enviar el log del job", "job_id", b.jobID, "error", err.Error())
		}
		buf = buf[:0]
	}

	for {
		select {
		case line, ok := <-b.ch:
			if !ok {
				flush()
				return
			}
			buf = append(buf, line)
			if len(buf) >= logBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
