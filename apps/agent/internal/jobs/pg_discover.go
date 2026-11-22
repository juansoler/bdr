// Detección de instancias PostgreSQL locales (job PG_DISCOVER): sin
// registro universal como el de SQL Server, se usan heurísticas por SO.
// Función pura de parsing portable + resolución por plataforma.
package jobs

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"

	"github.com/juansoler/bdr/packages/contracts"
)

// Discover ejecuta un job PG_DISCOVER.
func (m *PGMotor) Discover(ctx context.Context) (json.RawMessage, error) {
	items, err := pgDiscoverLocal(ctx)
	if err != nil {
		return nil, &CodeError{Code: "E_PG_DISCOVER", Message: err.Error()}
	}
	out, err := json.Marshal(contracts.PGDiscoverJobResult{Items: items})
	if err != nil {
		return nil, &CodeError{Code: "E_INTERNAL", Message: err.Error()}
	}
	return out, nil
}

// parsePgLSClusters parsea la salida de pg_lsclusters (Debian/Ubuntu):
// "16 main 5432 online postgres /var/lib/..." → instancias. Función pura.
func parsePgLSClusters(out string) []contracts.PGInstanceView {
	var items []contracts.PGInstanceView
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		// Ver Cluster Port Status Owner ...
		if len(f) < 4 || f[0] == "Ver" {
			continue
		}
		port := ""
		if p, err := strconv.Atoi(f[2]); err == nil {
			port = strconv.Itoa(p)
		}
		if port == "" {
			continue
		}
		items = append(items, contracts.PGInstanceView{
			Port:    port,
			Service: f[0] + "/" + f[1],
			Version: f[0],
		})
	}
	return items
}

// parseScQueryExtract extrae el nombre de servicio de la salida de
// `sc queryex type= service state= all` filtrando postgresql. Función pura.
func parseScServiceNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		// "SERVICE_NAME: postgresql-x64-16"
		if i := strings.Index(line, "SERVICE_NAME:"); i >= 0 {
			name := strings.TrimSpace(line[i+len("SERVICE_NAME:"):])
			if strings.Contains(strings.ToLower(name), "postgres") {
				names = append(names, name)
			}
		}
	}
	return names
}

// portFromServiceName deduce el puerto por convención de nombre
// postgresql-x64-<ver> (los instaladores usan puertos 5432+ por instancia).
func portFromServiceName(name string) string {
	i := strings.LastIndex(name, "-")
	if i >= 0 {
		if v, err := strconv.Atoi(name[i+1:]); err == nil {
			// Convención del instalador de EDB: la primera instancia usa 5432;
			// no se puede deducir el puerto real solo del nombre — lo deja
			// vacío para que el panel sugiera 5432 y el operador lo confirme.
			_ = v
		}
	}
	return ""
}

// pgDiscoverSystemd ejecuta `systemctl list-units` filtrando postgresql
// (fallback para distros sin pg_lsclusters).
func pgDiscoverSystemd(ctx context.Context) []contracts.PGInstanceView {
	c, cancel := context.WithTimeout(ctx, 10_000_000_000) // 10s
	defer cancel()
	out, err := exec.CommandContext(c, "systemctl", "list-units", "--type=service",
		"--no-legend", "--all").Output()
	if err != nil {
		return nil
	}
	var items []contracts.PGInstanceView
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 1 || !strings.Contains(f[0], "postgres") {
			continue
		}
		items = append(items, contracts.PGInstanceView{Service: f[0], Port: "5432"})
	}
	return items
}
