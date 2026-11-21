package restore

import (
	"context"
	"fmt"
	"time"

	"github.com/juansoler/bdr/internal/connector"
)

// Mode indicates the restoration mode.
type Mode string

const (
	ModeNoRecovery Mode = "NORECOVERY"
	ModeRecovery   Mode = "RECOVERY"
)

// Options controls the parameters of a restore operation.
type Options struct {
	Database    string     `json:"database"`
	SourceFiles []string   `json:"source_files"`
	MoveData    string     `json:"move_data,omitempty"`
	MoveLog     string     `json:"move_log,omitempty"`
	StopAt      *time.Time `json:"stop_at,omitempty"`
	Mode        Mode       `json:"mode"`
	Stats       int        `json:"stats,omitempty"`
}

// Result contains the result of a restore operation.
type Result struct {
	Database    string    `json:"database"`
	SourceFile  string    `json:"source_file"`
	DurationSec float64   `json:"duration_sec"`
	Mode        Mode      `json:"mode"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Error       string    `json:"error,omitempty"`
}

// Execute runs a restore sequence from SourceFiles.
func Execute(ctx context.Context, db *connector.DB, opts Options) (*Result, error) {
	start := time.Now()

	if len(opts.SourceFiles) == 0 {
		return nil, fmt.Errorf("al menos un archivo origen requerido")
	}

	// For simplicity, handle only the first file in the stub.
	return &Result{
		Database:    opts.Database,
		SourceFile:  opts.SourceFiles[0],
		DurationSec: time.Since(start).Seconds(),
		Mode:        opts.Mode,
		StartedAt:   start,
		FinishedAt:  time.Now(),
	}, nil
}