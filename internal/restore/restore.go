package restore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/internal/connector"
)

// Mode indica cómo debe finalizar la restauración.
type Mode string

const (
	ModeNoRecovery Mode = "NORECOVERY" // deja la base en RESTORING para aplicar más backups
	ModeRecovery   Mode = "RECOVERY"   // finaliza la base y la pone ONLINE
)

// Options controla los parámetros de una operación RESTORE.
type Options struct {
	Database    string     `json:"database"`            // nombre de la base destino
	SourceFiles []string   `json:"source_files"`        // archivos de backup en orden (full, diff, log...)
	MoveData    string     `json:"move_data,omitempty"` // nueva ruta para los ficheros de datos (opcional)
	MoveLog     string     `json:"move_log,omitempty"`  // nueva ruta para el fichero de log (opcional)
	StopAt      *time.Time `json:"stop_at,omitempty"`   // Point-in-Time Recovery (solo con recuperación final)
	Mode        Mode       `json:"mode"`                // vacío = automático; NORECOVERY = toda la cadena a medias
	Stats       int        `json:"stats,omitempty"`     // progreso (ej. 10 = cada 10%)
}

// Result contiene el resultado de una restauración.
type Result struct {
	Database    string    `json:"database"`
	Files       []string  `json:"files"`       // archivos aplicados en orden
	SourceFile  string    `json:"source_file"` // último archivo aplicado
	DurationSec float64   `json:"duration_sec"`
	Mode        Mode      `json:"mode"` // modo aplicado al último archivo
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Error       string    `json:"error,omitempty"`
}

// movePair es una cláusula MOVE: nombre lógico -> ruta física destino.
type movePair struct{ Logical, Physical string }

// Execute restaura la cadena completa de archivos indicada en orden
// (full -> diff -> logs). Semántica de modos:
//
//   - Mode vacío (automático): NORECOVERY en todos los archivos salvo el
//     último, que termina con RECOVERY.
//   - Mode = RECOVERY explícito con más de un archivo: se degrada a
//     automático (SQL Server no permite recuperar en medio de una cadena).
//   - Mode = NORECOVERY explícito: TODOS los archivos quedan en
//     NORECOVERY (p.ej. para seguir aplicando logs después).
//
// El STOPAT (PITR) solo se aplica a la restauración final con RECOVERY.
// La restauración de 'master' se rechaza: exige arrancar la instancia en
// modo single-user (ver RF-46 y el runbook de instancia).
func Execute(ctx context.Context, db *connector.DB, opts Options) (*Result, error) {
	start := time.Now()
	res := &Result{
		Database:  opts.Database,
		Files:     append([]string(nil), opts.SourceFiles...),
		StartedAt: start,
	}

	fail := func(err error) (*Result, error) {
		res.FinishedAt = time.Now()
		res.Error = err.Error()
		return res, err
	}

	if len(opts.SourceFiles) == 0 {
		return fail(fmt.Errorf("al menos un archivo origen requerido"))
	}

	// La restauración de 'master' requiere single-user y no se automatiza.
	if strings.EqualFold(opts.Database, "master") {
		return fail(fmt.Errorf("master no se restaura automáticamente: requiere instar la instancia en single-user (ver runbook de instancia)"))
	}

	if opts.Stats <= 0 {
		opts.Stats = 10
	}

	switch opts.Mode {
	case ModeNoRecovery:
		if opts.StopAt != nil {
			return fail(fmt.Errorf("STOPAT requiere finalizar con RECOVERY; la cadena está pedida en NORECOVERY"))
		}
	case ModeRecovery:
		if len(opts.SourceFiles) > 1 {
			// RECOVERY explícito con cadena: solo el último archivo recupera.
			opts.Mode = ""
		}
	default:
		opts.Mode = "" // modo automático
	}

	// MOVE: obtener los nombres lógicos reales del primer archivo en lugar
	// de asumir que coinciden con el nombre de la base.
	var moves []movePair
	if opts.MoveData != "" || opts.MoveLog != "" {
		var err error
		if moves, err = planMoves(ctx, db, opts); err != nil {
			return fail(err)
		}
	}

	for i, file := range opts.SourceFiles {
		mode := opts.Mode
		if mode == "" {
			if i == len(opts.SourceFiles)-1 {
				mode = ModeRecovery
			} else {
				mode = ModeNoRecovery
			}
		}
		stopAt := opts.StopAt
		if mode != ModeRecovery {
			stopAt = nil // STOPAT solo en la restauración final
		}

		q := buildRestoreTSQL(opts.Database, file, mode, moves, stopAt, opts.Stats)
		if err := db.ExecContext(ctx, q); err != nil {
			res.SourceFile, res.Mode = file, mode
			return fail(fmt.Errorf("RESTORE %s: %w", file, err))
		}
		res.SourceFile, res.Mode = file, mode
	}

	res.DurationSec = time.Since(start).Seconds()
	res.FinishedAt = time.Now()
	return res, nil
}

