package backup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/juansoler/bdr/internal/connector"
)

func ext(t Type) string {
	if t == TypeLog {
		return "trn"
	}
	return "bak"
}

// quoteTSQL escapa comillas simples para literales T-SQL.
func quoteTSQL(s string) string {
	return "N'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Encryption describe el cifrado nativo del backup (RF-45): algoritmo y
// certificado residente en master. SIN custodia del certificado (job
// SQL_CERTBACKUP) el backup cifrado es irrecuperable.
type Encryption struct {
	Algorithm string // AES_128 | AES_192 | AES_256 (vacío = AES_256)
	CertName  string
}

// NormalizeAlgorithm valida y normaliza el algoritmo de cifrado
// (AES_128 | AES_192 | AES_256; vacío = AES_256).
func NormalizeAlgorithm(a string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(a)) {
	case "", "AES_256":
		return "AES_256", nil
	case "AES_128":
		return "AES_128", nil
	case "AES_192":
		return "AES_192", nil
	default:
		return "", fmt.Errorf("algoritmo de cifrado no soportado: %s (AES_128|AES_192|AES_256)", a)
	}
}

// bracketId cita un identificador T-SQL con corchetes (escapa ]). En
// BACKUP/RESTORE DATABASE|LOG el nombre de base es un IDENTIFICADOR, no
// un literal: BACKUP DATABASE N'x' falla con error 102 (hallazgo del lab
// P-28). Los apóstrofos son legales dentro de los corchetes.
func bracketId(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

func buildBackupTSQL(database string, t Type, copyOnly bool, path string, enc *Encryption) (string, error) {
	dbq := bracketId(database)
	pathq := quoteTSQL(path)

	encClause := ""
	if enc != nil {
		cert := strings.TrimSpace(enc.CertName)
		if cert == "" {
			return "", fmt.Errorf("cifrado solicitado sin certificado")
		}
		alg, err := NormalizeAlgorithm(enc.Algorithm)
		if err != nil {
			return "", err
		}
		encClause = fmt.Sprintf(", ENCRYPTION (ALGORITHM = %s, SERVER CERTIFICATE = %s)",
			alg, quoteTSQL(cert))
	}

	switch t {
	case TypeFull:
		opts := "WITH COMPRESSION, CHECKSUM, STATS = 10"
		if copyOnly {
			opts = "WITH COMPRESSION, CHECKSUM, COPY_ONLY, STATS = 10"
		}
		return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s %s%s", dbq, pathq, opts, encClause), nil
	case TypeDifferential:
		return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH DIFFERENTIAL, COMPRESSION, CHECKSUM, STATS = 10%s", dbq, pathq, encClause), nil
	case TypeLog:
		return fmt.Sprintf("BACKUP LOG %s TO DISK = %s WITH COMPRESSION, CHECKSUM, STATS = 10%s", dbq, pathq, encClause), nil
	case TypeCopyOnly:
		return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH COMPRESSION, CHECKSUM, COPY_ONLY, STATS = 10%s", dbq, pathq, encClause), nil
	default:
		return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH COMPRESSION, CHECKSUM, STATS = 10%s", dbq, pathq, encClause), nil
	}
}

// Full ejecuta un backup completo.
//
// backupPath es la ruta FINAL del fichero .bak (resuelta por el
// StagingManager); el motor no genera nombres, solo escribe en esa ruta.
// verifyAfter controla si se ejecuta RESTORE VERIFYONLY tras el backup.
// enc != nil cifra el medio con el certificado indicado (RF-45): el
// certificado DEBE estar custodiado (SQL_CERTBACKUP) o el medio es
// irrecuperable.
func Full(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error) {
	return execute(ctx, db, database, TypeFull, false, backupPath, verifyAfter, enc)
}

// Differential ejecuta un backup diferencial. Mismo contrato de ruta que Full.
func Differential(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error) {
	return execute(ctx, db, database, TypeDifferential, false, backupPath, verifyAfter, enc)
}

// Log ejecuta un backup de log (transacciones). Mismo contrato de ruta que Full.
func Log(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error) {
	return execute(ctx, db, database, TypeLog, false, backupPath, verifyAfter, enc)
}

// CopyOnly ejecuta un backup copy-only (no rompe la cadena de restauración).
// Mismo contrato de ruta que Full.
func CopyOnly(ctx context.Context, db *connector.DB, database, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error) {
	return execute(ctx, db, database, TypeCopyOnly, true, backupPath, verifyAfter, enc)
}

func execute(ctx context.Context, db *connector.DB, database string, t Type, copyOnly bool, backupPath string, verifyAfter bool, enc *Encryption) (*Result, error) {
	start := time.Now()
	path := backupPath

	q, err := buildBackupTSQL(database, t, copyOnly, path, enc)
	if err != nil {
		return &Result{
			Database:   database,
			Type:       t,
			Path:       path,
			StartedAt:  start,
			FinishedAt: time.Now(),
			Error:      err.Error(),
		}, err
	}

	if err := db.ExecContext(ctx, q); err != nil {
		return &Result{
			Database:   database,
			Type:       t,
			Path:       path,
			StartedAt:  start,
			FinishedAt: time.Now(),
			Error:      err.Error(),
		}, fmt.Errorf("BACKUP %s de %s: %w", t, database, err)
	}

	verified := false
	if verifyAfter {
		if err := Verify(ctx, db, path); err != nil {
			return &Result{
				Database:   database,
				Type:       t,
				Path:       path,
				StartedAt:  start,
				FinishedAt: time.Now(),
				Error:      err.Error(),
			}, err
		}
		verified = true
	}

	fi, err := os.Stat(path)
	if err != nil {
		return &Result{
			Database:   database,
			Type:       t,
			Path:       path,
			StartedAt:  start,
			FinishedAt: time.Now(),
			Error:      err.Error(),
		}, fmt.Errorf("stat staging %s: %w", path, err)
	}

	size := int64(0)
	if fi != nil {
		size = fi.Size()
	}

	certName := ""
	if enc != nil {
		certName = strings.TrimSpace(enc.CertName)
	}

	return &Result{
		Database:    database,
		Type:        t,
		Path:        path,
		DurationSec: time.Since(start).Seconds(),
		SizeBytes:   size,
		Checksum:    true, // CHECKSUM siempre activo por diseño
		Compression: true, // COMPRESSION siempre activo por diseño
		Verified:    verified,
		Encrypted:   enc != nil && certName != "",
		CertName:    certName,
		StartedAt:   start,
		FinishedAt:  time.Now(),
	}, nil
}

// Verify ejecuta RESTORE VERIFYONLY WITH CHECKSUM sobre un medio.
// VERIFYONLY crea internamente una base temporal, por lo que exige permiso
// de CREATE DATABASE (rol de servidor dbcreator para el login del agente):
// si llega el error 262, el mensaje lo traduce a la acción concreta
// (hallazgo del lab P-25 — permisos mínimos).
func Verify(ctx context.Context, db *connector.DB, path string) error {
	q := fmt.Sprintf("RESTORE VERIFYONLY FROM DISK = %s WITH CHECKSUM", quoteTSQL(path))
	if err := db.ExecContext(ctx, q); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "(262)") || strings.Contains(msg, "CREATE DATABASE permission denied") {
			return fmt.Errorf("RESTORE VERIFYONLY %s: el login del agente carece del rol de servidor dbcreator "+
				"(VERIFYONLY crea una base temporal y exige CREATE DATABASE). Añádelo con: "+
				"ALTER SERVER ROLE dbcreator ADD MEMBER [<login del agente>]; — error original: %s", path, msg)
		}
		return fmt.Errorf("RESTORE VERIFYONLY %s: %w", path, err)
	}
	return nil
}
