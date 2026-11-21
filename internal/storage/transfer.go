package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/juansoler/bdr/internal/backup"
)

type Repository struct {
	Type      string // "nas" | "s3"
	Path      string // ruta local o bucket/S3
	Retention backup.RetentionPolicy
	Labels    map[string]string
}

type Transferer struct {
	repo Repository
}

// NewTransferer crea un transferidor para el repositorio dado.
func NewTransferer(repo Repository) *Transferer {
	return &Transferer{repo: repo}
}

// Transfer mueve un archivo de staging al repositorio.
// En producción, si Type == "s3", se usaría la SDK de S3 (AWS CLI, MinIO, etc.).
func (t *Transferer) Transfer(ctx context.Context, stagingPath string) (repoPath string, err error) {
	if err := os.MkdirAll(t.repo.Path, 0750); err != nil {
		return "", fmt.Errorf("crear destino repo %s: %w", t.repo.Path, err)
	}

	base := filepath.Base(stagingPath)
	dest := filepath.Join(t.repo.Path, base)

	// Copiar archivo (en producción, esto sería un upload S3 o movimiento NFS)
	if err := copyFile(stagingPath, dest); err != nil {
		return "", fmt.Errorf("transferir %s -> %s: %w", stagingPath, dest, err)
	}

	// En el control plane real, este paso registraría el objeto y su checksum
	// en el catálogo (ver metadata.BackupInfo.RepoPath).

	return dest, nil
}

// Cleanup elimina el archivo del repositorio (para pruebas o emergencia).
// En S3 Object Lock, esto depende de las credenciales del agente (sin bypass).
func (t *Transferer) Cleanup(ctx context.Context, repoPath string) error {
	if err := os.Remove(repoPath); err != nil {
		return fmt.Errorf("eliminar %s: %w", repoPath, err)
	}
	return nil
}

// List lista los objetos del repositorio (para diagnóstico).
func (t *Transferer) List(ctx context.Context, prefix string) ([]string, error) {
	entries, err := os.ReadDir(t.repo.Path)
	if err != nil {
		return nil, fmt.Errorf("leer directorio repo: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if len(prefix) == 0 || len(e.Name()) >= len(prefix) && e.Name()[:len(prefix)] == prefix {
			out = append(out, filepath.Join(t.repo.Path, e.Name()))
		}
	}
	return out, nil
}

// copyFile copia un archivo local.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("abrir origen: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("crear destino: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copiar: %w", err)
	}
	return nil
}

// RetentionPolicyApply es un hook para aplicar retención en producción.
// Se ejecuta desde el control plane, no desde el agente (el agente solo transfiere).
func RetentionPolicyApply(ctx context.Context, repo Repository, appliedAt time.Time) error {
	// en producción: limpia según GFS, respetando inmutabilidad
	return nil
}