// planMoves construye las cláusulas MOVE consultando los nombres lógicos
// reales del backup (RESTORE FILELISTONLY). El primer fichero de datos va
// a MoveData y el de log a MoveLog; los ficheros adicionales (.ndf, logs
// múltiples) se colocan junto al destino principal conservando su nombre
// físico.
func planMoves(ctx context.Context, db *connector.DB, opts Options) ([]movePair, error) {
	files, err := listBackupFiles(ctx, db, opts.SourceFiles[0])
	if err != nil {
		return nil, fmt.Errorf("leer ficheros del backup para construir MOVE: %w", err)
	}

	var moves []movePair
	dataAssigned, logAssigned := false, false
	for _, f := range files {
		switch f.Type {
		case "D":
			if opts.MoveData == "" {
				continue
			}
			if !dataAssigned {
				moves = append(moves, movePair{f.Logical, opts.MoveData})
				dataAssigned = true
				continue
			}
			moves = append(moves, movePair{f.Logical,
				filepath.Join(filepath.Dir(opts.MoveData), filepath.Base(f.Physical))})
		case "L":
			if opts.MoveLog == "" {
				continue
			}
			if !logAssigned {
				moves = append(moves, movePair{f.Logical, opts.MoveLog})
				logAssigned = true
				continue
			}
			moves = append(moves, movePair{f.Logical,
				filepath.Join(filepath.Dir(opts.MoveLog), filepath.Base(f.Physical))})
		}
	}

	if opts.MoveData != "" && !dataAssigned {
		return nil, fmt.Errorf("el backup no contiene ficheros de datos; no hay MOVE que aplicar a -move-data")
	}
	if opts.MoveLog != "" && !logAssigned {
		return nil, fmt.Errorf("el backup no contiene fichero de log; no hay MOVE que aplicar a -move-log")
	}
	return moves, nil
}

// backupFile es una fila de RESTORE FILELISTONLY (solo lo que se usa).
type backupFile struct {
	Logical  string
	Physical string
	Type     string // "D" = datos, "L" = log
}

// listBackupFiles ejecuta RESTORE FILELISTONLY y devuelve los ficheros del
// backup. El result set tiene ~22 columnas según la versión de SQL Server;
// se escanea dinámicamente para no acoplar el motor al esquema exacto.
// Las posiciones 1-3 (LogicalName, PhysicalName, Type) son estables en
// todas las versiones soportadas.
func listBackupFiles(ctx context.Context, db *connector.DB, backupPath string) ([]backupFile, error) {
	rows, err := db.QueryContext(ctx,
		"RESTORE FILELISTONLY FROM DISK = "+quoteTSQL(backupPath)+" WITH CHECKSUM")
	if err != nil {
		return nil, fmt.Errorf("RESTORE FILELISTONLY %s: %w", backupPath, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	const idxLogical, idxPhysical, idxType = 0, 1, 2
	if len(cols) <= idxType {
		return nil, fmt.Errorf("RESTORE FILELISTONLY devolvió %d columnas; se esperaban al menos %d", len(cols), idxType+1)
	}

	var out []backupFile
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("leer FILELISTONLY: %w", err)
		}
		out = append(out, backupFile{
			Logical:  asString(vals[idxLogical]),
			Physical: asString(vals[idxPhysical]),
			Type:     strings.TrimSpace(asString(vals[idxType])),
		})
	}
	return out, rows.Err()
}

// asString normaliza un valor escaneado dinámicamente a texto.
func asString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	case nil:
		return ""
	default:
		return fmt.Sprint(s)
	}
}

// buildRestoreTSQL genera la sentencia RESTORE nativa. El nombre de base
// es un identificador ([corchetes], no literal: error 102, hallazgo del
// lab P-28). moves lleva pares (nombre lógico, ruta física) ya resueltos;
// stopAt solo se pasa en la restauración final con RECOVERY.
func buildRestoreTSQL(database, file string, mode Mode, moves []movePair, stopAt *time.Time, stats int) string {
	q := fmt.Sprintf("RESTORE DATABASE %s FROM DISK = %s WITH REPLACE, %s, STATS = %d",
		bracketId(database), quoteTSQL(file), mode, stats)
	for _, m := range moves {
		q += fmt.Sprintf(", MOVE %s TO %s", quoteTSQL(m.Logical), quoteTSQL(m.Physical))
	}
	if stopAt != nil && mode == ModeRecovery {
		// Normalizado a UTC para que sea determinista entre máquinas;
		// validar en el lab (P-28) contra la TZ del servidor SQL.
		q += fmt.Sprintf(", STOPAT = %s", quoteTSQL(stopAt.UTC().Format("2006-01-02T15:04:05")))
	}
	return q
}

// quoteTSQL escapa comillas simples para literales T-SQL.
func quoteTSQL(s string) string {
	return "N'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// bracketId cita un identificador T-SQL con corchetes (escapa ]).
func bracketId(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// ValidateChain valida que la cadena de backups tenga continuidad de LSN.
// Requiere que las entradas vengan ordenadas cronológicamente
// (full -> diff -> log...), tal como devuelve connector.ListBackupChain.
// La comparación es numérica con big.Int (numeric(25,0) no cabe en uint64,
// ver P-26).
func ValidateChain(chain []connector.ChainEntry) error {
	if len(chain) == 0 {
		return fmt.Errorf("cadena vacía")
	}
	for i := 1; i < len(chain); i++ {
		prev := chain[i-1]
		curr := chain[i]
		// Validación básica: el first_lsn del actual debe ser >= last_lsn del anterior
		if curr.FirstLSN.Compare(prev.LastLSN) < 0 {
			return fmt.Errorf("discontinuidad de LSN en posición %d: prev last_lsn=%s > curr first_lsn=%s",
				i, prev.LastLSN, curr.FirstLSN)
		}
	}
	return nil
}
