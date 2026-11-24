package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/internal/backup"
)

type Repository struct {
	Type      string // "nas" | "s3" (solo informativo para el backend local)
	Path      string // ruta local o bucket/S3
	Retention backup.RetentionPolicy
	Labels    map[string]string
}

// localTransferer transfiere a una ruta local o montada en el host
// (laboratorio: NAS simulado o NAS ya montado por el sistema).
type localTransferer struct {
	repo Repository
}

// Transfer copia el fichero local al repositorio.
// La escritura es atómica (temporal + rename): si el proceso muere a mitad
// no queda un fichero a medias con nombre definitivo.
func (t *localTransferer) Transfer(ctx context.Context, stagingPath string) (string, error) {
	return t.TransferTo(ctx, stagingPath, filepath.Base(stagingPath))
}

// TransferTo es Transfer bajo la clave relativa relKey, creando los
// subdirectorios intermedios dentro del repositorio.
func (t *localTransferer) TransferTo(ctx context.Context, stagingPath, relKey string) (string, error) {
	if err := validateRelKey(relKey); err != nil {
		return "", err
	}

	dest := filepath.Join(t.repo.Path, filepath.FromSlash(relKey))
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return "", fmt.Errorf("crear destino repo %s: %w", filepath.Dir(dest), err)
	}

	tmp := dest + ".bdr-tmp"

	if err := copyFile(stagingPath, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("transferir %s -> %s: %w", stagingPath, dest, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("renombrar %s -> %s: %w", tmp, dest, err)
	}

	// En el control plane real, este paso registraría el objeto y su checksum
	// en el catálogo (ver metadata.BackupInfo.RepoPath).

	return dest, nil
}

// validateRelKey valida una clave relativa de objeto (segmentos separados
// por "/", sin recorridos de directorio): es la ruta del objeto dentro del
// destino y debe poder crearse en filesystem local, clave S3 y recurso SMB
// por igual. Rechaza absolutas, segmentos vacíos, "." / ".." y "\" (la clave
// es portátil: solo barra "/").
func validateRelKey(rel string) error {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsRune(rel, 0) || strings.Contains(rel, `\`) {
		return fmt.Errorf("ruta relativa de objeto inválida: %q", rel)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("ruta relativa de objeto inválida: %q", rel)
		}
	}
	return nil
}

// Fetch copia un objeto del repositorio local al staging (por ruta).
func (t *localTransferer) Fetch(ctx context.Context, uri, destPath string) error {
	if uri == "" {
		return fmt.Errorf("URI de objeto vacía")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return err
	}
	tmp := destPath + ".bdr-tmp"
	if err := copyFile(uri, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destPath)
}

// Cleanup elimina el archivo del repositorio (para pruebas o emergencia).
// En S3 Object Lock, esto depende de las credenciales del agente (sin bypass).
func (t *localTransferer) Cleanup(ctx context.Context, repoPath string) error {
	if err := os.Remove(repoPath); err != nil {
		return fmt.Errorf("eliminar %s: %w", repoPath, err)
	}
	return nil
}

// List lista los objetos del repositorio (para diagnóstico).
func (t *localTransferer) List(ctx context.Context, prefix string) ([]string, error) {
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

// Close no-op para el backend local.
func (t *localTransferer) Close() error { return nil }

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
