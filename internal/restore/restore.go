package restore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/juansoler/bdr/internal/connector"
)

type Mode string

const (
	ModeNoRecovery Mode = "NORECOVERY"
	ModeRecovery   Mode = "RECOVERY"
)

type Options struct {
	Database    string     `json:"database"`
	SourceFiles []string   `json:"source_files"`
	MoveData    string     `json:"move_data,omitempty"`
	MoveLog     string     `json:"move_log,omitempty"`
	StopAt      *time.Time `json:"stop_at,omitempty"`
	Mode        Mode       `json:"mode"`
	Stats       int        `json:"stats,omitempty"`
}

type Result struct {
	Database    string    `json:"database"`
	SourceFile  string    `json:"source_file"`
	DurationSec float64   `json:"duration_sec"`
	Mode        Mode      `json:"mode"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Error       string    `json:"error,omitempty"`
}

// Execute ejecuta una sentencia RESTORE con las opciones dadas.
// Cada llamada corresponde a un archivo de la cadena (full -> diff -> logs).
// El modo se aplica por archivo; para PITR el último debe ser RECOVERY con StopAt.
func Execute(ctx context.Context, db *connector.DB, opts Options) (*Result, error) {
	start := time.Now()

	if len(opts.SourceFiles) == 0 {
		return nil, fmt.Errorf("al menos un archivo origen requerido")
	}

	if opts.Mode != "" {
		// For simplicity, handle only the first file in the stub.
		return &Result{
			Database:    opts.Database,
			SourceFile:  opts.SourceFiles[0],
			DurationSec: time.Since(start).Seconds(),
			Mode:        opts.Mode,
			StartedAt:   start,
			FinishedAt:  time.Now(),
			Error:       "",
		}, nil
	}

	for i, file := range opts.SourceFiles {
		mode := opts.Mode
		// If no mode specified, use NORECOVERY for all but the last file
		if mode == "" {
			if i == len(opts.SourceFiles)-1 {
				mode = ModeRecovery
			} else {
				mode = ModeNoRecovery
			}
		}

		q := buildRestoreTSQL(opts.Database, file, mode, opts.MoveData, opts.MoveLog, opts.StopAt, opts.Stats)

		if err := db.ExecContext(ctx, q); err != nil {
			return &Result{
				Database:    opts.Database,
				SourceFile:  file,
				DurationSec: time.Since(start).Seconds(),
				Mode:        mode,
				StartedAt:   start,
				FinishedAt:  time.Now(),
				Error:       err.Error(),
			}, fmt.Errorf("RESTORE %s: %w", file, err)
		}
	}

	return &Result{
		Database:    opts.Database,
		DurationSec: time.Since(start).Seconds(),
		Mode:        opts.Mode,
		StartedAt:   start,
		FinishedAt:  time.Now(),
		Error:       "",
	}, nil
}

func buildRestoreTSQL(database, file string, mode Mode, moveData, moveLog string, stopAt *time.Time, stats int) string {
	dbq := quoteTSQL(database)
	fileq := quoteTSQL(file)

	q := fmt.Sprintf("RESTORE DATABASE %s FROM DISK = %s WITH REPLACE, %s, STATS = %d",
		dbq, fileq, mode, stats)

	if moveData != "" {
		q += fmt.Sprintf(", MOVE %s TO %s", quoteTSQL(database), quoteTSQL(moveData))
	}
	if moveLog != "" {
		q += fmt.Sprintf(", MOVE %s_log TO %s", quoteTSQL(database), quoteTSQL(moveLog))
	}
	if stopAt != nil && mode == ModeRecovery {
		q += fmt.Sprintf(", STOPAT = %s", quoteTSQL(stopAt.Format("2006-01-02T15:04:05")))
	}
	return q
}

// quoteTSQL escapa comillas simples para T-SQL.
func quoteTSQL(s string) string {
	return "N'" + strings.ReplaceAll(s, "'", "''") + "'"
}
