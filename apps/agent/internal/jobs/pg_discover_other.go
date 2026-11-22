//go:build !windows

// Detección PostgreSQL fuera de Windows: pg_lsclusters (Debian/Ubuntu) y
// systemctl como fallback. En macOS (dev) devuelve vacío sin error duro.
package jobs

import (
	"context"
	"os"
	"os/exec"

	"github.com/juansoler/bdr/packages/contracts"
)

func pgDiscoverLocal(ctx context.Context) ([]contracts.PGInstanceView, error) {
	if _, err := exec.LookPath("pg_lsclusters"); err == nil {
		c, cancel := context.WithTimeout(ctx, 10_000_000_000)
		defer cancel()
		if out, err := exec.CommandContext(c, "pg_lsclusters").Output(); err == nil {
			if items := parsePgLSClusters(string(out)); len(items) > 0 {
				return items, nil
			}
		}
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		if items := pgDiscoverSystemd(ctx); len(items) > 0 {
			return items, nil
		}
	}
	// Host sin PostgreSQL detectado: lista vacía (no es error).
	if _, err := os.Stat("/usr/lib/postgresql"); err != nil && os.Getenv("BDR_PG_STRICT_DISCOVER") == "" {
		return nil, nil
	}
	return nil, nil
}
